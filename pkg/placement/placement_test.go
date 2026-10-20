package placement

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func manifest(sizes ...uint64) Manifest {
	m := Manifest{RequestID: "req", VideoID: "video", SourceNodeID: "A1"}
	for i, s := range sizes {
		m.Segments = append(m.Segments, Segment{uint64(i), s})
		m.Total += s
	}
	return m
}
func node(id, zone string, capacity uint64, now time.Time) Node {
	return Node{ID: id, Zone: zone, State: Available, Available: capacity, Generation: 1, Version: 1, Updated: now, Heartbeat: now}
}
func TestManifestAndExtents(t *testing.T) {
	m := manifest(3, 5, 11, 2)
	p, e := Extents(m, 8)
	if e != nil || !reflect.DeepEqual(p, []Extent{{0, 0, 1, 8}, {1, 2, 2, 11}, {2, 3, 3, 2}}) {
		t.Fatalf("%v %v", p, e)
	}
	bad := []Manifest{manifest(0), manifest(math.MaxInt64, 1), manifest(math.MaxUint64, 1), manifest()}
	a := manifest(1, 2)
	a.Segments[1].ID = 0
	bad = append(bad, a)
	a = manifest(1)
	a.Total++
	bad = append(bad, a)
	a = manifest(1)
	a.RequestID = "../x"
	bad = append(bad, a)
	a = manifest(1, 1)
	a.Segments[1].ID = 2
	bad = append(bad, a)
	for i, m := range bad {
		if m.Validate() == nil {
			t.Fatalf("accepted %d", i)
		}
	}
	for _, target := range []uint64{0, math.MaxUint64} {
		if _, err := Extents(manifest(1), target); err == nil {
			t.Fatal("target")
		}
	}
}
func TestHRWDeterminismAndEligibility(t *testing.T) {
	now := time.Now()
	ns := []Node{node("A1", "A", 20, now), node("A2", "A", 20, now)}
	ext, _ := Extents(manifest(3, 5, 2, 7), 5)
	a, left, err := Plan("v", "r", ext, ns, now, time.Minute)
	if err != nil || len(left) != 0 {
		t.Fatal(err, left)
	}
	ns[0], ns[1] = ns[1], ns[0]
	b, _, _ := Plan("v", "r", ext, ns, now, time.Minute)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("iteration dependence")
	}
	if !math.IsInf(Score("v", 0, "n", 0), 1) || Score("v", 0, "n", 1) <= 0 {
		t.Fatal("score bounds")
	}
	ns[0].State = Degraded
	ns[1].Available = 3
	p, _, _ := Plan("v", "r", []Extent{{0, 0, 0, 3}, {1, 1, 1, 3}}, ns, now, time.Minute)
	if p[0].NodeID != ns[1].ID || p[1].NodeID != ns[0].ID {
		t.Fatal("state preference", p)
	}
	ns[0].Heartbeat = now.Add(-2 * time.Minute)
	ns[1].State = Unavailable
	_, left, _ = Plan("v", "r", ext, ns, now, time.Minute)
	if len(left) != len(ext) {
		t.Fatal("liveness")
	}
}
func TestDistributionAndLargeManifest(t *testing.T) {
	now := time.Now()
	ns := []Node{node("small", "A", 1, now), node("large", "A", 3, now)}
	wins := map[string]int{}
	for i := uint64(0); i < 10000; i++ {
		p, _, err := Plan("v", "r", []Extent{{i, i, i, 1}}, ns, now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		wins[p[0].NodeID]++
	}
	if wins["large"] < 7000 || wins["large"] > 8000 {
		t.Fatal(wins)
	}
	sizes := make([]uint64, 100000)
	for i := range sizes {
		sizes[i] = uint64(i%13 + 1)
	}
	m := manifest(sizes...)
	ext, err := Extents(m, 100)
	if err != nil {
		t.Fatal(err)
	}
	p, left, err := Plan("v", "r", ext, []Node{node("n", "A", m.Total, now)}, now, time.Minute)
	if err != nil || len(left) > 0 {
		t.Fatal(err)
	}
	var total uint64
	for _, a := range p {
		total += a.Size
	}
	if total != m.Total {
		t.Fatal("capacity accounting")
	}
	ranges := Ranges(p)
	if len(ranges) != 1 || ranges[0].Size != m.Total {
		t.Fatal("range grouping")
	}
}
func TestReservationsConcurrentExpirationAndSnapshots(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	_ = z.Upsert(node("n", "A", 10, now), now)
	rq := ReservationRequest{"req", "v", "r", []Extent{{0, 0, 0, 10}}, now.Add(time.Second), nil}
	offer, err := z.Offer(rq, now)
	if err != nil || len(offer.Plan) != 1 || z.Summary(now).Available != 10 {
		t.Fatal("offer engaged")
	}
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := rq
			r.ReservationID = reservationID("q", string(rune('a'+i)))
			p, e := z.Reserve(r, now)
			if e != nil {
				t.Error(e)
			}
			if len(p.Plan) > 0 {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 || z.Summary(now).Available != 0 {
		t.Fatal("over-allocation")
	}
	if z.Summary(now.Add(2*time.Second)).Available != 10 {
		t.Fatal("expiration leak")
	}
	z = NewZone("A", time.Minute)
	_ = z.Upsert(node("n", "A", 10, now), now)
	a, _ := z.Reserve(rq, now)
	b, e := z.Reserve(rq, now)
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("redelivery")
	}
	conflict := rq
	conflict.VideoID = "other"
	if _, e = z.Reserve(conflict, now); e == nil {
		t.Fatal("identity conflict")
	}
	n := node("n", "A", 0, now)
	n.Version = 2
	n.Included = []string{"r"}
	if e = z.Upsert(n, now); e != nil {
		t.Fatal(e)
	}
	if z.Summary(now).Available != 0 {
		t.Fatal("net capacity")
	}
	if e = z.Release("r"); e != nil {
		t.Fatal(e)
	}
	_ = z.Release("r")
	if _, e = z.Reserve(rq, now); e == nil {
		t.Fatal("revived release")
	}
	n.Version = 3
	n.Available = 10
	n.Included = nil
	_ = z.Upsert(n, now)
	if z.Summary(now).Available != 10 {
		t.Fatal("snapshot reconciliation")
	}
	if z.Upsert(n, now) == nil {
		t.Fatal("stale version")
	}
}

type testPeer struct {
	zone   *Zone
	offers *atomic.Int64
	delay  time.Duration
	gate   chan struct{}
}

func (p testPeer) Offer(ctx context.Context, r ReservationRequest) (Reservation, error) {
	p.offers.Add(1)
	if p.gate != nil {
		p.gate <- struct{}{}
		select {
		case <-ctx.Done():
			return Reservation{}, ctx.Err()
		case <-p.gate:
		}
	}
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return Reservation{}, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	return p.zone.Offer(r, time.Now())
}
func (p testPeer) Reserve(ctx context.Context, r ReservationRequest) (Reservation, error) {
	if err := ctx.Err(); err != nil {
		return Reservation{}, err
	}
	return p.zone.Reserve(r, time.Now())
}
func (p testPeer) Release(ctx context.Context, id string) error { return p.zone.Release(id) }
func TestDistributedBatchesAndRollback(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	_ = z.Upsert(node("A1", "A", 2, now), now)
	cache := NewSummaryCache()
	var offers atomic.Int64
	c := Coordinator{Local: z, Cache: cache, Timeout: 50 * time.Millisecond, SummaryTTL: time.Minute}
	for _, id := range []string{"B", "C", "D"} {
		peer := NewZone(id, time.Minute)
		_ = peer.Upsert(node(id+"1", id, 2, now), now)
		_ = cache.Put(peer.Summary(now), now)
		c.Neighbors = append(c.Neighbors, Neighbor{ID: id, Peer: testPeer{zone: peer, offers: &offers}})
	}
	p, err := c.Place(context.Background(), manifest(1), 1, now, time.Second)
	if err != nil || offers.Load() != 0 {
		t.Fatal("local caused offer", err)
	}
	for zone, id := range p.Reservations {
		if zone == "A" {
			_ = z.Release(id)
		}
	}
	m := manifest(2, 2, 2, 2)
	m.RequestID = "big"
	p, err = c.Place(context.Background(), m, 2, now, 2*time.Second)
	if err != nil || len(p.Reservations) != 4 || offers.Load() != 3 {
		t.Fatal("batch expansion", p, err, offers.Load())
	}
	m.RequestID = "fail"
	_, err = c.Place(context.Background(), m, 2, now, 2*time.Second)
	if !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
}
func TestStaleSummaryAndTimeout(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	cache := NewSummaryCache()
	var count atomic.Int64
	b := NewZone("B", time.Minute)
	_ = b.Upsert(node("B1", "B", 10, now), now)
	s := b.Summary(now.Add(-2 * time.Minute))
	_ = cache.Put(s, now)
	c := Coordinator{Local: z, Cache: cache, Neighbors: []Neighbor{{ID: "B", Peer: testPeer{zone: b, offers: &count, delay: time.Second}}}, Timeout: time.Millisecond, SummaryTTL: time.Minute}
	_, err := c.Place(context.Background(), manifest(1), 1, now, time.Second)
	if !errors.Is(err, ErrCapacity) || count.Load() != 0 {
		t.Fatal(err)
	}
	_ = cache.Put(b.Summary(now), now)
	m := manifest(1)
	m.RequestID = "timeout"
	_, err = c.Place(context.Background(), m, 1, now, time.Second)
	if !errors.Is(err, ErrCapacity) || count.Load() != 1 {
		t.Fatal(err)
	}
}

type batchPeer struct {
	testPeer
	count *atomic.Int64
	gate  chan struct{}
}

func (p batchPeer) Offer(ctx context.Context, r ReservationRequest) (Reservation, error) {
	if p.count.Add(1) == 2 {
		close(p.gate)
	}
	select {
	case <-ctx.Done():
		return Reservation{}, ctx.Err()
	case <-p.gate:
		return p.zone.Offer(r, time.Now())
	}
}

type lostReplyPeer struct{ testPeer }

func (p lostReplyPeer) Reserve(ctx context.Context, r ReservationRequest) (Reservation, error) {
	_, _ = p.zone.Reserve(r, time.Now())
	return Reservation{}, context.DeadlineExceeded
}
func TestInitialFanoutAndLostReserveReply(t *testing.T) {
	now := time.Now()
	cache := NewSummaryCache()
	var count atomic.Int64
	var unused atomic.Int64
	gate := make(chan struct{})
	c := Coordinator{Local: NewZone("A", time.Minute), Cache: cache, Timeout: time.Second, SummaryTTL: time.Minute}
	for _, id := range []string{"B", "C", "D"} {
		z := NewZone(id, time.Minute)
		_ = z.Upsert(node(id+"1", id, 2, now), now)
		_ = cache.Put(z.Summary(now), now)
		c.Neighbors = append(c.Neighbors, Neighbor{ID: id, Peer: batchPeer{testPeer: testPeer{zone: z, offers: &unused}, count: &count, gate: gate}})
	}
	p, err := c.Place(context.Background(), manifest(2, 2, 2), 2, now, 5*time.Second)
	if err != nil || len(p.Assignments) != 3 || count.Load() != 3 {
		t.Fatal("offers must run concurrently, then expand", err, p)
	}
	b := NewZone("B", time.Minute)
	_ = b.Upsert(node("B1", "B", 10, now), now)
	cache = NewSummaryCache()
	_ = cache.Put(b.Summary(now), now)
	c = Coordinator{Local: NewZone("A", time.Minute), Cache: cache, Timeout: time.Second, SummaryTTL: time.Minute, Neighbors: []Neighbor{{ID: "B", Peer: lostReplyPeer{testPeer{zone: b, offers: &unused}}}}}
	_, err = c.Place(context.Background(), manifest(5), 5, now, 5*time.Second)
	if !errors.Is(err, ErrCapacity) || b.Summary(time.Now()).Available != 10 {
		t.Fatal("lost reserve reply leaked capacity", err)
	}
}
func TestMobilityGenerationQuarantinesSource(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	n := node("A1", "A", 10, now)
	_ = z.Upsert(n, now)
	if err := z.Depart("A1", 2); err != nil {
		t.Fatal(err)
	}
	n.Version = 100
	if z.Upsert(n, now) == nil {
		t.Fatal("old owner revived")
	}
	r, e := z.Offer(ReservationRequest{"r", "v", "r", []Extent{{0, 0, 0, 1}}, now.Add(time.Minute), nil}, now)
	if e != nil || len(r.Plan) != 0 {
		t.Fatal("departure admission", e, r)
	}
}

func TestSnapshotAcceptedAfterCallerClock(t *testing.T) {
	captured := time.Now()
	observed := captured.Add(time.Millisecond)
	z := NewZone("A", time.Minute)
	if err := z.Upsert(node("n", "A", 10, observed), observed); err != nil {
		t.Fatal(err)
	}
	q := ReservationRequest{"r", "v", "r", []Extent{{0, 0, 0, 1}}, captured.Add(time.Minute), nil}
	r, err := z.Reserve(q, captured)
	if err != nil || len(r.Plan) != 1 {
		t.Fatal("newer snapshot raced captured caller time", r, err)
	}
}
