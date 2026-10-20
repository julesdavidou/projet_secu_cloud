package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/catalog"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type centralState struct {
	Discoveries map[string]Discovery     `json:"discoveries,omitempty"`
	Nodes       map[string]Registered    `json:"nodes"`
	Uploads     map[string]UploadRecord  `json:"uploads"`
	Outbox      map[string]catalog.Event `json:"outbox"`
}
type Central struct {
	mu     sync.Mutex
	cfg    Config
	db     *catalog.Catalog
	path   string
	state  centralState
	failed bool
}

func OpenCentral(cfg Config, path string) (*Central, error) {
	db, e := catalog.Open(path)
	if e != nil {
		return nil, e
	}
	c := &Central{cfg: cfg, db: db, path: filepath.Join(filepath.Dir(path), "control-metadata.json"), state: centralState{Nodes: map[string]Registered{}, Uploads: map[string]UploadRecord{}, Outbox: map[string]catalog.Event{}}}
	if e = ReadJSON(c.path, &c.state); e != nil {
		db.Close()
		return nil, e
	}
	ids := []string{}
	for id := range c.state.Outbox {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, e = c.db.Apply(c.state.Outbox[id], time.Now()); e != nil {
			db.Close()
			return nil, fmt.Errorf("outbox reconciliation %s: %w", id, e)
		}
		delete(c.state.Outbox, id)
	}
	if e = c.save(); e != nil {
		db.Close()
		return nil, e
	}
	return c, nil
}
func (c *Central) Close() error { return c.db.Close() }
func (c *Central) save() error {
	e := AtomicJSON(c.path, c.state)
	if e != nil {
		c.failed = true
	}
	return e
}
func (c *Central) commit(e catalog.Event) (catalog.Result, error) {
	c.state.Outbox[e.ID] = e
	if err := c.save(); err != nil {
		return catalog.Result{}, err
	}
	result, err := c.db.Apply(e, time.Now())
	if err != nil {
		delete(c.state.Outbox, e.ID)
		_ = c.save()
		return result, err
	}
	delete(c.state.Outbox, e.ID)
	return result, c.save()
}
func (c *Central) Handle(ctx context.Context, sender string, r mqttbus.Request) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed {
		return nil, errors.New("central metadata persistence failed; mutations closed")
	}
	switch r.Op {
	case "PING":
		return map[string]bool{"ready": true}, nil
	case "REGISTER_NODE":
		var n Registered
		if e := mqttbus.Decode(r.Body, &n); e != nil {
			return nil, e
		}
		spec, ok := c.cfg.Node(n.Node.ID)
		if !ok || sender != "edge-"+n.Node.ID || !Has(spec.AllowedZones, n.Node.Zone) || n.Endpoint != spec.Endpoint || n.Node.Available > spec.Quota {
			return nil, errors.New("node identity, endpoint, zone or project quota not provisioned")
		}
		if e := n.Node.Validate(time.Now()); e != nil {
			return nil, e
		}
		old, exists := c.state.Nodes[n.Node.ID]
		if exists && (n.Node.Generation < old.Node.Generation || n.Node.Generation == old.Node.Generation && n.Node.Zone != old.Node.Zone) {
			return nil, errors.New("stale ownership generation")
		}
		c.state.Nodes[n.Node.ID] = n
		if !exists || n.Node.Generation > old.Node.Generation {
			if _, e := c.commit(catalog.Event{ID: EventID("node", n.Node.ID, n.Node.Generation), Op: "NODE", Node: n.Node}); e != nil {
				if exists {
					c.state.Nodes[n.Node.ID] = old
				} else {
					delete(c.state.Nodes, n.Node.ID)
				}
				_ = c.save()
				return nil, e
			}
		}
		return n, c.save()
	case "NODE":
		var key NodeKey
		if e := mqttbus.Decode(r.Body, &key); e != nil {
			return nil, e
		}
		if !Authorized(c.cfg, sender) && sender != "edge-"+key.NodeID {
			return nil, errors.New("node lookup unauthorized")
		}
		n, ok := c.state.Nodes[key.NodeID]
		if !ok {
			return nil, errors.New("node has not registered")
		}
		return n, nil
	case "LOOKUP":
		if !Authorized(c.cfg, sender) {
			return nil, errors.New("lookup unauthorized")
		}
		var key LookupKey
		if e := mqttbus.Decode(r.Body, &key); e != nil {
			return nil, e
		}
		u, ok := c.state.Uploads[key.RequestID]
		if !ok {
			return nil, nil
		}
		return c.record(key.RequestID, u)
	case "DISCOVERY_GET":
		if !Authorized(c.cfg, sender) && !strings.HasPrefix(sender, "edge-") {
			return nil, errors.New("directory unauthorized")
		}
		return discoveryRoutes(c.cfg, c.state.Discoveries, time.Now()), nil
	case "DISCOVERY_SET":
		var d Discovery
		if e := mqttbus.Decode(r.Body, &d); e != nil {
			return nil, e
		}
		if !c.cfg.DiscoveryEnabled || sender != "zone-"+d.ZoneID || validateDiscovery(c.cfg, d, time.Now()) != nil {
			return nil, errors.New("discovery authority mismatch")
		}
		if n, ok := c.state.Nodes[d.NodeID]; ok && (n.Node.Zone != d.ZoneID || n.Node.Generation != d.Generation) {
			return nil, errors.New("discovery differs from registered mobility generation")
		}
		if old, ok := c.state.Discoveries[d.NodeID]; ok && (old.Generation > d.Generation || old.Observed.After(d.Observed)) {
			return nil, errors.New("stale discovery")
		}
		if c.state.Discoveries == nil {
			c.state.Discoveries = map[string]Discovery{}
		}
		c.state.Discoveries[d.NodeID] = d
		return map[string]bool{"admitted": true}, c.save()
	case "PLAN":
		var p PlanCommit
		if e := mqttbus.Decode(r.Body, &p); e != nil {
			return nil, e
		}
		if e := p.Upload.Validate(); e != nil {
			return nil, e
		}
		source, ok := c.state.Nodes[p.Upload.Manifest.SourceNodeID]
		if !ok || sender != "zone-"+source.Node.Zone {
			return nil, errors.New("source zone not authoritative")
		}
		if old, ok := c.state.Uploads[p.Upload.Manifest.RequestID]; ok {
			a, _ := json.Marshal(old.Upload)
			b, _ := json.Marshal(p.Upload)
			if string(a) != string(b) {
				return nil, errors.New("request identity conflict")
			}
			return c.record(p.Upload.Manifest.RequestID, old)
		}
		if _, _, exists := c.findVideo(p.Upload.Manifest.VideoID); exists {
			return nil, errors.New("video identity already belongs to another request")
		}
		if e := validateService(c.cfg, p.Upload, p.Plan, time.Now()); e != nil {
			return nil, e
		}
		u := UploadRecord{Upload: p.Upload, Plan: p.Plan, Expires: p.Expires, State: "RESERVED"}
		ext, e := placement.Extents(p.Upload.Manifest, p.Upload.Target)
		if e != nil {
			return nil, e
		}
		if len(p.Plan.Assignments) != len(ext) {
			return nil, errors.New("plan extent count")
		}
		for i, a := range p.Plan.Assignments {
			if a.Extent != ext[i] {
				return nil, errors.New("plan boundary mismatch")
			}
			n, ok := c.state.Nodes[a.NodeID]
			if !ok {
				return nil, errors.New("unknown target node")
			}
			if p.Upload.Policy != nil && p.Plan.Service.Observations[a.NodeID].Generation != n.Node.Generation {
				return nil, errors.New("node moved after service observation")
			}
			u.Units = append(u.Units, Unit{OwnerID: p.Upload.OwnerID, Assignment: a, SHA256: p.Upload.Contents[i].SHA256, Object: EventID("object", p.Upload.Manifest.VideoID, a.ID) + ".bin", Endpoint: n.Endpoint, State: "RESERVED"})
		}
		c.state.Uploads[p.Upload.Manifest.RequestID] = u
		event := catalog.Event{ID: EventID("plan", p.Upload.Manifest.RequestID), Op: "PLAN", Manifest: p.Upload.Manifest, Plan: p.Plan, Expires: p.Expires}
		if _, e = c.commit(event); e != nil {
			u.State = "FAILED"
			c.state.Uploads[p.Upload.Manifest.RequestID] = u
			_ = c.save()
			return nil, e
		}
		return c.record(p.Upload.Manifest.RequestID, u)
	case "AUTHORIZE_STORE":
		var q Prepare
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		if sender != "edge-"+q.NodeID {
			return nil, errors.New("store authorization must come from provisioned destination")
		}
		_, u, ok := c.findVideo(q.VideoID)
		if !ok || !time.Now().Before(u.Expires) || u.State == "FAILED" {
			return nil, errors.New("unknown, failed or expired video")
		}
		units := []Unit{}
		for _, unit := range u.Units {
			if unit.NodeID == q.NodeID && unit.ReservationID == q.ReservationID {
				units = append(units, unit)
			}
		}
		if len(units) == 0 {
			return nil, errors.New("no assignment for node/reservation")
		}
		return struct {
			Units   []Unit    `json:"units"`
			Expires time.Time `json:"expires_at"`
		}{units, u.Expires}, nil
	case "PREPARED":
		var q Prepare
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		id, u, ok := c.findVideo(q.VideoID)
		if !ok {
			return nil, errors.New("unknown video")
		}
		zone := ""
		for _, a := range u.Plan.Assignments {
			if a.ReservationID == q.ReservationID {
				zone = a.ZoneID
			}
		}
		if sender != "zone-"+zone {
			return nil, errors.New("prepared sender mismatch")
		}
		for i := range u.Units {
			if u.Units[i].ReservationID == q.ReservationID {
				u.Units[i].State = "TRANSFERRING"
			}
		}
		u.State = "TRANSFERRING"
		c.state.Uploads[id] = u
		return map[string]bool{"accepted": true}, c.save()
	case "STORE_COMPLETE":
		var proof Receipt
		if e := mqttbus.Decode(r.Body, &proof); e != nil {
			return nil, e
		}
		if sender != "edge-"+proof.NodeID {
			return nil, errors.New("completion sender mismatch")
		}
		id, u, ok := c.findVideo(proof.VideoID)
		if !ok {
			return nil, errors.New("unknown video")
		}
		spec, _ := c.cfg.Node(proof.NodeID)
		expectedProof := "HTTP_SHA256_VERIFIED"
		if spec.Mode == "mock" {
			expectedProof = "MOCK"
		}
		if proof.Proof != expectedProof {
			return nil, errors.New("completion proof does not match provisioned mode")
		}
		found := false
		for i, unit := range u.Units {
			if unit.ID == proof.ExtentID {
				if unit.NodeID != proof.NodeID || unit.ReservationID != proof.ReservationID || unit.Size != proof.Size || unit.SHA256 != proof.SHA256 || (proof.Proof != "HTTP_SHA256_VERIFIED" && proof.Proof != "MOCK") {
					return nil, errors.New("completion does not match immutable expected content")
				}
				u.Units[i].Proof = proof.Proof
				found = true
			}
		}
		if !found {
			return nil, errors.New("unknown extent")
		}
		c.state.Uploads[id] = u
		event := catalog.Event{ID: EventID("stored", proof.VideoID, proof.ExtentID), Op: "STORE_COMPLETE", VideoID: proof.VideoID, ExtentID: proof.ExtentID, NodeID: proof.NodeID, ReservationID: proof.ReservationID}
		result, e := c.commit(event)
		return result, e
	case "CANCEL":
		if !Authorized(c.cfg, sender) {
			return nil, errors.New("cancel unauthorized")
		}
		var q Prepare
		if e := mqttbus.Decode(r.Body, &q); e != nil {
			return nil, e
		}
		id, u, ok := c.findVideo(q.VideoID)
		if !ok {
			return nil, errors.New("unknown video")
		}
		zone, _ := c.db.OwnerZone("", q.VideoID)
		if sender != "central" && sender != "admin" && sender != "zone-"+zone {
			return nil, errors.New("cancel source mismatch")
		}
		result, e := c.commit(catalog.Event{ID: EventID("cancel", q.VideoID), Op: "CANCEL", VideoID: q.VideoID})
		if e == nil {
			u.State = "FAILED"
			c.state.Uploads[id] = u
			e = c.save()
		}
		return result, e
	case "SNAPSHOT":
		if !Authorized(c.cfg, sender) {
			return nil, errors.New("snapshot unauthorized")
		}
		return c.inspect()
	default:
		return nil, errors.New("unknown central operation")
	}
}
func (c *Central) findVideo(video string) (string, UploadRecord, bool) {
	for id, u := range c.state.Uploads {
		if u.Upload.Manifest.VideoID == video {
			return id, u, true
		}
	}
	return "", UploadRecord{}, false
}
func (c *Central) record(id string, u UploadRecord) (UploadRecord, error) {
	// Transport encoders run after the handler releases mu. Return owned units
	// so a concurrent STORE_COMPLETE cannot mutate a response being encoded.
	u.Units = append([]Unit(nil), u.Units...)
	l, e := c.db.Lookup(id)
	if e != nil {
		return u, e
	}
	if !l.Found {
		u.State = "FAILED"
		return u, nil
	}
	if l.State == "READY" {
		u.State = "READY"
	} else if l.State == "CANCELED" || !time.Now().Before(l.Expires) {
		u.State = "FAILED"
	}
	snapshot, e := c.db.Snapshot()
	if e != nil {
		return u, e
	}
	for i := range u.Units {
		for _, loc := range snapshot.Locations {
			if loc.VideoID == u.Upload.Manifest.VideoID && loc.ExtentID == u.Units[i].ID {
				u.Units[i].ZoneID = loc.ZoneID
				if loc.State == "STORED" {
					u.Units[i].State = "STORED"
				}
			}
		}
	}
	return u, nil
}
func (c *Central) inspect() (Inspection, error) {
	s, e := c.db.Snapshot()
	if e != nil {
		return Inspection{}, e
	}
	v := Inspection{Catalog: s, Nodes: map[string]Registered{}, Uploads: map[string]UploadRecord{}}
	for id, n := range c.state.Nodes {
		v.Nodes[id] = n
	}
	for id, u := range c.state.Uploads {
		u, e = c.record(id, u)
		if e != nil {
			return v, e
		}
		v.Uploads[id] = u
	}
	return v, nil
}
func RunCentral(ctx context.Context, cfg Config) error {
	c, e := OpenCentral(cfg, Env("DB_PATH", "/data/central.db"))
	if e != nil {
		return e
	}
	defer c.Close()
	b, e := NewBus(ctx, "central", nil, c.Handle, nil)
	if e != nil {
		return e
	}
	var ready atomic.Bool
	ready.Store(true)
	go func() {
		committed := map[string]bool{}
		released := map[string]bool{}
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				e := RPC(ctx, b, "central", "PING", struct{}{}, nil)
				ready.Store(e == nil)
				c.mu.Lock()
				pending := map[string]string{}
				cancelled := map[string]string{}
				for id, u := range c.state.Uploads {
					record, err := c.record(id, u)
					if err == nil && record.State == "FAILED" {
						for zone, res := range u.Plan.Reservations {
							if !released[res] {
								cancelled[res] = zone
							}
						}
					}
					if err == nil && record.State == "READY" {
						for zone, res := range u.Plan.Reservations {
							if !committed[res] {
								pending[res] = zone
							}
						}
					}
				}
				c.mu.Unlock()
				// Retry after broker outages and process restarts. RELEASE only
				// releases reservations; stored bytes and their quota stay intact.
				for res, zone := range cancelled {
					if RPC(ctx, b, "zone-"+zone, "RELEASE", Key{res}, nil) == nil {
						released[res] = true
					}
				}
				for res, zone := range pending {
					if RPC(ctx, b, "zone-"+zone, "COMMIT", Key{res}, nil) == nil {
						committed[res] = true
					}
				}
			}
		}
	}()
	mux := http.NewServeMux()
	Health(mux, ready.Load)
	token, e := Secret("operator.token")
	if e != nil {
		return e
	}
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		v, e := c.inspect()
		c.mu.Unlock()
		if e != nil {
			http.Error(w, e.Error(), 503)
			return
		}
		JSON(w, 200, v)
	})
	api.HandleFunc("GET /api/v1/videos/{id}/placements", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		id, u, ok := c.findVideo(r.PathValue("id"))
		var e error
		if ok {
			u, e = c.record(id, u)
		}
		c.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		if e != nil {
			http.Error(w, e.Error(), 503)
			return
		}
		JSON(w, 200, u)
	})
	api.HandleFunc("GET /api/v1/discovery/routes", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		routes := discoveryRoutes(cfg, c.state.Discoveries, time.Now())
		c.mu.Unlock()
		JSON(w, 200, routes)
	})
	api.HandleFunc("POST /api/v1/quotes", func(w http.ResponseWriter, r *http.Request) {
		var q Upload
		if e := ReadBody(w, r, &q); e != nil {
			http.Error(w, "invalid quote", 400)
			return
		}
		if e := q.Validate(); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		c.mu.Lock()
		n, ok := c.state.Nodes[q.Manifest.SourceNodeID]
		c.mu.Unlock()
		if !ok {
			http.Error(w, "source not registered", 409)
			return
		}
		var v placement.DistributedPlan
		if e := RPC(r.Context(), b, "zone-"+n.Node.Zone, "QUOTE", q, &v); e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		JSON(w, 200, v)
	})
	api.HandleFunc("POST /api/v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		var q Upload
		if e := ReadBody(w, r, &q); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if e := q.Validate(); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		c.mu.Lock()
		n, ok := c.state.Nodes[q.Manifest.SourceNodeID]
		c.mu.Unlock()
		if !ok {
			http.Error(w, "source not registered", 409)
			return
		}
		var v UploadRecord
		if e := RPC(r.Context(), b, "zone-"+n.Node.Zone, "UPLOAD", q, &v); e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		JSON(w, 201, v)
	})
	api.HandleFunc("POST /api/v1/rpc/{target}/{op}", func(w http.ResponseWriter, r *http.Request) {
		target := r.PathValue("target")
		valid := target == "central" || strings.HasPrefix(target, "zone-") && cfg.HasZone(strings.TrimPrefix(target, "zone-"))
		if strings.HasPrefix(target, "edge-") {
			_, valid = cfg.Node(strings.TrimPrefix(target, "edge-"))
		}
		if !valid {
			http.Error(w, "unknown target", 400)
			return
		}
		var q json.RawMessage
		if e := ReadBody(w, r, &q); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		var out json.RawMessage
		if e := RPC(r.Context(), b, target, r.PathValue("op"), q, &out); e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		JSON(w, 200, out)
	})
	c.operationAPI(api)
	c.liveAPI(api)
	mux.Handle("/api/", Bearer(token, api))
	return Serve(ctx, Env("HTTP_ADDR", ":8088"), mux)
}
