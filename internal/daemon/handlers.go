package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joyy/chrome-pilot/internal/snapshot"
)

// RegisterHandlers installs all RPC handlers on the daemon's RPC server.
func (d *Daemon) RegisterHandlers() {
	// Built-in
	d.rpcServer.Register("ping", func(_ json.RawMessage) (interface{}, error) {
		return "pong", nil
	})
	d.rpcServer.Register("status", d.handleStatus)

	// Tab commands
	d.rpcServer.Register("tab.list", d.forwardToExtension("tab.list"))
	d.rpcServer.Register("tab.new", d.forwardToExtension("tab.new"))
	d.rpcServer.Register("tab.select", d.forwardToExtension("tab.select"))
	d.rpcServer.Register("tab.close", d.forwardToExtension("tab.close"))

	// Page commands
	d.rpcServer.Register("page.navigate", d.forwardToExtension("page.navigate"))
	d.rpcServer.Register("page.back", d.forwardToExtension("page.back"))
	d.rpcServer.Register("page.screenshot", d.handleScreenshot)
	d.rpcServer.Register("page.wait", d.forwardToExtension("page.wait"))
	d.rpcServer.Register("page.console", d.forwardToExtension("page.console"))
	d.rpcServer.Register("page.network", d.forwardToExtension("page.network"))
	d.rpcServer.Register("page.resize", d.forwardToExtension("page.resize"))
	d.rpcServer.Register("page.dialog", d.forwardToExtension("page.dialog"))
	d.rpcServer.Register("page.close", d.forwardToExtension("page.close"))
	d.rpcServer.Register("page.content", d.forwardToExtension("page.content"))

	// DOM commands
	for _, m := range []string{
		"dom.click", "dom.type", "dom.hover", "dom.drag",
		"dom.key", "dom.select", "dom.fill", "dom.upload", "dom.eval",
	} {
		d.rpcServer.Register(m, d.forwardToExtension(m))
	}

	// Snapshot commands
	d.rpcServer.Register("snapshot", d.handleSnapshot)
	d.rpcServer.Register("snapshot.query", d.handleSnapshotQuery)
	d.rpcServer.Register("snapshot.info", d.handleSnapshotInfo)
	d.rpcServer.Register("snapshot.clear", d.handleSnapshotClear)

	// Cookie commands
	d.rpcServer.Register("cookie.list", d.forwardToExtension("cookie.list"))
	d.rpcServer.Register("cookie.get", d.forwardToExtension("cookie.get"))
}

// forwardToExtension returns a handler that forwards the RPC call to the
// Chrome extension over the WebSocket connection and returns its result.
func (d *Daemon) forwardToExtension(method string) func(json.RawMessage) (interface{}, error) {
	return func(params json.RawMessage) (interface{}, error) {
		if !d.wsServer.IsConnected() {
			return nil, fmt.Errorf("extension not connected, check Chrome extension status")
		}
		result, err := d.wsServer.SendAndWait(method, json.RawMessage(params), 10*time.Second)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(result), nil
	}
}

// handleStatus returns the daemon's current status.
func (d *Daemon) handleStatus(_ json.RawMessage) (interface{}, error) {
	ext := "not connected"
	if d.wsServer.IsConnected() {
		ext = "connected"
	}
	return map[string]interface{}{
		"daemon":    "running",
		"pid":       os.Getpid(),
		"extension": ext,
		"ws_port":   d.cfg.WSPort,
	}, nil
}

// handleScreenshot forwards the screenshot request to the extension and
// returns the result. When tmpManager is available it can save to a file;
// for now the raw dataUrl is returned directly.
func (d *Daemon) handleScreenshot(params json.RawMessage) (interface{}, error) {
	if !d.wsServer.IsConnected() {
		return nil, fmt.Errorf("extension not connected, check Chrome extension status")
	}
	result, err := d.wsServer.SendAndWait("page.screenshot", json.RawMessage(params), 15*time.Second)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

// handleSnapshot requests the accessibility tree from the extension, stores
// it in the snapshot store, and returns a summary.
func (d *Daemon) handleSnapshot(params json.RawMessage) (interface{}, error) {
	if !d.wsServer.IsConnected() {
		return nil, fmt.Errorf("extension not connected, check Chrome extension status")
	}

	result, err := d.wsServer.SendAndWait("snapshot", json.RawMessage(params), 15*time.Second)
	if err != nil {
		return nil, err
	}

	// Parse the tree result from the extension.
	var payload struct {
		TabID int            `json:"tabId"`
		URL   string         `json:"url"`
		Title string         `json:"title"`
		Tree  *snapshot.Node `json:"tree"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("snapshot: parse extension response: %w", err)
	}

	tabID := payload.TabID
	if tabID == 0 {
		tabID = d.session.WorkingTabID
	}

	snapID := d.snapStore.Save(tabID, payload.Tree, payload.URL, payload.Title)

	summary := d.snapStore.Summary(tabID)
	if summary == nil {
		return map[string]interface{}{"snapshotId": snapID}, nil
	}
	return summary, nil
}

// handleSnapshotQuery queries the snapshot store with the provided parameters.
func (d *Daemon) handleSnapshotQuery(params json.RawMessage) (interface{}, error) {
	var p struct {
		TabID       int    `json:"tabId"`
		Ref         string `json:"ref"`
		Depth       int    `json:"depth"`
		Role        string `json:"role"`
		Search      string `json:"search"`
		Interactable bool  `json:"interactable"`
	}
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("snapshot.query: invalid params: %w", err)
		}
	}

	tabID := p.TabID
	if tabID == 0 {
		tabID = d.session.WorkingTabID
	}

	switch {
	case p.Ref != "":
		if p.Depth > 0 {
			node := d.snapStore.Subtree(tabID, p.Ref, p.Depth)
			if node == nil {
				return nil, fmt.Errorf("snapshot.query: ref %q not found", p.Ref)
			}
			return node, nil
		}
		node := d.snapStore.QueryRef(tabID, p.Ref)
		if node == nil {
			return nil, fmt.Errorf("snapshot.query: ref %q not found", p.Ref)
		}
		return node, nil

	case p.Role != "":
		nodes := d.snapStore.QueryRole(tabID, p.Role)
		return nodes, nil

	case p.Search != "":
		nodes := d.snapStore.Search(tabID, p.Search)
		return nodes, nil

	case p.Interactable:
		nodes := d.snapStore.QueryInteractable(tabID)
		return nodes, nil

	default:
		// Return summary when no specific query is provided.
		summary := d.snapStore.Summary(tabID)
		if summary == nil {
			return nil, fmt.Errorf("snapshot.query: no snapshot for tab %d", tabID)
		}
		return summary, nil
	}
}

// handleSnapshotInfo returns snapshot info for the given tab (or all tabs).
func (d *Daemon) handleSnapshotInfo(params json.RawMessage) (interface{}, error) {
	var p struct {
		TabID int `json:"tabId"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}

	tabID := p.TabID
	if tabID == 0 {
		tabID = d.session.WorkingTabID
	}

	summary := d.snapStore.Summary(tabID)
	if summary == nil {
		return map[string]interface{}{
			"tabId":      tabID,
			"hasSnapshot": false,
		}, nil
	}
	return map[string]interface{}{
		"tabId":       tabID,
		"hasSnapshot": true,
		"snapshotId":  summary.SnapshotID,
		"url":         summary.URL,
		"title":       summary.Title,
		"totalNodes":  summary.Stats.TotalNodes,
	}, nil
}

// handleSnapshotClear clears the snapshot for the given tab or all tabs.
func (d *Daemon) handleSnapshotClear(params json.RawMessage) (interface{}, error) {
	var p struct {
		TabID  int    `json:"tabId"`
		TabIDStr string `json:"tab_id"`
		All    bool   `json:"all"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &p)
	}

	if p.All {
		d.snapStore.ClearAll()
		return map[string]interface{}{"cleared": "all"}, nil
	}

	tabID := p.TabID
	if tabID == 0 && p.TabIDStr != "" {
		tabID, _ = strconv.Atoi(p.TabIDStr)
	}
	if tabID == 0 {
		tabID = d.session.WorkingTabID
	}

	d.snapStore.Clear(tabID)
	return map[string]interface{}{"cleared": tabID}, nil
}
