package mqttbus

import (
	"context"
	"time"
)

// Coalesce flushes at most once per interval for ordinary changes. The channel
// has capacity one, so a burst doesn't allocate a timer/publication per extent.
// A separate priority channel is reserved for critical liveness transitions.
func Coalesce(ctx context.Context, interval time.Duration, dirty, priority <-chan struct{}, flush func()) {
	if interval <= 0 {
		return
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	pending := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-dirty:
			pending = true
		case <-priority:
			flush()
			pending = false
		case <-tick.C:
			if pending {
				flush()
				pending = false
			}
		}
	}
}
