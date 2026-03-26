package snapshot

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// Node represents a node in the accessibility tree.
type Node struct {
	Role     string  `json:"role,omitempty"`
	Name     string  `json:"name,omitempty"`
	Ref      string  `json:"ref,omitempty"`
	Children []*Node `json:"children,omitempty"`
}

// TabSnapshot holds the accessibility snapshot for a single tab.
type TabSnapshot struct {
	ID       string
	URL      string
	Title    string
	Tree     *Node
	Previous *Node
	Index    map[string]*Node // ref -> node
	LastUsed time.Time
}

// Store is the in-memory snapshot store, keyed by tab ID.
type Store struct {
	mu   sync.Mutex
	tabs map[int]*TabSnapshot
	seq  int
}

// Stats holds aggregate counts about the snapshot tree.
type Stats struct {
	TotalNodes      int `json:"totalNodes"`
	InteractableCount int `json:"interactableCount"`
}

// LandmarkInfo represents a landmark element in the accessibility tree.
type LandmarkInfo struct {
	Role string `json:"role"`
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

// SummaryResult is returned by Summary().
type SummaryResult struct {
	SnapshotID  string         `json:"snapshotId"`
	URL         string         `json:"url"`
	Title       string         `json:"title"`
	Stats       Stats          `json:"stats"`
	Landmarks   []LandmarkInfo `json:"landmarks"`
	Headings    []string       `json:"headings"`
	// Fallback: populated when no landmarks are found.
	Interactable []*Node `json:"interactable,omitempty"`
}

// landmarkRoles is the set of ARIA landmark roles.
var landmarkRoles = map[string]bool{
	"navigation":    true,
	"main":          true,
	"complementary": true,
	"banner":        true,
	"contentinfo":   true,
	"form":          true,
}

// NewStore creates and returns an empty Store.
func NewStore() *Store {
	return &Store{
		tabs: make(map[int]*TabSnapshot),
	}
}

// Save stores a new snapshot for the given tab and returns the snapshot ID.
func (s *Store) Save(tabID int, tree *Node, url, title string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	snapID := strconv.Itoa(s.seq)

	idx := make(map[string]*Node)
	buildIndex(tree, idx)

	var prev *Node
	if existing, ok := s.tabs[tabID]; ok {
		prev = existing.Tree
	}

	s.tabs[tabID] = &TabSnapshot{
		ID:       snapID,
		URL:      url,
		Title:    title,
		Tree:     tree,
		Previous: prev,
		Index:    idx,
		LastUsed: time.Now(),
	}

	return snapID
}

// Summary returns a SummaryResult for the given tab's snapshot.
// If no landmarks are present it falls back to the first 20 interactable elements.
func (s *Store) Summary(tabID int) *SummaryResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.tabs[tabID]
	if !ok {
		return nil
	}
	snap.LastUsed = time.Now()

	var landmarks []LandmarkInfo
	var headings []string
	totalNodes := 0
	interactableCount := 0

	walkNodes(snap.Tree, func(n *Node) {
		totalNodes++
		if n.Ref != "" {
			interactableCount++
		}
		if landmarkRoles[n.Role] {
			landmarks = append(landmarks, LandmarkInfo{
				Role: n.Role,
				Name: n.Name,
				Ref:  n.Ref,
			})
		}
		if n.Role == "heading" {
			headings = append(headings, n.Name)
		}
	})

	result := &SummaryResult{
		SnapshotID: snap.ID,
		URL:        snap.URL,
		Title:      snap.Title,
		Stats: Stats{
			TotalNodes:        totalNodes,
			InteractableCount: interactableCount,
		},
		Landmarks: landmarks,
		Headings:  headings,
	}

	// Fallback: no landmarks → return up to 20 interactable elements.
	if len(landmarks) == 0 {
		var interactable []*Node
		walkNodes(snap.Tree, func(n *Node) {
			if n.Ref != "" && len(interactable) < 20 {
				interactable = append(interactable, n)
			}
		})
		result.Interactable = interactable
	}

	return result
}

// QueryRef returns the node with the given ref, or nil.
func (s *Store) QueryRef(tabID int, ref string) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.tabs[tabID]
	if !ok {
		return nil
	}
	snap.LastUsed = time.Now()
	return snap.Index[ref]
}

// QueryRole returns all nodes whose Role matches the given string.
func (s *Store) QueryRole(tabID int, role string) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.tabs[tabID]
	if !ok {
		return nil
	}
	snap.LastUsed = time.Now()

	var results []*Node
	walkNodes(snap.Tree, func(n *Node) {
		if n.Role == role {
			results = append(results, n)
		}
	})
	return results
}

// QueryInteractable returns all nodes that have a non-empty Ref.
func (s *Store) QueryInteractable(tabID int) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.tabs[tabID]
	if !ok {
		return nil
	}
	snap.LastUsed = time.Now()

	var results []*Node
	walkNodes(snap.Tree, func(n *Node) {
		if n.Ref != "" {
			results = append(results, n)
		}
	})
	return results
}

// Search returns all nodes whose Name contains text (case-insensitive).
func (s *Store) Search(tabID int, text string) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.tabs[tabID]
	if !ok {
		return nil
	}
	snap.LastUsed = time.Now()

	lower := strings.ToLower(text)
	var results []*Node
	walkNodes(snap.Tree, func(n *Node) {
		if strings.Contains(strings.ToLower(n.Name), lower) {
			results = append(results, n)
		}
	})
	return results
}

// Subtree returns the subtree rooted at ref, pruned to the given depth.
// depth <= 0 means unlimited depth.
func (s *Store) Subtree(tabID int, ref string, depth int) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.tabs[tabID]
	if !ok {
		return nil
	}
	snap.LastUsed = time.Now()

	root, found := snap.Index[ref]
	if !found {
		return nil
	}
	return pruneDepth(root, depth)
}

// Clear removes the snapshot for the given tab.
func (s *Store) Clear(tabID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tabs, tabID)
}

// ClearAll removes all snapshots.
func (s *Store) ClearAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tabs = make(map[int]*TabSnapshot)
}

// ExpireOlderThan removes snapshots whose LastUsed time is older than d.
func (s *Store) ExpireOlderThan(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	threshold := time.Now().Add(-d)
	for id, snap := range s.tabs {
		if snap.LastUsed.Before(threshold) {
			delete(s.tabs, id)
		}
	}
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// buildIndex walks the tree and populates idx with ref -> node mappings.
func buildIndex(n *Node, idx map[string]*Node) {
	if n == nil {
		return
	}
	if n.Ref != "" {
		idx[n.Ref] = n
	}
	for _, child := range n.Children {
		buildIndex(child, idx)
	}
}

// walkNodes calls fn for every node in the tree (pre-order).
func walkNodes(n *Node, fn func(*Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, child := range n.Children {
		walkNodes(child, fn)
	}
}

// pruneDepth returns a shallow copy of the subtree rooted at n, capped to
// the given depth. depth <= 0 means no limit.
func pruneDepth(n *Node, depth int) *Node {
	if n == nil {
		return nil
	}
	copy := &Node{
		Role: n.Role,
		Name: n.Name,
		Ref:  n.Ref,
	}
	if depth == 1 {
		// No children at this level.
		return copy
	}
	nextDepth := depth - 1
	if depth <= 0 {
		nextDepth = 0 // 0 means unlimited
	}
	for _, child := range n.Children {
		copy.Children = append(copy.Children, pruneDepth(child, nextDepth))
	}
	return copy
}
