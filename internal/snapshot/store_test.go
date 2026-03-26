package snapshot

import (
	"strconv"
	"testing"
	"time"
)

// sampleTree builds a small accessibility tree used in multiple tests.
//
//	root (region)
//	  nav (navigation, ref="nav1")
//	    link "Home" (ref="link1")
//	    link "About" (ref="link2")
//	  main (main, ref="main1")
//	    heading "Welcome"
//	    button "Click Me" (ref="btn1")
//	    form (form, ref="form1")
//	      textbox "Search" (ref="input1")
//	  aside (complementary, ref="aside1")
func sampleTree() *Node {
	return &Node{
		Role: "region",
		Name: "Page",
		Children: []*Node{
			{
				Role: "navigation",
				Name: "Main Nav",
				Ref:  "nav1",
				Children: []*Node{
					{Role: "link", Name: "Home", Ref: "link1"},
					{Role: "link", Name: "About", Ref: "link2"},
				},
			},
			{
				Role: "main",
				Name: "Content",
				Ref:  "main1",
				Children: []*Node{
					{Role: "heading", Name: "Welcome"},
					{Role: "button", Name: "Click Me", Ref: "btn1"},
					{
						Role: "form",
						Name: "Search Form",
						Ref:  "form1",
						Children: []*Node{
							{Role: "textbox", Name: "Search", Ref: "input1"},
						},
					},
				},
			},
			{
				Role: "complementary",
				Name: "Sidebar",
				Ref:  "aside1",
			},
		},
	}
}

// noLandmarkTree builds a tree that has no landmark roles.
func noLandmarkTree() *Node {
	return &Node{
		Role: "region",
		Name: "Shell",
		Children: []*Node{
			{Role: "button", Name: "Save", Ref: "btn-save"},
			{Role: "button", Name: "Cancel", Ref: "btn-cancel"},
			{Role: "link", Name: "Help", Ref: "link-help"},
			{Role: "textbox", Name: "Username", Ref: "inp-user"},
			{Role: "textbox", Name: "Password", Ref: "inp-pass"},
			{Role: "checkbox", Name: "Remember Me", Ref: "chk-rem"},
		},
	}
}

// ---------------------------------------------------------------------------
// TestStoreSaveAndQuery
// ---------------------------------------------------------------------------

func TestStoreSaveAndQuery(t *testing.T) {
	store := NewStore()

	snapID := store.Save(1, sampleTree(), "https://example.com", "Example")
	if snapID == "" {
		t.Fatal("expected non-empty snap ID")
	}

	// QueryRef – existing ref.
	node := store.QueryRef(1, "btn1")
	if node == nil {
		t.Fatal("expected node for ref btn1")
	}
	if node.Role != "button" {
		t.Errorf("expected role button, got %s", node.Role)
	}
	if node.Name != "Click Me" {
		t.Errorf("expected name 'Click Me', got %s", node.Name)
	}

	// QueryRef – missing ref.
	if store.QueryRef(1, "nonexistent") != nil {
		t.Error("expected nil for unknown ref")
	}

	// QueryRole.
	links := store.QueryRole(1, "link")
	if len(links) != 2 {
		t.Errorf("expected 2 links, got %d", len(links))
	}

	// QueryInteractable – all nodes with Ref set.
	interactable := store.QueryInteractable(1)
	// nav1, link1, link2, main1, btn1, form1, input1, aside1 = 8
	if len(interactable) != 8 {
		t.Errorf("expected 8 interactable nodes, got %d", len(interactable))
	}

	// Search – case-insensitive.
	results := store.Search(1, "click")
	if len(results) != 1 {
		t.Errorf("expected 1 search result for 'click', got %d", len(results))
	}
	if results[0].Name != "Click Me" {
		t.Errorf("unexpected search result: %s", results[0].Name)
	}

	// Search with different casing.
	results = store.Search(1, "SEARCH")
	if len(results) != 2 { // "Search Form" and "Search" textbox
		t.Errorf("expected 2 search results for 'SEARCH', got %d", len(results))
	}

	// Subtree with depth 1 (no children).
	sub := store.Subtree(1, "nav1", 1)
	if sub == nil {
		t.Fatal("expected subtree for nav1")
	}
	if sub.Role != "navigation" {
		t.Errorf("expected navigation role, got %s", sub.Role)
	}
	if len(sub.Children) != 0 {
		t.Errorf("expected no children at depth 1, got %d", len(sub.Children))
	}

	// Subtree with depth 2 (one level of children).
	sub = store.Subtree(1, "nav1", 2)
	if len(sub.Children) != 2 {
		t.Errorf("expected 2 children at depth 2, got %d", len(sub.Children))
	}
	if len(sub.Children[0].Children) != 0 {
		t.Error("expected grandchildren pruned at depth 2")
	}

	// Subtree unlimited depth (depth=0).
	sub = store.Subtree(1, "main1", 0)
	if sub == nil {
		t.Fatal("expected unlimited subtree")
	}
	// main1 -> heading, button, form -> textbox
	if len(sub.Children) != 3 {
		t.Errorf("expected 3 children, got %d", len(sub.Children))
	}
	formNode := sub.Children[2]
	if len(formNode.Children) != 1 {
		t.Errorf("expected 1 grandchild under form, got %d", len(formNode.Children))
	}

	// Clear single tab.
	store.Clear(1)
	if store.QueryRef(1, "btn1") != nil {
		t.Error("expected nil after Clear")
	}

	// QueryRef on unknown tab.
	if store.QueryRef(99, "x") != nil {
		t.Error("expected nil for unknown tab")
	}
}

// ---------------------------------------------------------------------------
// TestStoreSummary
// ---------------------------------------------------------------------------

func TestStoreSummary(t *testing.T) {
	store := NewStore()
	store.Save(2, sampleTree(), "https://example.com/page", "My Page")

	result := store.Summary(2)
	if result == nil {
		t.Fatal("expected non-nil summary")
	}

	if result.URL != "https://example.com/page" {
		t.Errorf("unexpected URL: %s", result.URL)
	}
	if result.Title != "My Page" {
		t.Errorf("unexpected title: %s", result.Title)
	}

	// Landmarks: navigation, main, form, complementary = 4
	if len(result.Landmarks) != 4 {
		t.Errorf("expected 4 landmarks, got %d", len(result.Landmarks))
	}

	// Verify landmark roles.
	roles := make(map[string]bool)
	for _, lm := range result.Landmarks {
		roles[lm.Role] = true
	}
	for _, want := range []string{"navigation", "main", "form", "complementary"} {
		if !roles[want] {
			t.Errorf("expected landmark role %q in summary", want)
		}
	}

	// Headings.
	if len(result.Headings) != 1 {
		t.Errorf("expected 1 heading, got %d", len(result.Headings))
	}
	if result.Headings[0] != "Welcome" {
		t.Errorf("unexpected heading: %s", result.Headings[0])
	}

	// Stats.
	if result.Stats.TotalNodes == 0 {
		t.Error("expected non-zero TotalNodes")
	}
	if result.Stats.InteractableCount != 8 {
		t.Errorf("expected 8 interactable nodes in stats, got %d", result.Stats.InteractableCount)
	}

	// No fallback interactable list when landmarks exist.
	if len(result.Interactable) != 0 {
		t.Errorf("expected empty Interactable when landmarks present, got %d", len(result.Interactable))
	}

	// SnapshotID must be non-empty.
	if result.SnapshotID == "" {
		t.Error("expected non-empty SnapshotID in summary")
	}

	// Summary on unknown tab returns nil.
	if store.Summary(999) != nil {
		t.Error("expected nil summary for unknown tab")
	}
}

// ---------------------------------------------------------------------------
// TestStoreSummaryFallback
// ---------------------------------------------------------------------------

func TestStoreSummaryFallback(t *testing.T) {
	store := NewStore()
	store.Save(3, noLandmarkTree(), "https://example.com/login", "Login")

	result := store.Summary(3)
	if result == nil {
		t.Fatal("expected non-nil summary")
	}

	// No landmark roles in the tree.
	if len(result.Landmarks) != 0 {
		t.Errorf("expected 0 landmarks, got %d", len(result.Landmarks))
	}

	// Fallback: interactable elements should be populated (max 20).
	if len(result.Interactable) == 0 {
		t.Error("expected fallback interactable list when no landmarks")
	}
	// All 6 interactable nodes are under the limit.
	if len(result.Interactable) != 6 {
		t.Errorf("expected 6 fallback interactable nodes, got %d", len(result.Interactable))
	}
	for _, n := range result.Interactable {
		if n.Ref == "" {
			t.Error("fallback interactable node has empty ref")
		}
	}
}

// ---------------------------------------------------------------------------
// TestStoreFallbackCap
// ---------------------------------------------------------------------------

func TestStoreFallbackCap(t *testing.T) {
	// Build a tree with 25 interactable nodes and no landmarks.
	root := &Node{Role: "region", Name: "Many Buttons"}
	for i := 0; i < 25; i++ {
		root.Children = append(root.Children, &Node{
			Role: "button",
			Name: "Btn",
			Ref:  "btn-" + strconv.Itoa(i),
		})
	}

	store := NewStore()
	store.Save(4, root, "https://example.com", "Many")

	result := store.Summary(4)
	if len(result.Interactable) != 20 {
		t.Errorf("expected fallback capped at 20, got %d", len(result.Interactable))
	}
}

// ---------------------------------------------------------------------------
// TestStoreExpiry
// ---------------------------------------------------------------------------

func TestStoreExpiry(t *testing.T) {
	store := NewStore()
	store.Save(10, sampleTree(), "https://a.com", "A")
	store.Save(11, sampleTree(), "https://b.com", "B")

	// Force tab 10's LastUsed to be in the past.
	store.mu.Lock()
	store.tabs[10].LastUsed = time.Now().Add(-2 * time.Hour)
	store.mu.Unlock()

	store.ExpireOlderThan(1 * time.Hour)

	if store.QueryRef(10, "btn1") != nil {
		t.Error("expected tab 10 to be expired")
	}
	if store.QueryRef(11, "btn1") == nil {
		t.Error("expected tab 11 to still be present")
	}
}

// ---------------------------------------------------------------------------
// TestStoreClearAll
// ---------------------------------------------------------------------------

func TestStoreClearAll(t *testing.T) {
	store := NewStore()
	store.Save(20, sampleTree(), "https://a.com", "A")
	store.Save(21, sampleTree(), "https://b.com", "B")

	store.ClearAll()

	if store.QueryRef(20, "btn1") != nil {
		t.Error("expected tab 20 cleared")
	}
	if store.QueryRef(21, "btn1") != nil {
		t.Error("expected tab 21 cleared")
	}
}

// ---------------------------------------------------------------------------
// TestStoreSnapIDIncrement
// ---------------------------------------------------------------------------

func TestStoreSnapIDIncrement(t *testing.T) {
	store := NewStore()
	id1 := store.Save(30, sampleTree(), "https://a.com", "A")
	id2 := store.Save(31, sampleTree(), "https://b.com", "B")

	if id1 == id2 {
		t.Errorf("expected distinct snap IDs, both were %s", id1)
	}
	if id1 == "" || id2 == "" {
		t.Error("snap IDs must not be empty")
	}
}

// ---------------------------------------------------------------------------
// TestStorePrevious
// ---------------------------------------------------------------------------

func TestStorePrevious(t *testing.T) {
	store := NewStore()
	tree1 := &Node{Role: "region", Name: "First"}
	tree2 := &Node{Role: "region", Name: "Second"}

	store.Save(40, tree1, "https://a.com", "A")
	store.Save(40, tree2, "https://a.com", "A")

	store.mu.Lock()
	snap := store.tabs[40]
	store.mu.Unlock()

	if snap.Tree.Name != "Second" {
		t.Errorf("expected current tree Second, got %s", snap.Tree.Name)
	}
	if snap.Previous == nil || snap.Previous.Name != "First" {
		t.Error("expected previous tree to be First")
	}
}
