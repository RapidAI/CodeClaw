package memory

import (
	"math"
	"sync"
)

// vectorIndex stores embedding vectors keyed by entry ID and computes
// cosine similarity scores against a query vector.
// Vectors are stored L2-normalized, so cosine similarity = dot product.
type vectorIndex struct {
	mu         sync.RWMutex
	embeddings map[string][]float32
	dim        int
}

func newVectorIndex() *vectorIndex {
	return &vectorIndex{embeddings: make(map[string][]float32)}
}

func (v *vectorIndex) add(id string, emb []float32) {
	if len(emb) == 0 {
		return
	}
	v.mu.Lock()
	v.embeddings[id] = emb
	if v.dim == 0 {
		v.dim = len(emb)
	}
	v.mu.Unlock()
}

func (v *vectorIndex) remove(id string) {
	v.mu.Lock()
	delete(v.embeddings, id)
	v.mu.Unlock()
}

func (v *vectorIndex) update(id string, emb []float32) {
	if len(emb) == 0 {
		return
	}
	v.mu.Lock()
	v.embeddings[id] = emb
	v.mu.Unlock()
}

func (v *vectorIndex) rebuild(entries []Entry) {
	v.mu.Lock()
	v.embeddings = make(map[string][]float32, len(entries))
	for _, e := range entries {
		if len(e.Embedding) > 0 {
			v.embeddings[e.ID] = e.Embedding
			if v.dim == 0 {
				v.dim = len(e.Embedding)
			}
		}
	}
	v.mu.Unlock()
}

// score computes cosine similarity between queryEmb and all stored embeddings.
// If embeddings are L2-normalized (as produced by GemmaEmbedder), this reduces
// to a dot product. Returns a map of entry ID → similarity score.
func (v *vectorIndex) score(queryEmb []float32) map[string]float64 {
	if len(queryEmb) == 0 {
		return nil
	}
	v.mu.RLock()
	defer v.mu.RUnlock()

	if len(v.embeddings) == 0 {
		return nil
	}

	scores := make(map[string]float64, len(v.embeddings))
	for id, emb := range v.embeddings {
		sim := dotProduct(queryEmb, emb)
		if sim > 0 {
			scores[id] = sim
		}
	}
	return scores
}

// dotProduct computes the dot product of two vectors over their overlapping dimensions.
func dotProduct(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// cosineSim computes cosine similarity given a pre-computed query norm.
// Used when vectors are NOT pre-normalized.
func cosineSim(a, b []float32, aNorm float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot float64
	var bSq float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		bSq += float64(b[i]) * float64(b[i])
	}
	bNorm := math.Sqrt(bSq)
	if bNorm == 0 {
		return 0
	}
	return dot / (aNorm * bNorm)
}

func vecNorm(v []float32) float64 {
	var sq float64
	for _, x := range v {
		sq += float64(x) * float64(x)
	}
	return math.Sqrt(sq)
}
