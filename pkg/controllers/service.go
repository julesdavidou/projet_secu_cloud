package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

// Feed actual inter-zone probes into the coordinator's EXISTING latency/
// transfer-time ordering. This is a recent estimate, not an end-to-end SLA.
func measuredCoordinator(ctx context.Context, c *placement.Coordinator) *placement.Coordinator {
	copy := *c
	copy.Neighbors = append([]placement.Neighbor(nil), c.Neighbors...)
	origins := strings.Split(Env("DISCOVERY_ZONE_URLS", ""), ",")
	var wg sync.WaitGroup
	for i, n := range copy.Neighbors {
		origin := ""
		for _, candidate := range origins {
			u, e := url.Parse(candidate)
			if e == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "zone-"+n.ID) {
				origin = candidate
				break
			}
		}
		if origin == "" {
			continue
		}
		wg.Add(1)
		go func(i int, origin string) {
			defer wg.Done()
			op, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			req, e := http.NewRequestWithContext(op, "GET", origin+"/service/probe", nil)
			if e != nil {
				return
			}
			start := time.Now()
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				return
			}
			first := time.Since(start).Seconds()
			size, e := io.Copy(io.Discard, io.LimitReader(res.Body, (64<<10)+1))
			res.Body.Close()
			elapsed := time.Since(start).Seconds()
			if e == nil && size == 64<<10 && res.StatusCode == 200 && elapsed > 0 {
				goodput := float64(size) / elapsed
				copy.Neighbors[i].Path = placement.Path{RTTSeconds: &first, GoodputBytesPerSecond: &goodput}
			}
		}(i, origin)
	}
	wg.Wait()
	return &copy
}

func probeZone(ctx context.Context, cfg Config, z *placement.Zone, nodes func() []Registered) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		for _, n := range nodes() {
			if n.Node.State == placement.Unavailable {
				continue
			}
			spec, ok := cfg.Node(n.Node.ID)
			if !ok || spec.Mode != "real-http" {
				continue
			}
			op, cancel := context.WithTimeout(ctx, 2*time.Second)
			req, e := http.NewRequestWithContext(op, "GET", n.Endpoint+"/service/probe", nil)
			if e == nil {
				start := time.Now()
				res, err := http.DefaultClient.Do(req)
				if err == nil {
					first := time.Since(start)
					size, readErr := io.Copy(io.Discard, io.LimitReader(res.Body, (64<<10)+1))
					res.Body.Close()
					elapsed := time.Since(start)
					if readErr == nil && res.StatusCode == 200 && size == 64<<10 && elapsed > 0 {
						rtt := uint64(first.Microseconds())
						if rtt == 0 {
							rtt = 1
						}
						_ = z.Observe(placement.Observation{NodeID: n.Node.ID, ZoneID: z.ID, Generation: n.Node.Generation, RTTMicros: rtt, ProbeBytesPerSecond: uint64(float64(size) / elapsed.Seconds()), Rate: spec.ServiceRate, Observed: time.Now()}, time.Now())
					}
				}
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// The central catalog rechecks the source zone's receipt against provisioned
// rates and the exact policy. This does not turn a recent probe into an SLA.
func validateService(cfg Config, u Upload, p placement.DistributedPlan, now time.Time) error {
	if u.Policy == nil {
		if p.Service != nil {
			return errors.New("unexpected service receipt")
		}
		return nil
	}
	if p.Service == nil {
		return errors.New("missing service receipt")
	}
	a, _ := json.Marshal(u.Policy)
	b, _ := json.Marshal(p.Service.Policy)
	if string(a) != string(b) {
		return errors.New("service policy changed")
	}
	for _, unit := range p.Assignments {
		spec, ok := cfg.Node(unit.NodeID)
		o, exists := p.Service.Observations[unit.NodeID]
		if !ok || !exists || o.ZoneID != unit.ZoneID || o.Rate != spec.ServiceRate {
			return errors.New("service rate or authority mismatch")
		}
	}
	q, e := placement.Quote(u.Policy, p.Assignments, p.Service.Observations, now)
	if e != nil {
		return e
	}
	if q.Cost != p.Service.Cost {
		return errors.New("service cost mismatch")
	}
	return nil
}
