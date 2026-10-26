package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type zonePeer struct {
	b  *mqttbus.Bus
	id string
}

func (p zonePeer) Offer(ctx context.Context, q placement.ReservationRequest) (placement.Reservation, error) {
	var r placement.Reservation
	e := RPC(ctx, p.b, "zone-"+p.id, "OFFER", q, &r)
	return r, e
}
func (p zonePeer) Reserve(ctx context.Context, q placement.ReservationRequest) (placement.Reservation, error) {
	var r placement.Reservation
	e := RPC(ctx, p.b, "zone-"+p.id, "RESERVE", q, &r)
	return r, e
}
func (p zonePeer) Release(ctx context.Context, id string) error {
	return RPC(ctx, p.b, "zone-"+p.id, "RELEASE", Key{id}, nil)
}

// RunZone uses the registry created by Dan's allocator.go. onNode updates his
// local registry; no detailed foreign registry enters the placement coordinator.
func RunZone(ctx context.Context, cfg Config, z *placement.Zone, onNode func(Registered) error) error {
	if !cfg.HasZone(z.ID) {
		return errors.New("zone not configured")
	}
	path := Env("LEDGER_PATH", filepath.Join("/data", "controller-ledger.json"))
	var checkpoint placement.Checkpoint
	if _, e := os.Stat(path); e == nil {
		if e = ReadJSON(path, &checkpoint); e != nil {
			return e
		}
		if e = z.Restore(checkpoint); e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	z.SetCheckpointSink(func(c placement.Checkpoint) error { return AtomicJSON(path, c) })
	cache := placement.NewSummaryCache()
	var b *mqttbus.Bus
	started := make(chan struct{})
	var registrationMu sync.Mutex
	registered := map[string]Registered{}
	beats := map[string]time.Time{}
	var uploadMu sync.Mutex
	var offers, publications atomic.Uint64
	dirty := make(chan struct{}, 1)
	critical := make(chan struct{}, 1)
	mark := func() {
		select {
		case dirty <- struct{}{}:
		default:
		}
	}
	coordinator := &placement.Coordinator{Local: z, Cache: cache, Fanout: cfg.Fanout, Timeout: 2 * time.Second, SummaryTTL: time.Duration(cfg.NodeTTLSeconds) * time.Second, Clock: time.Now}
	state := func(topic string, raw []byte, retained bool) {
		now := time.Now()
		if strings.HasSuffix(topic, "/summary") {
			var s placement.Summary
			if mqttbus.Decode(raw, &s) == nil && cfg.HasZone(s.ZoneID) && topic == mqttbus.Root+"zone/"+s.ZoneID+"/summary" {
				_ = cache.Put(s, now)
			}
			return
		}
		var n placement.Node
		if mqttbus.Decode(raw, &n) != nil {
			return
		}
		var clockErr error
		n, clockErr = authenticatedNodeAtReceipt(n, now, retained)
		if clockErr != nil {
			return
		}
		registrationMu.Lock()
		owner, ok := registered[n.ID]
		if !ok || owner.Node.Generation != n.Generation || n.Zone != z.ID {
			registrationMu.Unlock()
			return
		}
		prefix := mqttbus.Root + "zone/" + z.ID + "/node/" + n.ID
		if topic != prefix+"/state" && topic != prefix+"/heartbeat" {
			registrationMu.Unlock()
			return
		}
		if topic == prefix+"/heartbeat" && !retained {
			beats[n.ID] = now
			n.Heartbeat = now
		} else if beat, ok := beats[n.ID]; ok {
			n.Heartbeat = beat
		} else {
			n.State = placement.Unavailable
		}
		owner.Node = n
		registered[n.ID] = owner
		registrationMu.Unlock()
		if z.Upsert(n, now) == nil {
			if onNode != nil {
				_ = onNode(owner)
			}
			mark()
		}
	}
	var prepareLocal func(context.Context, Prepare) error
	prepareLocal = func(opctx context.Context, q Prepare) error {
		_ = z.Summary(time.Now())
		r, ok := z.Checkpoint().Reservations[q.ReservationID]
		if !ok || r.State != "RESERVED" || r.Request.VideoID != q.VideoID || !time.Now().Before(r.Request.Expires) {
			return errors.New("local reservation inactive")
		}
		nodes := map[string]bool{}
		for _, a := range r.Plan {
			nodes[a.NodeID] = true
		}
		if len(nodes) == 0 {
			return nil
		}
		for id := range nodes {
			if e := RPC(opctx, b, "edge-"+id, "PREPARE_STORE", Prepare{VideoID: q.VideoID, ReservationID: q.ReservationID, NodeID: id}, nil); e != nil {
				return e
			}
		}
		return RPC(opctx, b, "central", "PREPARED", q, nil)
	}
	h := func(opctx context.Context, sender string, r mqttbus.Request) (any, error) {
		<-started
		switch r.Op {
		case "REGISTER":
			var n Registered
			if e := mqttbus.Decode(r.Body, &n); e != nil {
				return nil, e
			}
			spec, ok := cfg.Node(n.Node.ID)
			if !ok || sender != "edge-"+n.Node.ID || !Has(spec.AllowedZones, z.ID) || n.Node.Zone != z.ID {
				return nil, errors.New("unprovisioned local node")
			}
			var authoritative Registered
			if e := RPC(opctx, b, "central", "NODE", NodeKey{n.Node.ID}, &authoritative); e != nil {
				return nil, e
			}
			if authoritative.Node.Generation != n.Node.Generation || authoritative.Node.Zone != z.ID || authoritative.Endpoint != n.Endpoint {
				return nil, errors.New("node ownership not committed at central")
			}
			var clockErr error
			n.Node, clockErr = authenticatedNodeAtReceipt(n.Node, time.Now(), false)
			if clockErr != nil {
				return nil, clockErr
			}
			registrationMu.Lock()
			copy := n.Node
			if beat, ok := beats[n.Node.ID]; ok {
				copy.Heartbeat = beat
			} else {
				copy.State = placement.Unavailable
			}
			n.Node = copy
			registered[n.Node.ID] = n
			registrationMu.Unlock()
			_ = z.Upsert(copy, time.Now())
			if onNode != nil {
				if e := onNode(n); e != nil {
					return nil, e
				}
			}
			return map[string]bool{"registered": true}, nil
		case "OFFER", "RESERVE":
			if !Authorized(cfg, sender) {
				return nil, errors.New("zone operation unauthorized")
			}
			var q placement.ReservationRequest
			if e := mqttbus.Decode(r.Body, &q); e != nil {
				return nil, e
			}
			if r.Op == "OFFER" {
				offers.Add(1)
				return z.Offer(q, time.Now())
			}
			v, e := z.Reserve(q, time.Now())
			mark()
			return v, e
		case "RELEASE", "COMMIT":
			if !Authorized(cfg, sender) {
				return nil, errors.New("reservation operation unauthorized")
			}
			var q Key
			if e := mqttbus.Decode(r.Body, &q); e != nil {
				return nil, e
			}
			if r.Op == "COMMIT" {
				return map[string]bool{"committed": true}, z.Commit(q.ID, time.Now())
			}
			saved := z.Checkpoint().Reservations[q.ID]
			for _, a := range saved.Plan {
				if e := RPC(opctx, b, "edge-"+a.NodeID, "RELEASE", q, nil); e != nil {
					return nil, e
				}
			}
			e := z.Release(q.ID)
			mark()
			return map[string]bool{"released": e == nil}, e
		case "PREPARE":
			if !Authorized(cfg, sender) {
				return nil, errors.New("prepare unauthorized")
			}
			var q Prepare
			if e := mqttbus.Decode(r.Body, &q); e != nil {
				return nil, e
			}
			e := prepareLocal(opctx, q)
			mark()
			return map[string]bool{"prepared": e == nil}, e
		case "DEPART":
			var move Move
			if e := mqttbus.Decode(r.Body, &move); e != nil {
				return nil, e
			}
			if sender != "edge-"+move.NodeID {
				return nil, errors.New("departure identity mismatch")
			}
			e := z.Depart(move.NodeID, move.Generation)
			registrationMu.Lock()
			delete(registered, move.NodeID)
			delete(beats, move.NodeID)
			registrationMu.Unlock()
			mark()
			return map[string]bool{"departed": e == nil}, e
		case "STATS":
			if !Authorized(cfg, sender) {
				return nil, errors.New("stats unauthorized")
			}
			peers := map[string]placement.Summary{}
			for _, p := range coordinator.Neighbors {
				if s, ok := cache.Get(p.ID, time.Now(), coordinator.SummaryTTL); ok {
					peers[p.ID] = s
				}
			}
			registrationMu.Lock()
			nodes := map[string]Registered{}
			for id, n := range registered {
				nodes[id] = n
			}
			registrationMu.Unlock()
			return map[string]any{"summary": z.Summary(time.Now()), "neighbors": peers, "nodes": nodes, "offers": offers.Load(), "summary_publications": publications.Load()}, nil
		case "QUOTE", "UPLOAD":
			if sender != "central" && sender != "admin" {
				return nil, errors.New("upload unauthorized")
			}
			var q Upload
			if e := mqttbus.Decode(r.Body, &q); e != nil {
				return nil, e
			}
			if e := q.Validate(); e != nil {
				return nil, e
			}
			selected := coordinator
			if q.Policy != nil {
				selected = measuredCoordinator(opctx, coordinator)
			}
			if r.Op == "QUOTE" {
				return selected.OfferWithPolicy(opctx, q.Manifest, q.Target, time.Now(), time.Duration(cfg.LeaseSeconds)*time.Second, q.Policy)
			}
			uploadMu.Lock()
			defer uploadMu.Unlock()
			var previous *UploadRecord
			if e := RPC(opctx, b, "central", "LOOKUP", LookupKey{q.Manifest.RequestID}, &previous); e != nil {
				return nil, e
			}
			if previous != nil {
				old, _ := json.Marshal(previous.Upload)
				incoming, _ := json.Marshal(q)
				if string(old) != string(incoming) {
					return nil, errors.New("request content conflict")
				}
				if previous.State == "FAILED" {
					return nil, errors.New("request failed/expired; use a new request_id")
				}
				if previous.State != "READY" {
					for zone, id := range previous.Plan.Reservations {
						q := Prepare{VideoID: previous.Upload.Manifest.VideoID, ReservationID: id}
						var e error
						if zone == z.ID {
							e = prepareLocal(opctx, q)
						} else {
							e = RPC(opctx, b, "zone-"+zone, "PREPARE", q, nil)
						}
						if e != nil {
							return nil, e
						}
					}
				}
				return previous, nil
			}
			registrationMu.Lock()
			source, ok := registered[q.Manifest.SourceNodeID]
			registrationMu.Unlock()
			if !ok || source.Node.Zone != z.ID {
				return nil, errors.New("source is not registered locally")
			}
			now := time.Now()
			lease := time.Duration(cfg.LeaseSeconds) * time.Second
			plan, e := selected.PlaceWithPolicy(opctx, q.Manifest, q.Target, now, lease, q.Policy)
			mark()
			if e != nil {
				return nil, e
			}
			var record UploadRecord
			if e = RPC(opctx, b, "central", "PLAN", PlanCommit{q, plan, z.Checkpoint().Reservations[plan.Reservations[z.ID]].Request.Expires}, &record); e != nil {
				return nil, e
			}
			for zone, id := range plan.Reservations {
				p := Prepare{VideoID: q.Manifest.VideoID, ReservationID: id}
				if zone == z.ID {
					e = prepareLocal(opctx, p)
				} else {
					e = RPC(opctx, b, "zone-"+zone, "PREPARE", p, nil)
				}
				if e != nil {
					_ = RPC(opctx, b, "central", "CANCEL", Prepare{VideoID: q.Manifest.VideoID}, nil)
					for dest, res := range plan.Reservations {
						_ = RPC(opctx, b, "zone-"+dest, "RELEASE", Key{res}, nil)
					}
					return nil, e
				}
			}
			if e = RPC(opctx, b, "central", "LOOKUP", LookupKey{q.Manifest.RequestID}, &record); e != nil {
				return nil, e
			}
			return record, nil
		default:
			return nil, errors.New("unknown zone operation")
		}
	}
	subs := []string{mqttbus.Root + "zone/" + z.ID + "/node/+/state", mqttbus.Root + "zone/" + z.ID + "/node/+/heartbeat"}
	for _, id := range cfg.Zones {
		subs = append(subs, mqttbus.Root+"zone/"+id+"/summary")
	}
	var e error
	b, e = NewBus(ctx, "zone-"+z.ID, subs, h, state)
	if e != nil {
		return e
	}
	for _, id := range cfg.Zones {
		if id != z.ID {
			coordinator.Neighbors = append(coordinator.Neighbors, placement.Neighbor{ID: id, Peer: zonePeer{b, id}})
		}
	}
	close(started)
	if cfg.DiscoveryEnabled {
		go SyncDiscovery(ctx, b)
	}
	if Env("TLS_CA_FILE", "") != "" {
		go probeZone(ctx, cfg, z, func() []Registered {
			registrationMu.Lock()
			defer registrationMu.Unlock()
			out := []Registered{}
			for _, n := range registered {
				out = append(out, n)
			}
			return out
		})
	}
	var ready atomic.Bool
	ready.Store(true)
	go mqttbus.Coalesce(ctx, 250*time.Millisecond, dirty, critical, func() {
		op, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if b.Publish(op, mqttbus.Root+"zone/"+z.ID+"/summary", z.Summary(time.Now()), 1, true, uint32(cfg.NodeTTLSeconds)) == nil {
			publications.Add(1)
		}
	})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				mark()
				ready.Store(RPC(ctx, b, "central", "PING", struct{}{}, nil) == nil)
			}
		}
	}()
	mux := http.NewServeMux()
	Health(mux, ready.Load)
	mux.HandleFunc("GET /service/probe", func(w http.ResponseWriter, r *http.Request) {
		peer := r.Header.Get("X-Verified-Identity")
		if !strings.HasPrefix(peer, "zone-") || !cfg.HasZone(strings.TrimPrefix(peer, "zone-")) {
			http.Error(w, "zone certificate required", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Write(make([]byte, 64<<10))
	})
	if cfg.DiscoveryEnabled {
		if e := ZoneDiscovery(mux, cfg, z.ID, b); e != nil {
			return e
		}
	}
	return Serve(ctx, Env("CONTROLLER_HTTP_ADDR", ":8088"), mux)
}
