/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"allocator.go" gère le placement des fragments sur les noeuds
*/
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"os"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/service"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

// Allocator owns the existing Zone Controller's placement entry point. Pure
// algorithms are reusable in pkg/placement. No media or disk is inspected here.
type Allocator struct {
	zone   *placement.Zone
	target uint64
}

func newAllocator(zoneID string, target uint64, ttl time.Duration) (*Allocator, error) {
	if !placement.ValidID(zoneID) || target == 0 || ttl <= 0 {
		return nil, errors.New("invalid allocator configuration")
	}
	return &Allocator{zone: placement.NewZone(zoneID, ttl), target: target}, nil
}

// UpdateProjectCapacity accepts a NET project snapshot. StorageMax/StorageUsed
// in nodes.go and disk.Free in the Edge monitoring are intentionally not used.
func (a *Allocator) UpdateProjectCapacity(snapshot placement.Node, now time.Time) error {
	return a.zone.Upsert(snapshot, now)
}
func (a *Allocator) Offer(m placement.Manifest, reservationID string, expires, now time.Time) (placement.Reservation, error) {
	r, err := a.request(m, reservationID, expires)
	if err != nil {
		return placement.Reservation{}, err
	}
	return a.zone.Offer(r, now)
}
func (a *Allocator) Reserve(m placement.Manifest, reservationID string, expires, now time.Time) (placement.Reservation, error) {
	r, err := a.request(m, reservationID, expires)
	if err != nil {
		return placement.Reservation{}, err
	}
	return a.zone.Reserve(r, now)
}
func (a *Allocator) Release(id string) error { return a.zone.Release(id) }
func (a *Allocator) request(m placement.Manifest, id string, expires time.Time) (placement.ReservationRequest, error) {
	extents, err := placement.Extents(m, a.target)
	return placement.ReservationRequest{RequestID: m.RequestID, VideoID: m.VideoID, ReservationID: id, Extents: extents, Expires: expires}, err
}

// Opt-in MQTT5 reference integration. The process remains serveur-zone; topics
// are lab/placement/v1 and the Central owns a separate authoritative SQLite DB.
func initAllocator() {
	a, err := newAllocator(os.Getenv("ZONE_ID"), 64*1024*1024, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := service.ZoneWithRegistry(context.Background(), a.zone); err != nil {
			log.Fatal("placement adapter startup failed: ", err)
		}
	}()
}

// Compile-time hooks for Dan's unfinished legacy topic. Until the team defines
// identity, manifest and reply contracts, these callbacks deliberately reject
// mutations. LEGACY_COMMANDS_ENABLED is off by default in mqtt.go.
type Command struct {
	Action string `json:"action"`
	Token  string `json:"token"`
}

func legacyAuthorized(c Command) bool {
	token := os.Getenv("PLACEMENT_TOKEN")
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(c.Token)) == 1
}
func handleAllocate(c Command) {
	if !legacyAuthorized(c) {
		log.Print("legacy allocation rejected: authorization missing")
		return
	}
	log.Print("legacy allocation rejected: manifest and correlated response contract undefined; use MQTT5 reference adapter")
}
func handleDeallocate(c Command) {
	if !legacyAuthorized(c) {
		log.Print("legacy release rejected: authorization missing")
		return
	}
	log.Print("legacy release rejected: reservation identity contract undefined; use MQTT5 reference adapter")
}
