package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

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
