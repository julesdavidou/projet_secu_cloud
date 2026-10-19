package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
)

// These tests use HTTP doubles. Real Java, MQTT, SQLite and Edge tests are in smoke.py.
func TestSessionIsolationAndRealOnlyCatalogue(t *testing.T) {
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("session")
		if e != nil || c.Value != "alice" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "AUTHENTICATED", "client_id": "client-alice"})
	}))
	defer auth.Close()
	real := controllers.UploadRecord{State: "READY", Units: []controllers.Unit{{State: "STORED", Proof: "HTTP_SHA256_VERIFIED"}}}
	simulated := controllers.UploadRecord{State: "READY", Units: []controllers.Unit{{State: "STORED", Proof: "MOCK"}}}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("operator credential missing")
		}
		json.NewEncoder(w).Encode(controllers.Inspection{Uploads: map[string]controllers.UploadRecord{"real": real, "mock": simulated, "private": real}})
	}))
	defer control.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "operator.token"), []byte(strings.Repeat("x", 32)), 0600)
	s, e := New(Config{AuthURL: auth.URL, CentralURL: control.URL, SecretsDir: dir, DataDir: filepath.Join(dir, "data"), SourceNodeID: "A1"})
	if e != nil {
		t.Fatal(e)
	}
	s.state.Movies = map[string]Movie{"real": {ID: "real", Owner: "client-alice"}, "mock": {ID: "mock", Owner: "client-alice"}, "private": {ID: "private", Owner: "client-bob"}}
	request := func(method, path, cookie, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "session", Value: cookie})
		}
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/videos", "", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated=%d", w.Code)
	}
	w := request("GET", "/api/videos", "alice", "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].ID != "real" {
		t.Fatalf("mock/private leaked: %s", w.Body.String())
	}
	if w = request("GET", "/api/videos/private/playback", "alice", "", ""); w.Code != 404 {
		t.Fatalf("private access=%d", w.Code)
	}
	if w = request("PATCH", "/api/me", "alice", `{"name":"Alice"}`, "https://evil.example"); w.Code != 403 {
		t.Fatalf("cross origin=%d", w.Code)
	}
	if w = request("GET", "/assets/js/config.js", "", "", ""); !strings.Contains(w.Body.String(), `dataSource:"api"`) {
		t.Fatal("frontend not connected")
	}
	if realReady(controllers.UploadRecord{State: "READY"}) {
		t.Fatal("empty READY accepted")
	}
}

func TestCorruptMetadataRefusesStartup(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "operator.token"), []byte(strings.Repeat("x", 32)), 0600)
	os.WriteFile(filepath.Join(dir, "web-metadata.json"), []byte("{"), 0600)
	if _, e := New(Config{AuthURL: "http://auth", SecretsDir: dir, DataDir: dir, SourceNodeID: "A1"}); e == nil {
		t.Fatal("corrupt state accepted")
	}
}
