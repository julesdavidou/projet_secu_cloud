package placement

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// Peer deliberately exposes only a zone summary and business operations, never
// a remote node registry. OFFER does not promise any capacity until RESERVE.
type Peer interface {
	Offer(context.Context, ReservationRequest) (Reservation, error)
	Reserve(context.Context, ReservationRequest) (Reservation, error)
	Release(context.Context, string) error
}
type Path struct {
	// Measured by the source zone toward this peer. Nil means unknown.
	RTTSeconds            *float64 `json:"rtt_seconds,omitempty"`
	GoodputBytesPerSecond *float64 `json:"goodput_bytes_per_second,omitempty"`
}
type Neighbor struct {
	ID   string
	Peer Peer
	Path Path
}
type SummaryCache struct {
	mu     sync.Mutex
	values map[string]Summary
}

func NewSummaryCache() *SummaryCache { return &SummaryCache{values: map[string]Summary{}} }
func (c *SummaryCache) Put(s Summary, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !ValidID(s.ZoneID) || s.Generation == 0 || s.Updated.IsZero() || s.Updated.After(now) || s.Available > math.MaxInt64 || (s.State != Available && s.State != Degraded && s.State != Unavailable) {
		return errors.New("invalid summary")
	}
	if old, ok := c.values[s.ZoneID]; ok && (s.Generation < old.Generation || s.Updated.Before(old.Updated)) {
		return errors.New("stale summary")
	}
	c.values[s.ZoneID] = s
	return nil
}
func (c *SummaryCache) Get(id string, now time.Time, ttl time.Duration) (Summary, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.values[id]
	return s, ok && ttl > 0 && !s.Updated.After(now) && now.Sub(s.Updated) <= ttl && s.State != Unavailable && s.Available > 0
}

type Coordinator struct {
	Local      *Zone
	Neighbors  []Neighbor
	Cache      *SummaryCache
	Fanout     int
	Timeout    time.Duration
	SummaryTTL time.Duration
}
type DistributedPlan struct {
	RequestID    string            `json:"request_id"`
	VideoID      string            `json:"video_id"`
	State        string            `json:"state"`
	Assignments  []Assignment      `json:"assignments"`
	Reservations map[string]string `json:"reservations"`
}

func reservationID(request, zone string) string {
	return fmt.Sprintf("r-%x", sha256.Sum256([]byte(request+"\x00"+zone)))
}
func validMetric(p *float64, positive bool) bool {
	return p == nil || (!math.IsNaN(*p) && !math.IsInf(*p, 0) && *p >= 0 && (!positive || *p > 0))
}
func estimate(p Path, size uint64) (float64, bool) {
	if p.GoodputBytesPerSecond == nil || p.RTTSeconds == nil {
		return 0, false
	}
	return *p.RTTSeconds + float64(size) / *p.GoodputBytesPerSecond, true
}
func (c *Coordinator) Place(ctx context.Context, m Manifest, target uint64, now time.Time, lease time.Duration) (DistributedPlan, error) {
	extents, err := Extents(m, target)
	if err != nil {
		return DistributedPlan{}, err
	}
	if c.Local == nil || c.Cache == nil || c.Timeout <= 0 || c.SummaryTTL <= 0 || lease <= 0 || lease > 10*time.Minute || len(c.Neighbors) > 5 {
		return DistributedPlan{}, errors.New("invalid coordinator configuration")
	}
	fanout := c.Fanout
	if fanout == 0 {
		fanout = 2
	}
	if fanout < 1 || fanout > 5 {
		return DistributedPlan{}, errors.New("invalid fanout")
	}
	seen := map[string]bool{c.Local.ID: true}
	for _, n := range c.Neighbors {
		if !ValidID(n.ID) || seen[n.ID] || n.Peer == nil || !validMetric(n.Path.RTTSeconds, false) || !validMetric(n.Path.GoodputBytesPerSecond, true) {
			return DistributedPlan{}, errors.New("invalid topology or path metrics")
		}
		seen[n.ID] = true
	}
	out := DistributedPlan{RequestID: m.RequestID, VideoID: m.VideoID, State: "RESERVED", Reservations: map[string]string{}}
	request := func(zone string, left []Extent) ReservationRequest {
		return ReservationRequest{m.RequestID, m.VideoID, reservationID(m.RequestID, zone), left, now.Add(lease)}
	}
	local, err := c.Local.Reserve(request(c.Local.ID, extents), now)
	if err != nil {
		return DistributedPlan{}, err
	}
	out.Reservations[c.Local.ID] = local.Request.ReservationID
	out.Assignments = append(out.Assignments, local.Plan...)
	left := local.Remaining
	rollback := func() {
		_ = c.Local.Release(local.Request.ReservationID)
		for _, n := range c.Neighbors {
			if id, ok := out.Reservations[n.ID]; ok {
				clean, cancel := context.WithTimeout(context.Background(), c.Timeout)
				_ = n.Peer.Release(clean, id)
				cancel()
			}
		}
	}
	neighbors := []Neighbor{}
	summaries := map[string]Summary{}
	for _, n := range c.Neighbors {
		if s, ok := c.Cache.Get(n.ID, now, c.SummaryTTL); ok {
			neighbors = append(neighbors, n)
			summaries[n.ID] = s
		}
	}
	sort.Slice(neighbors, func(i, j int) bool {
		a, b := neighbors[i], neighbors[j]
		sa, sb := summaries[a.ID], summaries[b.ID]
		if sa.State != sb.State {
			return sa.State == Available
		}
		ea, ka := estimate(a.Path, m.Total)
		eb, kb := estimate(b.Path, m.Total)
		if ka != kb {
			return ka
		}
		if ka && ea != eb {
			return ea < eb
		}
		return a.ID < b.ID
	})
	for start := 0; len(left) > 0 && start < len(neighbors); start += fanout {
		end := start + fanout
		if end > len(neighbors) {
			end = len(neighbors)
		}
		batch := neighbors[start:end]
		type response struct {
			r   Reservation
			err error
		}
		responses := make([]response, len(batch))
		var wg sync.WaitGroup
		for i, n := range batch {
			wg.Add(1)
			go func(i int, n Neighbor) {
				defer wg.Done()
				op, cancel := context.WithTimeout(ctx, c.Timeout)
				defer cancel()
				responses[i].r, responses[i].err = n.Peer.Offer(op, request(n.ID, left))
			}(i, n)
		}
		wg.Wait()
		for i, n := range batch {
			if len(left) == 0 {
				break
			}
			if ctx.Err() != nil {
				rollback()
				return DistributedPlan{}, ctx.Err()
			}
			if responses[i].err != nil || len(responses[i].r.Plan) == 0 {
				continue
			}
			rq := request(n.ID, left)
			out.Reservations[n.ID] = rq.ReservationID
			op, cancel := context.WithTimeout(ctx, c.Timeout)
			reserved, e := n.Peer.Reserve(op, rq)
			cancel()
			if e == nil {
				e = validateReply(n.ID, rq, reserved)
			}
			if e != nil {
				// RESERVE may have committed before a response was lost. RELEASE
				// its known identity; the lease bounds loss if the peer is offline.
				clean, stop := context.WithTimeout(context.Background(), c.Timeout)
				_ = n.Peer.Release(clean, rq.ReservationID)
				stop()
				continue
			}
			out.Assignments = append(out.Assignments, reserved.Plan...)
			left = reserved.Remaining
		}
	}
	if ctx.Err() != nil {
		rollback()
		return DistributedPlan{}, ctx.Err()
	}
	if len(left) > 0 {
		rollback()
		return DistributedPlan{}, ErrCapacity
	}
	sort.Slice(out.Assignments, func(i, j int) bool { return out.Assignments[i].ID < out.Assignments[j].ID })
	return out, nil
}

// A remote response must be a complete partition of the requested extents;
// it cannot omit work, invent capacities or assign another zone's nodes.
func validateReply(zone string, q ReservationRequest, r Reservation) error {
	if r.State != "RESERVED" || r.Request.RequestID != q.RequestID || r.Request.VideoID != q.VideoID || r.Request.ReservationID != q.ReservationID || !r.Request.Expires.Equal(q.Expires) {
		return errors.New("invalid reservation response")
	}
	want := map[uint64]Extent{}
	for _, e := range q.Extents {
		want[e.ID] = e
	}
	seen := map[uint64]bool{}
	for _, a := range r.Plan {
		if a.ZoneID != zone || !ValidID(a.NodeID) || a.ReservationID != q.ReservationID || want[a.ID] != a.Extent || seen[a.ID] {
			return errors.New("invalid remote assignment")
		}
		seen[a.ID] = true
	}
	for _, e := range r.Remaining {
		if want[e.ID] != e || seen[e.ID] {
			return errors.New("invalid remaining extents")
		}
		seen[e.ID] = true
	}
	if len(seen) != len(want) {
		return errors.New("remote response dropped extents")
	}
	return nil
}
