package webapp

import (
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
	"strings"
	"time"
)

func validateAssets(m Movie, upload controllers.Upload) error {
	seen := map[uint64]bool{}
	names := map[string]bool{}
	original := false
	for _, a := range m.Assets {
		if !placement.ValidID(a.Name) || names[a.Name] || a.BlockSize == 0 || a.BlockSize > 1<<20 || a.Size == 0 || a.Size > 64<<20 || !controllers.ValidHash(a.SHA256) || uint64(len(a.Blocks)) != (a.Size+a.BlockSize-1)/a.BlockSize {
			return errors.New("invalid asset")
		}
		names[a.Name] = true
		if a.Name == "original.mp4" {
			original = true
		}
		for i, b := range a.Blocks {
			expected := a.BlockSize
			if remaining := a.Size - uint64(i)*a.BlockSize; remaining < expected {
				expected = remaining
			}
			if b.Index != uint64(i) || b.ExtentID >= uint64(len(upload.Contents)) || seen[b.ExtentID] || b.Size != expected || b.SHA256 != upload.Contents[b.ExtentID].SHA256 || b.Size != upload.Manifest.Segments[b.ExtentID].Size {
				return errors.New("inconsistent block mapping")
			}
			seen[b.ExtentID] = true
		}
	}
	if !original || len(seen) != len(upload.Contents) {
		return errors.New("incomplete package")
	}
	return nil
}
func (s *Server) fetchAsset(ctx context.Context, m Movie, v controllers.UploadRecord, a media.Asset, dest string) error {
	if !realReady(v) || len(v.Units) == 0 {
		return errors.New("not READY")
	}
	units := map[uint64]controllers.Unit{}
	for _, u := range v.Units {
		if _, ok := units[u.ID]; ok {
			return errors.New("duplicate catalogue unit")
		}
		units[u.ID] = u
	}
	job := media.Job{Version: 2, VideoID: m.ID, OwnerID: m.Owner, Asset: a}
	if m.LayoutVersion == 3 {
		job.Version = 3
		job.Security = m.Proofs[a.Name]
		job.Revision = m.Revision
		job.Previous = m.Previous
		job.Public = m.Public
		if len(job.Security) == 0 {
			return errors.New("missing required SIS/IBS proof")
		}
	}
	job.Asset.Blocks = append([]media.Block(nil), a.Blocks...)
	for i, block := range job.Asset.Blocks {
		u, ok := units[block.ExtentID]
		if !ok || u.Size != block.Size || u.SHA256 != block.SHA256 {
			return errors.New("catalogue/asset mismatch")
		}
		token, e := os.ReadFile(filepath.Join(s.cfg.SecretsDir, "edge-"+u.NodeID+".http-token"))
		if e != nil {
			return e
		}
		grant, e := media.MintRead(strings.TrimSpace(string(token)), u.NodeID, m.ID, u.ID, u.SHA256, time.Now().Add(90*time.Second))
		if e != nil {
			return e
		}
		job.Asset.Blocks[i].NodeID = u.NodeID
		job.Asset.Blocks[i].Endpoint = u.Endpoint
		job.Asset.Blocks[i].ReadCapability = grant
	}
	receiver := v.Units[0]
	token, e := os.ReadFile(filepath.Join(s.cfg.SecretsDir, "edge-"+receiver.NodeID+".http-token"))
	if e != nil {
		return e
	}
	raw, e := json.Marshal(job)
	if e != nil {
		return e
	}
	q, e := http.NewRequestWithContext(ctx, "POST", receiver.Endpoint+"/reconstruct", bytes.NewReader(raw))
	if e != nil {
		return e
	}
	q.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	q.Header.Set("Content-Type", "application/json")
	response, e := s.client.Do(q)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("Edge reconstruction refused or unavailable")
	}
	f, e := os.Create(dest)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, int64(a.Size)+1))
	if e != nil || uint64(n) != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("reconstructed asset size/SHA256 mismatch")
	}
	return nil
}
func (s *Server) original(w http.ResponseWriter, r *http.Request) {
	m, ok := s.movie(r.PathValue("id"), uid(r))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if m.LayoutVersion < 2 {
		http.Error(w, "source not retained for legacy videos", 409)
		return
	}
	v, e := s.record(r.Context(), m.ID)
	if e != nil || !realReady(v) {
		http.Error(w, "not READY", 409)
		return
	}
	var asset media.Asset
	for _, a := range m.Assets {
		if a.Name == "original.mp4" {
			asset = a
		}
	}
	dir, e := os.MkdirTemp(filepath.Join(s.cfg.DataDir, "tmp"), "original-")
	if e != nil {
		http.Error(w, "cache unavailable", 503)
		return
	}
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "original.mp4")
	if e = s.fetchAsset(r.Context(), m, v, asset, dest); e != nil {
		http.Error(w, "original reconstruction failed", 502)
		return
	}
	f, e := os.Open(dest)
	if e != nil {
		http.Error(w, "cache unavailable", 503)
		return
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		http.Error(w, "cache unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("X-Content-SHA256", asset.SHA256)
	w.Header().Set("X-Reconstruction-Node", v.Units[0].NodeID)
	http.ServeContent(w, r, "original.mp4", info.ModTime(), f)
}
