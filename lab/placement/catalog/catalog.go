// Package catalog is a temporary reference Central, not the team's zone DB.
// Only this service opens SQLite. Replies are emitted after the business commit.
package catalog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	_ "modernc.org/sqlite"
)

type Catalog struct{ db *sql.DB }
type Event struct {
	ID            string                    `json:"event_id"`
	Op            string                    `json:"op"`
	Node          placement.Node            `json:"node,omitempty"`
	Manifest      placement.Manifest        `json:"manifest,omitempty"`
	Plan          placement.DistributedPlan `json:"plan,omitempty"`
	Expires       time.Time                 `json:"expires_at,omitempty"`
	VideoID       string                    `json:"video_id,omitempty"`
	ExtentID      uint64                    `json:"extent_id,omitempty"`
	NodeID        string                    `json:"node_id,omitempty"`
	ReservationID string                    `json:"reservation_id,omitempty"`
}
type Result struct {
	Committed bool   `json:"committed"`
	State     string `json:"state"`
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes(node_id TEXT PRIMARY KEY,zone_id TEXT NOT NULL,generation INTEGER NOT NULL CHECK(generation>0),state TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS videos(video_id TEXT PRIMARY KEY,state TEXT NOT NULL CHECK(state IN ('RESERVED','READY','CANCELED')),request_id TEXT NOT NULL UNIQUE,source_node_id TEXT NOT NULL REFERENCES nodes(node_id),expires_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS extents(video_id TEXT NOT NULL REFERENCES videos(video_id),extent_id INTEGER NOT NULL,first_segment INTEGER NOT NULL,last_segment INTEGER NOT NULL,size_bytes INTEGER NOT NULL CHECK(size_bytes>0),node_id TEXT NOT NULL REFERENCES nodes(node_id),reservation_id TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('RESERVED','STORED')),PRIMARY KEY(video_id,extent_id));
CREATE INDEX IF NOT EXISTS extents_node ON extents(node_id);
CREATE INDEX IF NOT EXISTS extents_video_state ON extents(video_id,state);
CREATE TABLE IF NOT EXISTS processed_events(event_id TEXT PRIMARY KEY,fingerprint BLOB NOT NULL,result TEXT NOT NULL,processed_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS upload_requests(request_id TEXT PRIMARY KEY REFERENCES videos(request_id),manifest_json TEXT NOT NULL,plan_json TEXT NOT NULL,expires_at TEXT NOT NULL);
`

func Open(path string) (*Catalog, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Catalog, error) { db.Close(); return nil, e }
	for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(q); err != nil {
			return fail(err)
		}
	}
	var mode string
	if err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return fail(err)
	}
	if path != ":memory:" && mode != "wal" {
		return fail(fmt.Errorf("WAL unavailable: %s", mode))
	}
	if _, err = db.Exec(schema + operationSchema); err != nil {
		return fail(err)
	}
	return &Catalog{db}, nil
}
func (c *Catalog) Close() error { return c.db.Close() }

// OwnerZone resolves through node->zone, so mobility doesn't rewrite extents.
func (c *Catalog) OwnerZone(sourceNode, video string) (string, error) {
	var zone string
	var err error
	if video != "" {
		err = c.db.QueryRow(`SELECT n.zone_id FROM videos v JOIN nodes n ON n.node_id=v.source_node_id WHERE v.video_id=?`, video).Scan(&zone)
	} else {
		err = c.db.QueryRow("SELECT zone_id FROM nodes WHERE node_id=?", sourceNode).Scan(&zone)
	}
	return zone, err
}
func (c *Catalog) Apply(e Event, now time.Time) (Result, error) {
	if !placement.ValidID(e.ID) {
		return Result{}, errors.New("invalid event identity")
	}
	raw, _ := json.Marshal(e)
	fp := sha256.Sum256(raw)
	tx, err := c.db.Begin()
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	var oldFP []byte
	var resultJSON string
	err = tx.QueryRow("SELECT fingerprint,result FROM processed_events WHERE event_id=?", e.ID).Scan(&oldFP, &resultJSON)
	if err == nil {
		if string(oldFP) != string(fp[:]) {
			return Result{}, errors.New("event identity conflict")
		}
		var result Result
		err = json.Unmarshal([]byte(resultJSON), &result)
		return result, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	result := Result{Committed: true}
	switch e.Op {
	case "NODE":
		n := e.Node
		if err = n.Validate(now); err != nil {
			return Result{}, err
		}
		if n.Generation > math.MaxInt64 {
			return Result{}, errors.New("generation overflow")
		}
		// Equal generation never changes ownership, even when events reorder.
		_, err = tx.Exec(`INSERT INTO nodes VALUES(?,?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET zone_id=excluded.zone_id,generation=excluded.generation,state=excluded.state,updated_at=excluded.updated_at WHERE excluded.generation>nodes.generation`, n.ID, n.Zone, n.Generation, n.State, n.Updated.Format(time.RFC3339Nano))
		result.State = "NODE_ACCEPTED"
	case "PLAN":
		if err = e.Manifest.Validate(); err != nil {
			return Result{}, err
		}
		p := e.Plan
		if p.RequestID != e.Manifest.RequestID || p.VideoID != e.Manifest.VideoID || p.State != "RESERVED" || len(p.Assignments) == 0 || !now.Before(e.Expires) || e.Expires.Sub(now) > 10*time.Minute {
			return Result{}, errors.New("invalid accepted plan")
		}
		if err = validatePlan(tx, e.Manifest, p); err != nil {
			return Result{}, err
		}
		_, err = tx.Exec("INSERT INTO videos VALUES(?,?,?,?,?)", p.VideoID, "RESERVED", p.RequestID, e.Manifest.SourceNodeID, e.Expires.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return Result{}, err
		}
		for _, a := range p.Assignments {
			_, err = tx.Exec("INSERT INTO extents VALUES(?,?,?,?,?,?,?,?)", p.VideoID, a.ID, a.First, a.Last, a.Size, a.NodeID, a.ReservationID, "RESERVED")
			if err != nil {
				return Result{}, err
			}
		}
		manifestJSON, _ := json.Marshal(e.Manifest)
		planJSON, _ := json.Marshal(e.Plan)
		_, err = tx.Exec("INSERT INTO upload_requests VALUES(?,?,?,?)", p.RequestID, string(manifestJSON), string(planJSON), e.Expires.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return Result{}, err
		}
		result.State = "RESERVED"
	case "STORE_COMPLETE":
		var node, reservation, state, expires string
		err = tx.QueryRow(`SELECT e.node_id,e.reservation_id,v.state,v.expires_at FROM extents e JOIN videos v USING(video_id) WHERE e.video_id=? AND e.extent_id=?`, e.VideoID, e.ExtentID).Scan(&node, &reservation, &state, &expires)
		if err != nil {
			return Result{}, err
		}
		deadline, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			return Result{}, err
		}
		if node != e.NodeID || reservation != e.ReservationID || state == "CANCELED" || (state != "READY" && !now.Before(deadline)) {
			return Result{}, errors.New("unauthorized, canceled or expired completion")
		}
		_, err = tx.Exec("UPDATE extents SET state='STORED' WHERE video_id=? AND extent_id=?", e.VideoID, e.ExtentID)
		if err != nil {
			return Result{}, err
		}
		var pending int
		if err = tx.QueryRow("SELECT count(*) FROM extents WHERE video_id=? AND state!='STORED'", e.VideoID).Scan(&pending); err != nil {
			return Result{}, err
		}
		result.State = "RESERVED"
		if pending == 0 {
			_, err = tx.Exec("UPDATE videos SET state='READY' WHERE video_id=?", e.VideoID)
			result.State = "READY"
		}
	case "CANCEL":
		if !placement.ValidID(e.VideoID) {
			return Result{}, errors.New("invalid video")
		}
		var state string
		err = tx.QueryRow("SELECT state FROM videos WHERE video_id=?", e.VideoID).Scan(&state)
		if err != nil {
			return Result{}, err
		}
		if state == "READY" {
			return Result{}, errors.New("cannot cancel a ready video")
		}
		_, err = tx.Exec("UPDATE videos SET state='CANCELED' WHERE video_id=?", e.VideoID)
		result.State = "CANCELED"
	default:
		return Result{}, errors.New("unknown catalog operation")
	}
	if err != nil {
		return Result{}, err
	}
	answer, _ := json.Marshal(result)
	if _, err = tx.Exec("INSERT INTO processed_events VALUES(?,?,?,?)", e.ID, fp[:], string(answer), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return result, nil
}

type Lookup struct {
	Found    bool                      `json:"found"`
	Manifest placement.Manifest        `json:"manifest"`
	Plan     placement.DistributedPlan `json:"plan"`
	Expires  time.Time                 `json:"expires_at"`
	State    string                    `json:"state"`
}

// Lookup reconciles a source restart or lost PLAN commit reply without renewing
// leases or attempting a second allocation. The immutable request is persisted
// in the same SQLite transaction as its plan and processed event.
func (c *Catalog) Lookup(requestID string) (Lookup, error) {
	var l Lookup
	var manifestJSON, planJSON, expires string
	err := c.db.QueryRow(`SELECT u.manifest_json,u.plan_json,u.expires_at,v.state FROM upload_requests u JOIN videos v USING(request_id) WHERE u.request_id=?`, requestID).Scan(&manifestJSON, &planJSON, &expires, &l.State)
	if errors.Is(err, sql.ErrNoRows) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if err = json.Unmarshal([]byte(manifestJSON), &l.Manifest); err != nil {
		return l, err
	}
	if err = json.Unmarshal([]byte(planJSON), &l.Plan); err != nil {
		return l, err
	}
	l.Expires, err = time.Parse(time.RFC3339Nano, expires)
	l.Found = err == nil
	return l, err
}
func validatePlan(tx *sql.Tx, m placement.Manifest, p placement.DistributedPlan) error {
	next := uint64(0)
	for i, a := range p.Assignments {
		if a.ID != uint64(i) || a.First != next || a.Last < a.First || a.Last >= uint64(len(m.Segments)) || !placement.ValidID(a.NodeID) || !placement.ValidID(a.ZoneID) || !placement.ValidID(a.ReservationID) || p.Reservations[a.ZoneID] != a.ReservationID {
			return errors.New("invalid plan coverage or identity")
		}
		var size uint64
		for j := a.First; j <= a.Last; j++ {
			size += m.Segments[j].Size
		}
		if size != a.Size {
			return errors.New("plan size mismatch")
		}
		var zone string
		if err := tx.QueryRow("SELECT zone_id FROM nodes WHERE node_id=?", a.NodeID).Scan(&zone); err != nil {
			return err
		}
		if zone != a.ZoneID {
			return errors.New("node moved before plan commit")
		}
		next = a.Last + 1
	}
	if next != uint64(len(m.Segments)) {
		return errors.New("incomplete plan")
	}
	return nil
}

type Location struct {
	VideoID  string `json:"video_id"`
	ExtentID uint64 `json:"extent_id"`
	NodeID   string `json:"node_id"`
	ZoneID   string `json:"zone_id"`
	State    string `json:"state"`
}
type Snapshot struct {
	Videos    map[string]string `json:"videos"`
	Locations []Location        `json:"locations"`
	Events    int               `json:"processed_events"`
}

func (c *Catalog) Snapshot() (Snapshot, error) {
	s := Snapshot{Videos: map[string]string{}, Locations: []Location{}}
	rows, err := c.db.Query("SELECT video_id,state FROM videos ORDER BY video_id")
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var id, state string
		if err = rows.Scan(&id, &state); err != nil {
			rows.Close()
			return s, err
		}
		s.Videos[id] = state
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	rows, err = c.db.Query(`SELECT e.video_id,e.extent_id,e.node_id,n.zone_id,e.state FROM extents e JOIN nodes n USING(node_id) ORDER BY e.video_id,e.extent_id`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var l Location
		if err = rows.Scan(&l.VideoID, &l.ExtentID, &l.NodeID, &l.ZoneID, &l.State); err != nil {
			rows.Close()
			return s, err
		}
		s.Locations = append(s.Locations, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	err = c.db.QueryRow("SELECT count(*) FROM processed_events").Scan(&s.Events)
	return s, err
}
