package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/app"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("yijie-agent-host stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := app.LoadConfig()
	if err != nil {
		return err
	}
	runtime := codex.NewManager(config.Runtime, logger)
	var sessionService *session.Service
	var sessionStore *session.Store
	var apiToken string
	if config.HostHome != "" {
		sessionStore, err = session.OpenStore(config.HostHome)
		if err != nil {
			return err
		}
		defer sessionStore.Close()
		apiToken, err = security.LoadOrCreateAPIToken(config.HostHome)
		if err != nil {
			return err
		}
		sessionService = session.NewService(
			runtime,
			sessionStore,
			session.NewEventHub(512, 64),
			logger,
		)
		if err := runtime.SetNotificationHandler(sessionService.HandleNotification); err != nil {
			return err
		}
	}
	server := &http.Server{
		Addr:              "127.0.0.1:" + config.Port,
		Handler:           app.NewHandler(config, runtime, sessionService, apiToken),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       30 * time.Second,
	}

	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()
	runtimeStarted := make(chan struct{})
	go func() {
		defer close(runtimeStarted)
		if err := runtime.Start(runtimeCtx); err != nil {
			logger.Error("Codex Runtime unavailable", "failure_code", runtime.Snapshot().FailureCode)
		}
	}()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting yijie-agent-host", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	var serveErr error
	select {
	case <-stop:
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serve HTTP: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.Runtime.ShutdownTimeout)
	defer cancel()
	cancelRuntime()
	select {
	case <-runtimeStarted:
	case <-shutdownCtx.Done():
	}
	if err := runtime.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shutdown Codex Runtime: %w", err)
	}
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return serveErr
}
