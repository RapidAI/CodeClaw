//go:build cgo_embedding
// +build cgo_embedding

package embedding

// NewDefaultEmbedder attempts to create a GemmaEmbedder from modelPath.
// If initialization fails (model not found, CGO issue, etc.), it silently
// falls back to NoopEmbedder.
func NewDefaultEmbedder(modelPath string) Embedder {
	emb, err := NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		return NoopEmbedder{}
	}
	return emb
}
