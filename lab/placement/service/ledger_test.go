package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

func TestLedgerFileRecovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDGER_DIR", dir)
	path := filepath.Join(dir, "ledger.json")
	z := placement.NewZone("A", time.Minute)
	if err := restoreLedger(path, z); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	n := placement.Node{ID: "n", Zone: "A", State: placement.Available, Available: 10, Generation: 1, Version: 1, Updated: now, Heartbeat: now}
	_ = z.Upsert(n, now)
	z.SetCheckpointSink(func(c placement.Checkpoint) error { return saveLedger(path, c) })
	q := placement.ReservationRequest{RequestID: "r", VideoID: "v", ReservationID: "r", Extents: []placement.Extent{{ID: 0, First: 0, Last: 0, Size: 6}}, Expires: now.Add(time.Minute)}
	if _, err := z.Reserve(q, now); err != nil {
		t.Fatal(err)
	}
	restored := placement.NewZone("A", time.Minute)
	if err := restoreLedger(path, restored); err != nil {
		t.Fatal(err)
	}
	_ = restored.Upsert(n, now)
	if restored.Summary(now).Available != 4 {
		t.Fatal("durable debit lost")
	}
}
