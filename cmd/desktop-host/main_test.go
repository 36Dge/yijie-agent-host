package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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
