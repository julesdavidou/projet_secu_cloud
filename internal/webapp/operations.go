package webapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
)

func certificateIdentity(raw json.RawMessage) (string, string, error) {
	var value struct {
		Operation struct {
			EffectiveID string `json:"effective_id"`
		} `json:"operation"`
	}
	if json.Unmarshal(raw, &value) != nil || !strings.HasPrefix(value.Operation.EffectiveID, "delegate:") {
		return "", "", errors.New("missing delegation certificate")
	}
	var anyValue any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e := decoder.Decode(&anyValue); e != nil {
		return "", "", e
	}
	canonical, e := json.Marshal(anyValue)
	if e != nil {
		return "", "", e
	}
	hash := sha256.Sum256(canonical)
	return strings.TrimPrefix(value.Operation.EffectiveID, "delegate:"), hex.EncodeToString(hash[:]), nil
}
func (s *Server) grant(ctx context.Context, id, actor, root string) (media.Grant, error) {
	var g media.Grant
	e := s.control(ctx, "GET", "/api/v1/delegations/"+id, nil, &g)
	if e != nil {
		return g, e
	}
	now := time.Now().Unix()
	if g.Revoked || g.DelegateID != actor || root != "" && g.VideoID != root || now < g.NotBefore || now >= g.Expires {
		return g, errors.New("delegation inactive")
	}
	return g, nil
}
func (s *Server) operationState(ctx context.Context, m Movie) (*media.OperationState, error) {
	var state media.OperationState
	e := s.control(ctx, "GET", "/api/v1/videos/"+m.ID+"/operations", nil, &state)
	var status *controlFailure
	if errors.As(e, &status) && status.Status == 404 {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &state, nil
}
func (s *Server) ownerOperationState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	m, ok := s.state.Movies[r.PathValue("id")]
	s.mu.Unlock()
	if !ok || m.Owner != uid(r) || m.LayoutVersion != 3 {
		http.NotFound(w, r)
		return
	}
	state, e := s.operationState(r.Context(), m)
	if e != nil {
		http.Error(w, "operation catalog unavailable", 503)
		return
	}
	if state == nil {
		state = &media.OperationState{RootID: m.ID, OwnerID: m.Owner, CurrentID: m.ID, Public: m.Public}
	}
	controllers.JSON(w, 200, state)
}
func (s *Server) submitOperation(w http.ResponseWriter, r *http.Request) {
	var proof media.OperationProof
	if controllers.ReadBody(w, r, &proof) != nil || proof.Operation.VideoID != r.PathValue("id") || proof.Operation.OwnerID != uid(r) {
		http.Error(w, "signed owner operation required", 403)
		return
	}
	var state media.OperationState
	if e := s.control(r.Context(), "POST", "/api/v1/operations", proof, &state); e != nil {
		http.Error(w, "operation refused or catalog unavailable", 409)
		return
	}
	controllers.JSON(w, 200, state)
}
func (s *Server) delegatedAPI(mux *http.ServeMux) {
	actor := func(r *http.Request) string {
		a := r.Header.Get("X-Verified-Identity")
		if strings.HasPrefix(a, "reconstructor-") {
			return a
		}
		return ""
	}
	mux.HandleFunc("POST /api/v3/delegated/videos", s.signedUpload)
	mux.HandleFunc("GET /api/v3/delegated/grants/{id}", func(w http.ResponseWriter, r *http.Request) {
		if actor(r) == "" {
			http.Error(w, "delegate certificate required", 403)
			return
		}
		g, e := s.grant(r.Context(), r.PathValue("id"), actor(r), "")
		if e != nil {
			http.Error(w, "delegation inactive", 403)
			return
		}
		controllers.JSON(w, 200, g)
	})
	for _, path := range []string{"GET /api/v3/delegated/state/{id}", "GET /api/v3/delegated/original/{id}"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if actor(r) == "" {
				http.Error(w, "delegate certificate required", 403)
				return
			}
			g, e := s.grant(r.Context(), r.Header.Get("X-Delegation-ID"), actor(r), r.PathValue("id"))
			if e != nil {
				http.Error(w, "delegation inactive", 403)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), identityKey{}, g.OwnerID))
			if strings.Contains(r.URL.Path, "/original/") {
				s.original(w, r)
			} else {
				s.ownerOperationState(w, r)
			}
		})
	}
	mux.HandleFunc("POST /api/v3/delegated/operations", func(w http.ResponseWriter, r *http.Request) {
		if actor(r) == "" {
			http.Error(w, "delegate certificate required", 403)
			return
		}
		var p media.OperationProof
		if controllers.ReadBody(w, r, &p) != nil {
			http.Error(w, "invalid operation", 422)
			return
		}
		id, hash, e := certificateIdentity(p.Delegation)
		if e != nil {
			http.Error(w, "delegation certificate required", 403)
			return
		}
		g, e := s.grant(r.Context(), id, actor(r), p.Operation.VideoID)
		if e != nil || g.OwnerID != p.Operation.OwnerID || g.CertificateHash != hash {
			http.Error(w, "delegation inactive", 403)
			return
		}
		var state media.OperationState
		if e = s.control(r.Context(), "POST", "/api/v1/operations", p, &state); e != nil {
			http.Error(w, "delegated operation refused", 409)
			return
		}
		controllers.JSON(w, 200, state)
	})
}
func (s *Server) publicationGrant(ctx context.Context, pack signedPackage, actor string, total uint64) error {
	var grantID, hash string
	for _, raw := range pack.Proofs {
		var proof struct {
			Delegation json.RawMessage `json:"delegation"`
		}
		if json.Unmarshal(raw, &proof) != nil {
			return errors.New("invalid delegated asset")
		}
		id, h, e := certificateIdentity(proof.Delegation)
		if e != nil {
			return e
		}
		if grantID != "" && (grantID != id || hash != h) {
			return errors.New("mixed publication delegations")
		}
		grantID, hash = id, h
	}
	g, e := s.grant(ctx, grantID, actor, pack.RootID)
	if e != nil {
		return e
	}
	var budget *uint64
	if pack.Policy != nil {
		budget = pack.Policy.Budget
	}
	return controllers.CheckPublicationGrant(g, actor, pack.OwnerID, pack.RootID, pack.OperationKind, hash, total, budget)
}
