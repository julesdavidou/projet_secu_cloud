package transportsecurity

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// A route only changes the dial address. URL hostname, TLS SNI, certificate
// verification, storage identity and signed media remain unchanged.
type Route struct {
	Address string    `json:"address"`
	Expires time.Time `json:"expires_at"`
}

var directory = struct {
	sync.RWMutex
	routes map[string]Route
}{routes: map[string]Route{}}

func SetRoutes(routes map[string]Route) {
	v := map[string]Route{}
	for host, r := range routes {
		_, port, e := net.SplitHostPort(r.Address)
		ip, _, _ := net.SplitHostPort(r.Address)
		if e == nil && net.ParseIP(ip) != nil && port != "" {
			v[strings.ToLower(host)] = r
		}
	}
	directory.Lock()
	directory.routes = v
	directory.Unlock()
}
func Routes() map[string]Route {
	directory.RLock()
	defer directory.RUnlock()
	out := map[string]Route{}
	for h, r := range directory.routes {
		if time.Now().Before(r.Expires) {
			out[h] = r
		}
	}
	return out
}
func dialRoute(ctx context.Context, network, address string) (net.Conn, error) {
	directory.RLock()
	r, ok := directory.routes[strings.ToLower(address)]
	directory.RUnlock()
	if ok && time.Now().Before(r.Expires) {
		address = r.Address
	}
	return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, address)
}

// Zone directory leases are independently authenticated and remain usable when
// the hub is down. No detailed foreign capacity registry is exchanged here.
func RefreshZoneRoutes(ctx context.Context, client *http.Client, routes map[string]Route) {
	if routes == nil {
		routes = map[string]Route{}
	}
	for _, origin := range strings.Split(os.Getenv("DISCOVERY_ZONE_URLS"), ",") {
		if !strings.HasPrefix(origin, "https://") {
			continue
		}
		op, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, e := http.NewRequestWithContext(op, "GET", origin+"/api/v1/discovery/routes", nil)
		if e == nil {
			res, err := client.Do(req)
			if err == nil {
				if res.StatusCode == 200 {
					var values map[string]Route
					if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&values) == nil {
						for h, v := range values {
							if old, ok := routes[h]; !ok || v.Expires.After(old.Expires) {
								routes[h] = v
							}
						}
					}
				}
				res.Body.Close()
			}
		}
		cancel()
	}
	SetRoutes(routes)
}
