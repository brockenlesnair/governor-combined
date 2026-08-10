package protocol

// DefaultDevices returns the full device registry for governor-combined.
// Each device maps to an MCP tool. Tier determines minimum access level.
func DefaultDevices() []Device {
	return []Device{
		// ── Search ──────────────────────────────────────────────
		{
			Name:        "search_code",
			Description: "Fuzzy, regex, and type-aware code search across the codebase",
			Tier:        TierSplinter, // all agents need to find code
			Category:    "search",
			Tags:        []string{"discovery", "navigation"},
		},
		{
			Name:        "get_callers",
			Description: "Find all callers of a given function or method",
			Tier:        TierSplinter,
			Category:    "analysis",
			Tags:        []string{"callgraph", "dependencies"},
		},
		{
			Name:        "get_callees",
			Description: "Find all functions called by a given function or method",
			Tier:        TierSplinter,
			Category:    "analysis",
			Tags:        []string{"callgraph", "dependencies"},
		},
		{
			Name:        "get_impact",
			Description: "Analyze blast radius of changing a code entity",
			Tier:        TierSplinter,
			Category:    "analysis",
			Tags:        []string{"impact", "risk"},
		},

		// ── Safety ──────────────────────────────────────────────
		{
			Name:        "validate_code",
			Description: "Validate code against 12 builtin safety rules with risk scoring",
			Tier:        TierSplinter, // agents should self-check
			Category:    "safety",
			Tags:        []string{"validation", "security"},
		},
		{
			Name:        "validate_diff",
			Description: "Validate a diff/patch before applying changes",
			Tier:        TierSplinter,
			Category:    "safety",
			Tags:        []string{"validation", "diff"},
		},

		// ── Rigour ──────────────────────────────────────────────
		{
			Name:        "rigour_check",
			Description: "Check for AI drift, phantom APIs, and session artifacts",
			Tier:        TierSteer, // oversight only
			Category:    "governance",
			Tags:        []string{"rigour", "drift"},
		},
		{
			Name:        "rigour_state",
			Description: "Get current rigour supervisor state and active patterns",
			Tier:        TierSteer,
			Category:    "governance",
			Tags:        []string{"rigour", "state"},
		},
		{
			Name:        "rigour_stats",
			Description: "Get rigour statistics: patterns learned, drift detected, fixes applied",
			Tier:        TierSteer,
			Category:    "governance",
			Tags:        []string{"rigour", "stats"},
		},

		// ── SARIF ───────────────────────────────────────────────
		{
			Name:        "sarif_export",
			Description: "Export findings in SARIF 2.1.0 format for CI/CD integration",
			Tier:        TierSteer,
			Category:    "governance",
			Tags:        []string{"sarif", "export"},
		},

		// ── ADR ─────────────────────────────────────────────────
		{
			Name:        "adr_create",
			Description: "Create an Architecture Decision Record",
			Tier:        TierSplinter, // agents should document decisions
			Category:    "governance",
			Tags:        []string{"adr", "documentation"},
		},
		{
			Name:        "adr_list",
			Description: "List existing Architecture Decision Records",
			Tier:        TierSplinter,
			Category:    "governance",
			Tags:        []string{"adr", "documentation"},
		},

		// ── Audit ───────────────────────────────────────────────
		{
			Name:        "audit_project",
			Description: "Run full project audit: safety, rigour, coverage, staleness",
			Tier:        TierSteer, // project-wide oversight
			Category:    "governance",
			Tags:        []string{"audit", "health"},
		},

		// ── Hangar ──────────────────────────────────────────────
		{
			Name:        "hangar_score",
			Description: "Get repository health score from hangar service",
			Tier:        TierSteer,
			Category:    "governance",
			Tags:        []string{"hangar", "health"},
		},

		// ── Repo Butler ─────────────────────────────────────────
		{
			Name:        "repo_health",
			Description: "Get repository health metrics from repo-butler",
			Tier:        TierSteer,
			Category:    "governance",
			Tags:        []string{"repo", "health"},
		},
	}
}
