// Package mqttbus is an isolated MQTT 5 reference adapter. Its topic root is
// intentionally disjoint from the team's edge/* topics. Broker ACLs bind the
// last topic component to the authenticated username; payload identity alone
// is never used as authorization.
package mqttbus

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	mqtt3 "github.com/eclipse/paho.mqtt.golang"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
)

const Root = "lab/placement/v1/"
const MaxPayload = 1024 * 1024

type Request struct {
	Op            string          `json:"op"`
	Body          json.RawMessage `json:"body"`
	Expires       time.Time       `json:"expires_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	ReplyTo       string          `json:"reply_to,omitempty"`
}
type Response struct {
	Body          json.RawMessage `json:"body,omitempty"`
	Error         string          `json:"error,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
}
type Handler func(context.Context, string, Request) (any, error)
type pending struct {
	target string
	result chan Response
}
type Bus struct {
	id           string
	cm           *autopaho.ConnectionManager
	v3           mqtt3.Client
	jsonEnvelope atomic.Bool
	mu           sync.Mutex
	pending      map[string]pending
	handler      Handler
	state        func(string, []byte, bool)
	slots        chan struct{}
}

func Decode(raw []byte, out any) error {
	if len(raw) > MaxPayload {
		return errors.New("payload too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func New(ctx context.Context, broker, id, password string, subscriptions []string, h Handler, state func(string, []byte, bool)) (*Bus, error) {
	return NewWithProtocol(ctx, broker, id, password, subscriptions, h, state, "5")
}
func NewWithProtocol(ctx context.Context, broker, id, password string, subscriptions []string, h Handler, state func(string, []byte, bool), protocol string) (*Bus, error) {
	if protocol != "5" && protocol != "3.1.1" {
		return nil, errors.New("MQTT_PROTOCOL must be 5 or 3.1.1")
	}
	if protocol == "3.1.1" {
		return newV3(ctx, broker, id, password, subscriptions, h, state)
	}
	if !placement.ValidID(id) || password == "" {
		return nil, errors.New("MQTT identity and password required")
	}
	u, err := url.Parse(broker)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "mqtt" && u.Scheme != "tls") {
		return nil, errors.New("invalid broker URL")
	}
	b := &Bus{id: id, pending: map[string]pending{}, handler: h, state: state, slots: make(chan struct{}, 64)}
	ready := make(chan struct{}, 1)
	subs := []paho.SubscribeOptions{{Topic: Root + id + "/request/+", QoS: 1}, {Topic: Root + id + "/reply/+", QoS: 1}}
	for _, s := range subscriptions {
		subs = append(subs, paho.SubscribeOptions{Topic: s, QoS: 1})
	}
	cfg := autopaho.ClientConfig{ServerUrls: []*url.URL{u}, KeepAlive: 10, CleanStartOnInitialConnection: true, SessionExpiryInterval: 60, ConnectUsername: id, ConnectPassword: []byte(password), ReconnectBackoff: func(int) time.Duration { return time.Second }, ConnectTimeout: 3 * time.Second,
		WillMessage: &paho.WillMessage{Topic: Root + "presence/" + id, QoS: 1, Retain: true, Payload: []byte(`{"state":"UNAVAILABLE"}`)},
		OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
			go func() {
				op, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				ack, e := cm.Subscribe(op, &paho.Subscribe{Subscriptions: subs})
				if e != nil {
					return
				}
				for _, code := range ack.Reasons {
					if code >= 128 {
						return
					}
				}
				select {
				case ready <- struct{}{}:
				default:
				}
			}()
		},
		ClientConfig: paho.ClientConfig{ClientID: id, PacketTimeout: 3 * time.Second, OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(pr paho.PublishReceived) (bool, error) { b.receive(ctx, pr.Packet); return true, nil }}},
	}
	b.cm, err = autopaho.NewConnection(ctx, cfg)
	if err != nil {
		return nil, err
	}
	select {
	case <-ready:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		return nil, errors.New("MQTT subscription readiness timeout")
	}
}
func (b *Bus) receive(ctx context.Context, p *paho.Publish) {
	if len(p.Payload) > MaxPayload {
		return
	}
	if p.Properties == nil || len(p.Properties.CorrelationData) == 0 {
		var wire struct {
			CorrelationID string `json:"correlation_id"`
			ReplyTo       string `json:"reply_to"`
		}
		if json.Unmarshal(p.Payload, &wire) == nil {
			corr, e := hex.DecodeString(wire.CorrelationID)
			if e == nil && len(corr) == 16 {
				p.Properties = &paho.PublishProperties{CorrelationData: corr, ResponseTopic: wire.ReplyTo}
			}
		}
	}
	if len(p.Payload) > MaxPayload {
		return
	}
	replyPrefix := Root + b.id + "/reply/"
	requestPrefix := Root + b.id + "/request/"
	if strings.HasPrefix(p.Topic, replyPrefix) {
		if p.Retain || p.Properties == nil {
			return
		}
		target := strings.TrimPrefix(p.Topic, replyPrefix)
		key := hex.EncodeToString(p.Properties.CorrelationData)
		b.mu.Lock()
		wait, ok := b.pending[key]
		b.mu.Unlock()
		if !ok || wait.target != target {
			return
		}
		var response Response
		if Decode(p.Payload, &response) != nil || (response.CorrelationID != "" && response.CorrelationID != key) {
			return
		}
		select {
		case wait.result <- response:
		default:
		}
		return
	}
	if strings.HasPrefix(p.Topic, requestPrefix) {
		if p.Retain || p.Properties == nil || p.QoS != 1 || b.handler == nil {
			return
		}
		sender := strings.TrimPrefix(p.Topic, requestPrefix)
		if !placement.ValidID(sender) || p.Properties.ResponseTopic != Root+sender+"/reply/"+b.id || len(p.Properties.CorrelationData) != 16 {
			return
		}
		var req Request
		if Decode(p.Payload, &req) != nil || req.Op == "" || !time.Now().Before(req.Expires) || req.Expires.Sub(time.Now()) > time.Minute {
			return
		}
		if (req.CorrelationID != "" || req.ReplyTo != "") && (req.CorrelationID != hex.EncodeToString(p.Properties.CorrelationData) || req.ReplyTo != p.Properties.ResponseTopic) {
			return
		}
		select {
		case b.slots <- struct{}{}:
		default:
			return
		}
		go func() {
			defer func() { <-b.slots }()
			op, cancel := context.WithDeadline(ctx, req.Expires)
			defer cancel()
			value, err := b.handler(op, sender, req)
			response := Response{}
			if req.CorrelationID != "" {
				response.CorrelationID = req.CorrelationID
			}
			if err != nil {
				response.Error = err.Error()
			} else {
				response.Body, _ = json.Marshal(value)
			}
			raw, _ := json.Marshal(response)
			_, _ = b.publishPacket(op, &paho.Publish{Topic: p.Properties.ResponseTopic, QoS: 1, Payload: raw, Properties: &paho.PublishProperties{CorrelationData: p.Properties.CorrelationData}})
		}()
		return
	}
	if b.state != nil {
		b.state(p.Topic, p.Payload, p.Retain)
	}
}
func (b *Bus) Call(ctx context.Context, target, op string, value, out any) error {
	if !placement.ValidID(target) {
		return errors.New("invalid target")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Minute {
		return errors.New("bounded request deadline required")
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	correlation := make([]byte, 16)
	if _, err = rand.Read(correlation); err != nil {
		return err
	}
	key := hex.EncodeToString(correlation)
	reqBody := Request{Op: op, Body: body, Expires: deadline}
	if b.v3 != nil || b.jsonEnvelope.Load() {
		reqBody.CorrelationID = key
		reqBody.ReplyTo = Root + b.id + "/reply/" + target
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	if len(raw) > MaxPayload {
		return errors.New("manifest exceeds MQTT control limit; use external resolver")
	}
	result := make(chan Response, 1)
	b.mu.Lock()
	if len(b.pending) >= 64 {
		b.mu.Unlock()
		return errors.New("too many in-flight RPCs")
	}
	b.pending[key] = pending{target, result}
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.pending, key); b.mu.Unlock() }()
	expiry := uint32(time.Until(deadline).Seconds()) + 1
	ack, err := b.publishPacket(ctx, &paho.Publish{Topic: Root + target + "/request/" + b.id, QoS: 1, Payload: raw, Properties: &paho.PublishProperties{ResponseTopic: Root + b.id + "/reply/" + target, CorrelationData: correlation, MessageExpiry: &expiry}})
	if err != nil {
		return err
	}
	if ack.ReasonCode >= 128 {
		return errors.New("MQTT publication rejected")
	}
	select {
	case response := <-result:
		if response.Error != "" {
			return errors.New(response.Error)
		}
		if out != nil {
			return Decode(response.Body, out)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (b *Bus) Publish(ctx context.Context, topic string, value any, qos byte, retained bool, ttl uint32) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > MaxPayload {
		return errors.New("payload too large")
	}
	ack, err := b.publishPacket(ctx, &paho.Publish{Topic: topic, QoS: qos, Retain: retained, Payload: raw, Properties: &paho.PublishProperties{MessageExpiry: &ttl}})
	if err != nil {
		return err
	}
	if ack != nil && ack.ReasonCode >= 128 {
		return errors.New("MQTT publication rejected")
	}
	return nil
}

// Retry repeats the SAME business body/event ID after a missing commit reply.
// QoS 1 acknowledgement alone never satisfies the caller.
func (b *Bus) Retry(ctx context.Context, target, op string, value, out any) error {
	var err error
	for i := 0; i < 3; i++ {
		attempt, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = b.Call(attempt, target, op, value, out)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}

// EnableJSONEnvelope permits MQTT5 callers to address MQTT3 peers. Enable on all
// peers in a mixed deployment; the default preserves pre-v2 MQTT5 wire format.
func (b *Bus) EnableJSONEnvelope() { b.jsonEnvelope.Store(true) }
