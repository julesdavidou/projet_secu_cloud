// Package edgecontrol bridges the reused MQTT5 business protocol to the actual
// Edge Agent HTTP storage. It never makes placement decisions.
package edgecontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type storedUnit struct {
	Unit     controllers.Unit `json:"unit"`
	VideoID  string           `json:"video_id"`
	Expires  time.Time        `json:"expires_at"`
	State    string           `json:"state"`
	Notified bool             `json:"notified"`
}
type ledger struct {
	Zone       string                `json:"zone"`
	Generation uint64                `json:"generation"`
	Version    uint64                `json:"version"`
	Units      map[string]storedUnit `json:"units"`
}
type networkIdentity struct {
	NodeID     string `json:"node_id"`
	ZoneID     string `json:"zone_id"`
	Generation uint64 `json:"generation"`
}

type Bridge struct {
	discovery atomic.Value
	mu        sync.Mutex
	cfg       controllers.Config
	spec      controllers.NodeSpec
	path      string
	state     ledger
	b         *mqttbus.Bus
	status    placement.State
	pause     bool
	failed    bool
	mode      string
	token     string
	backend   string
	client    *http.Client
}

func key(video string, id uint64) string { return video + "/" + strconv.FormatUint(id, 10) }
func Open(cfg controllers.Config, id, path, mode string) (*Bridge, error) {
	spec, ok := cfg.Node(id)
	if !ok || (mode != "real-http" && mode != "mock") {
		return nil, errors.New("unknown node or mode")
	}
	if spec.Mode != mode {
		return nil, errors.New("node mode differs from provisioned configuration")
	}
	b := &Bridge{cfg: cfg, spec: spec, path: path, status: placement.Available, mode: mode, backend: controllers.Env("EDGE_BACKEND_URL", "http://edge-agent:8080"), client: &http.Client{Timeout: 30 * time.Second}, state: ledger{Zone: spec.Zone, Generation: 1, Units: map[string]storedUnit{}}}
	if e := controllers.ReadJSON(path, &b.state); e != nil {
		return nil, e
	}
	if !controllers.Has(spec.AllowedZones, b.state.Zone) {
		return nil, errors.New("persisted node zone not provisioned")
	}
	var e error
	b.token, e = controllers.Secret("edge-" + id + ".http-token")
	if e != nil {
		return nil, e
	}
	if e = b.save(); e != nil {
		return nil, e
	}
	return b, nil
}
func (b *Bridge) save() error {
	e := controllers.AtomicJSON(b.path, b.state)
	if e != nil {
		b.failed = true
	} else {
		b.discovery.Store(networkIdentity{NodeID: b.spec.ID, ZoneID: b.state.Zone, Generation: b.state.Generation})
	}
	return e
}
func (b *Bridge) expire() {
	now := time.Now()
	for k, u := range b.state.Units {
		if u.State == "RESERVED" && !now.Before(u.Expires) {
			u.State = "EXPIRED"
			b.state.Units[k] = u
		}
	}
}
func (b *Bridge) available() (uint64, error) {
	var used uint64
	for _, u := range b.state.Units {
		if u.State != "RESERVED" && u.State != "STORED" {
			continue
		}
		if used > b.spec.Quota || u.Unit.Size > b.spec.Quota-used {
			return 0, errors.New("quota ledger exceeds provisioned quota; admission closed")
		}
		used += u.Unit.Size
	}
	return b.spec.Quota - used, nil
}
func (b *Bridge) snapshot() placement.Node {
	b.expire()
	available, e := b.available()
	state := b.status
	if e != nil || b.failed {
		state = placement.Unavailable
		available = 0
	}
	b.state.Version++
	now := time.Now()
	seen := map[string]bool{}
	for _, u := range b.state.Units {
		if u.State == "RESERVED" || u.State == "STORED" {
			seen[u.Unit.ReservationID] = true
		}
	}
	included := []string{}
	for id := range seen {
		included = append(included, id)
	}
	sort.Strings(included)
	return placement.Node{ID: b.spec.ID, Zone: b.state.Zone, State: state, Available: available, Generation: b.state.Generation, Version: b.state.Version, Updated: now, Heartbeat: now, Included: included}
}
func (b *Bridge) announce(ctx context.Context, retained bool) error {
	n := b.snapshot()
	qos := byte(0)
	suffix := "/heartbeat"
	if retained {
		qos = 1
		suffix = "/state"
	}
	if e := b.save(); e != nil {
		return e
	}
	return b.b.Publish(ctx, mqttbus.Root+"zone/"+n.Zone+"/node/"+n.ID+suffix, n, qos, retained, uint32(b.cfg.NodeTTLSeconds))
}
func (b *Bridge) register(ctx context.Context) error {
	n := b.snapshot()
	reg := controllers.Registered{Node: n, Endpoint: b.spec.Endpoint}
	if e := controllers.RPC(ctx, b.b, "central", "REGISTER_NODE", reg, nil); e != nil {
		return e
	}
	return controllers.RPC(ctx, b.b, "zone-"+n.Zone, "REGISTER", reg, nil)
}
func (b *Bridge) notify(ctx context.Context, k string) error {
	u := b.state.Units[k]
	proof := "HTTP_SHA256_VERIFIED"
	if b.mode == "mock" {
		proof = "MOCK"
	}
	r := controllers.Receipt{VideoID: u.VideoID, ExtentID: u.Unit.ID, NodeID: b.spec.ID, ReservationID: u.Unit.ReservationID, Size: u.Unit.Size, SHA256: u.Unit.SHA256, Proof: proof}
	if e := b.b.Retry(ctx, "central", "STORE_COMPLETE", r, nil); e != nil {
		return e
	}
	u.Notified = true
	b.state.Units[k] = u
	return b.save()
}
func (b *Bridge) Handle(ctx context.Context, sender string, r mqttbus.Request) (any, error) {
	// Peer liveness reads only immutable provisioned identities. It must not wait
	// for ledger operations or registration RPCs when Central is unreachable.
	if r.Op == "PEER_PING" {
		if !strings.HasPrefix(sender, "edge-") {
			return nil, errors.New("peer identity required")
		}
		if _, ok := b.cfg.Node(strings.TrimPrefix(sender, "edge-")); !ok {
			return nil, errors.New("unknown peer")
		}
		return map[string]string{"node_id": b.spec.ID, "peer": sender, "transport": "direct MQTT"}, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failed {
		return nil, errors.New("node ledger failed; admission closed")
	}
	switch r.Op {
	case "PREPARE_STORE":
		if sender != "zone-"+b.state.Zone {
			return nil, errors.New("only current owner zone may prepare storage")
		}
		var q controllers.Prepare
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		if q.NodeID != b.spec.ID {
			return nil, errors.New("prepare node mismatch")
		}
		if b.pause || b.status == placement.Unavailable {
			return nil, errors.New("node unavailable")
		}
		var accepted struct {
			Units   []controllers.Unit `json:"units"`
			Expires time.Time          `json:"expires_at"`
		}
		if e := controllers.RPC(ctx, b.b, "central", "AUTHORIZE_STORE", q, &accepted); e != nil {
			return nil, e
		}
		b.expire()
		free, e := b.available()
		if e != nil {
			return nil, e
		}
		var required uint64
		for _, unit := range accepted.Units {
			k := key(q.VideoID, unit.ID)
			if old, ok := b.state.Units[k]; ok {
				if old.Unit.OwnerID != unit.OwnerID || old.Unit.Size != unit.Size || old.Unit.SHA256 != unit.SHA256 || old.Unit.ReservationID != q.ReservationID {
					return nil, errors.New("unit identity conflict")
				}
				if old.State == "EXPIRED" || old.State == "RELEASED" {
					return nil, errors.New("unit lease terminal")
				}
				continue
			}
			if unit.Size > free-required {
				return nil, errors.New("project quota insufficient")
			}
			required += unit.Size
		}
		for _, unit := range accepted.Units {
			k := key(q.VideoID, unit.ID)
			if _, ok := b.state.Units[k]; !ok {
				b.state.Units[k] = storedUnit{Unit: unit, VideoID: q.VideoID, Expires: accepted.Expires, State: "RESERVED"}
			}
		}
		if e = b.save(); e != nil {
			return nil, e
		}
		if e = b.announce(ctx, true); e != nil {
			return nil, e
		}
		return map[string]any{"prepared": true, "mode": b.mode, "stored": false}, nil
	case "RELEASE":
		if sender != "zone-"+b.state.Zone && sender != "central" {
			return nil, errors.New("release unauthorized")
		}
		var q controllers.Key
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		for k, u := range b.state.Units {
			if u.Unit.ReservationID == q.ID && u.State == "RESERVED" {
				u.State = "RELEASED"
				b.state.Units[k] = u
			}
		}
		if e := b.save(); e != nil {
			return nil, e
		}
		return map[string]bool{"released": true}, b.announce(ctx, true)
	case "MOVE":
		if sender != "central" {
			return nil, errors.New("mobility unauthorized")
		}
		var q controllers.Move
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		if q.NodeID != b.spec.ID || !controllers.Has(b.spec.AllowedZones, q.ZoneID) {
			return nil, errors.New("mobility destination not provisioned")
		}
		if q.ZoneID == b.state.Zone {
			return b.snapshot(), nil
		}
		generation := b.state.Generation + 1
		if q.Generation != 0 && q.Generation != generation {
			return nil, errors.New("mobility generation must be next")
		}
		q.Generation = generation
		if e := controllers.RPC(ctx, b.b, "zone-"+b.state.Zone, "DEPART", q, nil); e != nil {
			return nil, e
		}
		b.state.Zone = q.ZoneID
		b.state.Generation = generation
		if e := b.save(); e != nil {
			return nil, e
		}
		if e := b.register(ctx); e != nil {
			return nil, e
		}
		if e := b.announce(ctx, true); e != nil {
			return nil, e
		}
		return b.snapshot(), nil
	case "CONTROL", "RETAINED_ONLY":
		if sender != "central" || os.Getenv("ENABLE_TEST_CONTROLS") != "1" {
			return nil, errors.New("test controls disabled or unauthorized")
		}
		if r.Op == "RETAINED_ONLY" {
			return map[string]bool{"heartbeat": false}, b.announce(ctx, true)
		}
		var q controllers.NodeControl
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		if q.State != placement.Available && q.State != placement.Degraded && q.State != placement.Unavailable {
			return nil, errors.New("invalid test state")
		}
		b.status = q.State
		b.pause = q.Pause
		if !b.pause {
			if e := b.register(ctx); e != nil {
				return nil, e
			}
			if e := b.announce(ctx, true); e != nil {
				return nil, e
			}
		}
		return b.snapshot(), nil
	case "REPLAY_COMPLETE":
		if sender != "central" || os.Getenv("ENABLE_TEST_CONTROLS") != "1" {
			return nil, errors.New("completion replay test disabled or unauthorized")
		}
		var q controllers.Prepare
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		count := 0
		for k, u := range b.state.Units {
			if u.VideoID != q.VideoID || u.State != "STORED" {
				continue
			}
			if b.mode == "real-http" {
				if e := b.verifyBackend(ctx, u); e != nil {
					return nil, e
				}
			}
			if e := b.notify(ctx, k); e != nil {
				return nil, e
			}
			count++
		}
		if count == 0 {
			return nil, errors.New("no stored completion to replay")
		}
		return map[string]int{"replayed": count}, nil
	case "DELETE_OBJECT":
		return nil, errors.New("managed deletion is not implemented; catalogue and quota must be coordinated")
	case "MOCK_STORE":
		if b.mode != "mock" || sender != "central" {
			return nil, errors.New("MOCK_STORE forbidden in real-http")
		}
		var q controllers.Prepare
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		for k, u := range b.state.Units {
			if u.VideoID == q.VideoID && u.State == "RESERVED" {
				if !time.Now().Before(u.Expires) {
					return nil, errors.New("mock lease expired")
				}
				u.State = "STORED"
				b.state.Units[k] = u
				if e := b.save(); e != nil {
					return nil, e
				}
				if e := b.notify(ctx, k); e != nil {
					return nil, e
				}
			}
		}
		return map[string]string{"proof": "MOCK"}, b.announce(ctx, true)
	case "STATUS", "REQUEST_STATUS", "REFRESH_STATE":
		if sender != "central" && !controllers.Authorized(b.cfg, sender) {
			return nil, errors.New("status unauthorized")
		}
		n := b.snapshot()
		units := make(map[string]storedUnit, len(b.state.Units))
		for k, u := range b.state.Units {
			units[k] = u
		}
		return map[string]any{"node": n, "quota_bytes": b.spec.Quota, "mode": b.mode, "units": units}, nil
	default:
		return nil, errors.New("unknown edge bridge operation")
	}
}
func (b *Bridge) backendRequest(ctx context.Context, method, path string, body io.Reader, length int64, digest string) (*http.Response, error) {
	r, e := http.NewRequestWithContext(ctx, method, b.backend+path, body)
	if e != nil {
		return nil, e
	}
	r.ContentLength = length
	r.Header.Set("Authorization", "Bearer "+b.token)
	if digest != "" {
		r.Header.Set("X-Content-SHA256", digest)
	}
	return b.client.Do(r)
}
func (b *Bridge) verifyBackend(ctx context.Context, u storedUnit) error {
	r, e := b.unitRequest(ctx, u.Unit, "GET", false, nil, 0, "")
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("backend read status %d", r.StatusCode)
	}
	h := sha256.New()
	size, e := io.Copy(h, io.LimitReader(r.Body, int64(u.Unit.Size)+1))
	if e != nil {
		return e
	}
	if uint64(size) != u.Unit.Size || hex.EncodeToString(h.Sum(nil)) != u.Unit.SHA256 {
		return errors.New("persisted unit failed size/SHA256 verification")
	}
	return nil
}
func (b *Bridge) write(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := key(r.PathValue("video"), parseExtent(r.PathValue("extent")))
	u, ok := b.state.Units[k]
	if !ok || b.mode != "real-http" || b.failed || b.pause || b.status == placement.Unavailable {
		http.Error(w, "unit not prepared or node unavailable", 409)
		return
	}
	if u.State == "STORED" {
		if e := b.verifyBackend(r.Context(), u); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if !u.Notified {
			if e := b.notify(r.Context(), k); e != nil {
				http.Error(w, e.Error(), 503)
				return
			}
		}
		controllers.JSON(w, 200, map[string]any{"stored": true, "sha256": u.Unit.SHA256, "idempotent": true})
		return
	}
	if u.State != "RESERVED" || !time.Now().Before(u.Expires) || r.ContentLength != int64(u.Unit.Size) {
		http.Error(w, "unit lease/length invalid", 422)
		return
	}
	response, e := b.unitRequest(r.Context(), u.Unit, "PUT", true, r.Body, r.ContentLength, u.Unit.SHA256)
	if e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != 201 {
		http.Error(w, "Edge rejected unit size/hash/write", 422)
		return
	}
	if e = b.verifyBackend(r.Context(), u); e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	u.State = "STORED"
	b.state.Units[k] = u
	if e = b.save(); e != nil {
		http.Error(w, e.Error(), 503)
		return
	}
	if e = b.notify(r.Context(), k); e != nil {
		http.Error(w, e.Error(), 503)
		return
	}
	_ = b.announce(r.Context(), true)
	controllers.JSON(w, 201, map[string]any{"stored": true, "sha256": u.Unit.SHA256, "size_bytes": u.Unit.Size, "proof": "HTTP_SHA256_VERIFIED"})
}
func parseExtent(s string) uint64 {
	v, e := strconv.ParseUint(s, 10, 64)
	if e != nil {
		return ^uint64(0)
	}
	return v
}
func (b *Bridge) read(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	u, ok := b.state.Units[key(r.PathValue("video"), parseExtent(r.PathValue("extent")))]
	b.mu.Unlock()
	if !ok || u.State != "STORED" || b.mode != "real-http" {
		http.NotFound(w, r)
		return
	}
	response, e := b.unitRequest(r.Context(), u.Unit, "GET", false, nil, 0, "")
	if e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		http.Error(w, "stored object unavailable", 502)
		return
	}
	w.Header().Set("X-Content-SHA256", u.Unit.SHA256)
	w.Header().Set("Content-Length", strconv.FormatUint(u.Unit.Size, 10))
	w.WriteHeader(200)
	_, _ = io.Copy(w, response.Body)
}
func Run(ctx context.Context, cfg controllers.Config) error {
	id := controllers.Env("EDGE_ID", "")
	b, e := Open(cfg, id, controllers.Env("EDGE_LEDGER_PATH", filepath.Join("/data", "edge-ledger.json")), controllers.Env("EDGE_MODE", "real-http"))
	if e != nil {
		return e
	}
	started := make(chan struct{})
	bus, e := controllers.NewBus(ctx, "edge-"+id, nil, func(ctx context.Context, sender string, r mqttbus.Request) (any, error) {
		<-started
		return b.Handle(ctx, sender, r)
	}, nil)
	if e != nil {
		return e
	}
	b.b = bus
	close(started)
	if cfg.DiscoveryEnabled {
		go controllers.SyncDiscovery(ctx, bus)
	}
	var ready atomic.Bool
	go func() {
		lastError := ""
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				b.mu.Lock()
				if !b.pause {
					op, cancel := context.WithTimeout(ctx, 8*time.Second)
					e := b.register(op)
					if e == nil {
						e = b.announce(op, false)
					}
					ready.Store(e == nil && !b.failed)
					if e != nil && e.Error() != lastError {
						log.Printf("edge %s registration/heartbeat: %v", id, e)
						lastError = e.Error()
					} else if e == nil && lastError != "" {
						log.Printf("edge %s registration recovered", id)
						lastError = ""
					}
					for k, u := range b.state.Units {
						if u.State == "STORED" && !u.Notified {
							_ = b.notify(op, k)
						}
					}
					cancel()
				}
				b.mu.Unlock()
			}
		}
	}()
	mux := http.NewServeMux()
	controllers.Health(mux, ready.Load)
	mux.HandleFunc("GET /service/probe", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("X-Verified-Identity"), "zone-") {
			http.Error(w, "zone certificate required", 403)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(make([]byte, 64<<10))
	})
	mux.HandleFunc("GET /discovery", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("X-Verified-Identity"), "zone-") {
			http.Error(w, "zone certificate required", 403)
			return
		}
		// Network identity is a committed immutable snapshot. It must not wait
		// for the ledger lock held by a central reconnect attempt.
		controllers.JSON(w, 200, b.discovery.Load())
	})
	storage := http.NewServeMux()
	storage.HandleFunc("PUT /store/{video}/{extent}", b.write)
	mux.HandleFunc("GET /store/{video}/{extent}", b.readAuthorized)
	mux.Handle("POST /reconstruct", controllers.Bearer(b.token, http.HandlerFunc(b.reconstruct)))
	mux.Handle("/store/", controllers.Bearer(b.token, storage))
	return controllers.Serve(ctx, controllers.Env("HTTP_ADDR", ":8088"), mux)
}
