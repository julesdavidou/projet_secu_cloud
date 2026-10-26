package placement

import (
	"context"
	"math"
	"testing"
	"time"
)

func number(v uint64) *uint64 { return &v }
func TestServiceAdmissionUsesExistingScheduler(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	for _, id := range []string{"fast", "slow"} {
		if e := z.Upsert(Node{ID: id, Zone: "A", State: Available, Available: 100, Generation: 1, Version: 1, Updated: now, Heartbeat: now}, now); e != nil {
			t.Fatal(e)
		}
		rtt := uint64(500)
		if id == "slow" {
			rtt = 100000
		}
		if e := z.Observe(Observation{NodeID: id, ZoneID: "A", Generation: 1, RTTMicros: rtt, ProbeBytesPerSecond: 10000, Rate: 1 << 30, Observed: now}, now); e != nil {
			t.Fatal(e)
		}
	}
	c := Coordinator{Local: z, Cache: NewSummaryCache(), Timeout: time.Second, SummaryTTL: time.Minute}
	m := Manifest{RequestID: "r", VideoID: "v", SourceNodeID: "fast", Total: 10, Segments: []Segment{{ID: 0, Size: 10}}}
	policy := &ServicePolicy{MaxRTTMillis: number(10), Hours: 2, Budget: number(20)}
	quote, e := c.OfferWithPolicy(context.Background(), m, 1, now, time.Minute, policy)
	if e != nil || quote.State != "PLANNED" || quote.Service.Cost != 20 || quote.Assignments[0].NodeID != "fast" {
		t.Fatalf("quote %+v %v", quote, e)
	}
	if len(z.Checkpoint().Reservations) != 0 {
		t.Fatal("quote reserved capacity")
	}
	p, e := c.PlaceWithPolicy(context.Background(), m, 1, now, time.Minute, policy)
	if e != nil || p.Service.Cost != quote.Service.Cost || p.Assignments[0].NodeID != "fast" {
		t.Fatalf("placement %+v %v", p, e)
	}
	// Idempotent reservation identity includes the policy. Caller mutations must
	// not alter the checkpoint or permit another policy under the same identity.
	*policy.Budget = 1
	if _, e = c.PlaceWithPolicy(context.Background(), m, 1, now, time.Minute, policy); e == nil {
		t.Fatal("changed policy accepted")
	}
	if z.Checkpoint().Reservations[reservationID("r", "A")].Request.Policy.Budget == policy.Budget {
		t.Fatal("policy alias")
	}
}
func TestServiceBudgetRollbackAndStaleMeasurement(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	n := Node{ID: "n", Zone: "A", State: Available, Available: 100, Generation: 1, Version: 1, Updated: now, Heartbeat: now}
	_ = z.Upsert(n, now)
	_ = z.Observe(Observation{NodeID: "n", ZoneID: "A", Generation: 1, RTTMicros: 1, ProbeBytesPerSecond: 1, Rate: 1 << 30, Observed: now}, now)
	c := Coordinator{Local: z, Cache: NewSummaryCache(), Timeout: time.Second, SummaryTTL: time.Minute}
	m := Manifest{RequestID: "r", VideoID: "v", SourceNodeID: "n", Total: 10, Segments: []Segment{{ID: 0, Size: 10}}}
	if _, e := c.PlaceWithPolicy(context.Background(), m, 1, now, time.Minute, &ServicePolicy{Hours: 1, Budget: number(9)}); e == nil {
		t.Fatal("over budget accepted")
	}
	if z.Checkpoint().Reservations[reservationID("r", "A")].State != "RELEASED" {
		t.Fatal("budget failure leaked reservation")
	}
	m.RequestID = "new"
	if _, e := c.PlaceWithPolicy(context.Background(), m, 1, now.Add(31*time.Second), time.Minute, &ServicePolicy{Hours: 1}); e != ErrCapacity {
		t.Fatalf("stale observation accepted: %v", e)
	}
	// An unknown observation must not affect legacy uploads without policy.
	m.RequestID = "legacy"
	if _, e := c.Place(context.Background(), m, 1, now.Add(31*time.Second), time.Minute); e != nil {
		t.Fatal(e)
	}
}
func TestPriceRoundingAndOverflow(t *testing.T) {
	now := time.Now()
	p := []Assignment{{Extent: Extent{Size: 1}, NodeID: "n", ZoneID: "A"}, {Extent: Extent{Size: 1}, NodeID: "n", ZoneID: "A"}}
	o := map[string]Observation{"n": {NodeID: "n", ZoneID: "A", Observed: now, Rate: 1}}
	q, e := Quote(&ServicePolicy{Hours: 1}, p, o, now)
	if e != nil || q.Cost != 1 {
		t.Fatalf("round each node once %+v %v", q, e)
	}
	p[0].Size = math.MaxUint64
	o["n"] = Observation{NodeID: "n", ZoneID: "A", Observed: now, Rate: math.MaxUint64}
	if _, e = Quote(&ServicePolicy{Hours: 8760}, p, o, now); e == nil {
		t.Fatal("overflow accepted")
	}
}

type serviceClockPeer struct {
	zone *Zone
	now  time.Time
}

func (p serviceClockPeer) Offer(_ context.Context, r ReservationRequest) (Reservation, error) {
	return p.zone.Offer(r, p.now)
}
func (p serviceClockPeer) Reserve(_ context.Context, r ReservationRequest) (Reservation, error) {
	return p.zone.Reserve(r, p.now)
}
func (p serviceClockPeer) Release(_ context.Context, id string) error { return p.zone.Release(id) }

func TestDistributedServiceObservationCollectedDuringRequest(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "reserve", true: "quote"}[dry], func(t *testing.T) {
			start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
			measured := start.Add(time.Second)
			finished := start.Add(2 * time.Second)
			a, b := NewZone("A", time.Minute), NewZone("B", time.Minute)
			if e := b.Upsert(node("B1", "B", 100, start), start); e != nil {
				t.Fatal(e)
			}
			o := Observation{NodeID: "B1", ZoneID: "B", Generation: 1, RTTMicros: 1, ProbeBytesPerSecond: 10000, Rate: 1, Observed: measured}
			if e := b.Observe(o, measured); e != nil {
				t.Fatal(e)
			}
			cache := NewSummaryCache()
			if e := cache.Put(b.Summary(start), start); e != nil {
				t.Fatal(e)
			}
			c := Coordinator{Local: a, Cache: cache, Timeout: time.Second, SummaryTTL: time.Minute, Clock: func() time.Time { return finished }, Neighbors: []Neighbor{{ID: "B", Peer: serviceClockPeer{b, finished}}}}
			m := Manifest{RequestID: "during-request", VideoID: "v", SourceNodeID: "A1", Total: 10, Segments: []Segment{{ID: 0, Size: 10}}}
			plan, e := c.distribute(context.Background(), m, 1, start, time.Minute, &ServicePolicy{Hours: 1}, dry)
			if e != nil || len(plan.Assignments) != 1 || plan.Assignments[0].ZoneID != "B" {
				t.Fatalf("legitimate measurement received during RPC rejected: %+v %v", plan, e)
			}
			if _, e := Quote(&ServicePolicy{Hours: 1}, plan.Assignments, plan.Service.Observations, start); e == nil {
				t.Fatal("regression fixture did not exercise the earlier timestamp")
			}
			for _, invalidTime := range []time.Time{measured.Add(-time.Nanosecond), measured.Add(31 * time.Second)} {
				if _, e := Quote(&ServicePolicy{Hours: 1}, plan.Assignments, plan.Service.Observations, invalidTime); e == nil {
					t.Fatal("future or expired observations must still be refused")
				}
			}
		})
	}
}

func TestZoneServiceObservationAcceptedAfterCallerClock(t *testing.T) {
	start := time.Now()
	measured := start.Add(time.Millisecond)
	z := NewZone("A", time.Minute)
	if e := z.Upsert(node("A1", "A", 100, start), start); e != nil {
		t.Fatal(e)
	}
	if e := z.Observe(Observation{NodeID: "A1", ZoneID: "A", Generation: 1, RTTMicros: 1, ProbeBytesPerSecond: 10000, Rate: 1, Observed: measured}, measured); e != nil {
		t.Fatal(e)
	}
	r, e := z.Offer(ReservationRequest{RequestID: "r", VideoID: "v", ReservationID: "res", Expires: start.Add(time.Minute), Extents: []Extent{{ID: 0, Size: 10}}, Policy: &ServicePolicy{Hours: 1}}, start)
	if e != nil || len(r.Plan) != 1 {
		t.Fatalf("observation validated at ingestion rejected after lock wait: %+v %v", r, e)
	}
}

func TestLostPlanAcknowledgementKeepsReservationLease(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	_ = z.Upsert(Node{ID: "n", Zone: "A", State: Available, Available: 100, Generation: 1, Version: 1, Updated: now, Heartbeat: now}, now)
	c := Coordinator{Local: z, Cache: NewSummaryCache(), Timeout: time.Second, SummaryTTL: time.Minute}
	m := Manifest{RequestID: "lost-reply", VideoID: "v", SourceNodeID: "n", Total: 10, Segments: []Segment{{ID: 0, Size: 10}}}
	first, e := c.Place(context.Background(), m, 1, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	second, e := c.Place(context.Background(), m, 1, now.Add(time.Second), time.Minute)
	if e != nil {
		t.Fatal("retry changed expiry and conflicted", e)
	}
	if first.Reservations["A"] != second.Reservations["A"] || len(z.Checkpoint().Reservations) != 1 {
		t.Fatal("retry created another reservation")
	}
	if !z.Checkpoint().Reservations[first.Reservations["A"]].Request.Expires.Equal(now.Add(time.Minute)) {
		t.Fatal("retry extended lease")
	}
}
