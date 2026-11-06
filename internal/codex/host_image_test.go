package codex

import (
	"context"
	"strings"
	"testing"

	broker "github.com/36Dge/yijie-agent-host/internal/contracts/marketbrokercontrol"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/markethost"
	image "github.com/36Dge/yijie-agent-host/internal/contracts/runtimehostimages"
)

func TestHostImageRouteUsesVerifiedExpertAndFrozenMethod(t *testing.T) {
	r := image.ExpertImageRoutes[0]
	ctx, err := WithExpertImageRoute(context.Background(), r.ExpertID, r.Version, r.RoleSHA256)
	if err != nil || ImageRoute(ctx) != r.Route {
		t.Fatalf("verified expert route: %v", err)
	}
	var pending market.MarketApproval
	pending.Identity.ServiceId = broker.ServiceId(r.Service)
	pending.Review.Title = r.Proxy
	args := `{"toolName":"aigc/imageGenAsync","arguments":{"prompt":"studio lamp"}}`
	digest := strings.Repeat("a", 64)
	pending.Review.ArgumentsJson, pending.Review.SchemaDigest = &args, &digest
	if create, err := authorizeImageMethod(pending, ImageRoute(ctx)); !create || err != nil {
		t.Fatal("selected expert missing its configured method")
	}
	if _, err := authorizeImageMethod(pending, "builtin"); err == nil {
		t.Fatal("unselected expert creation admitted")
	}
	args = `{"toolName":"aigc/taskQuery","arguments":{"taskId":"earlier-task"}}`
	if create, err := authorizeImageMethod(pending, "builtin"); create || err != nil {
		t.Fatal("ordinary task query changed")
	}
	if _, err := WithExpertImageRoute(context.Background(), r.ExpertID, r.Version, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unreviewed binding accepted")
	}
}

func TestHostImageNativeRequest(t *testing.T) {
	manager := NewManager(Config{HostImagesEnabled: true}, nil)
	manager.artifact = ArtifactInfo{BinarySHA256: image.RuntimeBinarySHA256, ManifestSHA256: image.RuntimeManifestSHA256}
	var calls int
	if err := manager.SetHostImageHandler(func(_ context.Context, p image.HostImageGenerateParams) bool {
		calls++
		return p.Prompt == "studio lamp" && !p.UseCurrentReference
	}); err != nil {
		t.Fatal(err)
	}
	r := serverRequest{method: image.Method, exactEnvelope: true, params: []byte(`{"threadId":"thread","turnId":"turn","callId":"call","prompt":"studio lamp","useCurrentReference":false}`)}
	out := manager.handleServerRequest(context.Background(), r)
	if !out.respond || !out.value.(image.HostImageGenerateResponse).Success || calls != 1 {
		t.Fatal("managed image call failed")
	}
	if manager.Snapshot().ExperimentalAPI {
		t.Fatal("managed image call enabled experimental API")
	}
}
