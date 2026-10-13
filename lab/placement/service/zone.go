package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/catalog"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type Upload struct {
	Manifest placement.Manifest `json:"manifest"`
	Target   uint64             `json:"target_size_bytes"`
}
type uploadRecord struct {
	body [32]byte
	plan placement.DistributedPlan
}

func Zone(ctx context.Context) error {
	return ZoneWithRegistry(ctx, placement.NewZone(env("ZONE_ID", "A"), duration("NODE_TTL", "5s")))
}

// ZoneWithRegistry is the transport adapter used by serveur-zone/app/allocator.go.
func ZoneWithRegistry(ctx context.Context, z *placement.Zone) error {
	id := z.ID
	ledgerPath := env("LEDGER_PATH", "/data/placement-ledger.json")
	if err := restoreLedger(ledgerPath, z); err != nil {
		return err
	}
	z.SetCheckpointSink(func(c placement.Checkpoint) error { return saveLedger(ledgerPath, c) })
	cache := placement.NewSummaryCache()
	dirty := make(chan struct{}, 1)
	priority := make(chan struct{}, 1)
	mark := func() {
		select {
		case dirty <- struct{}{}:
		default:
		}
	}
	var publishes atomic.Uint64
	var offers atomic.Uint64
	var b *mqttbus.Bus
	ready := make(chan struct{})
	var uploadMu sync.Mutex
	records := map[string]uploadRecord{}
	neighbors := strings.Split(env("NEIGHBORS", "B"), ",")
	coordinator := &placement.Coordinator{Local: z, Cache: cache, Fanout: int(number("FANOUT", "2")), Timeout: duration("RPC_TIMEOUT", "2s"), SummaryTTL: duration("SUMMARY_TTL", "5s")}
	var stateMu sync.Mutex
	beats := map[string]time.Time{}
	state := func(topic string, raw []byte, retained bool) {
		stateMu.Lock()
		defer stateMu.Unlock()
		now := time.Now()
		if strings.HasSuffix(topic, "/summary") {
			var s placement.Summary
			if mqttbus.Decode(raw, &s) == nil && topic == mqttbus.Root+"zone/"+s.ZoneID+"/summary" {
				_ = cache.Put(s, now)
			}
			return
		}
		var n placement.Node
		if mqttbus.Decode(raw, &n) != nil {
			return
		}
		prefix := mqttbus.Root + "zone/" + id + "/node/" + n.ID
		if topic != prefix+"/state" && topic != prefix+"/heartbeat" {
			return
		}
		if topic == prefix+"/heartbeat" && !retained {
			beats[n.ID] = now
			n.Heartbeat = now
		} else {
			if beat, ok := beats[n.ID]; ok {
				n.Heartbeat = beat
			} else {
				n.State = placement.Unavailable
			}
		}
		if z.Upsert(n, now) == nil {
			mark()
			if n.State == placement.Unavailable {
				select {
				case priority <- struct{}{}:
				default:
				}
			}
		}
	}
	h := func(opctx context.Context, sender string, r mqttbus.Request) (any, error) {
		<-ready
		switch r.Op {
		case "OFFER", "RESERVE":
			if err := authorized(sender, "zone-A", "zone-B", "scenario"); err != nil {
				return nil, err
			}
			var q placement.ReservationRequest
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			if r.Op == "OFFER" {
				offers.Add(1)
				return z.Offer(q, time.Now())
			}
			result, err := z.Reserve(q, time.Now())
			mark()
			return result, err
		case "RELEASE", "COMMIT":
			if err := authorized(sender, "zone-A", "zone-B", "scenario"); err != nil {
				return nil, err
			}
			var q reservationKey
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			var err error
			if r.Op == "RELEASE" {
				err = z.Release(q.ID)
			} else {
				err = z.Commit(q.ID, time.Now())
			}
			mark()
			return map[string]bool{"accepted": err == nil}, err
		case "DEPART":
			if err := authorized(sender, "edge-A1", "edge-A2", "edge-B1", "edge-B2"); err != nil {
				return nil, err
			}
			var q struct {
				NodeID     string `json:"node_id"`
				Generation uint64 `json:"generation"`
			}
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			if sender != "edge-"+q.NodeID {
				return nil, errors.New("node departure identity mismatch")
			}
			err := z.Depart(q.NodeID, q.Generation)
			mark()
			return map[string]bool{"accepted": err == nil}, err
		case "STATS":
			if err := authorized(sender, "scenario"); err != nil {
				return nil, err
			}
			peers := map[string]placement.Summary{}
			for _, n := range coordinator.Neighbors {
				if s, ok := cache.Get(n.ID, time.Now(), coordinator.SummaryTTL); ok {
					peers[n.ID] = s
				}
			}
			return map[string]any{"offers": offers.Load(), "summary_publications": publishes.Load(), "summary": z.Summary(time.Now()), "neighbors": peers}, nil
		case "UPLOAD":
			if err := authorized(sender, "scenario"); err != nil {
				return nil, err
			}
			var q Upload
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			if err := q.Manifest.Validate(); err != nil {
				return nil, err
			}
			uploadMu.Lock()
			defer uploadMu.Unlock()
			body, _ := json.Marshal(q)
			bodyHash := sha256.Sum256(body)
			if old, ok := records[q.Manifest.RequestID]; ok {
				if old.body != bodyHash {
					return nil, errors.New("upload request identity conflict")
				}
				return old.plan, nil
			}
			now := time.Now()
			var previous catalog.Lookup
			if err := b.Retry(opctx, "central", "LOOKUP", map[string]string{"request_id": q.Manifest.RequestID}, &previous); err != nil {
				return nil, errors.New("catalog unavailable; no new reservation admitted")
			}
			if previous.Found {
				original, _ := json.Marshal(previous.Manifest)
				incoming, _ := json.Marshal(q.Manifest)
				expected, e := placement.Extents(q.Manifest, q.Target)
				if e != nil {
					return nil, e
				}
				if string(original) != string(incoming) || len(expected) != len(previous.Plan.Assignments) {
					return nil, errors.New("reconciled request identity conflict")
				}
				for i, a := range previous.Plan.Assignments {
					if a.Extent != expected[i] {
						return nil, errors.New("reconciled extent target conflict")
					}
				}
				if previous.State == "CANCELED" || (previous.State != "READY" && !now.Before(previous.Expires)) {
					return nil, errors.New("catalog request is canceled or expired; new request identity required")
				}
				records[q.Manifest.RequestID] = uploadRecord{bodyHash, previous.Plan}
				return previous.Plan, nil
			}
			lease := duration("RESERVATION_TTL", "60s")
			now = time.Now()
			p, err := coordinator.Place(opctx, q.Manifest, q.Target, now, lease)
			mark()
			if err != nil {
				return nil, err
			}
			event := catalog.Event{ID: eventID(q.Manifest.RequestID, "-plan"), Op: "PLAN", Manifest: q.Manifest, Plan: p, Expires: now.Add(lease)}
			var result catalog.Result
			if err = b.Retry(opctx, "central", "EVENT", event, &result); err != nil {
				// Outcome may be unknown after loss of the commit reply. Preserve
				// leases, don't start storage; manual reconciliation is required.
				return nil, errors.New("catalog commit unconfirmed; reservations retained until lease expiry; reconcile request " + q.Manifest.RequestID)
			}
			records[q.Manifest.RequestID] = uploadRecord{bodyHash, p}
			return p, nil
		default:
			return nil, errors.New("unknown zone operation")
		}
	}
	var err error
	b, err = bus(ctx, "zone-"+id, []string{mqttbus.Root + "zone/+/summary", mqttbus.Root + "zone/" + id + "/node/+/state", mqttbus.Root + "zone/" + id + "/node/+/heartbeat"}, h, state)
	if err != nil {
		return err
	}
	for _, n := range neighbors {
		if n != "" {
			coordinator.Neighbors = append(coordinator.Neighbors, placement.Neighbor{ID: n, Peer: remote{b, n}})
		}
	}
	close(ready)
	go mqttbus.Coalesce(ctx, duration("SUMMARY_INTERVAL", "250ms"), dirty, priority, func() {
		op, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		s := z.Summary(time.Now())
		if e := b.Publish(op, mqttbus.Root+"zone/"+id+"/summary", s, 1, true, 5); e == nil {
			publishes.Add(1)
		} else {
			log.Printf("summary publication failed")
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
			}
		}
	}()
	health(ctx)
	<-ctx.Done()
	return nil
}
