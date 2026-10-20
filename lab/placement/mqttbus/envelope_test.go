package mqttbus

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/eclipse/paho.golang/paho"
	"testing"
	"time"
)

func TestMQTT3ReplyCorrelationAndSender(t *testing.T) {
	corr := make([]byte, 16)
	key := hex.EncodeToString(corr)
	ch := make(chan Response, 1)
	b := &Bus{id: "central", pending: map[string]pending{key: {target: "edge-A1", result: ch}}}
	for _, tc := range []struct {
		sender, corr       string
		retained, accepted bool
	}{
		{"edge-B1", key, false, false}, {"edge-A1", key, true, false}, {"edge-A1", "abcd", false, false}, {"edge-A1", key, false, true},
	} {
		raw, _ := json.Marshal(Response{CorrelationID: tc.corr})
		b.receive(context.Background(), &paho.Publish{Topic: Root + "central/reply/" + tc.sender, Payload: raw, Retain: tc.retained})
		select {
		case <-ch:
			if !tc.accepted {
				t.Fatal("forged reply accepted", tc)
			}
		default:
			if tc.accepted {
				t.Fatal("MQTT3 JSON reply not correlated")
			}
		}
	}
}
func TestRejectedCommandEnvelopes(t *testing.T) {
	calls := make(chan struct{}, 1)
	b := &Bus{id: "edge-A1", slots: make(chan struct{}, 1), handler: func(context.Context, string, Request) (any, error) { calls <- struct{}{}; return nil, nil }}
	corr := make([]byte, 16)
	for _, tc := range []struct {
		sender, reply string
		ttl           time.Duration
		retained      bool
		qos           byte
		id            string
	}{
		{"central", Root + "central/reply/edge-A1", time.Minute, true, 1, ""},
		{"central", Root + "central/reply/edge-A1", -time.Second, false, 1, ""},
		{"central", Root + "admin/reply/edge-A1", time.Second, false, 1, ""},
		{"central", Root + "central/reply/edge-A1", time.Second, false, 0, ""},
		{"central", Root + "central/reply/edge-A1", time.Second, false, 1, "wrong"},
	} {
		raw, _ := json.Marshal(Request{Op: "STATUS", Expires: time.Now().Add(tc.ttl), Body: json.RawMessage(`{}`), CorrelationID: tc.id})
		b.receive(context.Background(), &paho.Publish{Topic: Root + "edge-A1/request/" + tc.sender, Payload: raw, Retain: tc.retained, QoS: tc.qos, Properties: &paho.PublishProperties{CorrelationData: corr, ResponseTopic: tc.reply}})
	}
	select {
	case <-calls:
		t.Fatal("invalid command reached business handler")
	case <-time.After(20 * time.Millisecond):
	}
}
