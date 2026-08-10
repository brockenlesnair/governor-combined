// Package integration wires all governor components together.
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/brockenlesnair/governor-combined/pkg/adr"
	"github.com/brockenlesnair/governor-combined/pkg/hangar"
	"github.com/brockenlesnair/governor-combined/pkg/repo"
	"github.com/brockenlesnair/governor-combined/pkg/rigour"
	"github.com/brockenlesnair/governor-combined/pkg/safety"
	"github.com/brockenlesnair/governor-combined/pkg/sarif"
)

// Config holds configuration for all governor components.
type Config struct {
	Safety *safety.Config
	Rigour rigour.SupervisorConfig
	SARIF  sarif.DedupConfig
	ADR    *adr.Config
	Hangar hangar.Config
	Repo   repo.Config
}

// DefaultConfig returns a Config with sensible defaults.
// External services (Hangar, Repo) require explicit BaseURL/APIKey/ServerCommand.
func DefaultConfig() *Config {
	return &Config{
		Safety: safety.DefaultConfig(),
		Rigour: rigour.DefaultSupervisorConfig(),
		SARIF:  sarif.DedupConfig{},
		ADR: &adr.Config{
			AdrRoot:  "docs/adr",
			RepoRoot: ".",
		},
		Hangar: hangar.Config{},
		Repo:   repo.Config{},
	}
}

// Governor holds all wired governance components.
type Governor struct {
	Safety   *safety.SafetyValidator
	Rigour   *rigour.RigourSupervisor
	SARIF    *sarif.DedupPipeline
	ADRHook  *adr.CIHookHandler
	ADRCmd   *adr.CommandHandler
	ADRClient adr.AdrMcpClient
	Hangar   *hangar.Client
	Repo     *repo.Client
	Logger   *slog.Logger
}

// New creates a Governor with all components wired together.
// Components that require external services (Hangar, Repo) are optional:
// they are skipped if their config is incomplete, and their tools return
// "not configured" errors at call time.
func New(logger *slog.Logger, cfg *Config) (*Governor, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	g := &Governor{Logger: logger}

	// ── Safety ──────────────────────────────────────────────────────
	safetyVal, err := safety.NewValidator(cfg.Safety)
	if err != nil {
		return nil, fmt.Errorf("init safety: %w", err)
	}
	g.Safety = safetyVal

	// ── Rigour (Rigour→Safety: DLP pre-filters feed findings into safety) ──
	g.Rigour = rigour.NewRigourSupervisor(logger, cfg.Rigour)

	// ── SARIF (SARIF→Safety: dedup pipeline normalises findings across tools) ──
	g.SARIF = sarif.NewDedupPipeline(cfg.SARIF, logger)

	// ── ADR (ADR→DocGov: CI hook triggers ADR creation from merged PRs) ──
	adrClient := adr.NewGoStubAdrMcpClient(logger)
	g.ADRClient = adrClient
	g.ADRHook = adr.NewCIHookHandler(cfg.ADR, adrClient, logger)
	g.ADRCmd = adr.NewCommandHandler(cfg.ADR, adrClient, logger)

	// ── Hangar (optional — requires API key) ────────────────────────
	if cfg.Hangar.BaseURL != "" {
		hangarClient, err := hangar.NewClient(cfg.Hangar, logger)
		if err != nil {
			logger.Warn("hangar client init failed (optional)", "err", err)
		} else {
			g.Hangar = hangarClient
		}
	}

	// ── Repo Butler (optional — requires server_command) ────────────
	if cfg.Repo.ServerCommand != "" {
		repoClient, err := repo.NewClient(cfg.Repo, logger)
		if err != nil {
			logger.Warn("repo-butler client init failed (optional)", "err", err)
		} else {
			g.Repo = repoClient
		}
	}

	return g, nil
}

// Start begins background components (rigour supervisor).
func (g *Governor) Start(ctx context.Context) error {
	if err := g.Rigour.Start(); err != nil {
		return fmt.Errorf("start rigour: %w", err)
	}

	// Connect repo-butler if available
	if g.Repo != nil {
		connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := g.Repo.Connect(connectCtx); err != nil {
			g.Logger.Warn("repo-butler connect failed (non-fatal)", "err", err)
		}
	}

	return nil
}

// Stop gracefully shuts down all components.
func (g *Governor) Stop() {
	if g.Rigour != nil {
		_ = g.Rigour.Stop()
	}
	if g.Hangar != nil {
		g.Hangar.Close()
	}
	if g.Repo != nil {
		_ = g.Repo.Close()
	}
	if g.Safety != nil {
		_ = g.Safety.Close()
	}
}
