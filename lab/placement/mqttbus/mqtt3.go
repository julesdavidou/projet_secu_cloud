package mqttbus

import (
	"context"
	"errors"
	"github.com/eclipse/paho.golang/paho"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/julesdavidou/projet_secu_cloud/internal/mqttclient/transport"
	"github.com/julesdavidou/projet_secu_cloud/internal/transportsecurity"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"net/url"
	"strings"
	"time"
)

// MQTT3 uses the SAME topic authorization, handlers, IDs, deadlines and business
// acknowledgements as MQTT5. Only response properties move into the JSON envelope.
func newV3(ctx context.Context, broker, id, password string, subscriptions []string, h Handler, state func(string, []byte, bool)) (*Bus, error) {
	u, e := url.Parse(broker)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "mqtt" && u.Scheme != "tls") || !placement.ValidID(id) || password == "" {
		return nil, errors.New("invalid MQTT3 identity or broker")
	}
	if u.Scheme == "mqtt" {
		u.Scheme = "tcp"
	}
	b := &Bus{id: id, pending: map[string]pending{}, handler: h, state: state, slots: make(chan struct{}, 64)}
	ready := make(chan struct{}, 1)
	topics := map[string]byte{Root + id + "/request/+": 1, Root + id + "/reply/+": 1}
	for _, topic := range subscriptions {
		topics[topic] = 1
	}
	options := transport.Options(u.String(), id, id, password)
	if u.Scheme == "tls" {
		cfg, e := transportsecurity.Client()
		if e != nil {
			return nil, e
		}
		options.SetTLSConfig(cfg)
	}
	options.SetWill(Root+"presence/"+id, `{"state":"UNAVAILABLE"}`, 1, true)
	options.OnConnect = func(c mqtt.Client) {
		go func() {
			op, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if awaitV3(op, c.SubscribeMultiple(topics, func(_ mqtt.Client, m mqtt.Message) {
				b.receive(ctx, &paho.Publish{Topic: m.Topic(), QoS: m.Qos(), Retain: m.Retained(), Payload: append([]byte(nil), m.Payload()...)})
			})) == nil {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}()
	}
	b.v3 = mqtt.NewClient(options)
	if e = awaitV3(ctx, b.v3.Connect()); e != nil {
		b.v3.Disconnect(0)
		return nil, e
	}
	select {
	case <-ready:
	case <-ctx.Done():
		b.v3.Disconnect(0)
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		b.v3.Disconnect(0)
		return nil, errors.New("MQTT3 subscriptions not ready")
	}
	go func() { <-ctx.Done(); b.v3.Disconnect(100) }()
	return b, nil
}
func awaitV3(ctx context.Context, token mqtt.Token) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-token.Done():
		return token.Error()
	}
}
func (b *Bus) publishPacket(ctx context.Context, p *paho.Publish) (*paho.PublishResponse, error) {
	if b.v3 == nil {
		return b.cm.Publish(ctx, p)
	}
	if !strings.HasPrefix(p.Topic, Root) {
		return nil, errors.New("noncanonical MQTT topic")
	}
	e := awaitV3(ctx, b.v3.Publish(p.Topic, p.QoS, p.Retain, p.Payload))
	return &paho.PublishResponse{}, e
}
