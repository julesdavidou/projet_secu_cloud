// Package webapp is an HTTP integration adapter, never a placement authority.
package webapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type Config struct {
	CentralURL, AuthURL, SecretsDir, DataDir, StaticDir, ConsoleDir, VideoScript, SourceNodeID string
	MaxUpload                                                                                  int64
	NodeTTL                                                                                    time.Duration
	BlockPipeline                                                                              bool
}
type MediaFile struct {
	Name     string `json:"name"`
	ExtentID uint64 `json:"extent_id"`
}
type Movie struct {
	ID            string                     `json:"id"`
	LayoutVersion int                        `json:"layout_version,omitempty"`
	Proofs        map[string]json.RawMessage `json:"proofs,omitempty"`
	Revision      uint64                     `json:"revision,omitempty"`
	Previous      string                     `json:"previous,omitempty"`
	Assets        []media.Asset              `json:"assets,omitempty"`
	NativeSource  bool                       `json:"native_source,omitempty"`
	Owner         string                     `json:"owner"`
	Title         string                     `json:"title"`
	Category      string                     `json:"category"`
	Description   string                     `json:"description"`
	Public        bool                       `json:"public"`
	PublishedAt   string                     `json:"publishedAt"`
	Duration      float64                    `json:"duration"`
	Playlist      string                     `json:"playlist"`
	Files         []MediaFile                `json:"files"`
}
type History struct {
	VideoID   string  `json:"videoId"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	UpdatedAt string  `json:"updatedAt"`
}
type Profile struct {
	Name      string    `json:"name"`
	Course    string    `json:"course"`
	Promotion string    `json:"promotion"`
	Favorites []string  `json:"favorites"`
	History   []History `json:"history"`
}
type metadata struct {
	Movies   map[string]Movie   `json:"movies"`
	Profiles map[string]Profile `json:"profiles"`
}
type secureCacheEntry struct {
	Path, ContentHash, ProofHash string
	Expires                      time.Time
}
type Server struct {
	videoLocks  sync.Map
	secureCache map[string]secureCacheEntry
	cfg         Config
	client      *http.Client
	token       string
	mu          sync.Mutex
	state       metadata
	mediaMu     sync.Mutex
	verified    map[string]string
	authProxy   *httputil.ReverseProxy
}
type identityKey struct{}

func New(cfg Config) (*Server, error) {
	if cfg.NodeTTL <= 0 {
		cfg.NodeTTL = 5 * time.Second
	}
	if cfg.MaxUpload <= 0 {
		cfg.MaxUpload = 64 << 20
	}
	if !placement.ValidID(cfg.SourceNodeID) {
		return nil, errors.New("invalid source node")
	}
	for _, path := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "tmp"), filepath.Join(cfg.DataDir, "cache")} {
		if e := os.MkdirAll(path, 0750); e != nil {
			return nil, e
		}
	}
	raw, e := os.ReadFile(filepath.Join(cfg.SecretsDir, "operator.token"))
	if e != nil {
		return nil, e
	}
	if len(strings.TrimSpace(string(raw))) < 24 {
		return nil, errors.New("operator secret missing")
	}
	s := &Server{cfg: cfg, token: strings.TrimSpace(string(raw)), client: &http.Client{Timeout: 90 * time.Second}, state: metadata{Movies: map[string]Movie{}, Profiles: map[string]Profile{}}, verified: map[string]string{}, secureCache: map[string]secureCacheEntry{}}
	if e = controllers.ReadJSON(filepath.Join(cfg.DataDir, "web-metadata.json"), &s.state); e != nil {
		return nil, e
	}
	if s.state.Movies == nil || s.state.Profiles == nil {
		return nil, errors.New("invalid web metadata; refusing startup")
	}
	auth, e := url.Parse(cfg.AuthURL)
	if e != nil || auth.Host == "" {
		return nil, errors.New("invalid auth origin")
	}
	s.authProxy = httputil.NewSingleHostReverseProxy(auth)
	s.authProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		http.Error(w, "authentication unavailable", 503)
	}
	return s, nil
}
func (s *Server) save() error {
	return controllers.AtomicJSON(filepath.Join(s.cfg.DataDir, "web-metadata.json"), s.state)
}
func uid(r *http.Request) string { v, _ := r.Context().Value(identityKey{}).(string); return v }
func (s *Server) session(r *http.Request) (string, error) {
	cookie, e := r.Cookie("session")
	if e != nil || len(cookie.Value) > 256 {
		return "", errors.New("session required")
	}
	req, e := http.NewRequestWithContext(r.Context(), "GET", s.cfg.AuthURL+"/api/auth/session", nil)
	if e != nil {
		return "", e
	}
	req.AddCookie(cookie)
	res, e := s.client.Do(req)
	if e != nil {
		return "", e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", errors.New("invalid session")
	}
	var body struct {
		Status   string `json:"status"`
		ClientID string `json:"client_id"`
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body); e != nil {
		return "", e
	}
	if body.Status != "AUTHENTICATED" || !placement.ValidID(body.ClientID) {
		return "", errors.New("invalid authentication response")
	}
	return body.ClientID, nil
}
func (s *Server) protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, e := s.session(r)
		if e != nil {
			http.Error(w, "session required or expired", 401)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		controllers.JSON(w, 200, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		for _, origin := range []string{s.cfg.CentralURL, s.cfg.AuthURL} {
			path := "/health"
			if origin == s.cfg.CentralURL {
				path = "/ready"
			}
			q, _ := http.NewRequestWithContext(ctx, "GET", origin+path, nil)
			res, e := s.client.Do(q)
			if e != nil {
				http.Error(w, "dependency unavailable", 503)
				return
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				http.Error(w, "dependency not ready", 503)
				return
			}
		}
		controllers.JSON(w, 200, map[string]bool{"ready": true})
	})
	mux.Handle("/api/auth/", s.authProxy)
	api := http.NewServeMux()
	api.HandleFunc("GET /api/videos", s.list)
	api.HandleFunc("POST /api/videos", s.upload)
	api.HandleFunc("POST /api/videos/signed", s.signedUpload)
	api.HandleFunc("GET /api/videos/{id}/playback", s.playback)
	api.HandleFunc("GET /api/videos/{id}/original", s.original)
	api.HandleFunc("GET /api/videos/{id}/placement", s.placement)
	api.HandleFunc("GET /api/me", s.profile)
	api.HandleFunc("PATCH /api/me", s.profile)
	api.HandleFunc("PUT /api/me/favorites/{id}", s.favorite)
	api.HandleFunc("DELETE /api/me/favorites/{id}", s.favorite)
	api.HandleFunc("PUT /api/me/progress/{id}", s.progress)
	api.HandleFunc("DELETE /api/me/history", s.clearHistory)
	api.HandleFunc("GET /api/nodes", s.topology)
	api.HandleFunc("GET /api/zones", s.topology)
	api.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, r *http.Request) {
		integrity, brokers := "SHA256; legacy SIS/IBS prototype not integrated", "authenticated broker"
		if controllers.Env("REQUIRE_SIS_IBS", "0") == "1" {
			integrity = "SIS-HESS-BLS12381-v3 + SHA256; local owner signatures required"
			brokers = "mutual TLS, stable node identities, direct broker links"
		}
		controllers.JSON(w, 200, map[string]string{"auth": "Java opaque session", "placement": "Go HRW", "storage": "real-http", "vod": "HLS or native MP4, verified Edge reconstruction", "integrity": integrity, "live": "prototype not integrated", "brokers": brokers})
	})
	mux.Handle("/api/", s.protected(api))
	mux.Handle("GET /media/{id}/video.mp4", s.protected(http.HandlerFunc(s.media)))
	mux.HandleFunc("GET /assets/js/config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		io.WriteString(w, `export const config=Object.freeze({dataSource:"api",apiBaseUrl:"/api",requestTimeoutMs:120000,storageKey:"cariboutv.viewer.api.v1"});`)
	})
	mux.Handle("/integration/", http.StripPrefix("/integration/", http.FileServer(http.Dir(s.cfg.ConsoleDir))))
	mux.Handle("/", http.FileServer(http.Dir(s.cfg.StaticDir)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || u.Host != r.Host {
					http.Error(w, "cross-origin mutation denied", 403)
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) control(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = strings.NewReader(string(raw))
	}
	q, e := http.NewRequestWithContext(ctx, method, s.cfg.CentralURL+path, reader)
	if e != nil {
		return e
	}
	q.Header.Set("Authorization", "Bearer "+s.token)
	q.Header.Set("Content-Type", "application/json")
	res, e := s.client.Do(q)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return fmt.Errorf("control plane status %d", res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
	}
	return nil
}
func (s *Server) movie(id, client string) (Movie, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.state.Movies[id]
	return m, ok && (m.Owner == client || m.Public)
}
func (s *Server) record(ctx context.Context, id string) (controllers.UploadRecord, error) {
	var record controllers.UploadRecord
	e := s.control(ctx, "GET", "/api/v1/videos/"+url.PathEscape(id)+"/placements", nil, &record)
	return record, e
}
func realReady(v controllers.UploadRecord) bool {
	if v.State != "READY" || len(v.Units) == 0 {
		return false
	}
	for _, u := range v.Units {
		if u.State != "STORED" || u.Proof != "HTTP_SHA256_VERIFIED" {
			return false
		}
	}
	return true
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	var snap controllers.Inspection
	if e := s.control(r.Context(), "GET", "/api/v1/snapshot", nil, &snap); e != nil {
		http.Error(w, "catalogue unavailable", 503)
		return
	}
	items := []map[string]any{}
	s.mu.Lock()
	for _, m := range s.state.Movies {
		if m.Owner != uid(r) && !m.Public {
			continue
		}
		v, ok := snap.Uploads[m.ID]
		if !ok || !realReady(v) {
			continue
		}
		items = append(items, map[string]any{"id": m.ID, "title": m.Title, "category": m.Category, "minutes": m.Duration / 60, "publishedAt": m.PublishedAt, "views": 0, "description": m.Description, "tags": []string{}, "image": "/assets/images/favicon.svg", "campus": "", "creator": m.Owner, "contributors": []string{}})
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i]["publishedAt"].(string) > items[j]["publishedAt"].(string) })
	controllers.JSON(w, 200, map[string]any{"items": items})
}
func defaultProfile(id string) Profile {
	return Profile{Name: id, Favorites: []string{}, History: []History{}}
}
func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	id := uid(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[id]
	if !ok {
		p = defaultProfile(id)
	}
	if r.Method == "PATCH" {
		var q struct {
			Name      string `json:"name"`
			Course    string `json:"course"`
			Promotion string `json:"promotion"`
		}
		if e := controllers.ReadBody(w, r, &q); e != nil || len(strings.TrimSpace(q.Name)) == 0 || len(q.Name) > 120 || len(q.Course) > 180 || len(q.Promotion) > 4 {
			http.Error(w, "invalid profile", 400)
			return
		}
		p.Name = strings.TrimSpace(q.Name)
		p.Course = q.Course
		p.Promotion = q.Promotion
		s.state.Profiles[id] = p
		if e := s.save(); e != nil {
			http.Error(w, "profile persistence failed", 503)
			return
		}
	}
	controllers.JSON(w, 200, p)
}
func (s *Server) favorite(w http.ResponseWriter, r *http.Request) {
	video := r.PathValue("id")
	if _, ok := s.movie(video, uid(r)); !ok {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[uid(r)]
	if !ok {
		p = defaultProfile(uid(r))
	}
	ids := []string{}
	for _, id := range p.Favorites {
		if id != video {
			ids = append(ids, id)
		}
	}
	if r.Method == "PUT" {
		ids = append(ids, video)
	}
	p.Favorites = ids
	s.state.Profiles[uid(r)] = p
	if e := s.save(); e != nil {
		http.Error(w, "profile persistence failed", 503)
		return
	}
	controllers.JSON(w, 200, p)
}
func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	video := r.PathValue("id")
	if _, ok := s.movie(video, uid(r)); !ok {
		http.NotFound(w, r)
		return
	}
	var q struct {
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
	}
	if e := controllers.ReadBody(w, r, &q); e != nil || q.Duration <= 0 || q.Position < 0 || q.Position > q.Duration {
		http.Error(w, "invalid progress", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[uid(r)]
	if !ok {
		p = defaultProfile(uid(r))
	}
	h := []History{{VideoID: video, Position: q.Position, Duration: q.Duration, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}}
	for _, old := range p.History {
		if old.VideoID != video && len(h) < 100 {
			h = append(h, old)
		}
	}
	p.History = h
	s.state.Profiles[uid(r)] = p
	if e := s.save(); e != nil {
		http.Error(w, "profile persistence failed", 503)
		return
	}
	controllers.JSON(w, 200, p)
}
func (s *Server) clearHistory(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.state.Profiles[uid(r)]
	if !ok {
		p = defaultProfile(uid(r))
	}
	p.History = []History{}
	s.state.Profiles[uid(r)] = p
	if e := s.save(); e != nil {
		http.Error(w, "profile persistence failed", 503)
		return
	}
	controllers.JSON(w, 200, p)
}
func (s *Server) placement(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.movie(r.PathValue("id"), uid(r)); !ok {
		http.NotFound(w, r)
		return
	}
	v, e := s.record(r.Context(), r.PathValue("id"))
	if e != nil {
		http.Error(w, "catalogue unavailable", 503)
		return
	}
	units := []map[string]any{}
	for _, u := range v.Units {
		units = append(units, map[string]any{"extent_id": u.ID, "node_id": u.NodeID, "zone_id": u.ZoneID, "size_bytes": u.Size, "state": u.State, "sha256": u.SHA256, "proof": u.Proof})
	}
	controllers.JSON(w, 200, map[string]any{"video_id": v.Upload.Manifest.VideoID, "state": v.State, "units": units})
}
func (s *Server) topology(w http.ResponseWriter, r *http.Request) {
	var snap controllers.Inspection
	if e := s.control(r.Context(), "GET", "/api/v1/snapshot", nil, &snap); e != nil {
		http.Error(w, "catalogue unavailable", 503)
		return
	}
	nodes := []map[string]any{}
	zones := map[string]uint64{}
	for _, entry := range snap.Nodes {
		n := entry.Node
		fresh := time.Since(n.Heartbeat) < s.cfg.NodeTTL
		state := n.State
		if !fresh {
			state = placement.Unavailable
		}
		nodes = append(nodes, map[string]any{"node_id": n.ID, "zone_id": n.Zone, "state": state, "available_for_project_bytes": n.Available, "heartbeat_at": n.Heartbeat})
		if fresh && state != placement.Unavailable {
			zones[n.Zone] += n.Available
		}
	}
	if r.URL.Path == "/api/nodes" {
		controllers.JSON(w, 200, map[string]any{"items": nodes})
		return
	}
	controllers.JSON(w, 200, map[string]any{"capacity_by_zone": zones, "note": "net snapshots; no physical disk capacity"})
}
func (s *Server) edge(ctx context.Context, u controllers.Unit, method string, body io.Reader) (*http.Response, error) {
	raw, e := os.ReadFile(filepath.Join(s.cfg.SecretsDir, "edge-"+u.NodeID+".http-token"))
	if e != nil {
		return nil, e
	}
	q, e := http.NewRequestWithContext(ctx, method, u.Endpoint+"/store/"+url.PathEscape(ctx.Value(videoKey{}).(string))+"/"+fmt.Sprint(u.ID), body)
	if e != nil {
		return nil, e
	}
	q.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(raw)))
	if method == "PUT" {
		q.ContentLength = int64(u.Size)
		q.Header.Set("Content-Type", "application/octet-stream")
	}
	return s.client.Do(q)
}

type videoKey struct{}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if controllers.Env("REQUIRE_SIS_IBS", "0") == "1" {
		http.Error(w, "use the local owner companion to sign this publication", 428)
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	if len(title) == 0 || len(title) > 120 {
		http.Error(w, "title required (1-120 characters)", 400)
		return
	}
	category := r.URL.Query().Get("category")
	if category == "" {
		category = "projets"
	}
	if !controllers.Has([]string{"vie-etudiante", "evenements", "sport", "culture", "projets", "conferences", "campus", "associations"}, category) {
		http.Error(w, "invalid category", 400)
		return
	}
	dir, e := os.MkdirTemp(filepath.Join(s.cfg.DataDir, "tmp"), "publish-")
	if e != nil {
		http.Error(w, "upload storage unavailable", 503)
		return
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "input.mp4")
	f, e := os.Create(input)
	if e != nil {
		http.Error(w, "upload storage unavailable", 503)
		return
	}
	n, e := io.Copy(f, http.MaxBytesReader(w, r.Body, s.cfg.MaxUpload))
	closeErr := f.Close()
	if e != nil || closeErr != nil || n < 12 {
		http.Error(w, "invalid or oversized upload", 413)
		return
	}
	random := make([]byte, 16)
	if _, e = rand.Read(random); e != nil {
		http.Error(w, "identity unavailable", 503)
		return
	}
	id := "v-" + hex.EncodeToString(random)
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", s.cfg.VideoScript, input, filepath.Join(dir, "hls"))
	if s.cfg.BlockPipeline {
		cmd.Env = append(os.Environ(), "VIDEO_BLOCK_PIPELINE=1", "VIDEO_ID="+id, "VIDEO_OWNER_ID="+uid(r))
	}
	output, e := cmd.CombinedOutput()
	if e != nil {
		log.Printf("segmentation failed: %s", output)
		http.Error(w, "video segmentation failed; use an MP4 readable by FFmpeg", 422)
		return
	}
	var pack struct {
		Version      int           `json:"version"`
		Assets       []media.Asset `json:"assets"`
		NativeSource bool          `json:"native_source"`
		Duration     float64       `json:"duration"`
		Playlist     string        `json:"playlist"`
		Files        []string      `json:"files"`
	}
	raw, e := os.ReadFile(filepath.Join(dir, "hls", "package.json"))
	if e != nil || json.Unmarshal(raw, &pack) != nil || len(pack.Files) == 0 || len(pack.Files) > 512 {
		http.Error(w, "invalid video package", 502)
		return
	}
	movie := Movie{LayoutVersion: pack.Version, Assets: pack.Assets, NativeSource: pack.NativeSource, ID: id, Owner: uid(r), Title: title, Category: category, Public: r.URL.Query().Get("public") == "true", PublishedAt: time.Now().UTC().Format(time.RFC3339Nano), Duration: pack.Duration, Playlist: pack.Playlist}
	upload := controllers.Upload{Manifest: placement.Manifest{RequestID: id, VideoID: id, SourceNodeID: s.cfg.SourceNodeID}, Target: 1}
	var total int64
	for i, name := range pack.Files {
		if name != filepath.Base(name) || !placement.ValidID(name) {
			http.Error(w, "invalid package path", 502)
			return
		}
		raw, e := os.ReadFile(filepath.Join(dir, "hls", name))
		if e != nil || len(raw) == 0 {
			http.Error(w, "incomplete media package", 502)
			return
		}
		total += int64(len(raw))
		limit := s.cfg.MaxUpload * 2
		if pack.Version == 2 {
			limit = s.cfg.MaxUpload * 3
		}
		if total > limit {
			http.Error(w, "generated media exceeds limit", 413)
			return
		}
		hash := sha256.Sum256(raw)
		upload.Manifest.Segments = append(upload.Manifest.Segments, placement.Segment{ID: uint64(i), Size: uint64(len(raw))})
		upload.Contents = append(upload.Contents, controllers.Content{ExtentID: uint64(i), SHA256: hex.EncodeToString(hash[:])})
		movie.Files = append(movie.Files, MediaFile{Name: name, ExtentID: uint64(i)})
	}
	if pack.Version == 2 {
		if e := validateAssets(movie, upload); e != nil {
			http.Error(w, "invalid block package mapping", 502)
			return
		}
	}
	upload.Manifest.Total = uint64(total)
	s.mu.Lock()
	s.state.Movies[id] = movie
	e = s.save()
	s.mu.Unlock()
	if e != nil {
		http.Error(w, "metadata persistence failed", 503)
		return
	}
	var plan controllers.UploadRecord
	if e = s.control(ctx, "POST", "/api/v1/uploads", upload, &plan); e != nil {
		controllers.JSON(w, 409, map[string]string{"error": "placement refused or unavailable", "video_id": id})
		return
	}
	ctx = context.WithValue(ctx, videoKey{}, id)
	if len(plan.Units) != len(movie.Files) {
		http.Error(w, "unexpected Go plan boundaries", 502)
		return
	}
	for _, u := range plan.Units {
		if u.ID >= uint64(len(movie.Files)) || u.First != u.Last || u.First != u.ID {
			http.Error(w, "unexpected Go plan boundaries", 502)
			return
		}
		f, e := os.Open(filepath.Join(dir, "hls", movie.Files[u.ID].Name))
		if e != nil {
			http.Error(w, "package unavailable", 503)
			return
		}
		res, e := s.edge(ctx, u, "PUT", f)
		f.Close()
		if e != nil {
			http.Error(w, "Edge transfer failed; video is not READY", 502)
			return
		}
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		res.Body.Close()
		if res.StatusCode != 201 && res.StatusCode != 200 {
			http.Error(w, "Edge rejected transfer; video is not READY", 502)
			return
		}
	}
	result, e := s.record(ctx, id)
	if e != nil || !realReady(result) {
		http.Error(w, "all real completions not yet confirmed", 503)
		return
	}
	controllers.JSON(w, 201, map[string]any{"id": id, "state": result.State, "integrity": "SHA256 (not SIS/IBS)", "playback": "/api/videos/" + id + "/playback"})
}
func (s *Server) playback(w http.ResponseWriter, r *http.Request) {
	m, ok := s.movie(r.PathValue("id"), uid(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	v, e := s.record(r.Context(), m.ID)
	if e != nil || !realReady(v) {
		http.Error(w, "video not READY", 409)
		return
	}
	if e = s.preparePlayback(r.Context(), m, v); e != nil {
		log.Printf("playback %s failed: %v", m.ID, e)
		http.Error(w, "media missing or integrity check failed", 502)
		return
	}
	integrity := "SHA256 verified from Edges; legacy prototype SIS/IBS not active"
	if m.LayoutVersion == 3 {
		integrity = "SIS-HESS-BLS12381-v3 signed assets verified on Edge"
	}
	controllers.JSON(w, 200, map[string]any{"url": "/media/" + m.ID + "/video.mp4", "sources": []any{}, "subtitles": []any{}, "integrity": integrity})
}
func (s *Server) preparePlayback(ctx context.Context, m Movie, v controllers.UploadRecord) error {
	if e := validateSignedPlaylist(m); e != nil {
		return e
	}
	value, _ := s.videoLocks.LoadOrStore(m.ID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if m.LayoutVersion == 3 && s.secureCacheHit(m) {
		return nil
	}
	s.mediaMu.Lock()
	delete(s.verified, m.ID)
	s.mediaMu.Unlock()
	dir, e := os.MkdirTemp(filepath.Join(s.cfg.DataDir, "tmp"), "read-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	ctx = context.WithValue(ctx, videoKey{}, m.ID)
	if len(v.Units) != len(m.Files) {
		return errors.New("incomplete media mapping")
	}
	if m.LayoutVersion >= 2 {
		for _, asset := range m.Assets {
			if m.NativeSource && asset.Name != "original.mp4" || !m.NativeSource && asset.Name == "original.mp4" {
				continue
			}
			if e = s.fetchAsset(ctx, m, v, asset, filepath.Join(dir, asset.Name)); e != nil {
				return e
			}
		}
		if m.NativeSource {
			final := filepath.Join(s.cfg.DataDir, "cache", m.ID+".mp4")
			if e = os.Rename(filepath.Join(dir, "original.mp4"), final); e != nil {
				return e
			}
			s.mediaMu.Lock()
			s.verified[m.ID] = final
			s.mediaMu.Unlock()
			if m.LayoutVersion == 3 {
				s.rememberSecure(m, final)
			}
			return nil
		}
	} else {
		var total uint64
		for _, u := range v.Units {
			if u.ID >= uint64(len(m.Files)) || m.Files[u.ID].ExtentID != u.ID {
				return errors.New("unexpected extent")
			}
			total += u.Size
			if total > uint64(s.cfg.MaxUpload*2) {
				return errors.New("media read limit")
			}
			res, e := s.edge(ctx, u, "GET", nil)
			if e != nil {
				return e
			}
			if res.StatusCode != 200 {
				res.Body.Close()
				return errors.New("stored media unavailable")
			}
			f, e := os.Create(filepath.Join(dir, m.Files[u.ID].Name))
			if e != nil {
				res.Body.Close()
				return e
			}
			h := sha256.New()
			size, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, int64(u.Size)+1))
			f.Close()
			res.Body.Close()
			if e != nil || uint64(size) != u.Size || hex.EncodeToString(h.Sum(nil)) != u.SHA256 {
				return errors.New("stored unit size/SHA256 mismatch")
			}
		}
	}
	if e = os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(m.Playlist), 0600); e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-protocol_whitelist", "file,crypto,data", "-i", filepath.Join(dir, "index.m3u8"), "-c", "copy", "-movflags", "+faststart", filepath.Join(dir, "playback.mp4"))
	if output, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("remux: %s: %w", output, e)
	}
	final := filepath.Join(s.cfg.DataDir, "cache", m.ID+".mp4")
	if e = os.Rename(filepath.Join(dir, "playback.mp4"), final); e != nil {
		return e
	}
	s.mediaMu.Lock()
	s.verified[m.ID] = final
	s.mediaMu.Unlock()
	if m.LayoutVersion == 3 {
		s.rememberSecure(m, final)
	}
	return nil
}
func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.movie(id, uid(r)); !ok {
		http.NotFound(w, r)
		return
	}
	v, e := s.record(r.Context(), id)
	if e != nil || !realReady(v) {
		http.Error(w, "video not READY", 409)
		return
	}
	s.mediaMu.Lock()
	path := s.verified[id]
	var file *os.File
	if path != "" {
		file, e = os.Open(path)
	}
	s.mediaMu.Unlock()
	if file == nil || e != nil {
		http.Error(w, "request playback metadata first to verify media", 409)
		return
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		http.Error(w, "media unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, id+".mp4", info.ModTime(), file)
}
