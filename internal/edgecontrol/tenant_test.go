package edgecontrol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
)

func TestPeerLivenessDoesNotWaitForCentralRegistration(t *testing.T) {
	b := &Bridge{spec: controllers.NodeSpec{ID: "B1"}, cfg: controllers.Config{Nodes: []controllers.NodeSpec{{ID: "A1"}, {ID: "B1"}}}}
	b.mu.Lock() // Registration can hold this while the hub is unavailable.
	done := make(chan error, 1)
	go func() {
		_, e := b.Handle(context.Background(), "edge-A1", mqttbus.Request{Op: "PEER_PING"})
		done <- e
	}()
	select {
	case e := <-done:
		b.mu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		b.mu.Unlock()
		t.Fatal("peer liveness blocked on Central ledger mutex")
	}
	if _, e := b.Handle(context.Background(), "edge-unknown", mqttbus.Request{Op: "PEER_PING"}); e == nil {
		t.Fatal("unknown peer accepted")
	}
}

func TestTenantRoutingIsBoundToLedgerOwner(t *testing.T) {
	const owner = "client-a"
	const token = "separate-storage-token-32-characters"
	called := 0
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.URL.Path != "/v1/storage/client-a/block.bin" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("wrong namespace or credential", r.URL.Path)
		}
		_, _ = io.WriteString(w, "actual bytes")
	}))
	defer backend.Close()
	path := filepath.Join(t.TempDir(), "tenants.json")
	raw, _ := json.Marshal(map[string]any{owner: map[string]string{"owner_id": owner, "endpoint": backend.URL, "token": token}})
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("STORAGE_BACKENDS_FILE", path)
	b := &Bridge{backend: "http://legacy-must-not-be-used", client: backend.Client()}
	u := controllers.Unit{OwnerID: owner, Object: "block.bin"}
	r, e := b.unitRequest(context.Background(), u, "GET", false, nil, 0, "")
	if e != nil {
		t.Fatal(e)
	}
	_ = r.Body.Close()
	if called != 1 {
		t.Fatal("isolated backend not called")
	}
	u.OwnerID = "client-b"
	if _, e = b.unitRequest(context.Background(), u, "GET", false, nil, 0, ""); e == nil || called != 1 {
		t.Fatal("another owner reused storage")
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	u.OwnerID = owner
	if _, e = b.unitRequest(context.Background(), u, "GET", false, nil, 0, ""); e == nil || called != 1 {
		t.Fatal("missing registry downgraded to legacy")
	}
}

func TestExistingV2BackendRemainsReadable(t *testing.T) {
	t.Setenv("STORAGE_BACKENDS_FILE", "")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/storage/project/") {
			t.Error("legacy namespace changed", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer backend.Close()
	b := &Bridge{backend: backend.URL, client: backend.Client()}
	r, e := b.unitRequest(context.Background(), controllers.Unit{OwnerID: "client-legacy", Object: "old.bin"}, "GET", false, nil, 0, "")
	if e != nil {
		t.Fatal(e)
	}
	_ = r.Body.Close()
}
