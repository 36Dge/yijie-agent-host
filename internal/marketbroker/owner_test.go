package marketbroker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
)

func TestMarketOwnerClosedBeforeStart(t *testing.T) {
	owner := NewOwner("/not-configured/market-worker.json")
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := owner.start(context.Background(), ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if owner.started || owner.NormalExitConfirmed() {
		t.Fatal("an idle owner must not create an exit fact")
	}
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMarketOwnerPreCancelledDoesNotStart(t *testing.T) {
	owner := NewOwner("/not-configured/market-worker.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := owner.start(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if owner.started || owner.closed {
		t.Fatal("pre-cancellation should leave an unstarted owner")
	}
	if _, err := owner.exchange(ctx, []byte("{}"), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestMarketOwnerQueuedCancellationDoesNotWrite(t *testing.T) {
	owner := NewOwner("/not-configured/market-worker.json")
	owner.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- owner.acquireControl(ctx) }()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	<-owner.gate
	if owner.started || owner.closed || owner.active != nil {
		t.Fatal("queued cancellation changed process admission")
	}
}

func TestMarketRuntimeCapabilityScrubAndRedaction(t *testing.T) {
	input := []string{"PATH=/usr/bin", RuntimeCapabilityEnvironment + "=ordinary-old-marker", "LANG=en_US.UTF-8", strings.ToLower(RuntimeCapabilityEnvironment) + "=ordinary-other-marker", RuntimeCapabilityEnvironment + "_LABEL=ordinary=label", "ORDINARY_EMPTY", RuntimeCapabilityEnvironment}
	want := []string{"PATH=/usr/bin", "LANG=en_US.UTF-8", RuntimeCapabilityEnvironment + "_LABEL=ordinary=label", "ORDINARY_EMPTY"}
	if got := ScrubRuntimeCapability(input); !reflect.DeepEqual(got, want) {
		t.Fatal("ambient capability was not removed")
	}
	if len(input) != 7 {
		t.Fatal("caller environment changed in place")
	}
	owner := NewOwner("/private/path")
	owner.capability = "ordinary-private-marker"
	for _, value := range []string{fmt.Sprintf("%v", owner), fmt.Sprintf("%+v", owner), fmt.Sprintf("%#v", owner), fmt.Sprintf("%#v", *owner), fmt.Sprintf("%+v", *owner)} {
		if strings.Contains(value, "ordinary-private-marker") || strings.Contains(value, "/private/path") {
			t.Fatal("private state appeared in formatted diagnostics")
		}
	}
}

func TestMarketTerminationCapacityRetiresOnlyTerminationActions(t *testing.T) {
	cases := []struct {
		name    string
		request validatedRequest
		code    wire.ErrorCode
		closed  bool
	}{
		{"revoke at capacity", wire.RevokeRequest{}, wire.ErrorCodeCapacityExceeded, true},
		{"shutdown at capacity", wire.ShutdownRequest{}, wire.ErrorCodeCapacityExceeded, true},
		{"prepare at capacity", wire.PrepareRequest{}, wire.ErrorCodeCapacityExceeded, false},
		{"decision at capacity", wire.DecideCallRequest{}, wire.ErrorCodeCapacityExceeded, false},
		{"revoke conflict", wire.RevokeRequest{}, wire.ErrorCodeRevisionConflict, false},
		{"shutdown binding mismatch", wire.ShutdownRequest{}, wire.ErrorCodeBindingMismatch, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner := NewOwner("/not-configured/market-worker.json")
			retireTerminationAtCapacity(owner, tc.request, tc.code)
			if owner.closed != tc.closed {
				t.Fatal("control error changed an unrelated lifecycle")
			}
			if owner.started || owner.NormalExitConfirmed() {
				t.Fatal("capacity decision must not fabricate a process or exit")
			}
		})
	}
}

func TestMarketRuntimeCarrierRequiresExactLiveOwnerGeneration(t *testing.T) {
	process := wire.ProcessBinding{HostInstanceId: "11111111-1111-4111-8111-111111111111", RuntimeGeneration: "22222222-2222-4222-8222-222222222222"}
	owner := NewOwner("/not-configured/market-worker.json")
	if _, err := owner.EnvironmentForRuntime(nil, process); !errors.Is(err, ErrNotStarted) {
		t.Fatal("unstarted owner granted carrier")
	}
	owner.started, owner.initialized, owner.processKey, owner.capability = true, true, processKey(process), "ordinary-unit-owned-value"
	environment, err := owner.EnvironmentForRuntime([]string{"PATH=/usr/bin", RuntimeCapabilityEnvironment + "=ordinary-ambient-value"}, process)
	if err != nil || len(environment) != 2 || environment[1] != RuntimeCapabilityEnvironment+"=ordinary-unit-owned-value" {
		t.Fatal("carrier did not replace ambient state with its owned value")
	}
	other := process
	other.RuntimeGeneration = "33333333-3333-4333-8333-333333333333"
	if _, err := owner.EnvironmentForRuntime(nil, other); !errors.Is(err, ErrBinding) {
		t.Fatal("different Runtime generation obtained carrier")
	}
	owner.retire(ErrClosed)
	if _, err := owner.EnvironmentForRuntime(nil, process); !errors.Is(err, ErrClosed) {
		t.Fatal("closed owner granted carrier")
	}
}
