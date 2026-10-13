package mqttbus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestStrictDecode(t *testing.T) {
	var r Request
	for _, raw := range []string{`{"op":"OFFER","unknown":true}`, `{} {}`, `{"op":3}`} {
		if Decode([]byte(raw), &r) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if Decode(make([]byte, MaxPayload+1), &r) == nil {
		t.Fatal("unbounded payload")
	}
}
func TestCoalescingBurst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dirty := make(chan struct{}, 1)
	priority := make(chan struct{}, 1)
	var count atomic.Int64
	go Coalesce(ctx, 10*time.Millisecond, dirty, priority, func() { count.Add(1) })
	for i := 0; i < 1000; i++ {
		select {
		case dirty <- struct{}{}:
		default:
		}
	}
	time.Sleep(35 * time.Millisecond)
	if n := count.Load(); n < 1 || n > 4 {
		t.Fatalf("burst publications %d", n)
	}
	priority <- struct{}{}
	time.Sleep(10 * time.Millisecond)
	if count.Load() < 2 {
		t.Fatal("critical update missing")
	}
}
