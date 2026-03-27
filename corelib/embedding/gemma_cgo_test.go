//go:build cgo_embedding
// +build cgo_embedding

package embedding

import (
	"math"
	"os"
	"testing"
)

// testModelPath returns the Gemma embedding model path.
// It checks GEMMA_EMB_MODEL env var first, then falls back to the default location.
func testModelPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("GEMMA_EMB_MODEL"); p != "" {
		return p
	}
	p := DefaultModelPath()
	if p == "" {
		t.Skip("cannot determine home dir")
	}
	return p
}

// requireModel skips the test if the model file is not available.
func requireModel(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	path := testModelPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("model file not found: %s (set GEMMA_EMB_MODEL or place model at default path)", path)
	}
	return path
}

func TestNewGemmaEmbedder(t *testing.T) {
	modelPath := requireModel(t)

	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("NewGemmaEmbedder failed: %v", err)
	}
	defer emb.Close()

	if emb.Dim() != 256 {
		t.Errorf("Dim() = %d, want 256", emb.Dim())
	}
}

func TestGemmaEmbedder_Embed(t *testing.T) {
	modelPath := requireModel(t)

	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("NewGemmaEmbedder failed: %v", err)
	}
	defer emb.Close()

	vec, err := emb.Embed("hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vec) != 256 {
		t.Fatalf("Embed returned %d dims, want 256", len(vec))
	}

	// Verify L2 normalization: ||vec|| should be ~1.0
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 0.01 {
		t.Errorf("L2 norm = %f, want ~1.0", norm)
	}

	// Verify not all zeros
	allZero := true
	for _, v := range vec {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("Embed returned all-zero vector")
	}
}

func TestGemmaEmbedder_EmbedBatch(t *testing.T) {
	modelPath := requireModel(t)

	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("NewGemmaEmbedder failed: %v", err)
	}
	defer emb.Close()

	texts := []string{"hello", "world", "embedding test"}
	vecs, err := emb.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("EmbedBatch returned %d vectors, want %d", len(vecs), len(texts))
	}
	for i, vec := range vecs {
		if len(vec) != 256 {
			t.Errorf("vecs[%d] has %d dims, want 256", i, len(vec))
		}
	}

	// Different texts should produce different embeddings
	if cosine(vecs[0], vecs[1]) > 0.999 {
		t.Error("different texts produced nearly identical embeddings")
	}
}

func TestGemmaEmbedder_Close(t *testing.T) {
	modelPath := requireModel(t)

	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		t.Fatalf("NewGemmaEmbedder failed: %v", err)
	}

	// Close should not panic
	emb.Close()

	// Double close should not panic
	emb.Close()
}

func TestNewGemmaEmbedder_InvalidPath(t *testing.T) {
	_, err := NewGemmaEmbedder("/nonexistent/model.gguf", 256)
	if err == nil {
		t.Fatal("expected error for invalid model path, got nil")
	}
}

// cosine computes cosine similarity between two vectors.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
