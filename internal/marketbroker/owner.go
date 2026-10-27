// Package marketbroker owns the private FEAT-157 Connectors worker. It is not
// wired to public Host routes or Native grants. All real providers remain
// unqualified. No process is replaced, signalled, killed or automatically restarted.
package marketbroker

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
)

const (
	// RuntimeCapabilityEnvironment carries an internal generation capability,
	// never a platform credential. It must be excluded from every tool environment.
	RuntimeCapabilityEnvironment = "YIJIE_MARKET_RUNTIME_CAPABILITY"
	maxFrameBytes                = wire.MaxFrameBytes
	controlTimeout               = 5 * time.Second
)

var (
	ErrClosed         = errors.New("market worker owner is permanently closed")
	ErrNotStarted     = errors.New("market worker owner is not initialized")
	ErrStarted        = errors.New("market worker owner already started")
	ErrProtocol       = errors.New("market worker control response unavailable")
	ErrBinding        = errors.New("market worker control binding mismatch")
	ErrOutcomeUnknown = errors.New("market worker control outcome is unknown; owner retired")
	ErrStopPending    = errors.New("market worker normal exit is not confirmed; ownership retained")
	ErrExit           = errors.New("market worker exited unsuccessfully")
)

type exchangeResult struct {
	frame []byte
	err   error
}

// Owner permits one in-flight control exchange and one child in its entire
// lifetime. A timeout closes stdin and permanently retires admission. A caller
// may still wait for normal exit; it cannot revive the old generation.
type Owner struct {
	*ownerState
}

// The public handle holds only a pointer, allowing safe diagnostic methods on
// both values and pointers without copying a mutex or exposing private fields.
type ownerState struct {
	mu                   sync.Mutex
	gate                 chan struct{}
	manifest             string
	libraryHome          string
	externalCallsEnabled bool
	started              bool
	initialized          bool
	closed               bool
	stdin                *os.File
	modelKeyWriter       *os.File
	kimiModelKey         string // existing model snapshot; private startup pipe only
	done                 chan struct{}
	exitErr              error // published before done closes
	active               chan exchangeResult
	capability           string // never serialized, returned in diagnostics or persisted
	processKey           string // validated process binding, encoded only for comparison
}

// NewOwner only accepts assembly-owned manifest paths. It does not start a
// process. There is deliberately no HTTP adapter or environment auto-enable.
func NewOwner(manifest string) *Owner {
	return &Owner{ownerState: &ownerState{manifest: manifest, gate: make(chan struct{}, 1)}}
}

// NewOwnerWithStorage is the product assembly entry. The single worker owns
// authorization and Gateway execution. Other platform credentials remain in
// Connectors Keyring; the explicit Kimi model reuse constructor is separate.
func NewOwnerWithStorage(manifest, home string) (*Owner, error) {
	if err := prepareLibraryHome(home); err != nil {
		return nil, err
	}
	owner := NewOwner(manifest)
	owner.libraryHome = home
	return owner, nil
}

// String prevents accidental formatted logging of private process state.
func (o Owner) String() string   { return "marketbroker.Owner([redacted])" }
func (o Owner) GoString() string { return o.String() }

// ScrubRuntimeCapability removes inherited values before any new process is
// assembled. Ordinary Runtime/tool process builders can use it without getting
// access to the owner-generated value.
func ScrubRuntimeCapability(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, RuntimeCapabilityEnvironment) && !strings.EqualFold(key, wire.KimiModelCredentialEnvironmentKey) {
			result = append(result, entry)
		}
	}
	return result
}

// EnvironmentForRuntime is an assembly-only carrier handoff. The caller must
// first enforce the managed shell policy and pass the exact Runtime generation
// initialized with this owner. It never reads or accepts an ambient capability.
// The returned environment must not be logged, serialized or exposed over IPC.
func (o *Owner) EnvironmentForRuntime(environment []string, process wire.ProcessBinding) ([]string, error) {
	if process.Validate() != nil {
		return nil, ErrBinding
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, ErrClosed
	}
	if !o.started || !o.initialized || o.capability == "" {
		return nil, ErrNotStarted
	}
	if o.processKey != processKey(process) {
		return nil, ErrBinding
	}
	return append(ScrubRuntimeCapability(environment), RuntimeCapabilityEnvironment+"="+o.capability), nil
}

// StartControl starts the single child without inventing a Runtime binding.
// Provider authorization may use it while data-plane admission stays closed.
func (o *Owner) StartControl(ctx context.Context) error {
	err := o.start(ctx, "")
	if errors.Is(err, ErrStarted) {
		return nil
	}
	return err
}

func (o *Owner) bindProcess(process wire.ProcessBinding) error {
	if process.Validate() != nil {
		return ErrBinding
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrClosed
	}
	if !o.started {
		return ErrNotStarted
	}
	key := processKey(process)
	if o.processKey != "" && o.processKey != key {
		return ErrBinding
	}
	o.processKey = key
	return nil
}

// ReserveRuntimeEnvironment is called immediately before the verified Runtime
// process spawn. It binds only the local carrier handoff; worker data admission
// remains closed until Initialize succeeds after the actual native handshake.
func (o *Owner) ReserveRuntimeEnvironment(environment []string, process wire.ProcessBinding) ([]string, error) {
	if err := o.bindProcess(process); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.capability == "" {
		return nil, ErrClosed
	}
	return append(ScrubRuntimeCapability(environment), RuntimeCapabilityEnvironment+"="+o.capability), nil
}

// Done identifies this owner's only child. It closes only after process Wait;
// observing it is not evidence of provider completion. Before start it is nil.
func (o *Owner) Done() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.done
}

func (o *Owner) start(ctx context.Context, processKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	closed, started := o.closed, o.started
	o.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if started {
		return ErrStarted
	}
	build, err := loadArtifact(o.manifest)
	if err != nil {
		return err
	}
	if build.SchemaVersion == 2 && o.libraryHome == "" {
		return ErrLibraryHome
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return ErrProtocol
	}
	capability := hex.EncodeToString(secret)
	childInput, input, err := os.Pipe()
	if err != nil {
		return ErrProtocol
	}
	output, childOutput, err := os.Pipe()
	if err != nil {
		childInput.Close()
		input.Close()
		return ErrProtocol
	}
	// os.Pipe files are close-on-exec; only explicit descriptors are inherited.
	command := exec.Command(build.Binary, "--broker-control-stdio")
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=en_US.UTF-8", RuntimeCapabilityEnvironment + "=" + capability}
	if o.libraryHome != "" {
		command.Env = append(command.Env, "YIJIE_MARKET_WORKER_HOME="+o.libraryHome, "CODEX_HOME="+o.libraryHome)
	}
	command.Stdin, command.Stdout, command.Stderr = childInput, childOutput, io.Discard
	o.mu.Lock()
	if o.closed {
		err = ErrClosed
	} else if o.started {
		err = ErrStarted
	} else {
		err = ctx.Err()
	}
	if err == nil {
		key := ""
		if build.ExternalCallsEnabled && build.ProviderProfile == "generic-mcp-v1" {
			key = o.kimiModelKey
		}
		var reader, writer *os.File
		reader, writer, err = prepareModelCredential(key)
		if reader != nil {
			command.ExtraFiles = []*os.File{reader} // canonical child FD 3
			command.Env = append(command.Env, modelCredentialHint())
			defer reader.Close()
		}
		if err == nil {
			err = command.Start()
		}
		if writer != nil {
			if err != nil {
				writer.Close()
			} else {
				o.modelKeyWriter = writer
				go writeModelCredential(writer, key)
			}
		}
		if err == nil {
			o.kimiModelKey = ""
		}
	}
	childInput.Close()
	childOutput.Close()
	if err != nil {
		o.mu.Unlock()
		input.Close()
		output.Close()
		if errors.Is(err, ErrClosed) || errors.Is(err, ErrStarted) || ctx.Err() != nil {
			return err
		}
		return ErrProtocol
	}
	o.started, o.stdin, o.done = true, input, make(chan struct{})
	o.externalCallsEnabled = build.ExternalCallsEnabled
	o.capability, o.processKey = capability, processKey
	done := o.done
	o.mu.Unlock()
	go o.readOutput(output)
	go func() {
		waitErr := command.Wait()
		o.mu.Lock()
		o.exitErr = waitErr
		o.retireLocked(ErrProtocol)
		close(done)
		o.mu.Unlock()
	}()
	return nil
}

func (o *Owner) readOutput(output *os.File) {
	defer output.Close()
	reader := bufio.NewReaderSize(output, maxFrameBytes+1)
	for {
		frame, err := reader.ReadSlice('\n')
		if err != nil || len(frame) > maxFrameBytes {
			o.mu.Lock()
			o.retireLocked(ErrProtocol)
			o.mu.Unlock()
			// Continue draining after protocol retirement so EOF cleanup is not
			// blocked by a child's full stdout pipe. No diagnostic bytes retained.
			io.Copy(io.Discard, reader)
			return
		}
		o.mu.Lock()
		if o.active == nil || o.closed {
			o.retireLocked(ErrProtocol)
		} else {
			o.active <- exchangeResult{frame: append([]byte(nil), frame...)}
			o.active = nil
		}
		o.mu.Unlock()
	}
}

// retireLocked closes only our stdin pipe. It does not claim external work
// completed, close another process's descriptors, signal or drop child ownership.
func (o *Owner) retireLocked(reason error) {
	o.closed, o.initialized, o.capability = true, false, ""
	o.kimiModelKey = ""
	if o.modelKeyWriter != nil {
		o.modelKeyWriter.Close()
		o.modelKeyWriter = nil
	}
	if o.stdin != nil {
		o.stdin.Close()
		o.stdin = nil
	}
	if o.active != nil {
		o.active <- exchangeResult{err: reason}
		o.active = nil
	}
}

func (o *Owner) retire(reason error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.retireLocked(reason)
}

func (o *Owner) acquireControl(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case o.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-o.gate
		return err
	}
	return nil
}

// exchange is called with the control gate held through typed decoding and all
// response binding checks, so no subsequent mutation overtakes validation.
func (o *Owner) exchange(ctx context.Context, frame []byte, initializing bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(frame)+1 > maxFrameBytes {
		return nil, ErrProtocol
	}
	o.mu.Lock()
	if err := ctx.Err(); err != nil {
		o.mu.Unlock()
		return nil, err
	}
	if o.closed {
		o.mu.Unlock()
		return nil, ErrClosed
	}
	if !o.started || (!initializing && !o.initialized) {
		o.mu.Unlock()
		return nil, ErrNotStarted
	}
	reply := make(chan exchangeResult, 1)
	o.active = reply
	input := o.stdin
	o.mu.Unlock()
	timer := time.NewTimer(controlTimeout)
	defer timer.Stop()
	written := make(chan error, 1)
	go func() { _, err := input.Write(append(frame, '\n')); written <- err }()
	select {
	case err := <-written:
		if err != nil {
			o.retire(ErrOutcomeUnknown)
			return nil, ErrOutcomeUnknown
		}
	case <-ctx.Done():
		o.retire(ErrOutcomeUnknown)
		return nil, ErrOutcomeUnknown
	case <-timer.C:
		o.retire(ErrOutcomeUnknown)
		return nil, ErrOutcomeUnknown
	}
	select {
	case result := <-reply:
		return result.frame, result.err
	case <-ctx.Done():
		o.retire(ErrOutcomeUnknown)
		return nil, ErrOutcomeUnknown
	case <-timer.C:
		o.retire(ErrOutcomeUnknown)
		return nil, ErrOutcomeUnknown
	}
}

// Close permanently closes control admission, sends normal EOF and waits. On
// deadline it retains the process and returns STOP_PENDING. Call Close again to
// wait longer; no new process is started and no force-kill escalation exists.
func (o *Owner) Close(ctx context.Context) error {
	o.mu.Lock()
	o.retireLocked(ErrOutcomeUnknown)
	done := o.done
	o.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.exitErr != nil {
			return ErrExit
		}
		return nil
	case <-ctx.Done():
		return ErrStopPending
	}
}

// NormalExitConfirmed reports observation only; it is not provider readiness or
// a promise that an external tool completed or was rolled back.
func (o *Owner) NormalExitConfirmed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.done == nil {
		return false
	}
	select {
	case <-o.done:
		return o.exitErr == nil
	default:
		return false
	}
}
