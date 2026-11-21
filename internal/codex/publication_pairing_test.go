package codex

import (
	"context"
	pin "github.com/36Dge/yijie-agent-host/internal/contracts/runtimeskillmarket"
	"strings"
	"testing"
)

func TestPublicMessagePublicationRequiresPairedArtifactAndStandaloneMarker(t *testing.T) {
	m := NewManager(Config{RuntimePermissionsEnabled: true, ChatModelsEnabled: true, KimiK31Candidate: true}, nil)
	legacy := context.Background()
	ctx := WithPublicMessagePublication(legacy)
	if strings.Contains(m.publicationInstructions(ctx), "[yijie-publication-v2]") {
		t.Fatal("unpaired binary enabled")
	}
	m.setArtifact(ArtifactInfo{BinarySHA256: pin.RuntimeBinarySHA256, ManifestSHA256: pin.RuntimeManifestSHA256})
	for _, test := range []struct {
		ctx  context.Context
		want bool
	}{{legacy, false}, {ctx, true}} {
		instructions := m.publicationInstructions(test.ctx) + m.imageToolInstructions(test.ctx) + m.documentInstructions() + retrievalCapabilityInstructions
		count := 0
		for _, line := range strings.Split(instructions, "\n") {
			if line == "[yijie-publication-v2]" {
				count++
			}
		}
		if (count == 1) != test.want {
			t.Fatal("wrong publication capability in composed ordinary instructions")
		}
	}
}

func TestPublicPreviewRequiresNegotiationOnThePairedRuntime(t *testing.T) {
	m := NewManager(Config{RuntimePermissionsEnabled: true, ChatModelsEnabled: true, KimiK31Candidate: true}, nil)
	ctx := WithPublicMessagePreview(context.Background())
	const marker = "[yijie-public-preview-v1]"
	if strings.Contains(m.publicationInstructions(ctx), marker) {
		t.Fatal("unpaired preview")
	}
	m.setArtifact(ArtifactInfo{BinarySHA256: pin.RuntimeBinarySHA256, ManifestSHA256: pin.RuntimeManifestSHA256})
	if strings.Contains(m.publicationInstructions(WithPublicMessagePublication(context.Background())), marker) {
		t.Fatal("preview on v2")
	}
	if !strings.Contains(m.publicationInstructions(ctx), "\n"+marker+"\n") {
		t.Fatal("missing negotiated marker")
	}
}
