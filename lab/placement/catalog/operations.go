package catalog

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

const operationSchema = `
CREATE TABLE IF NOT EXISTS media_heads(root_id TEXT PRIMARY KEY,owner_id TEXT NOT NULL,current_id TEXT NOT NULL,revision INTEGER NOT NULL,commit_hash TEXT NOT NULL,public INTEGER NOT NULL,deleted INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS media_members(video_id TEXT PRIMARY KEY,root_id TEXT NOT NULL REFERENCES media_heads(root_id));
CREATE TABLE IF NOT EXISTS media_operations(nonce TEXT PRIMARY KEY,root_id TEXT NOT NULL REFERENCES media_heads(root_id),fingerprint TEXT NOT NULL,proof_json TEXT NOT NULL,result_json TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS media_grants(grant_id TEXT PRIMARY KEY,root_id TEXT NOT NULL REFERENCES media_heads(root_id),grant_json TEXT NOT NULL,revoked INTEGER NOT NULL DEFAULT 0);
`

func (c *Catalog) OperationState(id string) (*media.OperationState, error) {
	var s media.OperationState
	e := c.db.QueryRow(`SELECT h.root_id,h.owner_id,h.current_id,h.revision,h.commit_hash,h.public,h.deleted FROM media_heads h JOIN media_members m ON m.root_id=h.root_id WHERE m.video_id=?`, id).Scan(&s.RootID, &s.OwnerID, &s.CurrentID, &s.Revision, &s.Commit, &s.Public, &s.Deleted)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, e
}
func readGrant(tx *sql.Tx, id string) (media.Grant, error) {
	var g media.Grant
	var raw string
	var revoked bool
	e := tx.QueryRow(`SELECT grant_json,revoked FROM media_grants WHERE grant_id=?`, id).Scan(&raw, &revoked)
	if e != nil {
		return g, e
	}
	e = json.Unmarshal([]byte(raw), &g)
	g.Revoked = revoked
	return g, e
}
func (c *Catalog) Grant(id string) (media.Grant, error) {
	tx, e := c.db.Begin()
	if e != nil {
		return media.Grant{}, e
	}
	defer tx.Rollback()
	return readGrant(tx, id)
}

// ApplyOperation is called only AFTER trusted IBS verification and a check of
// the authoritative READY upload's owner. The SQLite transaction rechecks
// revocation/expiry/revision, so authorization cannot race the actual commit.
func (c *Catalog) ApplyOperation(p media.OperationProof, v media.OperationVerification, owner string, now time.Time) (media.OperationState, error) {
	op := p.Operation
	raw, e := json.Marshal(p)
	if e != nil {
		return media.OperationState{}, e
	}
	digest := sha256.Sum256(raw)
	fp := hex.EncodeToString(digest[:])
	empty := media.OperationState{}
	if !v.Verified || v.OwnerID != owner || op.OwnerID != owner || !placement.ValidID(owner) || !placement.ValidID(op.VideoID) || !placement.ValidID(op.Nonce) || len(op.Nonce) < 20 || op.ExpectedRevision >= math.MaxInt64 || len(raw) > 64<<10 {
		return empty, errors.New("invalid verified operation")
	}
	tx, e := c.db.Begin()
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	var previousFP, result string
	e = tx.QueryRow(`SELECT fingerprint,result_json FROM media_operations WHERE nonce=?`, op.Nonce).Scan(&previousFP, &result)
	if e == nil {
		if previousFP != fp {
			return empty, errors.New("operation nonce conflict")
		}
		var old media.OperationState
		e = json.Unmarshal([]byte(result), &old)
		return old, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return empty, e
	}
	var s media.OperationState
	e = tx.QueryRow(`SELECT root_id,owner_id,current_id,revision,commit_hash,public,deleted FROM media_heads WHERE root_id=?`, op.VideoID).Scan(&s.RootID, &s.OwnerID, &s.CurrentID, &s.Revision, &s.Commit, &s.Public, &s.Deleted)
	if errors.Is(e, sql.ErrNoRows) {
		s = media.OperationState{RootID: op.VideoID, OwnerID: owner, CurrentID: op.VideoID, Public: op.InitialPublic}
		if _, e = tx.Exec(`INSERT INTO media_heads VALUES(?,?,?,?,?,?,?)`, s.RootID, s.OwnerID, s.CurrentID, 0, "", s.Public, false); e != nil {
			return empty, e
		}
		if _, e = tx.Exec(`INSERT INTO media_members VALUES(?,?)`, s.RootID, s.RootID); e != nil {
			return empty, e
		}
	} else if e != nil {
		return empty, e
	}
	if s.OwnerID != owner || s.Revision != op.ExpectedRevision || s.Commit != op.Previous || s.Deleted {
		return empty, errors.New("operation revision conflict or deleted media")
	}
	if v.DelegateGrantID != "" {
		g, e := readGrant(tx, v.DelegateGrantID)
		if e != nil || g.Revoked || g.OwnerID != owner || g.VideoID != op.VideoID || g.CertificateHash != v.CertificateHash || now.Unix() < g.NotBefore || now.Unix() >= g.Expires || s.Revision+1 > g.MaxRevision || !slices.Contains(g.Operations, op.Kind) {
			return empty, errors.New("delegation inactive or operation out of scope")
		}
		if op.Kind == "delegate" || op.Kind == "revoke" {
			return empty, errors.New("delegation cannot be redelegated")
		}
	}
	switch op.Kind {
	case "share":
		s.Public = true
	case "unshare":
		s.Public = false
	case "delete":
		s.Deleted = true
		s.Public = false // explicit tombstone; retained history is not reclaimed capacity
	case "replace", "insert_blocks", "delete_blocks", "modify_blocks":
		if !placement.ValidID(op.NewVideoID) || op.NewVideoID == s.CurrentID {
			return empty, errors.New("new immutable READY version required")
		}
		var state string
		if e = tx.QueryRow(`SELECT state FROM videos WHERE video_id=?`, op.NewVideoID).Scan(&state); e != nil || state != "READY" {
			return empty, errors.New("replacement version not READY")
		}
		if _, e = tx.Exec(`INSERT INTO media_members VALUES(?,?)`, op.NewVideoID, s.RootID); e != nil {
			return empty, errors.New("replacement already belongs to a media")
		}
		s.CurrentID = op.NewVideoID
	case "delegate":
		g := v.NewGrant
		if g == nil || g.OwnerID != owner || g.VideoID != op.VideoID || !placement.ValidID(g.ID) || len(g.ID) != 64 || !placement.ValidID(g.DelegateID) || g.Expires <= now.Unix() || g.NotBefore > now.Unix() || g.Expires-g.NotBefore > 3600 || g.MaxRevision < s.Revision+1 || g.MaxRevision > s.Revision+100 || g.MaxBytes < 1 || g.MaxBytes > 16<<20 || len(g.Operations) == 0 || len(g.CertificateHash) != 64 {
			return empty, errors.New("invalid delegation mandate")
		}
		for _, kind := range g.Operations {
			if !slices.Contains([]string{"replace", "insert_blocks", "delete_blocks", "modify_blocks", "share", "unshare", "delete"}, kind) {
				return empty, errors.New("invalid delegated operation")
			}
		}
		encoded, _ := json.Marshal(g)
		if _, e = tx.Exec(`INSERT INTO media_grants(grant_id,root_id,grant_json) VALUES(?,?,?)`, g.ID, s.RootID, string(encoded)); e != nil {
			return empty, e
		}
	case "revoke":
		g, e := readGrant(tx, op.GrantID)
		if e != nil || g.OwnerID != owner || g.VideoID != s.RootID {
			return empty, errors.New("unknown owner delegation")
		}
		if _, e = tx.Exec(`UPDATE media_grants SET revoked=1 WHERE grant_id=?`, g.ID); e != nil {
			return empty, e
		}
	default:
		return empty, errors.New("unsupported operation")
	}
	s.Revision++
	s.Commit = fp
	resultRaw, _ := json.Marshal(s)
	if _, e = tx.Exec(`UPDATE media_heads SET current_id=?,revision=?,commit_hash=?,public=?,deleted=? WHERE root_id=?`, s.CurrentID, s.Revision, s.Commit, s.Public, s.Deleted, s.RootID); e != nil {
		return empty, e
	}
	if _, e = tx.Exec(`INSERT INTO media_operations VALUES(?,?,?,?,?,?)`, op.Nonce, s.RootID, fp, string(raw), string(resultRaw), now.UTC().Format(time.RFC3339Nano)); e != nil {
		return empty, e
	}
	if e = tx.Commit(); e != nil {
		return empty, e
	}
	return s, nil
}
