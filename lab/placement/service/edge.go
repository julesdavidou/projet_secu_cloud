package service

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/catalog"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type StoreCommand struct {
	VideoID     string                 `json:"video_id"`
	Assignments []placement.Assignment `json:"assignments"`
	Expires     time.Time              `json:"expires_at"`
}
type MockControl struct {
	State placement.State `json:"state"`
	Pause bool            `json:"pause"`
	Zone  string          `json:"zone_id,omitempty"`
}

func Edge(ctx context.Context) error {
	id := env("NODE_ID", "A1")
	now := time.Now()
	n := placement.Node{ID: id, Zone: env("ZONE_ID", "A"), State: placement.Available, Available: number("PROJECT_CAPACITY_BYTES", "1000"), Generation: 1, Version: 1, Updated: now, Heartbeat: now}
	var mu sync.Mutex
	included := map[string]bool{}
	processed := map[string]string{}
	fingerprints := map[string]string{}
	pause := false
	var b *mqttbus.Bus
	ready := make(chan struct{})
	announce := func(opctx context.Context, retained bool) error {
		n.Version++
		n.Updated = time.Now()
		n.Heartbeat = n.Updated
		n.Included = nil
		for id := range included {
			n.Included = append(n.Included, id)
		}
		sort.Strings(n.Included)
		suffix := "/heartbeat"
		qos := byte(0)
		if retained {
			suffix = "/state"
			qos = 1
		}
		return b.Publish(opctx, mqttbus.Root+"zone/"+n.Zone+"/node/"+id+suffix, n, qos, retained, 5)
	}
	register := func(opctx context.Context) error {
		e := catalog.Event{ID: eventID(id, "-generation-", n.Generation), Op: "NODE", Node: n}
		return b.Retry(opctx, "central", "EVENT", e, nil)
	}
	h := func(opctx context.Context, sender string, r mqttbus.Request) (any, error) {
		<-ready
		mu.Lock()
		defer mu.Unlock()
		switch r.Op {
		case "CONTROL":
			if err := authorized(sender, "scenario"); err != nil {
				return nil, err
			}
			var q MockControl
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			if q.State != placement.Available && q.State != placement.Degraded && q.State != placement.Unavailable {
				return nil, errors.New("invalid mock state")
			}
			if q.Zone != "" && q.Zone != n.Zone {
				if q.Zone != "A" && q.Zone != "B" {
					return nil, errors.New("unknown mock destination")
				}
				next := n.Generation + 1
				if err := b.Call(opctx, "zone-"+n.Zone, "DEPART", map[string]any{"node_id": id, "generation": next}, nil); err != nil {
					return nil, err
				}
				n.Zone = q.Zone
				n.Generation = next
				n.Version++
				n.Updated = time.Now()
				n.Heartbeat = n.Updated
				if err := register(opctx); err != nil {
					return nil, err
				}
			}
			n.State = q.State
			pause = q.Pause
			if !pause {
				if err := announce(opctx, true); err != nil {
					return nil, err
				}
			}
			return n, nil
		case "STORE":
			if err := authorized(sender, "scenario", "zone-A", "zone-B"); err != nil {
				return nil, err
			}
			var q StoreCommand
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			if !placement.ValidID(q.VideoID) || len(q.Assignments) == 0 {
				return nil, errors.New("invalid simulated command")
			}
			reservation := q.Assignments[0].ReservationID
			key := q.VideoID + "/" + reservation
			fingerprint := eventID(string(r.Body))
			if previous, ok := fingerprints[key]; ok && previous != fingerprint {
				return nil, errors.New("mock command identity conflict")
			}
			if previous, ok := processed[key]; ok {
				if previous != fingerprint {
					return nil, errors.New("mock command identity conflict")
				}
				return map[string]bool{"simulated": true, "committed": true}, nil
			}
			if !time.Now().Before(q.Expires) || pause || n.State == placement.Unavailable {
				return nil, errors.New("mock unavailable or command expired")
			}
			var total uint64
			seen := map[uint64]bool{}
			for _, a := range q.Assignments {
				if a.NodeID != id || a.ZoneID != n.Zone || a.ReservationID != reservation || a.Size == 0 || a.Size > math.MaxInt64 || total > math.MaxInt64-a.Size || seen[a.ID] {
					return nil, errors.New("invalid mock node reservation")
				}
				seen[a.ID] = true
				total += a.Size
			}
			// Reserve project bytes before any completion event. On an uncertain
			// outcome retain the debit conservatively; never report it free.
			if !included[reservation] {
				if total > n.Available {
					return nil, errors.New("insufficient mock project capacity")
				}
				fingerprints[key] = fingerprint
				n.Available -= total
				included[reservation] = true
			}
			for _, a := range q.Assignments {
				e := catalog.Event{ID: eventID(q.VideoID, "-extent-", a.ID), Op: "STORE_COMPLETE", VideoID: q.VideoID, ExtentID: a.ID, NodeID: id, ReservationID: reservation}
				if err := b.Retry(opctx, "central", "EVENT", e, nil); err != nil {
					return nil, err
				}
			}
			processed[key] = fingerprint
			if err := announce(opctx, true); err != nil {
				return nil, err
			}
			return map[string]bool{"simulated": true, "committed": true}, nil
		default:
			return nil, errors.New("unknown mock operation")
		}
	}
	var err error
	b, err = bus(ctx, "edge-"+id, nil, h, nil)
	if err != nil {
		return err
	}
	close(ready)
	op, cancel := context.WithTimeout(ctx, 12*time.Second)
	err = register(op)
	cancel()
	if err != nil {
		return err
	}
	op, cancel = context.WithTimeout(ctx, 3*time.Second)
	err = announce(op, true)
	cancel()
	if err != nil {
		return err
	}
	health(ctx)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			mu.Lock()
			if !pause {
				op, cancel := context.WithTimeout(ctx, time.Second)
				_ = announce(op, false)
				cancel()
			}
			mu.Unlock()
		}
	}
}
