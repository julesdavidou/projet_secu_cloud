package controllers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/internal/transportsecurity"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
)

type DiscoveryCandidate struct {
	NodeID     string `json:"node_id"`
	IP         string `json:"ip"`
	HTTPPort   uint16 `json:"http_port"`
	BrokerPort uint16 `json:"broker_port,omitempty"`
	BrokerIP   string `json:"broker_ip,omitempty"`
	WorkerPort uint16 `json:"worker_port,omitempty"`
	WorkerIP   string `json:"worker_ip,omitempty"`
}
type Discovery struct {
	DiscoveryCandidate
	ZoneID     string    `json:"zone_id"`
	Generation uint64    `json:"generation"`
	Observed   time.Time `json:"observed_at"`
	Expires    time.Time `json:"expires_at"`
}

func validCandidate(cfg Config, q DiscoveryCandidate) error {
	n, ok := cfg.Node(q.NodeID)
	ip, e := netip.ParseAddr(q.IP)
	if !ok || n.Mode != "real-http" || e != nil || !ip.Is4() || !ip.IsPrivate() || q.HTTPPort == 0 {
		return errors.New("candidate must name a provisioned real Edge at a private IPv4 address")
	}
	for _, address := range []string{q.BrokerIP, q.WorkerIP} {
		if address == "" {
			continue
		}
		ip, e := netip.ParseAddr(address)
		if e != nil || !ip.Is4() || !ip.IsPrivate() {
			return errors.New("invalid broker address")
		}
	}
	u, e := url.Parse(n.Endpoint)
	if e != nil || u.Scheme != "https" {
		return errors.New("discovery requires a TLS endpoint")
	}
	return nil
}
func validateDiscovery(cfg Config, d Discovery, now time.Time) error {
	if e := validCandidate(cfg, d.DiscoveryCandidate); e != nil {
		return e
	}
	n, _ := cfg.Node(d.NodeID)
	if !Has(n.AllowedZones, d.ZoneID) || d.Generation == 0 || d.Observed.IsZero() || d.Observed.After(now.Add(time.Second)) || now.Sub(d.Observed) > 30*time.Second || !d.Expires.After(now) || d.Expires.Sub(d.Observed) > 30*time.Second {
		return errors.New("invalid discovery lease")
	}
	return nil
}
func discoveryRoutes(cfg Config, values map[string]Discovery, now time.Time) map[string]transportsecurity.Route {
	out := map[string]transportsecurity.Route{}
	for _, d := range values {
		if validateDiscovery(cfg, d, now) != nil {
			continue
		}
		n, _ := cfg.Node(d.NodeID)
		u, _ := url.Parse(n.Endpoint)
		port := u.Port()
		if port == "" {
			port = "443"
		}
		out[net.JoinHostPort(strings.ToLower(u.Hostname()), port)] = transportsecurity.Route{Address: net.JoinHostPort(d.IP, strconv.Itoa(int(d.HTTPPort))), Expires: d.Expires}
		if d.WorkerPort != 0 {
			ip := d.WorkerIP
			if ip == "" {
				ip = d.IP
			}
			out[net.JoinHostPort(strings.ToLower("reconstructor-"+d.NodeID), "8443")] = transportsecurity.Route{Address: net.JoinHostPort(ip, strconv.Itoa(int(d.WorkerPort))), Expires: d.Expires}
		}
		if d.BrokerPort != 0 {
			out[net.JoinHostPort(strings.ToLower("broker-"+d.NodeID), "8883")] = transportsecurity.Route{Address: net.JoinHostPort(brokerAddress(d.DiscoveryCandidate), strconv.Itoa(int(d.BrokerPort))), Expires: d.Expires}
		}
	}
	return out
}
func discoveryTLS(host, actor string) (*tls.Config, error) {
	c, e := transportsecurity.Client()
	if e != nil {
		return nil, e
	}
	if c == nil {
		return nil, errors.New("mTLS discovery unavailable")
	}
	c.ServerName = host
	// Keep default chain/hostname verification; additionally bind the expected
	// project actor BEFORE sending the authenticated HTTP request.
	c.VerifyConnection = func(s tls.ConnectionState) error {
		if len(s.PeerCertificates) == 0 || s.PeerCertificates[0].Subject.CommonName != actor {
			return errors.New("discovery certificate identity mismatch")
		}
		return nil
	}
	return c, nil
}
func probeDiscovery(ctx context.Context, cfg Config, zone string, q DiscoveryCandidate) (Discovery, error) {
	if e := validCandidate(cfg, q); e != nil {
		return Discovery{}, e
	}
	spec, _ := cfg.Node(q.NodeID)
	if !Has(spec.AllowedZones, zone) {
		return Discovery{}, errors.New("Edge not admitted in this zone")
	}
	u, _ := url.Parse(spec.Endpoint)
	tc, e := discoveryTLS(u.Hostname(), "edge-"+q.NodeID)
	if e != nil {
		return Discovery{}, e
	}
	transport := &http.Transport{TLSClientConfig: tc, Proxy: nil, DialContext: func(c context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(c, n, net.JoinHostPort(q.IP, strconv.Itoa(int(q.HTTPPort))))
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "GET", spec.Endpoint+"/discovery", nil)
	if e != nil {
		return Discovery{}, e
	}
	res, e := client.Do(req)
	if e != nil {
		return Discovery{}, e
	}
	defer res.Body.Close()
	var identity struct {
		NodeID     string `json:"node_id"`
		ZoneID     string `json:"zone_id"`
		Generation uint64 `json:"generation"`
	}
	if res.StatusCode != 200 || json.NewDecoder(http.MaxBytesReader(nil, res.Body, 4096)).Decode(&identity) != nil || identity.NodeID != q.NodeID || !cfg.HasZone(identity.ZoneID) || !Has(spec.AllowedZones, identity.ZoneID) || identity.Generation == 0 {
		return Discovery{}, errors.New("candidate's authenticated zone or identity differs")
	}
	if q.BrokerPort != 0 {
		brokerTLS, e := discoveryTLS("broker-"+q.NodeID, "broker-"+q.NodeID)
		if e != nil {
			return Discovery{}, e
		}
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 2 * time.Second}, Config: brokerTLS}
		conn, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(brokerAddress(q), strconv.Itoa(int(q.BrokerPort))))
		if e != nil {
			return Discovery{}, e
		}
		conn.Close()
	}
	if q.WorkerPort != 0 {
		ip := q.WorkerIP
		if ip == "" {
			ip = q.IP
		}
		workerTLS, e := discoveryTLS("reconstructor-"+q.NodeID, "reconstructor-"+q.NodeID)
		if e != nil {
			return Discovery{}, e
		}
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 2 * time.Second}, Config: workerTLS}
		conn, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(int(q.WorkerPort))))
		if e != nil {
			return Discovery{}, e
		}
		conn.Close()
	}
	now := time.Now()
	return Discovery{DiscoveryCandidate: q, ZoneID: identity.ZoneID, Generation: identity.Generation, Observed: now, Expires: now.Add(30 * time.Second)}, nil
}
func ZoneDiscovery(mux *http.ServeMux, cfg Config, zone string, b *mqttbus.Bus) error {
	var mu sync.Mutex
	last := map[string]time.Time{}
	local := map[string]Discovery{}
	path := filepath.Join(filepath.Dir(Env("LEDGER_PATH", "/data/controller-ledger.json")), "network-directory.json")
	if e := ReadJSON(path, &local); e != nil {
		return e
	}
	if local == nil {
		local = map[string]Discovery{}
	}
	authorized := func(r *http.Request) bool {
		return r.Header.Get("X-Verified-Identity") == "zone-"+zone || r.Header.Get("X-Verified-Identity") == "admin"
	}
	mux.HandleFunc("POST /api/v1/discovery/candidates", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "zone discovery certificate required", 403)
			return
		}
		var q DiscoveryCandidate
		if e := ReadBody(w, r, &q); e != nil || validCandidate(cfg, q) != nil {
			http.Error(w, "invalid provisioned candidate", 422)
			return
		}
		mu.Lock()
		limited := time.Since(last[q.NodeID]) < 2*time.Second
		last[q.NodeID] = time.Now()
		mu.Unlock()
		if limited {
			http.Error(w, "candidate probe rate limit", 429)
			return
		}
		d, e := probeDiscovery(r.Context(), cfg, zone, q)
		if e != nil {
			http.Error(w, "candidate TLS admission failed", 422)
			return
		}
		mu.Lock()
		local[d.NodeID] = d
		e = AtomicJSON(path, local)
		mu.Unlock()
		if e != nil {
			http.Error(w, "local network lease unavailable", 503)
			return
		}
		// Foreign peers contribute ONLY authenticated network routes. They never
		// enter this zone's capacity registry or authorize a storage assignment.
		if d.ZoneID == zone {
			go func() {
				op, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = RPC(op, b, "central", "DISCOVERY_SET", d, nil)
			}()
		}
		JSON(w, 200, d)
	})
	mux.HandleFunc("GET /api/v1/discovery/routes", func(w http.ResponseWriter, r *http.Request) {
		peer := r.Header.Get("X-Verified-Identity")
		allowed := authorized(r) || peer == "web" || strings.HasPrefix(peer, "owner-") || strings.HasPrefix(peer, "zone-") && cfg.HasZone(strings.TrimPrefix(peer, "zone-"))
		if strings.HasPrefix(peer, "edge-") {
			_, allowed = cfg.Node(strings.TrimPrefix(peer, "edge-"))
		}
		if !allowed {
			http.Error(w, "project discovery certificate required", 403)
			return
		}
		mu.Lock()
		v := discoveryRoutes(cfg, local, time.Now())
		mu.Unlock()
		JSON(w, 200, v)
	})
	return nil
}
func SyncDiscovery(ctx context.Context, b *mqttbus.Bus) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		routes := map[string]transportsecurity.Route{}
		op, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = RPC(op, b, "central", "DISCOVERY_GET", struct{}{}, &routes)
		cancel()
		transportsecurity.RefreshZoneRoutes(ctx, http.DefaultClient, routes)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (d DiscoveryCandidate) String() string {
	return fmt.Sprintf("%s at %s:%d", d.NodeID, d.IP, d.HTTPPort)
}

func brokerAddress(q DiscoveryCandidate) string {
	if q.BrokerIP != "" {
		return q.BrokerIP
	}
	return q.IP
}
