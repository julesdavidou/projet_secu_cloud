package controllers

import (
	"errors"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"time"
)

// authenticatedNodeAtReceipt tolerates the same one-second future-clock bound
// as discovery. Only live, authenticated input can be clamped to receipt time.
// Retained messages and old timestamps never gain freshness. Scheduler and
// generation/version ordering remain unchanged.
func authenticatedNodeAtReceipt(n placement.Node, now time.Time, retained bool) (placement.Node, error) {
	if !retained {
		if n.Updated.After(now.Add(time.Second)) || n.Heartbeat.After(now.Add(time.Second)) {
			return n, errors.New("Edge clock is over one second ahead; synchronize host/WSL time")
		}
		if n.Updated.After(now) {
			n.Updated = now
		}
		if n.Heartbeat.After(now) {
			n.Heartbeat = now
		}
	}
	return n, n.Validate(now)
}
