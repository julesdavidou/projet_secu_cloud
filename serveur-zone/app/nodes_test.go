package main

import (
	"database/sql"
	"testing"
)

func TestGetNodeScansAllSixColumns(t *testing.T) {
	var err error
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE nodes(id TEXT PRIMARY KEY,hostname TEXT,ip TEXT,status TEXT,storage_max INTEGER,storage_used INTEGER)`)
	if err != nil {
		t.Fatal(err)
	}
	want := Node{ID: "regression", Hostname: "host", IP: "192.0.2.9", Status: "AVAILABLE", StorageMax: 123, StorageUsed: 45}
	if err = registerNode(want); err != nil {
		t.Fatal(err)
	}
	got, err := getNode(want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}
