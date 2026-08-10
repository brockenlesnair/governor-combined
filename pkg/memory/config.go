package memory

import (
	"time"
)

// Config holds memory system configuration.
type Config struct {
	// DBPath is the SQLite database file path.
	DBPath string `json:"db_path" yaml:"db_path"`

	// EmbeddingModelPath is the path to the ONNX model file.
	EmbeddingModelPath string `json:"embedding_model_path" yaml:"embedding_model_path"`

	// EmbeddingDim is the embedding dimension (384 for bge-small-en-v1.5).
	EmbeddingDim int `json:"embedding_dim" yaml:"embedding_dim"`

	// MaxEntities is the maximum number of entities before pruning.
	MaxEntities int `json:"max_entities" yaml:"max_entities"`

	// DecayInterval is how often to run the decay pass.
	DecayInterval time.Duration `json:"decay_interval" yaml:"decay_interval"`

	// PruneThreshold is the decay score below which entities are pruned.
	PruneThreshold float64 `json:"prune_threshold" yaml:"prune_threshold"`

	// FSRS parameters
	FSRS FSRSConfig `json:"fsrs" yaml:"fsrs"`

	// Search weights
	VectorWeight float64 `json:"vector_weight" yaml:"vector_weight"`
	FTSWeight    float64 `json:"fts_weight" yaml:"fts_weight"`
	GraphWeight  float64 `json:"graph_weight" yaml:"graph_weight"`

	// Enable debug logging
	Debug bool `json:"debug" yaml:"debug"`
}

// FSRSConfig holds FSRS v6 parameters.
type FSRSConfig struct {
	// Desired retention rate (default 0.9)
	DesiredRetention float64 `json:"desired_retention" yaml:"desired_retention"`

	// Learning rate parameters
	W []float64 `json:"w" yaml:"w"`

	// Minimum interval (days)
	MinInterval int `json:"min_interval" yaml:"min_interval"`

	// Maximum interval (days)
	MaxInterval int `json:"max_interval" yaml:"max_interval"`
}

// DefaultConfig returns a default configuration.
func DefaultConfig() *Config {
	return &Config{
		DBPath:            "./data/memory.db",
		EmbeddingModelPath: "./models/bge-small-en-v1.5.onnx",
		EmbeddingDim:      384,
		MaxEntities:       100000,
		DecayInterval:     time.Hour,
		PruneThreshold:    0.1,
		FSRS: FSRSConfig{
			DesiredRetention: 0.9,
			W: []float64{
				0.4, 0.6, 2.4, 5.8, 4.9, 0.9, 0.6, 0.6, 0.4, 0.4,
				0.4, 0.6, 0.4, 0.4, 0.6, 0.6, 0.6, 0.6, 0.4,
			},
			MinInterval: 1,
			MaxInterval: 36500,
		},
		VectorWeight: 0.5,
		FTSWeight:    0.3,
		GraphWeight:  0.2,
		Debug:        false,
	}
}