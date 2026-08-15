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
		storeOptions := make([]session.StoreOption, 0, 1)
		if config.FEAT126ProjectDir != "" {
			storeOptions = append(storeOptions, session.WithFEAT126Authority(config.FEAT126ProjectDir, config.FEAT126TestRunID))
		}
		sessionStore, err = session.OpenStore(config.HostHome, storeOptions...)
		if err != nil {
			return err
		}
		defer sessionStore.Close()
		apiToken, err = security.LoadOrCreateAPIToken(config.HostHome)
		if err != nil {
			return err
		}
		serviceOptions := make([]session.ServiceOption, 0, 2)
		if config.RawReasoningV2Enabled {
			serviceOptions = append(serviceOptions, session.WithV2Events(session.NewEventHubVersion(session.EventSchemaVersionV2, 512, 64)))
		}
		if config.TitleV2Enabled {
			serviceOptions = append(serviceOptions, session.WithTitleGenerator(runtime))
		}
		sessionService = session.NewService(
			runtime,
			sessionStore,
			session.NewEventHub(512, 64),
			logger,
			serviceOptions...,
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
	watchdogContext, cancelWatchdog := context.WithCancel(context.Background())
	defer cancelWatchdog()
	var parentExited <-chan struct{}
	if config.FEAT126TestParentPID != 0 {
		parentExited = watchParent(watchdogContext, config.FEAT126TestParentPID, 100*time.Millisecond, os.Getppid)
	}

	var serveErr error
	select {
	case <-stop:
	case <-parentExited:
		logger.Warn("FEAT-126 test parent exited", "failure_code", "test_parent_exited")
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

func watchParent(ctx context.Context, expectedPID int, interval time.Duration, parentPID func() int) <-chan struct{} {
	exited := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if parentPID() != expectedPID {
				close(exited)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return exited
}
