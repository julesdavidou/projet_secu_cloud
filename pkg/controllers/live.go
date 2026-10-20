package controllers

import (
	"fmt"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"net/http"
	"time"
)

func (c *Central) liveAPI(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/live", func(w http.ResponseWriter, r *http.Request) {
		s, e := c.db.Lives()
		if e != nil {
			http.Error(w, "live catalog unavailable", 503)
			return
		}
		JSON(w, 200, s)
	})
	api.HandleFunc("GET /api/v1/live/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, e := c.db.Live(r.PathValue("id"))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		JSON(w, 200, s)
	})
	api.HandleFunc("POST /api/v1/live/operations", func(w http.ResponseWriter, r *http.Request) {
		var p media.OperationProof
		if ReadBody(w, r, &p) != nil || p.Operation.Live == nil {
			http.Error(w, "signed live operation required", 422)
			return
		}
		v, e := verifyOperation(r.Context(), p)
		if e != nil {
			http.Error(w, "live IBS verification refused/unavailable", 422)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if p.Operation.Kind == "live_append" {
			state, e := c.db.Live(p.Operation.VideoID)
			if e != nil {
				http.NotFound(w, r)
				return
			}
			_, up, ok := c.findVideo(p.Operation.NewVideoID)
			// An already committed append can be retried with its original sequence.
			seq := len(state.Chunks)
			for i, chunk := range state.Chunks {
				if chunk.VideoID == p.Operation.NewVideoID {
					seq = i
					break
				}
			}
			if !ok || up.Upload.OwnerID != p.Operation.OwnerID || up.Upload.RootID != p.Operation.VideoID || up.Upload.OperationKind != fmt.Sprintf("live_segment-%d", seq) {
				http.Error(w, "fragment owner/session/sequence mismatch", 409)
				return
			}
		}
		state, e := c.db.ApplyLive(p, v, time.Now())
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		JSON(w, 200, state)
	})
}
