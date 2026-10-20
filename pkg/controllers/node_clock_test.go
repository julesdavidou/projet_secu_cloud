package controllers

import (
	"context"
	"encoding/json"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"path/filepath"
	"testing"
	"time"
)

func TestCrossHostClockDoesNotRefreshRetainedOrStaleNodes(t *testing.T) {
	now := time.Now()
	n := placement.Node{ID: "pc", Zone: "A", State: placement.Available, Available: 100, Generation: 1, Version: 8, Updated: now.Add(200 * time.Millisecond), Heartbeat: now.Add(200 * time.Millisecond)}
	if n.Validate(now) == nil {
		t.Fatal("scheduler validation was loosened")
	}
	accepted, e := authenticatedNodeAtReceipt(n, now, false)
	if e != nil || !accepted.Updated.Equal(now) || !accepted.Heartbeat.Equal(now) || accepted.Version != n.Version {
		t.Fatalf("small live skew rejected or ordering changed: %v", e)
	}
	if _, e = authenticatedNodeAtReceipt(n, now, true); e == nil {
		t.Fatal("retained future snapshot was refreshed")
	}
	n.Updated = now.Add(-time.Hour)
	n.Heartbeat = n.Updated
	accepted, e = authenticatedNodeAtReceipt(n, now, false)
	if e != nil || !accepted.Heartbeat.Equal(n.Heartbeat) || !accepted.Updated.Equal(n.Updated) {
		t.Fatal("stale snapshot gained freshness")
	}
	n.Updated = now.Add(2 * time.Second)
	if _, e = authenticatedNodeAtReceipt(n, now, false); e == nil {
		t.Fatal("large clock drift accepted")
	}
}

func TestCentralAdmitsAuthenticatedSlightlyFastEdgeClock(t *testing.T) {
	cfg := Config{Zones: []string{"A"}, Nodes: []NodeSpec{{ID: "pc", Zone: "A", AllowedZones: []string{"A"}, Quota: 100, Endpoint: "https://edge-pc:8443", Mode: "real-http"}}, NodeTTLSeconds: 5, LeaseSeconds: 30, Fanout: 1}
	c, e := OpenCentral(cfg, filepath.Join(t.TempDir(), "central.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	n := placement.Node{ID: "pc", Zone: "A", State: placement.Available, Available: 100, Generation: 1, Version: 1, Updated: time.Now().Add(200 * time.Millisecond), Heartbeat: time.Now().Add(200 * time.Millisecond)}
	raw, _ := json.Marshal(Registered{n, cfg.Nodes[0].Endpoint})
	if _, e = c.Handle(context.Background(), "edge-impostor", mqttbus.Request{Op: "REGISTER_NODE", Body: raw}); e == nil {
		t.Fatal("clock adaptation bypassed identity")
	}
	result, e := c.Handle(context.Background(), "edge-pc", mqttbus.Request{Op: "REGISTER_NODE", Body: raw})
	if e != nil {
		t.Fatal(e)
	}
	if result.(Registered).Node.Updated.After(time.Now()) {
		t.Fatal("future timestamp persisted")
	}
}
