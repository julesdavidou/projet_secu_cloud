package edgecontrol

import (
	"bytes"
	"encoding/json"
	"github.com/julesdavidou/projet_secu_cloud/internal/transportsecurity"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"github.com/julesdavidou/projet_secu_cloud/pkg/media"
	"io"
	"net/http"
	"strconv"
	"time"
)

func (b *Bridge) readAuthorized(w http.ResponseWriter, r *http.Request) {
	if grant := r.Header.Get("X-Read-Capability"); grant != "" {
		video, id := r.PathValue("video"), parseExtent(r.PathValue("extent"))
		b.mu.Lock()
		u, ok := b.state.Units[key(video, id)]
		b.mu.Unlock()
		if !ok || u.State != "STORED" || !media.VerifyRead(grant, b.token, b.spec.ID, video, id, u.Unit.SHA256, time.Now()) {
			http.Error(w, "invalid or expired read capability", 401)
			return
		}
		b.read(w, r)
		return
	}
	controllers.Bearer(b.token, http.HandlerFunc(b.read)).ServeHTTP(w, r)
}
func (b *Bridge) reconstruct(w http.ResponseWriter, r *http.Request) {
	endpoint := controllers.Env("EDGE_RECONSTRUCTOR_URL", "")
	if endpoint == "" {
		http.Error(w, "Edge reconstruction worker not configured", 503)
		return
	}
	var job media.Job
	if controllers.ReadBody(w, r, &job) != nil || (job.Version != 2 && job.Version != 3) || len(job.Asset.Blocks) == 0 || len(job.Asset.Blocks) > 512 || job.Asset.Size > 64<<20 {
		http.Error(w, "invalid reconstruction contract", 400)
		return
	}
	// Destinations come exclusively from the provisioned topology, never arbitrary URLs.
	for _, block := range job.Asset.Blocks {
		node, ok := b.cfg.Node(block.NodeID)
		if !ok || node.Endpoint != block.Endpoint || block.ReadCapability == "" {
			http.Error(w, "unprovisioned block destination", 400)
			return
		}
	}
	// Ignore routes submitted by callers; only the authenticated central directory may supply them.
	job.DialRoutes = nil
	if b.cfg.DiscoveryEnabled {
		job.DialRoutes = map[string]media.DialRoute{}
		for h, v := range transportsecurity.Routes() {
			job.DialRoutes[h] = media.DialRoute{Address: v.Address, Expires: v.Expires}
		}
	}
	raw, e := json.Marshal(job)
	if e != nil {
		http.Error(w, "invalid reconstruction job", 400)
		return
	}
	route := "/v2/reconstruct"
	if job.Version == 3 {
		route = "/v3/reconstruct"
	}
	q, e := http.NewRequestWithContext(r.Context(), "POST", endpoint+route, bytes.NewReader(raw))
	if e != nil {
		http.Error(w, "invalid worker configuration", 503)
		return
	}
	q.Header.Set("Authorization", "Bearer "+b.token)
	q.Header.Set("Content-Type", "application/json")
	response, e := (&http.Client{Timeout: 120 * time.Second}).Do(q)
	if e != nil {
		http.Error(w, "Edge reconstruction worker unavailable", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		http.Error(w, "Edge reconstruction verification failed", 502)
		return
	}
	if response.ContentLength != int64(job.Asset.Size) {
		http.Error(w, "worker length mismatch", 502)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatUint(job.Asset.Size, 10))
	w.Header().Set("X-Reconstruction-Node", b.spec.ID)
	w.WriteHeader(200)
	io.Copy(w, io.LimitReader(response.Body, int64(job.Asset.Size)+1))
}
