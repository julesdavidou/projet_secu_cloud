package mqttclient

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/julesdavidou/projet_secu_cloud/pkg/protocol"
)

type Client struct {
	client mqtt.Client
	edgeID string
}

func New(
	broker string,
	edgeID string,
) *Client {

	options := mqtt.NewClientOptions()

	options.AddBroker(broker)
	options.SetClientID(edgeID)

	options.SetAutoReconnect(true)
	options.SetConnectRetry(true)
	options.SetConnectRetryInterval(
		2 * time.Second,
	)

	statusTopic := fmt.Sprintf(
		"edge/%s/status",
		edgeID,
	)

	offlinePayload, _ := json.Marshal(
		map[string]string{
			"edge_id": edgeID,
			"state":   "offline",
		},
	)

	options.SetWill(
		statusTopic,
		string(offlinePayload),
		1,
		true,
	)

	result := &Client{
		edgeID: edgeID,
	}

	options.OnConnect = func(
		client mqtt.Client,
	) {
		log.Printf(
			"connected to MQTT broker %s",
			broker,
		)

		onlinePayload, _ := json.Marshal(
			map[string]string{
				"edge_id": edgeID,
				"state":   "online",
			},
		)

		token := client.Publish(
			statusTopic,
			1,
			true,
			onlinePayload,
		)

		token.Wait()

		commandTopic := fmt.Sprintf(
			"edge/%s/commands/#",
			edgeID,
		)

		token = client.Subscribe(
			commandTopic,
			1,
			func(
				client mqtt.Client,
				message mqtt.Message,
			) {
				log.Printf(
					"MQTT command: %s -> %s",
					message.Topic(),
					string(message.Payload()),
				)
			},
		)

		token.Wait()
	}

	options.OnConnectionLost = func(
		client mqtt.Client,
		err error,
	) {
		log.Printf(
			"MQTT connection lost: %v",
			err,
		)
	}

	result.client = mqtt.NewClient(options)

	return result
}

func (c *Client) Connect() error {
	token := c.client.Connect()

	token.Wait()

	return token.Error()
}

func (c *Client) IsConnected() bool {
	return c.client.IsConnected()
}

func (c *Client) PublishHeartbeat(
	status protocol.NodeStatus,
) error {

	payload, err := json.Marshal(status)

	if err != nil {
		return err
	}

	topic := fmt.Sprintf(
		"edge/%s/heartbeat",
		c.edgeID,
	)

	token := c.client.Publish(
		topic,
		0,
		false,
		payload,
	)

	token.Wait()

	return token.Error()
}

func (c *Client) Disconnect() {
	if c.client.IsConnected() {
		c.client.Disconnect(250)
	}
}
