// Package placement implements transport-independent extent placement. It never
// reads disks or moves media. A plan is not evidence of storage.
package placement

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"
)

const MaxSegments = 1_000_000

var ErrCapacity = errors.New("insufficient capacity at extent granularity")
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidID(s string) bool { return safeID.MatchString(s) }

type Segment struct {
	ID   uint64 `json:"segment_id"`
	Size uint64 `json:"size_bytes"`
}
type Manifest struct {
	RequestID    string    `json:"request_id"`
	VideoID      string    `json:"video_id"`
	SourceNodeID string    `json:"source_node_id"`
	Total        uint64    `json:"total_size_bytes"`
	Segments     []Segment `json:"segments"`
}

// ExternalManifestResolver is the only extension for manifests fetched outside
// the control plane. Implementations must enforce their own URL/size policy.
type ExternalManifestResolver interface {
	Resolve(context.Context, string) (Manifest, error)
}

func (m Manifest) Validate() error {
	if !ValidID(m.RequestID) || !ValidID(m.VideoID) || !ValidID(m.SourceNodeID) {
		return errors.New("invalid manifest identity")
	}
	if len(m.Segments) == 0 || len(m.Segments) > MaxSegments {
		return errors.New("invalid segment count")
	}
	var sum uint64
	for i, s := range m.Segments {
		if s.ID != uint64(i) || s.Size == 0 || s.Size > math.MaxInt64 || sum > math.MaxInt64-s.Size {
			return errors.New("segments must start at zero, be contiguous, positive, and fit signed 64 bits")
		}
		sum += s.Size
	}
	if sum != m.Total {
		return errors.New("manifest total mismatch")
	}
	return nil
}

type Extent struct {
	ID    uint64 `json:"extent_id"`
	First uint64 `json:"first_segment"`
	Last  uint64 `json:"last_segment"`
	Size  uint64 `json:"size_bytes"`
}

func Extents(m Manifest, target uint64) ([]Extent, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if target == 0 || target > math.MaxInt64 {
		return nil, errors.New("invalid extent target")
	}
	out := []Extent{}
	current := Extent{}
	for i, s := range m.Segments {
		if current.Size > 0 && (current.Size >= target || s.Size > target-current.Size) {
			out = append(out, current)
			current = Extent{ID: uint64(len(out)), First: uint64(i)}
		}
		current.Last = uint64(i)
		current.Size += s.Size
	}
	out = append(out, current)
	return out, nil
}

type State string

const (
	Available   State = "AVAILABLE"
	Degraded    State = "DEGRADED"
	Unavailable State = "UNAVAILABLE"
)

type Node struct {
	ID         string    `json:"node_id"`
	Zone       string    `json:"zone_id"`
	State      State     `json:"state"`
	Available  uint64    `json:"available_for_project_bytes"`
	Generation uint64    `json:"generation"`
	Version    uint64    `json:"snapshot_version"`
	Updated    time.Time `json:"updated_at"`
	Heartbeat  time.Time `json:"heartbeat_at"`
	// Included reservations are already deducted in Available by the Edge.
	Included []string `json:"included_reservation_ids,omitempty"`
}

func (n Node) Validate(now time.Time) error {
	if !ValidID(n.ID) || !ValidID(n.Zone) || n.Generation == 0 || n.Generation > math.MaxInt64 || n.Version == 0 || n.Version > math.MaxInt64 || n.Available > math.MaxInt64 || n.Updated.IsZero() || n.Heartbeat.IsZero() || n.Updated.After(now) || n.Heartbeat.After(now) {
		return errors.New("invalid node snapshot")
	}
	if n.State != Available && n.State != Degraded && n.State != Unavailable {
		return errors.New("invalid node state")
	}
	seen := map[string]bool{}
	for _, id := range n.Included {
		if !ValidID(id) || seen[id] {
			return errors.New("invalid included reservation")
		}
		seen[id] = true
	}
	return nil
}

type Assignment struct {
	Extent
	NodeID        string `json:"node_id"`
	ZoneID        string `json:"zone_id"`
	ReservationID string `json:"reservation_id"`
}
type Range struct {
	NodeID        string   `json:"node_id"`
	ZoneID        string   `json:"zone_id"`
	ReservationID string   `json:"reservation_id"`
	First         uint64   `json:"first_segment"`
	Last          uint64   `json:"last_segment"`
	Size          uint64   `json:"size_bytes"`
	ExtentIDs     []uint64 `json:"extent_ids"`
}

func Ranges(plan []Assignment) []Range {
	p := append([]Assignment(nil), plan...)
	sort.Slice(p, func(i, j int) bool { return p[i].First < p[j].First })
	out := []Range{}
	for _, a := range p {
		if len(out) > 0 {
			r := &out[len(out)-1]
			if r.NodeID == a.NodeID && r.ZoneID == a.ZoneID && r.ReservationID == a.ReservationID && r.Last+1 == a.First && r.Size <= math.MaxInt64-a.Size {
				r.Last = a.Last
				r.Size += a.Size
				r.ExtentIDs = append(r.ExtentIDs, a.ID)
				continue
			}
		}
		out = append(out, Range{a.NodeID, a.ZoneID, a.ReservationID, a.First, a.Last, a.Size, []uint64{a.ID}})
	}
	return out
}

// Score uses SHA-256 over three length-prefixed binary values. 52 bits and
// half-unit offset ensure U is strictly inside (0,1), including after rounding.
func Score(video string, extent uint64, node string, weight uint64) float64 {
	if weight == 0 {
		return math.Inf(1)
	}
	h := sha256.New()
	var b [8]byte
	for _, s := range []string{video, fmt.Sprint(extent), node} {
		binary.BigEndian.PutUint64(b[:], uint64(len(s)))
		h.Write(b[:])
		h.Write([]byte(s))
	}
	sum := h.Sum(nil)
	v := binary.BigEndian.Uint64(sum[:8]) >> 12
	u := (float64(v) + 0.5) / 4503599627370496.0
	return -math.Log(u) / float64(weight)
}

// Plan returns a provisional partial plan plus unplaceable extents. Capacities
// are virtual copies; callers must atomically reserve before using the result.
func Plan(video, reservation string, extents []Extent, nodes []Node, now time.Time, ttl time.Duration) ([]Assignment, []Extent, error) {
	if !ValidID(video) || !ValidID(reservation) || ttl <= 0 {
		return nil, nil, errors.New("invalid planning parameters")
	}
	seenNodes := map[string]bool{}
	for _, n := range nodes {
		if err := n.Validate(now); err != nil {
			return nil, nil, err
		}
		if seenNodes[n.ID] {
			return nil, nil, errors.New("duplicate node")
		}
		seenNodes[n.ID] = true
	}
	seen := map[uint64]bool{}
	var total uint64
	for i, e := range extents {
		if e.Size == 0 || e.Size > math.MaxInt64 || total > math.MaxInt64-e.Size || e.First > e.Last || e.Last >= MaxSegments || seen[e.ID] || (i > 0 && (e.First <= extents[i-1].Last || e.ID <= extents[i-1].ID)) {
			return nil, nil, errors.New("invalid extent")
		}
		seen[e.ID] = true
		total += e.Size
	}
	nodes = append([]Node(nil), nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	plan := []Assignment{}
	remaining := []Extent{}
	for _, e := range extents {
		winner := -1
		best := math.Inf(1)
		for _, state := range []State{Available, Degraded} {
			for i, n := range nodes {
				if n.State != state || now.Sub(n.Updated) > ttl || now.Sub(n.Heartbeat) > ttl || n.Available < e.Size {
					continue
				}
				score := Score(video, e.ID, n.ID, n.Available)
				if score < best {
					winner = i
					best = score
				}
			}
			if winner >= 0 {
				break
			}
		}
		if winner < 0 {
			remaining = append(remaining, e)
			continue
		}
		n := &nodes[winner]
		n.Available -= e.Size
		plan = append(plan, Assignment{e, n.ID, n.Zone, reservation})
	}
	return plan, remaining, nil
}

// LivePolicy is an extension only: an unbounded stream is not a complete VOD.
type LivePolicy interface {
	Ingress(context.Context, string) (string, error)
	CacheTTL(string) time.Duration
}
