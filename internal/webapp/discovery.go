package webapp

import (
	"context"
	"github.com/julesdavidou/projet_secu_cloud/internal/transportsecurity"
	"time"
)

func (s *Server) SyncDiscovery(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		var routes map[string]transportsecurity.Route
		_ = s.control(ctx, "GET", "/api/v1/discovery/routes", nil, &routes)
		transportsecurity.RefreshZoneRoutes(ctx, s.client, routes)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
