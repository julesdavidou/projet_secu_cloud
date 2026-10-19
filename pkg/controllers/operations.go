package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
)

func verifyOperation(ctx context.Context, p media.OperationProof) (media.OperationVerification, error) {
	var v media.OperationVerification
	origin := Env("SECURITY_OPERATION_VERIFY_URL", "")
	if origin == "" {
		return v, errors.New("IBS operation verifier unavailable")
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return v, e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", origin+"/v3/verify/operation", bytes.NewReader(raw))
	if e != nil {
		return v, e
	}
	r.Header.Set("Content-Type", "application/json")
	response, e := (&http.Client{Timeout: 30 * time.Second}).Do(r)
	if e != nil {
		return v, e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return v, errors.New("IBS operation refused")
	}
	if e = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&v); e != nil {
		return v, e
	}
	if !v.Verified || v.OwnerID != p.Operation.OwnerID {
		return v, errors.New("operation verifier identity mismatch")
	}
	return v, nil
}
func (c *Central) operationAPI(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/videos/{id}/operations", func(w http.ResponseWriter, r *http.Request) {
		s, e := c.db.OperationState(r.PathValue("id"))
		if e != nil {
			http.Error(w, "operation catalog unavailable", 503)
			return
		}
		if s == nil {
			http.NotFound(w, r)
			return
		}
		JSON(w, 200, s)
	})
	api.HandleFunc("GET /api/v1/delegations/{id}", func(w http.ResponseWriter, r *http.Request) {
		g, e := c.db.Grant(r.PathValue("id"))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		JSON(w, 200, g)
	})
	api.HandleFunc("POST /api/v1/operations", func(w http.ResponseWriter, r *http.Request) {
		// Verify expensive pairings outside the controller's heartbeat/placement lock.
		var p media.OperationProof
		if e := ReadBody(w, r, &p); e != nil {
			http.Error(w, "invalid operation envelope", 422)
			return
		}
		v, e := verifyOperation(r.Context(), p)
		if e != nil {
			http.Error(w, "operation proof refused or verifier unavailable", 422)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		_, root, ok := c.findVideo(p.Operation.VideoID)
		if !ok || root.Upload.OwnerID != p.Operation.OwnerID {
			http.Error(w, "unknown signed owner media", 409)
			return
		}
		ready, e := c.record(root.Upload.Manifest.RequestID, root)
		if e != nil || ready.State != "READY" {
			http.Error(w, "media not READY", 409)
			return
		}
		if p.Operation.NewVideoID != "" {
			_, next, ok := c.findVideo(p.Operation.NewVideoID)
			if !ok || next.Upload.OwnerID != p.Operation.OwnerID || next.Upload.RootID != p.Operation.VideoID || next.Upload.OperationKind != p.Operation.Kind {
				http.Error(w, "replacement owner, root or operation differs", 409)
				return
			}
		}
		s, e := c.db.ApplyOperation(p, v, p.Operation.OwnerID, time.Now())
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		JSON(w, 200, s)
	})
}

// CheckPublicationGrant runs before reservation and again before the operation
// commit. An expired/revoked grant cannot authorize a new content version.
func CheckPublicationGrant(g media.Grant, actor, owner, root, kind, certificateHash string, bytes uint64, policyBudget *uint64) error {
	now := time.Now().Unix()
	if g.Revoked || g.OwnerID != owner || g.VideoID != root || g.DelegateID != actor || g.CertificateHash != certificateHash || now < g.NotBefore || now >= g.Expires || bytes > g.MaxBytes || policyBudget == nil || *policyBudget > g.MaxBudget {
		return errors.New("publication delegation inactive or exceeds scope")
	}
	for _, op := range g.Operations {
		if op == kind && strings.Contains("replace,insert_blocks,delete_blocks,modify_blocks", kind) {
			return nil
		}
	}
	return errors.New("publication operation outside delegation")
}
