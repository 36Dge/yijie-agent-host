package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type feat137HTTPShutdownRecorder struct {
	events chan<- string
}

func (recorder feat137HTTPShutdownRecorder) Shutdown(context.Context) error {
	recorder.events <- "http-ingress-stopped"
	return nil
}

type feat137DelayedRuntimeShutdown struct {
	mu      sync.Mutex
	calls   int
	events  chan<- string
	release <-chan struct{}
}

func (runtime *feat137DelayedRuntimeShutdown) Shutdown(ctx context.Context) error {
	runtime.mu.Lock()
	runtime.calls++
	call := runtime.calls
	runtime.mu.Unlock()
	if call == 1 {
		runtime.events <- "runtime-bounded"
		<-ctx.Done()
		return ctx.Err()
	}
	runtime.events <- "runtime-continued"
	<-runtime.release
	runtime.events <- "runtime-normal-exit"
	return nil
}

func TestFEAT137HostStopsIngressThenWaitsPastRuntimeDeadline(t *testing.T) {
	events := make(chan string, 8)
	release := make(chan struct{})
	runtime := &feat137DelayedRuntimeShutdown{events: events, release: release}
	result := make(chan error, 1)
	go func() {
		result <- shutdownHost(
			feat137HTTPShutdownRecorder{events: events},
			runtime,
			func() { events <- "runtime-context-cancelled" },
			20*time.Millisecond,
		)
	}()

	for _, want := range []string{
		"http-ingress-stopped",
		"runtime-context-cancelled",
		"runtime-bounded",
		"runtime-continued",
	} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("shutdown ordering drifted: got %q want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for shutdown phase %q", want)
		}
	}
	select {
	case err := <-result:
		t.Fatalf("Host returned after bounded Runtime deadline: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case got := <-events:
		if got != "runtime-normal-exit" {
			t.Fatalf("unexpected final Runtime phase: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("normal Runtime exit was not observed")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("graceful Host shutdown failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Host did not return after normal Runtime exit")
	}
}

func TestFEAT126ProcessFailureLoggingIsContentFree(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	pathCanary := "/private/feat126-run/project/operator-secret"
	tokenCanary := "feat126-token-canary"
	logProcessFailure(logger, errors.New(pathCanary+" "+tokenCanary), true)
	encoded := output.String()
	if strings.Contains(encoded, pathCanary) || strings.Contains(encoded, tokenCanary) {
		t.Fatalf("exact-profile process failure leaked sensitive error content")
	}
	if !strings.Contains(encoded, `"failure_code":"feat126_host_process_failed"`) ||
		strings.Contains(encoded, `"error"`) {
		t.Fatalf("exact-profile process failure did not use the closed projection: %s", encoded)
	}
}

func TestDefaultProcessFailureLoggingKeepsDiagnosticCompatibility(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logProcessFailure(logger, errors.New("default diagnostic"), false)
	if !strings.Contains(output.String(), `"error":"default diagnostic"`) {
		t.Fatalf("default process failure logging changed: %s", output.String())
	}
}

func TestWatchParentSignalsOnlyAfterTheExpectedParentChanges(t *testing.T) {
	var current atomic.Int64
	current.Store(42)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exited := watchParent(ctx, 42, time.Millisecond, func() int { return int(current.Load()) })
	select {
	case <-exited:
		t.Fatal("watchdog signaled while the expected parent was alive")
	case <-time.After(5 * time.Millisecond):
	}
	current.Store(1)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not signal after parent exit")
	}
}

func TestWatchParentSignalsWhenTheParentChangedBeforeWatching(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exited := watchParent(ctx, 42, time.Hour, func() int { return 1 })
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not immediately detect an already exited parent")
	}
}
