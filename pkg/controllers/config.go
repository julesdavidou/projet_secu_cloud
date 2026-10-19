// Package controllers adapts the existing placement core, MQTT5 bus and
// authoritative catalog to a configured deployment. It does not implement HRW.
package controllers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/julesdavidou/projet_secu_cloud/internal/transportsecurity"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

type NodeSpec struct {
	ID                string   `json:"id"`
	Zone              string   `json:"zone"`
	Quota             uint64   `json:"quota_bytes"`
	Endpoint          string   `json:"endpoint"`
	AllowedZones      []string `json:"allowed_zones"`
	Mode              string   `json:"mode"`
	BrokerName        string   `json:"broker_hostname,omitempty"`
	ReconstructorName string   `json:"reconstructor_hostname,omitempty"`
	ServiceRate       uint64   `json:"microcredits_per_gib_hour,omitempty"`
}
type Config struct {
	DiscoveryEnabled bool       `json:"discovery_enabled,omitempty"`
	Zones            []string   `json:"zones"`
	Nodes            []NodeSpec `json:"nodes"`
	NodeTTLSeconds   int        `json:"node_ttl_seconds"`
	LeaseSeconds     int        `json:"lease_seconds"`
	Fanout           int        `json:"fanout"`
}

func LoadConfig(path string) (Config, error) {
	if e := transportsecurity.ConfigureHTTP(); e != nil {
		return Config{}, e
	}
	var c Config
	raw, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	if e = mqttbus.Decode(raw, &c); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if len(c.Zones) < 1 || len(c.Zones) > 6 || len(c.Nodes) == 0 || c.NodeTTLSeconds < 2 || c.NodeTTLSeconds > 60 || c.LeaseSeconds < 5 || c.LeaseSeconds > 600 || c.Fanout < 1 || c.Fanout > 5 {
		return errors.New("invalid topology, TTL, lease or fanout")
	}
	seen := map[string]bool{"central": true, "admin": true}
	for _, id := range c.Zones {
		actor := "zone-" + id
		if !placement.ValidID(actor) || seen[actor] {
			return errors.New("duplicate/invalid zone")
		}
		seen[actor] = true
	}
	for _, n := range c.Nodes {
		actor := "edge-" + n.ID
		if !placement.ValidID(actor) || seen[actor] || !c.HasZone(n.Zone) || n.Quota == 0 || n.Quota > 1<<50 || len(n.AllowedZones) == 0 || !Has(n.AllowedZones, n.Zone) || (n.Mode != "real-http" && n.Mode != "mock") {
			return errors.New("invalid node identity, zone, mode or project quota")
		}
		if n.ReconstructorName != "" && !placement.ValidID(n.ReconstructorName) {
			return errors.New("invalid reconstructor hostname")
		}
		if n.BrokerName != "" && !placement.ValidID(n.BrokerName) {
			return errors.New("invalid broker hostname")
		}
		seen[actor] = true
		u, e := url.Parse(n.Endpoint)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("node endpoint must be an explicit http origin")
		}
		zones := map[string]bool{}
		for _, z := range n.AllowedZones {
			if !c.HasZone(z) || zones[z] {
				return errors.New("invalid allowed zones")
			}
			zones[z] = true
		}
	}
	return nil
}
func Has(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}
func (c Config) HasZone(id string) bool { return Has(c.Zones, id) }
func (c Config) Node(id string) (NodeSpec, bool) {
	for _, n := range c.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return NodeSpec{}, false
}
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func Secret(name string) (string, error) {
	raw, e := os.ReadFile(filepath.Join(Env("SECRETS_DIR", "/run/lab-secrets"), name))
	if e != nil {
		return "", e
	}
	s := strings.TrimSpace(string(raw))
	if len(s) < 24 {
		return "", errors.New("missing or weak provisioned secret")
	}
	return s, nil
}
func EventID(parts ...any) string {
	raw, _ := json.Marshal(parts)
	h := sha256.Sum256(raw)
	return "e-" + hex.EncodeToString(h[:])
}
func RPC(ctx context.Context, b *mqttbus.Bus, target, op string, value, out any) error {
	bounded, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return b.Call(bounded, target, op, value, out)
}
func NewBus(ctx context.Context, id string, subs []string, h mqttbus.Handler, state func(string, []byte, bool)) (*mqttbus.Bus, error) {
	password, e := Secret(id + ".password")
	if e != nil {
		return nil, e
	}
	b, e := mqttbus.NewWithProtocol(ctx, Env("MQTT_BROKER", "mqtt://broker:1883"), id, password, subs, h, state, Env("MQTT_PROTOCOL", "5"))
	if e == nil && Env("MQTT_JSON_ENVELOPE", "0") == "1" {
		b.EnableJSONEnvelope()
	}
	return b, e
}
func Authorized(c Config, sender string) bool {
	return sender == "central" || sender == "admin" || strings.HasPrefix(sender, "zone-") && c.HasZone(strings.TrimPrefix(sender, "zone-"))
}
func Bearer(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func ReadBody(w http.ResponseWriter, r *http.Request, out any) error {
	raw := http.MaxBytesReader(w, r.Body, mqttbus.MaxPayload)
	defer raw.Close()
	value, e := io.ReadAll(raw)
	if e != nil {
		return e
	}
	return mqttbus.Decode(value, out)
}
func Serve(ctx context.Context, addr string, mux *http.ServeMux) error {
	s := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdown)
	}()
	e := s.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
func Health(mux *http.ServeMux, ready func() bool) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "alive"}) })
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		code := 200
		if !ready() {
			code = 503
		}
		JSON(w, code, map[string]bool{"ready": code == 200})
	})
}
func ValidHash(s string) bool {
	raw, e := hex.DecodeString(s)
	return e == nil && len(raw) == 32 && s == strings.ToLower(s)
}
func AtomicJSON(path string, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(raw) > 32<<20 {
		return errors.New("durable state limit exceeded")
	}
	if e = os.MkdirAll(filepath.Dir(path), 0750); e != nil {
		return e
	}
	f, e := os.OpenFile(path+".next", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(raw); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(path+".next", path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func ReadJSON(path string, out any) error {
	raw, e := os.ReadFile(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if len(raw) > 32<<20 {
		return fmt.Errorf("state too large: %s", filepath.Base(path))
	}
	return json.Unmarshal(raw, out)
}
