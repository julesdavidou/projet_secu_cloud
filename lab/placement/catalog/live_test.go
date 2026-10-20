package catalog

import (
	"encoding/json"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real SQLite, explicit crypto-verifier double; real IBS is exercised in Docker.
func TestLiveCASReplayAccessEndAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "central.db")
	c, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	p := media.OperationProof{Suite: "verifier-double", Operation: media.Operation{OwnerID: "client-owner", VideoID: "l-" + strings.Repeat("a", 32), Kind: "live_create", Nonce: "live-create-00000000000000001", Live: &media.LiveEvent{Title: "live"}}, Signature: json.RawMessage(`{}`), Zones: json.RawMessage(`[]`)}
	verified := media.OperationVerification{Verified: true, OwnerID: p.Operation.OwnerID}
	state, e := c.ApplyLive(p, verified, now)
	if e != nil || state.Revision != 1 {
		t.Fatal(state, e)
	}
	again, e := c.ApplyLive(p, verified, now)
	if e != nil || again.Commit != state.Commit {
		t.Fatal("replay", e)
	}
	changed := p
	changed.Operation.Live = &media.LiveEvent{Title: "changed"}
	if _, e = c.ApplyLive(changed, verified, now); e == nil {
		t.Fatal("nonce conflict accepted")
	}
	p.Operation.Kind = "live_share"
	p.Operation.Nonce = "live-share-000000000000000001"
	p.Operation.Live = &media.LiveEvent{}
	if _, e = c.ApplyLive(p, verified, now); e == nil {
		t.Fatal("stale head accepted")
	}
	p.Operation.ExpectedRevision = state.Revision
	p.Operation.Previous = state.Commit
	state, e = c.ApplyLive(p, verified, now)
	if e != nil || !state.Public {
		t.Fatal(state, e)
	}
	p.Operation.Kind = "live_end"
	p.Operation.Nonce = "live-end-0000000000000000001"
	p.Operation.ExpectedRevision = state.Revision
	p.Operation.Previous = state.Commit
	state, e = c.ApplyLive(p, verified, now)
	if e != nil || !state.Ended {
		t.Fatal(state, e)
	}
	p.Operation.Kind = "live_append"
	p.Operation.Nonce = "live-append-0000000000000001"
	p.Operation.ExpectedRevision = state.Revision
	p.Operation.Previous = state.Commit
	p.Operation.Live = &media.LiveEvent{DurationMS: 4000, ProgramMS: now.UnixMilli(), InitBytes: 20, InitSHA256: strings.Repeat("b", 64)}
	if _, e = c.ApplyLive(p, verified, now); e == nil {
		t.Fatal("append after end accepted")
	}
	c.Close()
	c, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	saved, e := c.Live(state.ID)
	if e != nil || saved.Commit != state.Commit || !saved.Ended {
		t.Fatal("restart", saved, e)
	}
}

func TestLiveAppendRequiresReadyAndImmutableTimeline(t *testing.T) {
	c, e := Open(filepath.Join(t.TempDir(), "live.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	now := time.Now()
	// Explicit DB fixtures, no claim of physical files or cryptographic verification.
	for _, sql := range []string{
		`INSERT INTO nodes VALUES('node','A',1,'AVAILABLE','now')`,
		`INSERT INTO videos VALUES('child','RESERVED','child','node','future')`,
		`INSERT INTO extents VALUES('child',0,0,0,100,'node','res','RESERVED')`,
	} {
		if _, e = c.db.Exec(sql); e != nil {
			t.Fatal(e)
		}
	}
	p := media.OperationProof{Operation: media.Operation{OwnerID: "client-owner", VideoID: "l-" + strings.Repeat("c", 32), Kind: "live_create", Nonce: "live-test-create-000000000001", Live: &media.LiveEvent{Title: "stream"}}}
	v := media.OperationVerification{Verified: true, OwnerID: p.Operation.OwnerID}
	head, e := c.ApplyLive(p, v, now)
	if e != nil {
		t.Fatal(e)
	}
	p.Operation = media.Operation{OwnerID: head.Owner, VideoID: head.ID, Kind: "live_append", Nonce: "live-test-append-000000000001", ExpectedRevision: head.Revision, Previous: head.Commit, NewVideoID: "child", Live: &media.LiveEvent{DurationMS: 4000, ProgramMS: now.UnixMilli(), InitBytes: 8, InitSHA256: strings.Repeat("a", 64)}}
	if _, e = c.ApplyLive(p, v, now); e == nil {
		t.Fatal("RESERVED fragment visible")
	}
	if _, e = c.db.Exec(`UPDATE videos SET state='READY' WHERE video_id='child'`); e != nil {
		t.Fatal(e)
	}
	head, e = c.ApplyLive(p, v, now)
	if e != nil || len(head.Chunks) != 1 {
		t.Fatal(head, e)
	}
	replay, e := c.ApplyLive(p, v, now)
	if e != nil || replay.Commit != head.Commit {
		t.Fatal("append replay", e)
	}
	p.Operation.Nonce = "live-test-append-000000000002"
	p.Operation.ExpectedRevision = head.Revision
	p.Operation.Previous = head.Commit
	p.Operation.Live = &media.LiveEvent{DurationMS: 4000, ProgramMS: now.UnixMilli() + 1000, InitBytes: 8, InitSHA256: strings.Repeat("a", 64)}
	if _, e = c.ApplyLive(p, v, now); e == nil {
		t.Fatal("overlapping media timeline accepted")
	}
	p.Operation.Live = &media.LiveEvent{DurationMS: 5000, ProgramMS: now.UnixMilli() + 4000, InitBytes: 8, InitSHA256: strings.Repeat("a", 64)}
	if _, e = c.ApplyLive(p, v, now); e == nil || !strings.Contains(e.Error(), "fixed HLS target") {
		t.Fatal("HLS target duration changed", e)
	}
	v.DelegateGrantID = "other"
	if _, e = c.ApplyLive(p, v, now); e == nil {
		t.Fatal("delegation accepted without live grant protocol")
	}
}
