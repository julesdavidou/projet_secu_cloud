package service

import (
	"context"
	"errors"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/catalog"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
)

func Central(ctx context.Context) error {
	c, err := catalog.Open(env("DB_PATH", "/data/central.db"))
	if err != nil {
		return err
	}
	defer c.Close()
	h := func(ctx context.Context, sender string, r mqttbus.Request) (any, error) {
		if r.Op == "LOOKUP" {
			if err := authorized(sender, "scenario", "zone-A", "zone-B"); err != nil {
				return nil, err
			}
			var q struct {
				RequestID string `json:"request_id"`
			}
			if err := mqttbus.Decode(r.Body, &q); err != nil {
				return nil, err
			}
			return c.Lookup(q.RequestID)
		}
		if r.Op == "SNAPSHOT" {
			if err := authorized(sender, "scenario", "zone-A", "zone-B"); err != nil {
				return nil, err
			}
			return c.Snapshot()
		}
		if r.Op != "EVENT" {
			return nil, errors.New("unknown central operation")
		}
		var e catalog.Event
		if err := mqttbus.Decode(r.Body, &e); err != nil {
			return nil, err
		}
		switch e.Op {
		case "NODE":
			if err := authorized(sender, "edge-"+e.Node.ID); err != nil {
				return nil, err
			}
		case "STORE_COMPLETE":
			if err := authorized(sender, "edge-"+e.NodeID); err != nil {
				return nil, err
			}
		case "PLAN", "CANCEL":
			if err := authorized(sender, "zone-A", "zone-B"); err != nil {
				return nil, err
			}
			video := ""
			if e.Op == "CANCEL" {
				video = e.VideoID
			}
			zone, err := c.OwnerZone(e.Manifest.SourceNodeID, video)
			if err != nil {
				return nil, err
			}
			if sender != "zone-"+zone {
				return nil, errors.New("source zone ownership mismatch")
			}
		default:
			return nil, errors.New("unauthorized event")
		}
		return c.Apply(e, time.Now())
	}
	_, err = bus(ctx, "central", nil, h, nil)
	if err != nil {
		return err
	}
	health(ctx)
	<-ctx.Done()
	return nil
}
