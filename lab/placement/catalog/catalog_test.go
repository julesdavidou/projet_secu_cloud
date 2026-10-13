package catalog

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

func TestCommitRedeliveryRollbackRestartAndMobility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "central.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	n := placement.Node{ID: "A1", Zone: "A", State: placement.Available, Available: 20, Generation: 1, Version: 1, Updated: now, Heartbeat: now}
	e := Event{ID: "node-1", Op: "NODE", Node: n}
	if _, err = c.Apply(e, now); err != nil {
		t.Fatal(err)
	}
	m := placement.Manifest{RequestID: "req", VideoID: "v", SourceNodeID: "A1", Total: 7, Segments: []placement.Segment{{ID: 0, Size: 3}, {ID: 1, Size: 4}}}
	p := placement.DistributedPlan{RequestID: "req", VideoID: "v", State: "RESERVED", Reservations: map[string]string{"A": "r"}, Assignments: []placement.Assignment{{Extent: placement.Extent{ID: 0, First: 0, Last: 0, Size: 3}, NodeID: "A1", ZoneID: "A", ReservationID: "r"}, {Extent: placement.Extent{ID: 1, First: 1, Last: 1, Size: 4}, NodeID: "A1", ZoneID: "A", ReservationID: "r"}}}
	plan := Event{ID: "plan", Op: "PLAN", Manifest: m, Plan: p, Expires: now.Add(time.Minute)}
	bad := plan
	bad.Plan = placement.DistributedPlan{}
	if _, err = c.Apply(bad, now); err == nil {
		t.Fatal("invalid plan accepted")
	}
	s, _ := c.Snapshot()
	if len(s.Videos) != 0 || s.Events != 1 {
		t.Fatal("non-atomic rollback", s)
	}
	if _, err = c.Apply(plan, now); err != nil {
		t.Fatal(err)
	}
	lookup, err := c.Lookup("req")
	if err != nil || !lookup.Found || !reflect.DeepEqual(lookup.Manifest, m) || lookup.State != "RESERVED" {
		t.Fatal("request reconciliation", lookup, err)
	}
	s, _ = c.Snapshot()
	if s.Videos["v"] != "RESERVED" {
		t.Fatal("premature ready")
	}
	store := Event{ID: "store-0", Op: "STORE_COMPLETE", VideoID: "v", ExtentID: 0, NodeID: "A1", ReservationID: "r"}
	r, err := c.Apply(store, now)
	if err != nil || r.State != "RESERVED" {
		t.Fatal(r, err)
	}
	before, _ := c.Snapshot()
	again, err := c.Apply(store, now)
	after, _ := c.Snapshot()
	if err != nil || again != r || !reflect.DeepEqual(before, after) {
		t.Fatal("duplicate commit", err)
	}
	conflict := store
	conflict.NodeID = "bad"
	if _, err = c.Apply(conflict, now); err == nil {
		t.Fatal("event collision")
	}
	store.ID = "store-1"
	store.ExtentID = 1
	if r, err = c.Apply(store, now); err != nil || r.State != "READY" {
		t.Fatal(r, err)
	}
	before, _ = c.Snapshot()
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	after, _ = c.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart persistence")
	}
	lookup, err = c.Lookup("req")
	if err != nil || !lookup.Found || lookup.State != "READY" {
		t.Fatal("durable reconciliation", lookup, err)
	}
	n.Zone = "B"
	n.Generation = 2
	e = Event{ID: "move-2", Op: "NODE", Node: n}
	if _, err = c.Apply(e, now); err != nil {
		t.Fatal(err)
	}
	n.Zone = "A"
	n.Generation = 1
	e = Event{ID: "late-1", Op: "NODE", Node: n}
	if _, err = c.Apply(e, now); err != nil {
		t.Fatal(err)
	}
	after, _ = c.Snapshot()
	for _, l := range after.Locations {
		if l.NodeID != "A1" || l.ZoneID != "B" {
			t.Fatal("mobility rewrote extent or accepted stale generation")
		}
	}
}

func TestRollbackMidEventAndCanceledCompletion(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	now := time.Now()
	n := placement.Node{ID: "n", Zone: "A", State: placement.Available, Generation: 1, Version: 1, Updated: now, Heartbeat: now}
	_, err = c.Apply(Event{ID: "node", Op: "NODE", Node: n}, now)
	if err != nil {
		t.Fatal(err)
	}
	m := placement.Manifest{RequestID: "r", VideoID: "v", SourceNodeID: "n", Total: 2, Segments: []placement.Segment{{ID: 0, Size: 1}, {ID: 1, Size: 1}}}
	p := placement.DistributedPlan{RequestID: "r", VideoID: "v", State: "RESERVED", Reservations: map[string]string{"A": "lease"}, Assignments: []placement.Assignment{{Extent: placement.Extent{ID: 0, First: 0, Last: 0, Size: 1}, NodeID: "n", ZoneID: "A", ReservationID: "lease"}, {Extent: placement.Extent{ID: 1, First: 1, Last: 1, Size: 1}, NodeID: "n", ZoneID: "A", ReservationID: "lease"}}}
	event := Event{ID: "plan", Op: "PLAN", Manifest: m, Plan: p, Expires: now.Add(time.Minute)}
	_, err = c.db.Exec(`CREATE TRIGGER inject_crash BEFORE INSERT ON extents WHEN NEW.extent_id=1 BEGIN SELECT RAISE(ABORT,'injected event failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply(event, now); err == nil {
		t.Fatal("injected failure ignored")
	}
	s, _ := c.Snapshot()
	if len(s.Videos) != 0 || len(s.Locations) != 0 || s.Events != 1 {
		t.Fatal("partial transaction survived", s)
	}
	_, _ = c.db.Exec("DROP TRIGGER inject_crash")
	if _, err = c.Apply(event, now); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply(Event{ID: "cancel", Op: "CANCEL", VideoID: "v"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply(Event{ID: "late", Op: "STORE_COMPLETE", VideoID: "v", ExtentID: 0, NodeID: "n", ReservationID: "lease"}, now); err == nil {
		t.Fatal("canceled video became stored")
	}
}
