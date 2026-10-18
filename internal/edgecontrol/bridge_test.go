package edgecontrol

import (
	"crypto/sha256"
	"fmt"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteFailureNeverNotifiesCompletion(t *testing.T) {
	for _, scenario := range []string{"disk-error", "corrupt-reread", "ledger-error"} {
		t.Run(scenario, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "PUT" {
					if scenario == "disk-error" {
						w.WriteHeader(500)
					} else {
						w.WriteHeader(201)
					}
					return
				}
				if scenario == "corrupt-reread" {
					fmt.Fprint(w, "wrong")
				} else {
					fmt.Fprint(w, "valid")
				}
			}))
			defer backend.Close()
			path := filepath.Join(t.TempDir(), "ledger.json")
			if scenario == "ledger-error" {
				path = t.TempDir()
			}
			b := &Bridge{path: path, status: placement.Available, mode: "real-http", backend: backend.URL, client: backend.Client(), state: ledger{Units: map[string]storedUnit{
				"video/0": {VideoID: "video", State: "RESERVED", Expires: time.Now().Add(time.Minute), Unit: controllers.Unit{Assignment: placement.Assignment{Extent: placement.Extent{Size: 5}}, Object: "unit", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("valid")))}}}}}
			r := httptest.NewRequest("PUT", "/store/video/0", strings.NewReader("valid"))
			r.SetPathValue("video", "video")
			r.SetPathValue("extent", "0")
			w := httptest.NewRecorder()
			b.write(w, r)
			if w.Code < 400 || b.state.Units["video/0"].Notified {
				t.Fatal("failed write acknowledged", w.Code)
			}
			// No MQTT bus is installed: reaching notify would panic and fail this test.
		})
	}
}

func TestQuotaDurableStoredBytesAndPendingExpiry(t *testing.T) {
	b := &Bridge{spec: controllers.NodeSpec{Quota: 100}, path: filepath.Join(t.TempDir(), "ledger.json"), state: ledger{Zone: "west", Generation: 1, Units: map[string]storedUnit{
		"stored":  {Unit: controllers.Unit{Assignment: placement.Assignment{Extent: placement.Extent{Size: 30}, ReservationID: "old"}}, State: "STORED", Expires: time.Now().Add(-time.Hour)},
		"pending": {Unit: controllers.Unit{Assignment: placement.Assignment{Extent: placement.Extent{Size: 40}, ReservationID: "pending"}}, State: "RESERVED", Expires: time.Now().Add(-time.Second)},
	}}}
	free, err := b.available()
	if err != nil || free != 30 {
		t.Fatal(free, err)
	}
	b.expire()
	free, err = b.available()
	if err != nil || free != 70 {
		t.Fatal("stored bytes freed or expired pending charged", free, err)
	}
	if err = b.save(); err != nil {
		t.Fatal(err)
	}
	var restored ledger
	if err = controllers.ReadJSON(b.path, &restored); err != nil {
		t.Fatal(err)
	}
	b.state = restored
	free, err = b.available()
	if err != nil || free != 70 {
		t.Fatal("quota lost after restart", free, err)
	}
	u := b.state.Units["stored"]
	u.Unit.Size = 101
	b.state.Units["stored"] = u
	if _, err = b.available(); err == nil {
		t.Fatal("over quota ledger admitted")
	}
}
