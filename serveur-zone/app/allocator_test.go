package main

import (
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"testing"
	"time"
)

func TestAllocatorUsesProjectSnapshotsAndReservations(t *testing.T) {
	now := time.Now()
	a, err := newAllocator("A", 5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m := placement.Manifest{RequestID: "r", VideoID: "v", SourceNodeID: "A1", Total: 9, Segments: []placement.Segment{{ID: 0, Size: 3}, {ID: 1, Size: 6}}}
	offer, err := a.Offer(m, "reservation", now.Add(time.Minute), now)
	if err != nil || len(offer.Plan) != 0 {
		t.Fatal("invented capacity", err)
	}
	if err = a.UpdateProjectCapacity(placement.Node{ID: "A1", Zone: "A", State: placement.Available, Available: 9, Generation: 1, Version: 1, Updated: now, Heartbeat: now}, now); err != nil {
		t.Fatal(err)
	}
	reserved, err := a.Reserve(m, "reservation", now.Add(time.Minute), now)
	if err != nil || len(reserved.Plan) != 2 || len(reserved.Remaining) != 0 {
		t.Fatal(reserved, err)
	}
	if err = a.Release("reservation"); err != nil {
		t.Fatal(err)
	}
	if err = a.Release("reservation"); err != nil {
		t.Fatal(err)
	}
}
