package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/app"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/36Dge/yijie-agent-host/internal/skillmarket"
	"github.com/36Dge/yijie-agent-host/internal/skills"
)

// Explicit operator entry using the same pinned configuration. The application
// must already have exited normally; a held session DB lock rejects the command.
func runSkillMaintenance(logger *slog.Logger, action string) error {
	if action != "rollback" && action != "resume" {
		return errors.New("unknown Skill maintenance action")
	}
	config, err := app.LoadConfig()
	if err != nil {
		return err
	}
	if !config.Runtime.NativeSkillMarketplaceEnabled {
		return errors.New("Skill maintenance requires the compatible native candidate")
	}
	artifact, checkErr := codex.VerifyArtifact(context.Background(), config.Runtime.BinaryPath, config.Runtime.ManifestPath, config.Runtime.RequestTimeout)
	if checkErr != nil || !artifact.SupportsNativeSkillMarketplace() {
		return errors.New("native Skill marketplace Runtime is unqualified")
	}
	owner, err := session.OpenMaintenanceReader(config.HostHome)
	if err != nil {
		return errors.New("normal Host must exit before Skill maintenance")
	}
	defer owner.Close()
	// No sessions, connectors, media producers, or model turns are started.
	config.Runtime.Market = codex.MarketConfig{}
	config.Runtime.BuiltinMediaEnabled = false
	config.Runtime.BuiltinVideoEnabled = false
	config.Runtime.DocumentRuntimeEnabled = false
	runtime := codex.NewManager(config.Runtime, logger)
	if err := skills.SnapshotNativeMigrationInput(config.Skills.InstallRoot, config.HostHome); err != nil {
		return err
	}
	old, err := configuredSkillService(config, runtime)
	if err != nil {
		return err
	}
	defer old.Close()
	service, err := skillmarket.Open(skillmarket.Config{BundleRoot: config.Skills.NativeBundleRoot, BundleSHA256: config.Skills.NativeBundleSHA256, DataRoot: config.Skills.NativeMarketRoot, HostHome: config.HostHome, Runtime: runtime})
	if err != nil {
		return err
	}
	defer service.Close()
	if err = old.FinalizeRecoveredOrdinaryOperations(service.RecordLegacyRecovery); err != nil {
		return err
	}
	if err = service.PrepareMigration(old); err != nil {
		return err
	}
	if err = runtime.Start(context.Background()); err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			logger.Error("normal Runtime shutdown pending", "failure_code", "skill_maintenance_shutdown_pending")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err = service.Initialize(ctx); err != nil {
		return err
	}
	if action == "rollback" {
		err = service.RollbackMigration(ctx, old)
	} else {
		err = service.ResumeMigration(ctx, old)
	}
	if err != nil {
		return err
	}
	logger.Info("Skill maintenance completed", "action", action)
	return nil
}
