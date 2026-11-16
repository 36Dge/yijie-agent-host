package codex

import (
	"context"
	"testing"
)

func TestFEAT175BuiltinWebRootMustStillBeCurrent(t *testing.T) {
	m := &Manager{permissionCallbacks: runtimeApprovalCallbacks{nativeRoots: map[string]bool{"root": true}, activeTurns: map[string]string{"root": "current"}}}
	m.status.Ready = true
	root, turn, err := m.BuiltinWebOwner(context.Background(), "root", "current")
	if err != nil || root != "root" || turn != "current" {
		t.Fatal("current owned root unavailable", err)
	}
	m.permissionCallbacks.activeTurns["root"] = "next"
	if _, _, err = m.BuiltinWebOwner(context.Background(), "root", "current"); err == nil {
		t.Fatal("old root turn attached to new turn")
	}
}
