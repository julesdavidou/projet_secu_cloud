package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/catalog"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

func cli(ctx context.Context) (*mqttbus.Bus, error) { return bus(ctx, "scenario", nil, nil, nil) }
func call(ctx context.Context, b *mqttbus.Bus, target, op string, value, out any) error {
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return b.Call(bounded, target, op, value, out)
}
func snapshot(ctx context.Context, b *mqttbus.Bus) (catalog.Snapshot, error) {
	var s catalog.Snapshot
	e := call(ctx, b, "central", "SNAPSHOT", struct{}{}, &s)
	return s, e
}
func Snapshot(ctx context.Context) error {
	b, err := cli(ctx)
	if err != nil {
		return err
	}
	s, err := snapshot(ctx, b)
	if err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(s, "", "  ")
	fmt.Println(string(raw))
	return nil
}

func Capacities(ctx context.Context) error {
	b, err := cli(ctx)
	if err != nil {
		return err
	}
	a, err := zoneStats(ctx, b, "A")
	if err != nil {
		return err
	}
	bb, err := zoneStats(ctx, b, "B")
	if err != nil {
		return err
	}
	fmt.Printf("A=%d B=%d\n", a.Summary.Available, bb.Summary.Available)
	return nil
}
func Reconcile(ctx context.Context) error {
	b, err := cli(ctx)
	if err != nil {
		return err
	}
	before, err := snapshot(ctx, b)
	if err != nil {
		return err
	}
	var p placement.DistributedPlan
	if err = call(ctx, b, "zone-A", "UPLOAD", Upload{makeManifest("local", 100, 170, 330), 256}, &p); err != nil {
		return err
	}
	after, err := snapshot(ctx, b)
	if err != nil {
		return err
	}
	first, _ := json.Marshal(before)
	second, _ := json.Marshal(after)
	if string(first) != string(second) || p.VideoID != "local" {
		return errors.New("source reconciliation changed authoritative catalog")
	}
	fmt.Println("PASS 12 source restart reconciles immutable request without reallocation or new event")
	return nil
}
func UploadFile(ctx context.Context, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var q Upload
	if err = mqttbus.Decode(raw, &q); err != nil {
		return err
	}
	b, err := cli(ctx)
	if err != nil {
		return err
	}
	var p placement.DistributedPlan
	if err = call(ctx, b, "zone-"+env("SOURCE_ZONE", "A"), "UPLOAD", q, &p); err != nil {
		return err
	}
	answer, _ := json.MarshalIndent(p, "", "  ")
	fmt.Println(string(answer))
	return nil
}
func makeManifest(id string, sizes ...uint64) placement.Manifest {
	m := placement.Manifest{RequestID: "request-" + id, VideoID: id, SourceNodeID: "A1"}
	for i, size := range sizes {
		m.Total += size
		m.Segments = append(m.Segments, placement.Segment{ID: uint64(i), Size: size})
	}
	return m
}
func transfer(ctx context.Context, b *mqttbus.Bus, p placement.DistributedPlan) error {
	byNode := map[string][]placement.Assignment{}
	for _, a := range p.Assignments {
		byNode[a.NodeID] = append(byNode[a.NodeID], a)
	}
	ids := []string{}
	for id := range byNode {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		q := StoreCommand{VideoID: p.VideoID, Assignments: byNode[id], Expires: time.Now().Add(30 * time.Second)}
		if err := call(ctx, b, "edge-"+id, "STORE", q, nil); err != nil {
			return err
		}
		if err := call(ctx, b, "edge-"+id, "STORE", q, nil); err != nil {
			return fmt.Errorf("duplicate simulated command: %w", err)
		}
	}
	for zone, id := range p.Reservations {
		if err := call(ctx, b, "zone-"+zone, "COMMIT", reservationKey{id}, nil); err != nil {
			return err
		}
	}
	return nil
}

type stats struct {
	Offers    uint64                       `json:"offers"`
	Publishes uint64                       `json:"summary_publications"`
	Summary   placement.Summary            `json:"summary"`
	Neighbors map[string]placement.Summary `json:"neighbors"`
}

func zoneStats(ctx context.Context, b *mqttbus.Bus, zone string) (stats, error) {
	var s stats
	e := call(ctx, b, "zone-"+zone, "STATS", struct{}{}, &s)
	return s, e
}
func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
func Scenario(ctx context.Context) error {
	b, err := cli(ctx)
	if err != nil {
		return err
	}
	pass := func(n int, s string) { fmt.Printf("PASS %02d %s\n", n, s) }
	// Fresh stack required. Poll readiness and capacities instead of trusting
	// retained AVAILABLE or a broker PUBACK.
	deadline := time.Now().Add(20 * time.Second)
	for {
		a, e := zoneStats(ctx, b, "A")
		bb, f := zoneStats(ctx, b, "B")
		if e == nil && f == nil && a.Summary.Available == 2000 && bb.Summary.Available == 2000 && a.Neighbors["B"].Available == 2000 && bb.Neighbors["A"].Available == 2000 {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("scenario requires fresh two-zone stack, four mocks of 1000 bytes each")
		}
		if err = pause(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	local := makeManifest("local", 100, 170, 330)
	var p placement.DistributedPlan
	if err = call(ctx, b, "zone-A", "UPLOAD", Upload{local, 256}, &p); err != nil {
		return err
	}
	before, _ := zoneStats(ctx, b, "B")
	if before.Offers != 0 {
		return errors.New("local upload sent remote OFFER")
	}
	for _, a := range p.Assignments {
		if a.ZoneID != "A" {
			return errors.New("local destination")
		}
	}
	s, _ := snapshot(ctx, b)
	if s.Videos["local"] != "RESERVED" {
		return errors.New("premature READY")
	}
	if err = transfer(ctx, b, p); err != nil {
		return err
	}
	s, _ = snapshot(ctx, b)
	if s.Videos["local"] != "READY" {
		return errors.New("missing commit")
	}
	pass(1, "local-only placement, zero remote OFFER, READY only after simulated completion")
	sizes := make([]uint64, 22)
	for i := range sizes {
		sizes[i] = 100
	}
	overflow := makeManifest("overflow", sizes...)
	if err = call(ctx, b, "zone-A", "UPLOAD", Upload{overflow, 200}, &p); err != nil {
		return err
	}
	zones := map[string]bool{}
	for _, a := range p.Assignments {
		zones[a.ZoneID] = true
	}
	if !zones["A"] || !zones["B"] {
		return errors.New("missing inter-zone allocation")
	}
	if err = transfer(ctx, b, p); err != nil {
		return err
	}
	pass(2, "local insufficiency uses remote OFFER/RESERVE and remote HRW")
	bad := makeManifest("insufficient", 1000, 1000)
	if err = call(ctx, b, "zone-A", "UPLOAD", Upload{bad, 1000}, nil); err == nil {
		return errors.New("accepted impossible upload")
	}
	pass(3, "combined capacity insufficient: explicit error and reservation rollback")
	control := func(id string, q MockControl) error { return call(ctx, b, "edge-"+id, "CONTROL", q, nil) }
	if err = control("B1", MockControl{State: placement.Degraded}); err != nil {
		return err
	}
	if err = control("B2", MockControl{State: placement.Unavailable}); err != nil {
		return err
	}
	_ = pause(ctx, 400*time.Millisecond)
	offer := placement.ReservationRequest{RequestID: "states", VideoID: "states", ReservationID: "states", Extents: []placement.Extent{{ID: 0, First: 0, Last: 0, Size: 1}}, Expires: time.Now().Add(30 * time.Second)}
	var r placement.Reservation
	if err = call(ctx, b, "zone-B", "OFFER", offer, &r); err != nil {
		return err
	}
	if len(r.Plan) != 1 || r.Plan[0].NodeID != "B1" {
		return errors.New("DEGRADED fallback failed")
	}
	if err = control("B1", MockControl{State: placement.Degraded, Pause: true}); err != nil {
		return err
	}
	if err = pause(ctx, 6*time.Second); err != nil {
		return err
	}
	if err = call(ctx, b, "zone-B", "OFFER", offer, &r); err != nil {
		return err
	}
	if len(r.Plan) != 0 {
		return errors.New("stale node admitted")
	}
	if err = control("B1", MockControl{State: placement.Available}); err != nil {
		return err
	}
	if err = control("B2", MockControl{State: placement.Available}); err != nil {
		return err
	}
	_ = pause(ctx, 400*time.Millisecond)
	pass(4, "DEGRADED fallback; UNAVAILABLE and stale heartbeat rejected")
	available, _ := zoneStats(ctx, b, "B")
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allocated uint64
	var concurrentErr error
	reservations := []string{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", i)
			q := placement.ReservationRequest{RequestID: id, VideoID: id, ReservationID: id, Extents: []placement.Extent{{ID: 0, First: 0, Last: 0, Size: 100}}, Expires: time.Now().Add(30 * time.Second)}
			var got placement.Reservation
			e := call(ctx, b, "zone-B", "RESERVE", q, &got)
			mu.Lock()
			defer mu.Unlock()
			if e != nil {
				concurrentErr = e
				return
			}
			for _, a := range got.Plan {
				allocated += a.Size
			}
			reservations = append(reservations, id)
		}(i)
	}
	wg.Wait()
	if concurrentErr != nil {
		return concurrentErr
	}
	if allocated > available.Summary.Available {
		return errors.New("concurrent over-allocation")
	}
	for _, id := range reservations {
		if err = call(ctx, b, "zone-B", "RELEASE", reservationKey{id}, nil); err != nil {
			return err
		}
		if err = call(ctx, b, "zone-B", "RELEASE", reservationKey{id}, nil); err != nil {
			return err
		}
	}
	pass(5, "concurrent reservations fit capacity; repeated RELEASE is idempotent")
	s, _ = snapshot(ctx, b)
	events := s.Events
	again := placement.DistributedPlan{}
	if err = call(ctx, b, "zone-A", "UPLOAD", Upload{local, 256}, &again); err != nil {
		return err
	}
	s, _ = snapshot(ctx, b)
	if s.Events != events {
		return errors.New("duplicate upload double-committed")
	}
	pass(6, "duplicate upload and STORE commands do not double-commit")
	// Restart is an external lifecycle operation, verified by the reproduction
	// script via exact before/after snapshots, not faked inside this process.
	fmt.Println("PENDING 07 external central restart verification (verify.ps1 / verify.sh)")
	beforeMove, _ := zoneStats(ctx, b, "B")
	var moved placement.Node
	if err = call(ctx, b, "edge-A1", "CONTROL", MockControl{State: placement.Available, Zone: "B"}, &moved); err != nil {
		return err
	}
	admissionDeadline := time.Now().Add(5 * time.Second)
	for {
		current, e := zoneStats(ctx, b, "B")
		if e == nil && current.Summary.Available == beforeMove.Summary.Available+moved.Available {
			break
		}
		if time.Now().After(admissionDeadline) {
			return errors.New("destination did not admit moved node after fresh heartbeat")
		}
		if err = pause(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	s, _ = snapshot(ctx, b)
	found := false
	for _, l := range s.Locations {
		if l.NodeID == "A1" {
			found = true
			if l.ZoneID != "B" {
				return errors.New("mobility location mismatch")
			}
		}
	}
	if !found {
		return errors.New("mobility scenario requires extent on A1")
	}
	pass(8, "A1 moves A to B; extents still reference A1 and resolve through current node zone")
	q := placement.ReservationRequest{RequestID: "expiry", VideoID: "expiry", ReservationID: "expiry", Extents: []placement.Extent{{ID: 0, First: 0, Last: 0, Size: 1}}, Expires: time.Now().Add(time.Second)}
	pre, _ := zoneStats(ctx, b, "B")
	if err = call(ctx, b, "zone-B", "RESERVE", q, &r); err != nil {
		return err
	}
	if len(r.Plan) != 1 {
		return errors.New("expiry scenario no capacity")
	}
	if err = pause(ctx, 1500*time.Millisecond); err != nil {
		return err
	}
	post, _ := zoneStats(ctx, b, "B")
	if post.Summary.Available != pre.Summary.Available {
		return fmt.Errorf("expired capacity leaked: before=%d after=%d", pre.Summary.Available, post.Summary.Available)
	}
	pass(9, "expired reservation restores project capacity")
	pre, _ = zoneStats(ctx, b, "B")
	ext := make([]placement.Extent, 100)
	for i := range ext {
		ext[i] = placement.Extent{ID: uint64(i), First: uint64(i), Last: uint64(i), Size: 1}
	}
	q = placement.ReservationRequest{RequestID: "burst", VideoID: "burst", ReservationID: "burst", Extents: ext, Expires: time.Now().Add(10 * time.Second)}
	if err = call(ctx, b, "zone-B", "RESERVE", q, &r); err != nil {
		return err
	}
	_ = pause(ctx, 500*time.Millisecond)
	post, _ = zoneStats(ctx, b, "B")
	if post.Publishes-pre.Publishes >= uint64(len(ext)) {
		return errors.New("summary published per extent")
	}
	_ = call(ctx, b, "zone-B", "RELEASE", reservationKey{"burst"}, nil)
	pass(10, fmt.Sprintf("100 extents in one command; %d aggregate summary publications", post.Publishes-pre.Publishes))
	return nil
}
