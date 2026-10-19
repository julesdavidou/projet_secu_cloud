package catalog

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
)

// These are transaction tests with an explicit verifier double. End-to-end
// Docker tests exercise real authority keys and Hess verification separately.
func TestOperationCASDelegationRevocationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	owner := "client-owner"
	root := "video"
	proof := media.OperationProof{Suite: "test-verifier-double", Operation: media.Operation{OwnerID: owner, VideoID: root, Nonce: "operation-00000000000000000001", Kind: "share"}, Signature: json.RawMessage(`{}`), Zones: json.RawMessage(`[]`)}
	v := media.OperationVerification{Verified: true, OwnerID: owner}
	state, e := db.ApplyOperation(proof, v, owner, now)
	if e != nil || !state.Public || state.Revision != 1 {
		t.Fatalf("state %+v %v", state, e)
	}
	again, e := db.ApplyOperation(proof, v, owner, now)
	if e != nil || again.Commit != state.Commit {
		t.Fatal("lost reply changed commit", e)
	}
	conflict := proof
	conflict.Operation.Kind = "unshare"
	if _, e = db.ApplyOperation(conflict, v, owner, now); e == nil {
		t.Fatal("nonce substitution accepted")
	}
	proof.Operation.Nonce = "operation-00000000000000000002"
	if _, e = db.ApplyOperation(proof, v, owner, now); e == nil {
		t.Fatal("stale revision accepted")
	}
	proof.Operation.ExpectedRevision = state.Revision
	proof.Operation.Previous = state.Commit
	proof.Operation.Kind = "delegate"
	grant := media.Grant{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OwnerID: owner, VideoID: root, DelegateID: "reconstructor-A1", Operations: []string{"unshare"}, NotBefore: now.Unix() - 1, Expires: now.Unix() + 100, MaxRevision: 10, MaxBytes: 1000, MaxBudget: 10, CertificateHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	v.NewGrant = &grant
	state, e = db.ApplyOperation(proof, v, owner, now)
	if e != nil {
		t.Fatal(e)
	}
	proof.Operation = media.Operation{OwnerID: owner, VideoID: root, Nonce: "operation-00000000000000000003", Kind: "unshare", ExpectedRevision: state.Revision, Previous: state.Commit}
	delegated := media.OperationVerification{Verified: true, OwnerID: owner, DelegateGrantID: grant.ID, CertificateHash: grant.CertificateHash}
	state, e = db.ApplyOperation(proof, delegated, owner, now)
	if e != nil || state.Public {
		t.Fatal("scoped delegate refused", e)
	}
	proof.Operation.Nonce = "operation-00000000000000000004"
	proof.Operation.ExpectedRevision = state.Revision
	proof.Operation.Previous = state.Commit
	proof.Operation.Kind = "share"
	if _, e = db.ApplyOperation(proof, delegated, owner, now); e == nil {
		t.Fatal("out-of-scope operation accepted")
	}
	proof.Operation.Kind = "revoke"
	proof.Operation.GrantID = grant.ID
	v.NewGrant = nil
	state, e = db.ApplyOperation(proof, v, owner, now)
	if e != nil {
		t.Fatal(e)
	}
	proof.Operation.Nonce = "operation-00000000000000000005"
	proof.Operation.Kind = "unshare"
	proof.Operation.ExpectedRevision = state.Revision
	proof.Operation.Previous = state.Commit
	if _, e = db.ApplyOperation(proof, delegated, owner, now); e == nil {
		t.Fatal("revoked grant accepted")
	}
	db.Close()
	db, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	g, e := db.Grant(grant.ID)
	if e != nil || !g.Revoked {
		t.Fatal("revocation lost after restart", e)
	}
	persisted, e := db.OperationState(root)
	if e != nil || persisted.Commit != state.Commit {
		t.Fatal("head lost after restart", e)
	}
}
