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
)

type Adapter struct {
	input      io.WriteCloser
	done       chan error
	URL, Token string
}

func StartAdapter(binary, hostOrigin, hostToken string) (*Adapter, error) {
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
	e = json.NewEncoder(a.input).Encode(map[string]string{"host_origin": hostOrigin, "host_token": hostToken, "capability": a.Token})
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
