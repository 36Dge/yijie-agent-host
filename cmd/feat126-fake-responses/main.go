package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/fakeresponses"
)

func main() {
	if err := run(); err != nil {
		log.Print("feat126_fake_responses_failed")
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED") != "true" {
		return errors.New("test profile is not enabled")
	}
	maxCalls := uint64(0)
	if value := os.Getenv("YIJIE_FEAT126_FAKE_RESPONSES_MAX_CALLS"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return errors.New("call limit is invalid")
		}
		maxCalls = parsed
	}
	server, err := fakeresponses.New(fakeresponses.Config{
		RunID: os.Getenv("YIJIE_FEAT126_S10_RUN_ID"), FixtureID: codex.FEAT126FakeFixtureID,
		Mode: fakeresponses.Mode(os.Getenv("YIJIE_FEAT126_FAKE_RESPONSES_MODE")), MaxCalls: maxCalls,
	})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:18082")
	if err != nil {
		return errors.New("loopback listener is unavailable")
	}
	httpServer := &http.Server{
		Handler: server.Handler(), ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- httpServer.Serve(listener) }()
	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-shutdownSignal:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
