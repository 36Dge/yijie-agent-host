package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

const artifactCheckTimeout = 15 * time.Second

type artifactVerifier func(context.Context, string, string, time.Duration) (codex.ArtifactInfo, error)

func run(args []string, stderr io.Writer, verify artifactVerifier) int {
	if len(args) != 4 || args[1] != "--artifact-only" ||
		!filepath.IsAbs(args[2]) || !filepath.IsAbs(args[3]) {
		_, _ = fmt.Fprintln(stderr, "runtime_artifact_arguments_invalid")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), artifactCheckTimeout)
	defer cancel()
	if _, err := verify(ctx, args[2], args[3], artifactCheckTimeout); err != nil {
		_, _ = fmt.Fprintln(stderr, "runtime_artifact_invalid")
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args, os.Stderr, codex.VerifyArtifact))
}
