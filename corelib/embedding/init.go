//go:build !cgo_embedding
// +build !cgo_embedding

package embedding

// NewDefaultEmbedder returns a NoopEmbedder when the cgo_embedding build tag
// is not active. The modelPath parameter is ignored.
func NewDefaultEmbedder(modelPath string) Embedder {
	return NoopEmbedder{}
}
