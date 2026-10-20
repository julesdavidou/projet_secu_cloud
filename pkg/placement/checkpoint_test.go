package placement

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDurableReservationsAndFailClosed(t *testing.T) {
	now := time.Now()
	z := NewZone("A", time.Minute)
	var persisted []byte
	z.SetCheckpointSink(func(c Checkpoint) error { var err error; persisted, err = json.Marshal(c); return err })
	_ = z.Upsert(node("A1", "A", 10, now), now)
	q := ReservationRequest{"r", "v", "r", []Extent{{0, 0, 0, 7}}, now.Add(time.Minute), nil}
	first, err := z.Reserve(q, now)
	if err != nil || len(persisted) == 0 {
		t.Fatal(err)
	}
	var c Checkpoint
	if err = json.Unmarshal(persisted, &c); err != nil {
		t.Fatal(err)
	}
	restored := NewZone("A", time.Minute)
	if err = restored.Restore(c); err != nil {
		t.Fatal(err)
	}
	if restored.Summary(now).Available != 0 {
		t.Fatal("restored liveness")
	}
	_ = restored.Upsert(node("A1", "A", 10, now), now)
	if restored.Summary(now).Available != 3 {
		t.Fatal("restored debit")
	}
	again, err := restored.Reserve(q, now)
	if err != nil || len(again.Plan) != len(first.Plan) {
		t.Fatal("restored idempotency", err)
	}
	_ = restored.Release("r")
	if restored.Summary(now).Available != 10 {
		t.Fatal("restored release")
	}
	z.SetCheckpointSink(func(Checkpoint) error { return errors.New("disk failure") })
	if err = z.Release("r"); err == nil {
		t.Fatal("acknowledged non-durable release")
	}
	if z.Summary(now).State != Unavailable {
		t.Fatal("failed ledger admitted")
	}
	q.ReservationID = "new"
	if _, err = z.Reserve(q, now); err == nil {
		t.Fatal("failed persistence permitted reserve")
	}
}
