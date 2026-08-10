package memory

import (
	"context"
	"math"
	"sync"
)

// EmbeddingEngine wraps ONNX Runtime for embedding generation.
// This is a simplified implementation that provides a fallback when ONNX is not available.
type EmbeddingEngine struct {
	config      *Config
	initialized bool
	mu          sync.Mutex
}

// NewEmbeddingEngine creates a new embedding engine.
func NewEmbeddingEngine(config *Config) *EmbeddingEngine {
	return &EmbeddingEngine{
		config: config,
	}
}

// initializeUnlocked initializes the engine without locking (caller must hold lock).
func (e *EmbeddingEngine) initializeUnlocked() error {
	if e.initialized {
		return nil
	}

	// For now, use fallback embeddings
	// Real implementation would initialize ONNX Runtime here
	e.initialized = true
	return nil
}

// generateFallbackEmbedding generates a deterministic embedding based on text content.
// This is a simple hash-based embedding for testing when ONNX is not available.
func (e *EmbeddingEngine) generateFallbackEmbedding(text string) []float32 {
	embedding := make([]float32, e.config.EmbeddingDim)

	// Simple hash-based embedding
	hash := uint64(0)
	for _, c := range text {
		hash = hash*31 + uint64(c)
	}

	// Use hash to generate pseudo-random but deterministic values
	for i := 0; i < e.config.EmbeddingDim; i++ {
		hash = hash*1664525 + 1013904223 // LCG
		embedding[i] = float32((hash>>16)&0x7FFF) / 32767.0*2 - 1 // [-1, 1]
	}

	// Normalize to unit length
	return normalizeEmbedding(embedding)
}

// normalizeEmbedding normalizes embedding to unit length.
func normalizeEmbedding(embedding []float32) []float32 {
	var norm float32
	for _, v := range embedding {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm == 0 {
		return embedding
	}

	normalized := make([]float32, len(embedding))
	for i, v := range embedding {
		normalized[i] = v / norm
	}
	return normalized
}

// quantizeEmbedding quantizes float32 embedding to int8 range.
func quantizeEmbedding(embedding []float32) []float32 {
	quantized := make([]float32, len(embedding))
	for i, v := range embedding {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		quantized[i] = float32(int8(v * 127))
	}
	return quantized
}

// GenerateEmbedding generates an embedding for the given text.
func (e *EmbeddingEngine) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Fast path: check if already initialized without locking
	if e.initialized {
		return e.generateFallbackEmbedding(text), nil
	}

	e.mu.Lock()
	if !e.initialized {
		if err := e.initializeUnlocked(); err != nil {
			e.mu.Unlock()
			return nil, err
		}
	}
	e.mu.Unlock()

	return e.generateFallbackEmbedding(text), nil
}

// dequantizeEmbedding dequantizes int8 embedding to float32.
func dequantizeEmbedding(quantized []float32) []float32 {
	dequantized := make([]float32, len(quantized))
	for i, v := range quantized {
		dequantized[i] = v / 127.0
	}
	return dequantized
}

// Close releases resources.
func (e *EmbeddingEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.initialized = false
	return nil
}