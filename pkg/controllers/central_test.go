package controllers

import (
	"context"
	"encoding/json"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCentralProofIdempotenceRestartAndOwnership(t *testing.T) {
	cfg := Config{Zones: []string{"west", "east"}, NodeTTLSeconds: 5, LeaseSeconds: 30, Fanout: 2, Nodes: []NodeSpec{{ID: "pi", Zone: "west", Quota: 100, Endpoint: "http://192.0.2.2:18081", AllowedZones: []string{"west", "east"}, Mode: "real-http"}}}
	path := filepath.Join(t.TempDir(), "central.db")
	c, err := OpenCentral(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	call := func(sender, op string, v any) error {
		raw, _ := json.Marshal(v)
		_, err := c.Handle(context.Background(), sender, mqttbus.Request{Op: op, Body: raw})
		return err
	}
	now := time.Now().Add(-time.Second)
	n := placement.Node{ID: "pi", Zone: "west", State: placement.Available, Available: 100, Generation: 1, Version: 1, Updated: now, Heartbeat: now}
	if err = call("edge-impostor", "REGISTER_NODE", Registered{n, cfg.Nodes[0].Endpoint}); err == nil {
		t.Fatal("spoofed registration accepted")
	}
	if err = call("edge-pi", "REGISTER_NODE", Registered{n, cfg.Nodes[0].Endpoint}); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	u := Upload{Manifest: placement.Manifest{RequestID: "request", VideoID: "video", SourceNodeID: "pi", Total: 5, Segments: []placement.Segment{{ID: 0, Size: 5}}}, Target: 5, Contents: []Content{{ExtentID: 0, SHA256: hash}}}
	plan := placement.DistributedPlan{RequestID: "request", VideoID: "video", State: "RESERVED", Reservations: map[string]string{"west": "reservation"}, Assignments: []placement.Assignment{{Extent: placement.Extent{ID: 0, First: 0, Last: 0, Size: 5}, NodeID: "pi", ZoneID: "west", ReservationID: "reservation"}}}
	if err = call("zone-west", "PLAN", PlanCommit{u, plan, time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err = call("zone-west", "PREPARED", Prepare{VideoID: "video", ReservationID: "reservation"}); err != nil {
		t.Fatal(err)
	}
	view, err := c.inspect()
	if err != nil || view.Uploads["request"].State == "READY" {
		t.Fatal("prepare falsely completed", err)
	}
	proof := Receipt{VideoID: "video", ExtentID: 0, NodeID: "pi", ReservationID: "reservation", Size: 5, SHA256: hash, Proof: "MOCK"}
	if err = call("edge-pi", "STORE_COMPLETE", proof); err == nil {
		t.Fatal("MOCK proof accepted for real node")
	}
	proof.Proof = "HTTP_SHA256_VERIFIED"
	proof.SHA256 = strings.Repeat("b", 64)
	if err = call("edge-pi", "STORE_COMPLETE", proof); err == nil {
		t.Fatal("wrong content accepted")
	}
	proof.SHA256 = hash
	if err = call("edge-pi", "STORE_COMPLETE", proof); err != nil {
		t.Fatal(err)
	}
	before, _ := c.inspect()
	if err = call("edge-pi", "STORE_COMPLETE", proof); err != nil {
		t.Fatal(err)
	}
	after, _ := c.inspect()
	if before.Catalog.Events != after.Catalog.Events || after.Uploads["request"].State != "READY" {
		t.Fatal("duplicate mutation or missing READY")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenCentral(cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	after, err = c.inspect()
	if err != nil || after.Uploads["request"].State != "READY" || after.Catalog.Events != before.Catalog.Events {
		t.Fatal("restart lost state", err)
	}
	n.Zone = "east"
	n.Generation = 2
	n.Version = 2
	if err = call("edge-pi", "REGISTER_NODE", Registered{n, cfg.Nodes[0].Endpoint}); err != nil {
		t.Fatal(err)
	}
	after, _ = c.inspect()
	if after.Uploads["request"].Units[0].ZoneID != "east" {
		t.Fatal("location join did not follow mobility")
	}
}

func TestHTTPRequiresBearerAndRejectsTrailingJSON(t *testing.T) {
	token := strings.Repeat("x", 32)
	h := Bearer(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, header := range []string{"", token, "Bearer wrong"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("unauthorized header accepted")
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{} {}`))
	if err := ReadBody(httptest.NewRecorder(), r, new(map[string]any)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
