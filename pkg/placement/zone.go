package placement

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sync"
	"time"
)

type ReservationRequest struct {
	RequestID     string         `json:"request_id"`
	VideoID       string         `json:"video_id"`
	ReservationID string         `json:"reservation_id"`
	Extents       []Extent       `json:"extents"`
	Expires       time.Time      `json:"expires_at"`
	Policy        *ServicePolicy `json:"service_policy,omitempty"`
}
type Reservation struct {
	Request      ReservationRequest     `json:"request"`
	Plan         []Assignment           `json:"plan"`
	Remaining    []Extent               `json:"remaining"`
	State        string                 `json:"state"`
	Observations map[string]Observation `json:"service_observations,omitempty"`
	Fingerprint  [32]byte               `json:"-"`
}
type Summary struct {
	ZoneID     string    `json:"zone_id"`
	Generation uint64    `json:"generation"`
	Updated    time.Time `json:"updated_at"`
	Available  uint64    `json:"available_for_project_bytes"`
	State      State     `json:"state"`
}

// Zone is a synchronized reservation ledger. Optional checkpoint persistence
// runs under its business-operation lock, before acknowledging any mutation.
type Zone struct {
	mu           sync.Mutex
	ID           string
	TTL          time.Duration
	nodes        map[string]Node
	observations map[string]Observation
	reservations map[string]Reservation
	generation   uint64
	sink         func(Checkpoint) error
	failed       bool
}

func NewZone(id string, ttl time.Duration) *Zone {
	return &Zone{ID: id, TTL: ttl, nodes: map[string]Node{}, reservations: map[string]Reservation{}, generation: uint64(time.Now().UnixNano())}
}
func (z *Zone) Upsert(n Node, now time.Time) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if err := n.Validate(now); err != nil {
		return err
	}
	if n.Zone != z.ID {
		return errors.New("foreign zone snapshot")
	}
	old, ok := z.nodes[n.ID]
	if ok && (n.Generation < old.Generation || (n.Generation == old.Generation && n.Version <= old.Version)) {
		return errors.New("stale snapshot")
	}
	n.Included = append([]string(nil), n.Included...)
	z.nodes[n.ID] = n
	z.generation++
	return nil
}

// Depart quarantines a node at the newer mobility generation. Old retained
// snapshots cannot re-enable it; destination admission requires a net snapshot.
func (z *Zone) Depart(id string, generation uint64) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	n, ok := z.nodes[id]
	if !ok {
		return errors.New("unknown departing node")
	}
	if generation <= n.Generation {
		if generation == n.Generation && n.State == Unavailable {
			return nil
		}
		return errors.New("stale departure")
	}
	n.Generation = generation
	n.Version++
	n.State = Unavailable
	z.nodes[id] = n
	z.generation++
	return nil
}
func contains(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}
func cloneReservation(r Reservation) Reservation {
	r.Plan = slices.Clone(r.Plan)
	r.Remaining = slices.Clone(r.Remaining)
	r.Request.Extents = slices.Clone(r.Request.Extents)
	r.Request.Policy = clonePolicy(r.Request.Policy)
	if r.Observations != nil {
		v := map[string]Observation{}
		for k, o := range r.Observations {
			v[k] = o
		}
		r.Observations = v
	}
	return r
}
func (z *Zone) expire(now time.Time) {
	for id, r := range z.reservations {
		if r.State == "RESERVED" && !now.Before(r.Request.Expires) {
			z.release(id, "EXPIRED")
		}
	}
}
func (z *Zone) release(id, state string) {
	r, ok := z.reservations[id]
	if !ok {
		z.reservations[id] = Reservation{State: state}
		return
	}
	if r.State != "RESERVED" {
		return
	}
	r.State = state
	z.reservations[id] = r
	// An acknowledged debit cannot be added back to a net snapshot safely.
	// Quarantine until a newer snapshot confirms actual release at the Edge.
	for nodeID, n := range z.nodes {
		if contains(n.Included, id) {
			n.State = Unavailable
			z.nodes[nodeID] = n
		}
	}
	z.generation++
}
func (z *Zone) effective() []Node {
	nodes := make([]Node, 0, len(z.nodes))
	for _, n := range z.nodes {
		for id, r := range z.reservations {
			if r.State != "RESERVED" && r.State != "COMMITTED" {
				continue
			}
			if contains(n.Included, id) {
				continue
			}
			for _, a := range r.Plan {
				if a.NodeID == n.ID {
					if n.Available < a.Size {
						n.Available = 0
					} else {
						n.Available -= a.Size
					}
				}
			}
		}
		nodes = append(nodes, n)
	}
	return nodes
}
func (z *Zone) valid(r ReservationRequest, now time.Time) error {
	if e := r.Policy.Validate(); e != nil {
		return e
	}
	if z.failed {
		return errors.New("reservation persistence failed; admission closed")
	}
	if !ValidID(z.ID) || z.TTL <= 0 || !ValidID(r.RequestID) || !ValidID(r.VideoID) || !ValidID(r.ReservationID) || !now.Before(r.Expires) || r.Expires.Sub(now) > 10*time.Minute || len(r.Extents) == 0 || len(r.Extents) > MaxSegments {
		return errors.New("invalid reservation request")
	}
	return nil
}
func (z *Zone) Offer(r ReservationRequest, now time.Time) (Reservation, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	now = z.operationTime(now)
	z.expire(now)
	return z.plan(r, now)
}
func (z *Zone) plan(r ReservationRequest, now time.Time) (Reservation, error) {
	if err := z.valid(r, now); err != nil {
		return Reservation{}, err
	}
	nodes := z.effective()
	eligible := nodes[:0]
	observed := map[string]Observation{}
	for _, n := range nodes {
		o, ok := z.observations[n.ID]
		if r.Policy != nil && (!ok || o.Generation != n.Generation || !o.accepts(r.Policy, now)) {
			continue
		}
		eligible = append(eligible, n)
		if ok {
			observed[n.ID] = o
		}
	}
	p, left, err := Plan(r.VideoID, r.ReservationID, r.Extents, eligible, now, z.TTL)
	if r.Policy == nil {
		observed = nil
	}
	return Reservation{Request: r, Plan: p, Remaining: left, State: "PLANNED", Observations: observed}, err
}
func (z *Zone) Reserve(r ReservationRequest, now time.Time) (Reservation, error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	now = z.operationTime(now)
	z.expire(now)
	if z.failed {
		return Reservation{}, errors.New("reservation persistence failed; admission closed")
	}
	raw, _ := json.Marshal(r)
	fp := sha256.Sum256(raw)
	if old, ok := z.reservations[r.ReservationID]; ok {
		if old.Fingerprint != fp {
			return Reservation{}, errors.New("reservation identity conflict or terminal tombstone")
		}
		if old.State != "RESERVED" && old.State != "COMMITTED" {
			return Reservation{}, errors.New("reservation is terminal")
		}
		return cloneReservation(old), nil
	}
	if len(z.reservations) >= 10000 {
		return Reservation{}, errors.New("reservation ledger limit: reconcile before admission")
	}
	result, err := z.plan(r, now)
	if err != nil {
		return Reservation{}, err
	}
	result.State = "RESERVED"
	result.Fingerprint = fp
	z.reservations[r.ReservationID] = cloneReservation(result)
	z.generation++
	if err := z.persist(); err != nil {
		return Reservation{}, err
	}
	return result, nil
}
func (z *Zone) Release(id string) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.failed {
		return errors.New("reservation persistence failed; admission closed")
	}
	if !ValidID(id) {
		return errors.New("invalid reservation id")
	}
	if _, ok := z.reservations[id]; !ok && len(z.reservations) >= 10000 {
		return errors.New("reservation ledger limit")
	}
	if r, ok := z.reservations[id]; ok && r.State == "COMMITTED" {
		return errors.New("committed reservation cannot be released")
	}
	z.release(id, "RELEASED")
	return z.persist()
}

// Commit marks capacity as consumed only after the central business commit.
// Expired reservations cannot be revived by a delayed completion.
func (z *Zone) Commit(id string, now time.Time) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	now = z.operationTime(now)
	if z.failed {
		return errors.New("reservation persistence failed; admission closed")
	}
	z.expire(now)
	r, ok := z.reservations[id]
	if !ok || (r.State != "RESERVED" && r.State != "COMMITTED") {
		return errors.New("reservation not active")
	}
	r.State = "COMMITTED"
	z.reservations[id] = r
	z.generation++
	return z.persist()
}

// A caller may capture its clock before waiting for this registry's lock. A
// concurrently accepted snapshot can then legitimately be newer. Upsert has
// already validated its timestamps against the ingestion clock; use the newer
// observation for this operation, without weakening pure Plan validation.
func (z *Zone) operationTime(now time.Time) time.Time {
	for _, observation := range z.observations {
		if observation.Observed.After(now) {
			now = observation.Observed
		}
	}
	for _, n := range z.nodes {
		if n.Updated.After(now) {
			now = n.Updated
		}
		if n.Heartbeat.After(now) {
			now = n.Heartbeat
		}
	}
	return now
}
func (z *Zone) Summary(now time.Time) Summary {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.expire(now)
	s := Summary{ZoneID: z.ID, Generation: z.generation, Updated: now, State: Unavailable}
	if z.failed {
		return s
	}
	for _, n := range z.effective() {
		if now.Sub(n.Updated) > z.TTL || now.Sub(n.Heartbeat) > z.TTL || n.State == Unavailable || n.Available == 0 {
			continue
		}
		if s.Available > math.MaxInt64-n.Available {
			s.Available = math.MaxInt64
		} else {
			s.Available += n.Available
		}
		if n.State == Available {
			s.State = Available
		} else if s.State == Unavailable {
			s.State = Degraded
		}
	}
	return s
}
