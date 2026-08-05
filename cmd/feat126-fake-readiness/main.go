package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("feat126_fake_readiness_failed\n")
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED") != "true" {
		return errors.New("test profile is not enabled")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	result, err := fakeresponses.ProbeReadiness(ctx, os.Getenv("YIJIE_FEAT126_S10_RUN_ID"))
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	return encoder.Encode(result)
}
