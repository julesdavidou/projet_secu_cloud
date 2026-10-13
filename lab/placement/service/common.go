package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func duration(key, def string) time.Duration {
	d, e := time.ParseDuration(env(key, def))
	if e != nil || d <= 0 {
		panic("invalid duration: " + key)
	}
	return d
}
func number(key, def string) uint64 {
	n, e := strconv.ParseUint(env(key, def), 10, 63)
	if e != nil {
		panic("invalid number: " + key)
	}
	return n
}
func eventID(parts ...any) string {
	return fmt.Sprintf("e-%x", sha256.Sum256([]byte(fmt.Sprint(parts...))))
}
func health(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready","simulation":true}`))
	})
	s := &http.Server{Addr: ":8088", Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	go func() {
		<-ctx.Done()
		op, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(op)
	}()
	go func() { _ = s.ListenAndServe() }()
}
func bus(ctx context.Context, id string, subs []string, h mqttbus.Handler, state func(string, []byte, bool)) (*mqttbus.Bus, error) {
	return mqttbus.New(ctx, env("MQTT_BROKER", "mqtt://broker:1883"), id, os.Getenv("LAB_PASSWORD"), subs, h, state)
}

type remote struct {
	b  *mqttbus.Bus
	id string
}

func (r remote) Offer(ctx context.Context, q placement.ReservationRequest) (placement.Reservation, error) {
	var result placement.Reservation
	e := r.b.Call(ctx, "zone-"+r.id, "OFFER", q, &result)
	return result, e
}
func (r remote) Reserve(ctx context.Context, q placement.ReservationRequest) (placement.Reservation, error) {
	var result placement.Reservation
	e := r.b.Call(ctx, "zone-"+r.id, "RESERVE", q, &result)
	return result, e
}
func (r remote) Release(ctx context.Context, id string) error {
	return r.b.Call(ctx, "zone-"+r.id, "RELEASE", map[string]string{"reservation_id": id}, nil)
}

type reservationKey struct {
	ID string `json:"reservation_id"`
}

func authorized(sender string, allowed ...string) error {
	for _, a := range allowed {
		if sender == a {
			return nil
		}
	}
	return errors.New("caller not authorized for this operation")
}
