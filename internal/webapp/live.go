package webapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Server) liveState(ctx context.Context, id, owner string) (media.LiveState, error) {
	var state media.LiveState
	e := s.control(ctx, "GET", "/api/v1/live/"+id, nil, &state)
	if e == nil && state.Owner != owner && !state.Public {
		e = fmt.Errorf("private live")
	}
	return state, e
}
func (s *Server) liveAPI(api *http.ServeMux) {
	api.HandleFunc("GET /api/live", func(w http.ResponseWriter, r *http.Request) {
		var states []media.LiveState
		if s.control(r.Context(), "GET", "/api/v1/live", nil, &states) != nil {
			http.Error(w, "live catalog unavailable", 503)
			return
		}
		out := []media.LiveState{}
		for _, state := range states {
			if state.Owner == uid(r) || state.Public {
				state.Chunks = nil
				out = append(out, state)
			}
		}
		controllers.JSON(w, 200, map[string]any{"items": out, "server_unix_ms": time.Now().UnixMilli()})
	})
	api.HandleFunc("POST /api/live/operations", func(w http.ResponseWriter, r *http.Request) {
		var p media.OperationProof
		if controllers.ReadBody(w, r, &p) != nil || p.Operation.OwnerID != uid(r) || p.Operation.Live == nil {
			http.Error(w, "owner live proof required", 403)
			return
		}
		var state media.LiveState
		if e := s.control(r.Context(), "POST", "/api/v1/live/operations", p, &state); e != nil {
			http.Error(w, "live commit refused/unavailable", 409)
			return
		}
		controllers.JSON(w, 200, state)
	})
	api.HandleFunc("GET /api/live/{id}", func(w http.ResponseWriter, r *http.Request) {
		state, e := s.liveState(r.Context(), r.PathValue("id"), uid(r))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		controllers.JSON(w, 200, map[string]any{"state": state, "server_unix_ms": time.Now().UnixMilli()})
	})
	api.HandleFunc("GET /api/live/{id}/index.m3u8", s.livePlaylist)
	api.HandleFunc("GET /api/live/{id}/segments/{sequence}/{asset}", s.liveSegment)
}
func liveManifest(state media.LiveState) string {
	start := 0
	if len(state.Chunks) > 6 {
		start = len(state.Chunks) - 6
	}
	target := int64(1)
	for _, chunk := range state.Chunks {
		if v := int64(math.Ceil(float64(chunk.DurationMS) / 1000)); v > target {
			target = v
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-INDEPENDENT-SEGMENTS\n", target, start)
	if start < len(state.Chunks) {
		fmt.Fprintf(&out, "#EXT-X-MAP:URI=\"segments/%d/init.mp4\"\n", start)
	}
	for _, chunk := range state.Chunks[start:] {
		fmt.Fprintf(&out, "#EXT-X-PROGRAM-DATE-TIME:%s\n#EXTINF:%.3f,\nsegments/%d/segment.m4s\n", time.UnixMilli(chunk.ProgramMS).UTC().Format(time.RFC3339Nano), float64(chunk.DurationMS)/1000, chunk.Sequence)
	}
	if state.Ended {
		out.WriteString("#EXT-X-ENDLIST\n")
	}
	return out.String()
}
func (s *Server) livePlaylist(w http.ResponseWriter, r *http.Request) {
	state, e := s.liveState(r.Context(), r.PathValue("id"), uid(r))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if len(state.Chunks) == 0 && !state.Ended {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "waiting for first verified fragment", 425)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	io.WriteString(w, liveManifest(state))
}
func (s *Server) liveSegment(w http.ResponseWriter, r *http.Request) {
	state, e := s.liveState(r.Context(), r.PathValue("id"), uid(r))
	seq, err := strconv.ParseUint(r.PathValue("sequence"), 10, 32)
	if e != nil || err != nil || seq >= uint64(len(state.Chunks)) {
		http.NotFound(w, r)
		return
	}
	chunk := state.Chunks[seq]
	asset := r.PathValue("asset")
	if asset != "init.mp4" && asset != "segment.m4s" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	m, ok := s.state.Movies[chunk.VideoID]
	s.mu.Unlock()
	if !ok || m.Owner != state.Owner || m.RootID != state.ID || m.OperationKind != fmt.Sprintf("live_segment-%d", seq) {
		http.Error(w, "live metadata mismatch", 502)
		return
	}
	v, e := s.record(r.Context(), m.ID)
	if e != nil || !realReady(v) {
		http.Error(w, "live fragment not READY", 503)
		return
	}
	start := time.Now()
	if e = s.preparePlayback(r.Context(), m, v); e != nil {
		http.Error(w, "live fragment integrity unavailable/refused", 502)
		return
	}
	s.mediaMu.Lock()
	path := s.verified[m.ID]
	s.mediaMu.Unlock()
	f, e := os.Open(path)
	if e != nil {
		http.Error(w, "verified fragment unavailable", 503)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || chunk.InitBytes >= uint64(info.Size()) {
		http.Error(w, "live init bounds mismatch", 502)
		return
	}
	hash := sha256.New()
	if _, e = io.CopyN(hash, f, int64(chunk.InitBytes)); e != nil || hex.EncodeToString(hash.Sum(nil)) != chunk.InitSHA256 {
		http.Error(w, "live init hash mismatch", 502)
		return
	}
	offset, size := int64(0), int64(chunk.InitBytes)
	if asset == "segment.m4s" {
		offset = int64(chunk.InitBytes)
		size = info.Size() - offset
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("X-Integrity", "SIS-HESS-BLS12381-v3 + SHA256 verified on Edge")
	w.Header().Set("Server-Timing", fmt.Sprintf("verified_reconstruction;dur=%.3f", float64(time.Since(start).Microseconds())/1000))
	http.ServeContent(w, r, asset, info.ModTime(), io.NewSectionReader(f, offset, size))
}
