package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

func TestArtifactOnlyInvokesTheExactVerifierOnce(t *testing.T) {
	var calls int
	var binaryPath string
	var manifestPath string
	var timeout time.Duration
	verify := func(_ context.Context, binary, manifest string, budget time.Duration) (codex.ArtifactInfo, error) {
		calls++
		binaryPath = binary
		manifestPath = manifest
		timeout = budget
		return codex.ArtifactInfo{}, nil
	}
	var stderr bytes.Buffer
	code := run([]string{"runtime-healthcheck", "--artifact-only", "/runtime/codex", "/runtime/manifest.json"}, &stderr, verify)
	if code != 0 || calls != 1 || binaryPath != "/runtime/codex" ||
		manifestPath != "/runtime/manifest.json" || timeout != artifactCheckTimeout || stderr.Len() != 0 {
		t.Fatalf("unexpected artifact-only result: code=%d calls=%d stderr=%q", code, calls, stderr.String())
	}
}

func TestArtifactOnlyRejectsEveryOtherInvocationBeforeVerification(t *testing.T) {
	for _, args := range [][]string{
		{"runtime-healthcheck"},
		{"runtime-healthcheck", "--artifact-only", "relative", "/runtime/manifest.json"},
		{"runtime-healthcheck", "--artifact-only", "/runtime/codex", "relative"},
		{"runtime-healthcheck", "--other", "/runtime/codex", "/runtime/manifest.json"},
		{"runtime-healthcheck", "--artifact-only", "/runtime/codex", "/runtime/manifest.json", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			calls := 0
			verify := func(context.Context, string, string, time.Duration) (codex.ArtifactInfo, error) {
				calls++
				return codex.ArtifactInfo{}, nil
			}
			var stderr bytes.Buffer
			if code := run(args, &stderr, verify); code != 2 || calls != 0 ||
				stderr.String() != "runtime_artifact_arguments_invalid\n" {
				t.Fatalf("invalid invocation was not closed: code=%d calls=%d stderr=%q", code, calls, stderr.String())
			}
		})
	}
}

func TestArtifactFailureIsFixedAndPathFree(t *testing.T) {
	verify := func(context.Context, string, string, time.Duration) (codex.ArtifactInfo, error) {
		return codex.ArtifactInfo{}, errors.New("/private/runtime/token-value")
	}
	var stderr bytes.Buffer
	code := run([]string{"runtime-healthcheck", "--artifact-only", "/runtime/codex", "/runtime/manifest.json"}, &stderr, verify)
	if code != 1 || stderr.String() != "runtime_artifact_invalid\n" {
		t.Fatalf("artifact failure leaked details: code=%d stderr=%q", code, stderr.String())
	}
}
