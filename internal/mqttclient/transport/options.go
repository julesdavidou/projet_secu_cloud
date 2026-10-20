// Package transport shares the colleague MQTT reconnect and timeout policy.
package transport

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"time"
)

func Options(broker, nodeID, username, password string) *mqtt.ClientOptions {
	return mqtt.NewClientOptions().AddBroker(broker).SetClientID(nodeID).SetUsername(username).SetPassword(password).SetProtocolVersion(4).SetOrderMatters(false).SetWriteTimeout(3 * time.Second).SetAutoReconnect(true).SetConnectRetry(false).SetConnectTimeout(3 * time.Second).SetKeepAlive(10 * time.Second)
}
