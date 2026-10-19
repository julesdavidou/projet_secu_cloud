package webapp

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type signedPackage struct {
	Version      int                        `json:"version"`
	VideoID      string                     `json:"video_id"`
	OwnerID      string                     `json:"owner_id"`
	Title        string                     `json:"title"`
	Category     string                     `json:"category"`
	Public       bool                       `json:"public"`
	Revision     uint64                     `json:"revision"`
	Previous     string                     `json:"previous"`
	Duration     float64                    `json:"duration"`
	Playlist     string                     `json:"playlist"`
	Files        []string                   `json:"files"`
	Assets       []media.Asset              `json:"assets"`
	Proofs       map[string]json.RawMessage `json:"proofs"`
	NativeSource bool                       `json:"native_source"`
}

func (s *Server) signedUpload(w http.ResponseWriter, r *http.Request) {
	if controllers.Env("SECURITY_VERIFY_URL", "") == "" {
		http.Error(w, "secure publication not configured", 503)
		return
	}
	dir, e := os.MkdirTemp(filepath.Join(s.cfg.DataDir, "tmp"), "signed-")
	if e != nil {
		http.Error(w, "temporary storage unavailable", 503)
		return
	}
	defer os.RemoveAll(dir)
	reader := tar.NewReader(http.MaxBytesReader(w, r.Body, s.cfg.MaxUpload*3))
	var pack signedPackage
	files := map[string][]byte{}
	var rawManifest []byte
	var total int64
	for count := 0; ; count++ {
		h, e := reader.Next()
		if e == io.EOF {
			break
		}
		if e != nil || count > 513 || h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != h.Name || h.Size <= 0 || h.Size > 4<<20 {
			http.Error(w, "invalid bounded signed package", 422)
			return
		}
		raw, e := io.ReadAll(io.LimitReader(reader, h.Size+1))
		if e != nil || int64(len(raw)) != h.Size {
			http.Error(w, "truncated package", 422)
			return
		}
		total += h.Size
		if total > s.cfg.MaxUpload*3 {
			http.Error(w, "package too large", 413)
			return
		}
		if h.Name == "signed-manifest.json" {
			if rawManifest != nil {
				http.Error(w, "duplicate manifest", 422)
				return
			}
			rawManifest = raw
			if json.Unmarshal(raw, &pack) != nil {
				http.Error(w, "invalid signed manifest", 422)
				return
			}
			continue
		}
		if !placement.ValidID(h.Name) || !strings.HasPrefix(h.Name, "block-") || !strings.HasSuffix(h.Name, ".bin") || files[h.Name] != nil {
			http.Error(w, "invalid or duplicate block", 422)
			return
		}
		files[h.Name] = raw
	}
	if pack.Version != 3 || pack.OwnerID != uid(r) || !placement.ValidID(pack.VideoID) || pack.Revision != 1 || pack.Previous != "" || len(pack.Files) == 0 || len(pack.Files) > 512 || len(pack.Files) != len(files) || len(pack.Assets) != len(pack.Proofs) || len(pack.Title) > 200 || len(pack.Playlist) > 1<<20 {
		http.Error(w, "owner, version or package mismatch", 422)
		return
	}
	m := Movie{ID: pack.VideoID, LayoutVersion: 3, Owner: uid(r), Title: pack.Title, Category: pack.Category, Public: pack.Public, PublishedAt: time.Now().UTC().Format(time.RFC3339Nano), Duration: pack.Duration, Playlist: pack.Playlist, Assets: pack.Assets, NativeSource: pack.NativeSource, Proofs: pack.Proofs, Revision: pack.Revision}
	digest := sha256.Sum256(rawManifest)
	upload := controllers.Upload{Manifest: placement.Manifest{RequestID: m.ID, VideoID: m.ID, SourceNodeID: s.cfg.SourceNodeID}, Target: 1, OwnerID: m.Owner, ManifestSHA256: hex.EncodeToString(digest[:])}
	payloads := map[string][]byte{}
	for i, name := range pack.Files {
		raw, ok := files[name]
		if !ok || len(raw) == 0 {
			http.Error(w, "missing block", 422)
			return
		}
		hash := sha256.Sum256(raw)
		id := uint64(i)
		upload.Manifest.Segments = append(upload.Manifest.Segments, placement.Segment{ID: id, Size: uint64(len(raw))})
		upload.Contents = append(upload.Contents, controllers.Content{ExtentID: id, SHA256: hex.EncodeToString(hash[:])})
		upload.Manifest.Total += uint64(len(raw))
		m.Files = append(m.Files, MediaFile{Name: name, ExtentID: id})
		payloads[strconv.FormatUint(id, 10)] = raw
		if e = os.WriteFile(filepath.Join(dir, name), raw, 0600); e != nil {
			http.Error(w, "package persistence unavailable", 503)
			return
		}
	}
	if validateAssets(m, upload) != nil || validateSignedPlaylist(m) != nil {
		http.Error(w, "invalid asset mapping", 422)
		return
	}
	verification := map[string]any{"owner_id": m.Owner, "video_id": m.ID, "revision": m.Revision, "previous": "", "public": m.Public, "assets": m.Assets, "proofs": m.Proofs, "payloads": payloads}
	if e = s.verifySecure(r.Context(), verification); e != nil {
		http.Error(w, "SIS/IBS verification refused publication", 422)
		return
	}
	s.mu.Lock()
	if _, ok := s.state.Movies[m.ID]; ok {
		s.mu.Unlock()
		http.Error(w, "video identity already exists", 409)
		return
	}
	s.state.Movies[m.ID] = m
	e = s.save()
	s.mu.Unlock()
	if e != nil {
		http.Error(w, "metadata persistence failed", 503)
		return
	}
	var plan controllers.UploadRecord
	if e = s.control(r.Context(), "POST", "/api/v1/uploads", upload, &plan); e != nil {
		http.Error(w, "Go placement refused", 409)
		return
	}
	ctx := context.WithValue(r.Context(), videoKey{}, m.ID)
	if len(plan.Units) != len(m.Files) {
		http.Error(w, "unexpected plan", 502)
		return
	}
	for _, unit := range plan.Units {
		if unit.ID >= uint64(len(m.Files)) {
			http.Error(w, "unexpected extent", 502)
			return
		}
		raw := files[m.Files[unit.ID].Name]
		res, e := s.edge(ctx, unit, "PUT", bytes.NewReader(raw))
		if e != nil {
			http.Error(w, "Edge transfer unavailable", 502)
			return
		}
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		res.Body.Close()
		if res.StatusCode != 201 && res.StatusCode != 200 {
			http.Error(w, "Edge refused verified write", 502)
			return
		}
	}
	final, e := s.record(ctx, m.ID)
	if e != nil || !realReady(final) {
		http.Error(w, "real completions not confirmed", 503)
		return
	}
	controllers.JSON(w, 201, map[string]any{"id": m.ID, "state": "READY", "integrity": "SIS-HESS-BLS12381-v3 + SHA256", "owner_id": m.Owner, "manifest_sha256": upload.ManifestSHA256, "playback": "/api/videos/" + m.ID + "/playback"})
}
func (s *Server) verifySecure(ctx context.Context, body any) error {
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", controllers.Env("SECURITY_VERIFY_URL", "")+"/v3/verify/package", bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	response, e := s.client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("secure verification failed")
	}
	var result struct {
		Verified bool `json:"verified"`
	}
	if json.NewDecoder(response.Body).Decode(&result) != nil || !result.Verified {
		return errors.New("invalid verifier reply")
	}
	return nil
}
