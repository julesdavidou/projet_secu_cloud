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
