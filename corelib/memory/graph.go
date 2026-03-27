package memory

import (
	"sort"
	"sync"
)

// maxRelatedPerEntry is the maximum number of related entries per node.
const maxRelatedPerEntry = 5

// memoryGraph maintains bidirectional weighted edges between memory entries.
type memoryGraph struct {
	mu    sync.RWMutex
	edges map[string]map[string]float64 // id → {relatedID → strength}
}

func newMemoryGraph() *memoryGraph {
	return &memoryGraph{edges: make(map[string]map[string]float64)}
}

// link creates a bidirectional edge between two entries.
func (g *memoryGraph) link(id1, id2 string, strength float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.linkOneSided(id1, id2, strength)
	g.linkOneSided(id2, id1, strength)
}

func (g *memoryGraph) linkOneSided(from, to string, strength float64) {
	if g.edges[from] == nil {
		g.edges[from] = make(map[string]float64)
	}
	// Enforce max related limit — only add if under limit or stronger than weakest.
	if len(g.edges[from]) >= maxRelatedPerEntry {
		weakestID := ""
		weakestStr := strength
		for id, s := range g.edges[from] {
			if s < weakestStr {
				weakestStr = s
				weakestID = id
			}
		}
		if weakestID == "" {
			return // new edge is weaker than all existing
		}
		delete(g.edges[from], weakestID)
	}
	g.edges[from][to] = strength
}

// remove deletes all edges involving the given entry.
func (g *memoryGraph) remove(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Remove outgoing edges.
	if neighbors, ok := g.edges[id]; ok {
		for neighbor := range neighbors {
			delete(g.edges[neighbor], id)
		}
		delete(g.edges, id)
	}
}

// expand performs a BFS expansion from the given seed IDs up to `hops` levels.
// Returns all discovered IDs (excluding seeds) with their accumulated edge weight.
func (g *memoryGraph) expand(seedIDs []string, hops int) map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := make(map[string]bool, len(seedIDs))
	for _, id := range seedIDs {
		visited[id] = true
	}

	result := make(map[string]float64)
	frontier := seedIDs

	for hop := 0; hop < hops && len(frontier) > 0; hop++ {
		var next []string
		for _, id := range frontier {
			for neighbor, strength := range g.edges[id] {
				if visited[neighbor] {
					continue
				}
				visited[neighbor] = true
				// Decay factor per hop.
				decayed := strength * 0.5
				if existing, ok := result[neighbor]; ok {
					if decayed > existing {
						result[neighbor] = decayed
					}
				} else {
					result[neighbor] = decayed
				}
				next = append(next, neighbor)
			}
		}
		frontier = next
	}
	return result
}

// neighborsOf returns the direct neighbors and their edge weights.
func (g *memoryGraph) neighborsOf(id string) map[string]float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]float64)
	for k, v := range g.edges[id] {
		out[k] = v
	}
	return out
}

// rebuild reconstructs the graph from Entry.RelatedIDs fields.
func (g *memoryGraph) rebuild(entries []Entry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges = make(map[string]map[string]float64, len(entries))
	idSet := make(map[string]bool, len(entries))
	for _, e := range entries {
		idSet[e.ID] = true
	}
	for _, e := range entries {
		for _, relID := range e.RelatedIDs {
			if !idSet[relID] {
				continue
			}
			if g.edges[e.ID] == nil {
				g.edges[e.ID] = make(map[string]float64)
			}
			g.edges[e.ID][relID] = 1.0 // default strength from persisted data
		}
	}
}

// relatedIDsFor returns the sorted list of neighbor IDs for persistence.
func (g *memoryGraph) relatedIDsFor(id string) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	neighbors := g.edges[id]
	if len(neighbors) == 0 {
		return nil
	}
	ids := make([]string, 0, len(neighbors))
	for k := range neighbors {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return ids
}
