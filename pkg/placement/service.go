package placement

import (
	"errors"
	"math/big"
	"time"
)

// ServicePolicy is admission policy, not a latency guarantee or a payment.
// Monetary values are integer millionths of a demonstration credit.
type ServicePolicy struct {
	MaxRTTMillis           *uint64 `json:"max_rtt_ms,omitempty"`
	MinProbeBytesPerSecond *uint64 `json:"min_probe_bytes_per_second,omitempty"`
	MaxRate                *uint64 `json:"max_microcredits_per_gib_hour,omitempty"`
	Budget                 *uint64 `json:"budget_microcredits,omitempty"`
	Hours                  uint64  `json:"retention_hours"`
}

func (p *ServicePolicy) Validate() error {
	if p == nil {
		return nil
	}
	if p.Hours < 1 || p.Hours > 8760 || p.MaxRTTMillis != nil && *p.MaxRTTMillis > 60000 {
		return errors.New("invalid service policy")
	}
	return nil
}

// Observation is collected by the owning zone over authenticated HTTP. The
// small probe measures that zone's path, not viewer-to-video latency or bitrate.
type Observation struct {
	NodeID              string    `json:"node_id"`
	ZoneID              string    `json:"zone_id"`
	Generation          uint64    `json:"generation"`
	RTTMicros           uint64    `json:"rtt_microseconds"`
	ProbeBytesPerSecond uint64    `json:"probe_bytes_per_second"`
	Rate                uint64    `json:"microcredits_per_gib_hour"`
	Observed            time.Time `json:"observed_at"`
}

func (o Observation) accepts(p *ServicePolicy, now time.Time) bool {
	if p == nil {
		return true
	}
	if o.Observed.IsZero() || o.Observed.After(now) || now.Sub(o.Observed) > 30*time.Second {
		return false
	}
	return (p.MaxRTTMillis == nil || o.RTTMicros <= *p.MaxRTTMillis*1000) && (p.MinProbeBytesPerSecond == nil || o.ProbeBytesPerSecond >= *p.MinProbeBytesPerSecond) && (p.MaxRate == nil || o.Rate <= *p.MaxRate)
}

type ServiceQuote struct {
	Policy       ServicePolicy          `json:"policy"`
	Cost         uint64                 `json:"cost_microcredits"`
	Observations map[string]Observation `json:"observations"`
}

// Quote rounds each node's total storage charge upward once. big.Int prevents
// user-supplied budgets/rates from wrapping around to an inexpensive result.
func Quote(p *ServicePolicy, plan []Assignment, observations map[string]Observation, now time.Time) (*ServiceQuote, error) {
	if p == nil {
		return nil, nil
	}
	if e := p.Validate(); e != nil {
		return nil, e
	}
	sizes := map[string]*big.Int{}
	used := map[string]Observation{}
	for _, a := range plan {
		o, ok := observations[a.NodeID]
		if !ok || o.NodeID != a.NodeID || o.ZoneID != a.ZoneID || !o.accepts(p, now) {
			return nil, errors.New("missing, expired or ineligible service observation")
		}
		if sizes[a.NodeID] == nil {
			sizes[a.NodeID] = new(big.Int)
		}
		sizes[a.NodeID].Add(sizes[a.NodeID], new(big.Int).SetUint64(a.Size))
		used[a.NodeID] = o
	}
	sum := new(big.Int)
	divisor := new(big.Int).SetUint64(1 << 30)
	for id, size := range sizes {
		value := new(big.Int).Mul(size, new(big.Int).SetUint64(used[id].Rate))
		value.Mul(value, new(big.Int).SetUint64(p.Hours))
		value.Add(value, new(big.Int).Sub(divisor, big.NewInt(1)))
		value.Div(value, divisor)
		sum.Add(sum, value)
	}
	if !sum.IsUint64() {
		return nil, errors.New("service cost overflow")
	}
	cost := sum.Uint64()
	if p.Budget != nil && cost > *p.Budget {
		return nil, errors.New("service budget exceeded")
	}
	return &ServiceQuote{Policy: *clonePolicy(p), Cost: cost, Observations: used}, nil
}
func clonePolicy(p *ServicePolicy) *ServicePolicy {
	if p == nil {
		return nil
	}
	q := *p
	cp := func(v *uint64) *uint64 {
		if v == nil {
			return nil
		}
		w := *v
		return &w
	}
	q.MaxRTTMillis = cp(p.MaxRTTMillis)
	q.MinProbeBytesPerSecond = cp(p.MinProbeBytesPerSecond)
	q.MaxRate = cp(p.MaxRate)
	q.Budget = cp(p.Budget)
	return &q
}
func (z *Zone) Observe(o Observation, now time.Time) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	n, ok := z.nodes[o.NodeID]
	if !ok || o.ZoneID != z.ID || o.Generation != n.Generation || o.RTTMicros == 0 || o.ProbeBytesPerSecond == 0 || o.Observed.IsZero() || o.Observed.After(now) || now.Sub(o.Observed) > 30*time.Second {
		return errors.New("invalid service observation")
	}
	if z.observations == nil {
		z.observations = map[string]Observation{}
	}
	if prev, ok := z.observations[o.NodeID]; ok && o.Observed.Before(prev.Observed) {
		return errors.New("stale service observation")
	}
	z.observations[o.NodeID] = o
	return nil
}
