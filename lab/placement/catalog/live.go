package catalog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"strings"
	"time"
)

const liveSchema = `
CREATE TABLE IF NOT EXISTS live_sessions(id TEXT PRIMARY KEY,state_json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS live_events(nonce TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,result_json TEXT NOT NULL,proof_json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS live_members(video_id TEXT PRIMARY KEY,live_id TEXT NOT NULL,sequence INTEGER NOT NULL,UNIQUE(live_id,sequence));
`

func (c *Catalog) Live(id string) (media.LiveState, error) {
	var s media.LiveState
	var raw string
	e := c.db.QueryRow(`SELECT state_json FROM live_sessions WHERE id=?`, id).Scan(&raw)
	if e == nil {
		e = json.Unmarshal([]byte(raw), &s)
	}
	return s, e
}
func (c *Catalog) Lives() ([]media.LiveState, error) {
	rows, e := c.db.Query(`SELECT state_json FROM live_sessions ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []media.LiveState{}
	for rows.Next() {
		var raw string
		var s media.LiveState
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(raw), &s); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ApplyLive is called after real IBS verification. Ordered head, idempotence,
// READY membership and lifetime are committed in the SAME Central SQLite.
func (c *Catalog) ApplyLive(p media.OperationProof, v media.OperationVerification, now time.Time) (media.LiveState, error) {
	var empty media.LiveState
	op := p.Operation
	d := op.Live
	if !v.Verified || v.OwnerID != op.OwnerID || v.DelegateGrantID != "" || len(op.VideoID) != 34 || !strings.HasPrefix(op.VideoID, "l-") || len(op.Nonce) < 20 || len(op.Nonce) > 128 || d == nil {
		return empty, errors.New("verified owner live operation required")
	}
	if _, e := hex.DecodeString(op.VideoID[2:]); e != nil {
		return empty, e
	}
	raw, e := json.Marshal(p)
	if e != nil || len(raw) > 65536 {
		return empty, errors.New("invalid live envelope")
	}
	sum := sha256.Sum256(raw)
	fp := hex.EncodeToString(sum[:])
	tx, e := c.db.Begin()
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	var oldFP, rawState string
	e = tx.QueryRow(`SELECT fingerprint,result_json FROM live_events WHERE nonce=?`, op.Nonce).Scan(&oldFP, &rawState)
	if e == nil {
		if oldFP != fp {
			return empty, errors.New("live nonce conflict")
		}
		e = json.Unmarshal([]byte(rawState), &empty)
		return empty, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return empty, e
	}
	s := media.LiveState{}
	e = tx.QueryRow(`SELECT state_json FROM live_sessions WHERE id=?`, op.VideoID).Scan(&rawState)
	if errors.Is(e, sql.ErrNoRows) {
		if op.Kind != "live_create" || op.ExpectedRevision != 0 || op.Previous != "" || len(strings.TrimSpace(d.Title)) == 0 || len(d.Title) > 200 {
			return empty, errors.New("unknown live or invalid create")
		}
		s = media.LiveState{ID: op.VideoID, Owner: op.OwnerID, Title: d.Title, Public: op.InitialPublic, Chunks: []media.LiveChunk{}}
	} else if e != nil {
		return empty, e
	} else {
		if e = json.Unmarshal([]byte(rawState), &s); e != nil {
			return empty, e
		}
		if op.Kind == "live_create" {
			return empty, errors.New("live already exists")
		}
	}
	if s.Owner != op.OwnerID || s.Revision != op.ExpectedRevision || s.Commit != op.Previous {
		return empty, errors.New("live owner/revision conflict")
	}
	switch op.Kind {
	case "live_create":
	case "live_append":
		if s.Ended || len(s.Chunks) >= 1024 || d.DurationMS < 1 || d.DurationMS > 60000 || d.ProgramMS < 1 || d.InitBytes < 8 || len(d.InitSHA256) != 64 || d.PublishMS < 0 {
			return empty, errors.New("live append bounds or ended")
		}
		if _, e = hex.DecodeString(d.InitSHA256); e != nil {
			return empty, e
		}
		if len(s.Chunks) > 0 {
			previous := s.Chunks[len(s.Chunks)-1]
			// HLS target duration cannot change within a session. The first
			// fragment fixes its ceiling; later oversized GOPs need a new session.
			targetMS := ((s.Chunks[0].DurationMS + 999) / 1000) * 1000
			if d.DurationMS > targetMS {
				return empty, errors.New("live duration exceeds fixed HLS target")
			}
			if previous.InitSHA256 != d.InitSHA256 || d.ProgramMS < previous.ProgramMS+previous.DurationMS-2 {
				return empty, errors.New("live init/timeline changed")
			}
		}
		var state string
		var size uint64
		e = tx.QueryRow(`SELECT v.state,SUM(e.size_bytes) FROM videos v JOIN extents e ON e.video_id=v.video_id WHERE v.video_id=? GROUP BY v.video_id`, op.NewVideoID).Scan(&state, &size)
		if e != nil || state != "READY" || d.InitBytes >= size {
			return empty, errors.New("live fragment not READY")
		}
		seq := uint64(len(s.Chunks))
		if _, e = tx.Exec(`INSERT INTO live_members VALUES(?,?,?)`, op.NewVideoID, s.ID, seq); e != nil {
			return empty, e
		}
		s.Chunks = append(s.Chunks, media.LiveChunk{Sequence: seq, VideoID: op.NewVideoID, LiveEvent: *d, CommittedMS: now.UnixMilli()})
	case "live_end":
		if len(d.Reason) > 200 {
			return empty, errors.New("invalid end reason")
		}
		s.Ended = true
		s.Reason = d.Reason
	case "live_share":
		s.Public = true
	case "live_unshare":
		s.Public = false
	default:
		return empty, errors.New("unknown live operation")
	}
	s.Revision++
	s.Commit = fp
	result, _ := json.Marshal(s)
	if _, e = tx.Exec(`INSERT INTO live_sessions VALUES(?,?) ON CONFLICT(id) DO UPDATE SET state_json=excluded.state_json`, s.ID, string(result)); e != nil {
		return empty, e
	}
	receipt := s
	receipt.Chunks = nil
	receiptJSON, _ := json.Marshal(receipt)
	if _, e = tx.Exec(`INSERT INTO live_events VALUES(?,?,?,?)`, op.Nonce, fp, string(receiptJSON), string(raw)); e != nil {
		return empty, e
	}
	return s, tx.Commit()
}
