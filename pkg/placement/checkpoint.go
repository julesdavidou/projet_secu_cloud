package placement

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
)

// Checkpoint persists only business reservations, never node liveness. The
// adapter must durably write this before acknowledging RESERVE/RELEASE/COMMIT.
// Fresh heartbeats are required again after restoring a zone.
type Checkpoint struct {
	ZoneID       string                 `json:"zone_id"`
	Generation   uint64                 `json:"generation"`
	Reservations map[string]Reservation `json:"reservations"`
}

func (z *Zone) Checkpoint() Checkpoint { z.mu.Lock(); defer z.mu.Unlock(); return z.checkpoint() }
func (z *Zone) checkpoint() Checkpoint {
	c := Checkpoint{ZoneID: z.ID, Generation: z.generation, Reservations: map[string]Reservation{}}
	for id, r := range z.reservations {
		c.Reservations[id] = cloneReservation(r)
	}
	return c
}

// The sink must not call back into the Zone. It receives an independent copy.
func (z *Zone) SetCheckpointSink(sink func(Checkpoint) error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.sink = sink
}
func (z *Zone) persist() error {
	if z.sink == nil {
		return nil
	}
	if err := z.sink(z.checkpoint()); err != nil {
		z.failed = true
		return errors.New("reservation persistence failed; admission closed")
	}
	return nil
}
func (z *Zone) Restore(c Checkpoint) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if c.ZoneID != z.ID || len(c.Reservations) > 10000 || len(z.reservations) != 0 || len(z.nodes) != 0 {
		return errors.New("invalid restore target")
	}
	out := map[string]Reservation{}
	for id, r := range c.Reservations {
		if !ValidID(id) {
			return errors.New("invalid checkpoint reservation")
		}
		switch r.State {
		case "RESERVED", "COMMITTED", "RELEASED", "EXPIRED":
		default:
			return errors.New("invalid checkpoint state")
		}
		if r.Request.ReservationID != "" {
			if r.Request.ReservationID != id {
				return errors.New("checkpoint identity mismatch")
			}
			raw, _ := json.Marshal(r.Request)
			r.Fingerprint = sha256.Sum256(raw)
			if r.State == "RESERVED" || r.State == "COMMITTED" {
				copy := r
				copy.State = "RESERVED"
				if err := validateReply(z.ID, r.Request, copy); err != nil {
					return err
				}
			}
		}
		out[id] = cloneReservation(r)
	}
	z.reservations = out
	if c.Generation >= z.generation {
		z.generation = c.Generation + 1
	}
	return nil
}

// ReservationIDs gives an ordered view for reconciliation interfaces.
func (c Checkpoint) ReservationIDs() []string {
	ids := make([]string, 0, len(c.Reservations))
	for id := range c.Reservations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
