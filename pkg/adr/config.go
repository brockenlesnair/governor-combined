package adr

// Config holds configuration for the ADR pipeline.
// TODO: Unify with governor-combined config in a future task.
type Config struct {
	GitHub struct {
		WebhookSecret string
	}
	DiffFilter struct {
		ArchitecturalPaths []string
		MinMeaningfulLines int
	}
	AdrRoot  string
	RepoRoot string
	Preview struct {
		CommentTag string
	}
}
