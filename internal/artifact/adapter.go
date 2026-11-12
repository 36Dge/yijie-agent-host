package artifact

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	document "github.com/36Dge/yijie-agent-host/internal/contracts/documentruntime"
)

type Adapter struct {
	input                                io.WriteCloser
	done                                 chan error
	URL, Token                           string
	MediaURL, MediaToken, MediaHostToken string
}

func StartAdapter(binary, hostOrigin, hostToken string, documentsEnabled ...bool) (*Adapter, error) {
	info, e := os.Lstat(binary)
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("artifact adapter missing")
	}
	// Canonical build emits an adjacent digest, verified before execution.
	expected, e := os.ReadFile(binary + ".sha256")
	if e != nil {
		return nil, e
	}
	data, e := os.ReadFile(binary)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(data)
	if string(expected) != hex.EncodeToString(sum[:])+"\n" {
		return nil, errors.New("artifact adapter identity mismatch")
	}
	random := make([]byte, 32)
	if _, e = rand.Read(random); e != nil {
		return nil, e
	}
	a := &Adapter{done: make(chan error, 1), Token: hex.EncodeToString(random)}
	if len(documentsEnabled) > 1 && documentsEnabled[1] {
		if _, e = rand.Read(random); e != nil {
			return nil, e
		}
		a.MediaToken = hex.EncodeToString(random)
		if _, e = rand.Read(random); e != nil {
			return nil, e
		}
		a.MediaHostToken = hex.EncodeToString(random)
	}
	cmd := exec.Command(binary)
	cmd.Dir = filepath.Dir(binary)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stderr = os.Stderr
	a.input, e = cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		_ = a.input.Close()
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		_ = a.input.Close()
		return nil, e
	}
	go func() { a.done <- cmd.Wait() }()
	startup := document.AdapterStartup{HostOrigin: hostOrigin, HostToken: hostToken, Capability: a.Token}
	if len(documentsEnabled) > 0 && documentsEnabled[0] {
		startup.DocumentRuntimeEnabled = &documentsEnabled[0]
	}
	if a.MediaToken != "" {
		startup.Media = &document.MediaAdapterCapability{Capability: a.MediaToken, HostToken: a.MediaHostToken}
	}
	e = json.NewEncoder(a.input).Encode(startup)
	if e != nil {
		_ = a.Stop(context.Background())
		return nil, e
	}
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 1024), 4096)
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
	}()
	select {
	case a.URL = <-ready:
	case <-time.After(15 * time.Second):
		_ = a.Stop(context.Background())
		return nil, errors.New("artifact adapter startup timed out")
	}
	parsed, e := url.Parse(a.URL)
	if e != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Path != "/mcp" || parsed.RawQuery != "" {
		_ = a.Stop(context.Background())
		return nil, errors.New("artifact adapter endpoint invalid")
	}
	if a.MediaToken != "" {
		parsed.Path = "/media/mcp"
		a.MediaURL = parsed.String()
	}
	return a, nil
}
func (a *Adapter) Stop(ctx context.Context) error {
	_ = a.input.Close()
	select {
	case err := <-a.done:
		a.done <- err
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
