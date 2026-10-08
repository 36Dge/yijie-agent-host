package marketbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	selection "github.com/36Dge/yijie-agent-host/internal/contracts/marketselection"
	"github.com/google/uuid"
)

func controlID() wire.CanonicalId { return wire.CanonicalId(uuid.NewString()) }

func realWorker(t *testing.T) (*Owner, wire.ProcessBinding) {
	t.Helper()
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("canonical worker integration requires explicit YIJIE_MARKET_BROKER_TEST_MANIFEST")
	}
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	home := filepath.Join(parent, "host-home")
	if e := os.Mkdir(home, 0700); e != nil {
		t.Fatal(e)
	}
	owner, e := NewOwnerWithStorage(manifest, filepath.Join(home, "market-worker", "library-home"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := owner.Close(ctx); err != nil {
			t.Errorf("normal worker cleanup: %v; no force-kill attempted", err)
		}
	})
	process := wire.ProcessBinding{HostInstanceId: controlID(), RuntimeGeneration: controlID()}
	response, err := owner.Initialize(context.Background(), wire.InitializeRequest{SchemaVersion: 1, RequestId: controlID(), Method: "initialize", Payload: wire.InitializePayload{OperationId: controlID(), Process: process}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, e := loadArtifact(manifest)
	if e != nil {
		t.Fatal(e)
	}
	if response.Data.ExternalCallsEnabled != artifact.ExternalCallsEnabled || response.Data.Process != process {
		t.Fatal("unexpected worker initialization")
	}
	return owner, process
}

func syntheticContext(process wire.ProcessBinding) wire.ContextBinding {
	thread := wire.NativeId("ordinary-control-qualification-thread")
	return wire.ContextBinding{Process: process, Scope: wire.ScopeBinding{OwnerUserId: controlID(), TenantId: controlID(), NativeProcessEpoch: controlID(), AuthorizationRevision: 1, AuthorizationExpiresAtUnixMs: wire.UnixMilliseconds(time.Now().Add(time.Minute).UnixMilli())}, AgentSessionId: controlID(), NativeThreadId: &thread}
}

func syntheticSnapshot(t *testing.T, references selection.Selection) selection.SelectionSnapshot {
	t.Helper()
	operation := controlID()
	digest, err := selection.SelectionDigest(string(operation), references)
	if err != nil {
		t.Fatal(err)
	}
	return selection.SelectionSnapshot{SchemaVersion: 1, TurnOperationId: operation, Selection: references, SelectionDigest: selection.SelectionDigestValue(digest)}
}

// These tests run the actual canonical-built worker and ordinary synthetic
// identity data. They prove the private owner protocol, not real Native grant,
// actual Runtime Turn association, OAuth or supplier execution qualification.
func TestMarketOwnerCanonicalControlLifecycle(t *testing.T) {
	owner, process := realWorker(t)
	ctx := context.Background()
	request := wire.PrepareRequest{SchemaVersion: 1, RequestId: controlID(), Method: "prepare", Payload: wire.PreparePayload{OperationId: controlID(), Context: syntheticContext(process), Snapshot: syntheticSnapshot(t, selection.Selection{}), TtlMs: 30000}}
	prepared, err := owner.Prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Data.State != wire.LeaseStatePrepared || prepared.Data.NativeTurnId != nil {
		t.Fatal("unexpected prepared state")
	}
	request.RequestId = controlID()
	replay, err := owner.Prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replay.Data.Binding, prepared.Data.Binding) || replay.Data.RemainingTtlMs > prepared.Data.RemainingTtlMs {
		t.Fatal("replay changed capability or extended deadline")
	}
	bound, err := owner.BindTurn(ctx, wire.BindTurnRequest{SchemaVersion: 1, RequestId: controlID(), Method: "bind_turn", Payload: wire.BindTurnPayload{OperationId: controlID(), Binding: prepared.Data.Binding, ExpectedRevision: prepared.Data.Revision, NativeThreadId: *request.Payload.Context.NativeThreadId, NativeTurnId: "ordinary-control-qualification-turn"}})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Data.State != wire.LeaseStateBound {
		t.Fatal("turn binding missing")
	}
	// Ordinary concurrent readers use one owned JSONL pipe; replies cannot be
	// delivered to a different request even though every lease is the same.
	var readers sync.WaitGroup
	readErrors := make(chan error, 8)
	for range 8 {
		readers.Go(func() {
			status, err := owner.Status(ctx, wire.StatusRequest{SchemaVersion: 1, RequestId: controlID(), Method: "status", Payload: wire.StatusPayload{Binding: bound.Data.Binding}})
			if err == nil && status.Data.State != wire.LeaseStateBound {
				err = ErrBinding
			}
			readErrors <- err
		})
	}
	readers.Wait()
	close(readErrors)
	for err := range readErrors {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = owner.PendingCall(ctx, wire.PendingCallRequest{SchemaVersion: 1, RequestId: controlID(), Method: "pending_call", Payload: wire.PendingCallPayload{Binding: bound.Data.Binding, NativeThreadId: *bound.Data.BoundNativeThreadId, NativeTurnId: *bound.Data.NativeTurnId, CallRef: controlID()}})
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code() != wire.ErrorCodeNotFound {
		t.Fatalf("query must not create a pending call: %v", err)
	}
	revoke := wire.RevokeRequest{SchemaVersion: 1, RequestId: controlID(), Method: "revoke", Payload: wire.RevokePayload{OperationId: controlID(), Binding: bound.Data.Binding, ExpectedRevision: bound.Data.Revision, Reason: wire.RevokeReasonTurnTerminal}}
	revoked, err := owner.Revoke(ctx, revoke)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Data.State != wire.LeaseStateRevoked {
		t.Fatal("revocation missing")
	}
	status, err := owner.Status(ctx, wire.StatusRequest{SchemaVersion: 1, RequestId: controlID(), Method: "status", Payload: wire.StatusPayload{Binding: revoked.Data.Binding}})
	if err != nil || status.Data.State != wire.LeaseStateRevoked {
		t.Fatalf("revoked status missing: %v", err)
	}
	request.RequestId = controlID()
	_, err = owner.Prepare(ctx, request)
	if !errors.As(err, &remote) || remote.Code() != wire.ErrorCodeLeaseRevoked {
		t.Fatalf("revoked prepare revived: %v", err)
	}
	_, err = owner.Shutdown(ctx, wire.ShutdownRequest{SchemaVersion: 1, RequestId: controlID(), Method: "shutdown", Payload: wire.ShutdownPayload{OperationId: controlID(), Process: process, Reason: wire.ShutdownReasonOwnerShutdown}})
	if err != nil {
		t.Fatal(err)
	}
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := owner.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if !owner.NormalExitConfirmed() {
		t.Fatal("worker normal exit not observed")
	}
	if err := owner.start(ctx, processKey(process)); !errors.Is(err, ErrClosed) {
		t.Fatal("closed owner may not restart")
	}
}

func TestMarketOwnerCanonicalNonemptyUnqualifiedAndEOF(t *testing.T) {
	owner, process := realWorker(t)
	refs := selection.Selection{{InstallationId: controlID(), Revision: 1, Generation: 1}}
	_, err := owner.Prepare(context.Background(), wire.PrepareRequest{SchemaVersion: 1, RequestId: controlID(), Method: "prepare", Payload: wire.PreparePayload{OperationId: controlID(), Context: syntheticContext(process), Snapshot: syntheticSnapshot(t, refs), TtlMs: 30000}})
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code() != wire.ErrorCodeNotQualified {
		t.Fatalf("nonempty provider became qualified: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !owner.NormalExitConfirmed() {
		t.Fatal("normal EOF exit not observed")
	}
}

func TestMarketOwnerCanonicalNewThreadPreparation(t *testing.T) {
	owner, process := realWorker(t)
	ctx := context.Background()
	contextBinding := syntheticContext(process)
	contextBinding.NativeThreadId = nil // Explicit new-thread intent, not an omitted authority field.
	request := wire.PrepareRequest{SchemaVersion: 1, RequestId: controlID(), Method: "prepare", Payload: wire.PreparePayload{OperationId: controlID(), Context: contextBinding, Snapshot: syntheticSnapshot(t, selection.Selection{}), TtlMs: 30000}}
	prepared, err := owner.Prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Data.State != wire.LeaseStatePrepared || prepared.Data.Binding.Context.NativeThreadId != nil || prepared.Data.BoundNativeThreadId != nil || prepared.Data.NativeTurnId != nil {
		t.Fatal("new thread gained a binding before actual IDs")
	}
	bound, err := owner.BindTurn(ctx, wire.BindTurnRequest{SchemaVersion: 1, RequestId: controlID(), Method: "bind_turn", Payload: wire.BindTurnPayload{OperationId: controlID(), Binding: prepared.Data.Binding, ExpectedRevision: prepared.Data.Revision, NativeThreadId: "ordinary-new-qualification-thread", NativeTurnId: "ordinary-new-qualification-turn"}})
	if err != nil {
		t.Fatal(err)
	}
	if bound.Data.State != wire.LeaseStateBound || bound.Data.Binding.Context.NativeThreadId != nil || bound.Data.BoundNativeThreadId == nil || *bound.Data.BoundNativeThreadId != "ordinary-new-qualification-thread" {
		t.Fatal("new thread immutable intent/bound observation mismatch")
	}
	request.RequestId = controlID()
	replay, err := owner.Prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Data.State != wire.LeaseStateBound || !reflect.DeepEqual(replay.Data.Binding, bound.Data.Binding) || !reflect.DeepEqual(replay.Data.BoundNativeThreadId, bound.Data.BoundNativeThreadId) || replay.Data.RemainingTtlMs > bound.Data.RemainingTtlMs {
		t.Fatal("prepare replay lost original real IDs or renewed capability")
	}
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := owner.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if !owner.NormalExitConfirmed() {
		t.Fatal("new-thread owner normal EOF exit not observed")
	}
}

// This is ordinary capacity use: every request is valid and acknowledged by
// the canonical product worker. No process stall, substituted executable or
// failure injection is used. Each termination action needs its own generation.
func TestMarketOwnerCanonicalCapacityTermination(t *testing.T) {
	for _, action := range []string{"revoke", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			owner, process := realWorker(t)
			ctx := context.Background()
			contextBinding := syntheticContext(process)
			contextBinding.Scope.AuthorizationExpiresAtUnixMs = wire.UnixMilliseconds(time.Now().Add(5 * time.Minute).UnixMilli())
			request := wire.PrepareRequest{SchemaVersion: 1, RequestId: controlID(), Method: "prepare", Payload: wire.PreparePayload{OperationId: controlID(), Context: contextBinding, Snapshot: syntheticSnapshot(t, selection.Selection{}), TtlMs: wire.MaxLeaseTtlMs}}
			prepared, err := owner.Prepare(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			// Initialize and original prepare have already retained two receipts.
			for range wire.MaxMutationReceipts - 2 {
				request.RequestId, request.Payload.OperationId = controlID(), controlID()
				replay, err := owner.Prepare(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(replay.Data.Binding, prepared.Data.Binding) {
					t.Fatal("ordinary replay allocated a second lease")
				}
			}
			statusRequest := wire.StatusRequest{SchemaVersion: 1, RequestId: controlID(), Method: "status", Payload: wire.StatusPayload{Binding: prepared.Data.Binding}}
			status, err := owner.Status(ctx, statusRequest)
			if err != nil || status.Data.State != wire.LeaseStatePrepared {
				t.Fatalf("lease should remain prepared at the capacity boundary: %v", err)
			}
			if action == "revoke" {
				_, err = owner.Revoke(ctx, wire.RevokeRequest{SchemaVersion: 1, RequestId: controlID(), Method: "revoke", Payload: wire.RevokePayload{OperationId: controlID(), Binding: prepared.Data.Binding, ExpectedRevision: prepared.Data.Revision, Reason: wire.RevokeReasonOwnerShutdown}})
			} else {
				_, err = owner.Shutdown(ctx, wire.ShutdownRequest{SchemaVersion: 1, RequestId: controlID(), Method: "shutdown", Payload: wire.ShutdownPayload{OperationId: controlID(), Process: process, Reason: wire.ShutdownReasonOwnerShutdown}})
			}
			var remote *RemoteError
			if !errors.As(err, &remote) || remote.Code() != wire.ErrorCodeCapacityExceeded {
				t.Fatalf("termination must preserve the explicit capacity error: %v", err)
			}
			statusRequest.RequestId = controlID()
			if _, err := owner.Status(ctx, statusRequest); !errors.Is(err, ErrClosed) {
				t.Fatalf("capacity termination must close subsequent admission: %v", err)
			}
			closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := owner.Close(closeCtx); err != nil {
				t.Fatal(err)
			}
			if !owner.NormalExitConfirmed() {
				t.Fatal("capacity termination did not observe normal EOF exit")
			}
			if err := owner.start(ctx, processKey(process)); !errors.Is(err, ErrClosed) {
				t.Fatal("capacity-retired owner may not restart")
			}
		})
	}
}

func TestMarketOwnerCanonicalProviderStartupReusesOneChild(t *testing.T) {
	manifest := os.Getenv("YIJIE_MARKET_BROKER_TEST_MANIFEST")
	if manifest == "" {
		t.Skip("explicit canonical worker manifest required")
	}
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	home := filepath.Join(parent, "host-home")
	if e := os.Mkdir(home, 0700); e != nil {
		t.Fatal(e)
	}
	owner, e := NewOwnerWithStorage(manifest, filepath.Join(home, "market-worker", "library-home"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e := owner.Close(ctx); e != nil {
			t.Errorf("normal close: %v; no force kill", e)
		}
	})
	if e := owner.StartControl(context.Background()); e != nil {
		t.Fatal(e)
	}
	original := owner.Done()
	if owner.initialized || owner.processKey != "" {
		t.Fatal("provider startup invented a Runtime generation")
	}
	if e := owner.StartControl(context.Background()); e != nil || owner.Done() != original {
		t.Fatal("provider startup replaced owner", e)
	}
	process := wire.ProcessBinding{HostInstanceId: controlID(), RuntimeGeneration: controlID()}
	_, e = owner.Initialize(context.Background(), wire.InitializeRequest{SchemaVersion: 1, RequestId: controlID(), Method: "initialize", Payload: wire.InitializePayload{OperationId: controlID(), Process: process}})
	if e != nil || owner.Done() != original {
		t.Fatal("actual Runtime initialization did not reuse provider child", e)
	}
	if e := owner.Close(context.Background()); e != nil || !owner.NormalExitConfirmed() {
		t.Fatal("normal EOF not observed", e)
	}
	if e := owner.StartControl(context.Background()); !errors.Is(e, ErrClosed) {
		t.Fatal("closed provider owner reopened", e)
	}
}
