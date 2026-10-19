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
	client      mqtt.Client
	nodeID      string
	zoneID      string
	storageRoot string
}

func New(
	broker string,
	nodeID string,
	zoneID string,
	username string,
	password string,
	storageRoot string,
	onDeleteRequested func(tenant, object string) error,
) *Client {

	options := mqtt.NewClientOptions()

	options.AddBroker(broker)
	options.SetClientID(nodeID)
	options.SetUsername(username)
	options.SetPassword(password)

	options.SetAutoReconnect(true)
	options.SetConnectRetry(true)
	options.SetConnectRetryInterval(2 * time.Second)

	stateTopic := fmt.Sprintf("zone/%s/node/%s/state", zoneID, nodeID)
	commandTopic := fmt.Sprintf("node/%s/command", nodeID)

	// Message Testament (LWT) en cas de crash ou déconnexion
	offlinePayload, _ := json.Marshal(map[string]interface{}{
		"op":      "UPDATE",
		"status":  "OFFLINE",
		"node_id": nodeID,
	})

	options.SetWill(stateTopic, string(offlinePayload), 1, true)

	result := &Client{
		nodeID:      nodeID,
		zoneID:      zoneID,
		storageRoot: storageRoot,
	}

	options.OnConnect = func(client mqtt.Client) {
		log.Printf("Connecté au broker MQTT %s (Zone: %s)", broker, zoneID)

		// 1. Déclaration de la zone
		zoneTopic := fmt.Sprintf("node/%s/zone", nodeID)
		client.Publish(zoneTopic, 1, true, zoneID)

		// 2. Écoute et traitement des commandes du Serveur de Zone
		client.Subscribe(commandTopic, 1, func(client mqtt.Client, message mqtt.Message) {
			log.Printf("Commande MQTT reçue sur %s : %s", message.Topic(), string(message.Payload()))

			var cmd struct {
				Op       string `json:"op"`
				Tenant   string `json:"tenant,omitempty"`
				Object   string `json:"object,omitempty"`
				ActionID string `json:"action_id,omitempty"`
			}

			if err := json.Unmarshal(message.Payload(), &cmd); err != nil {
				log.Printf("Erreur décodage commande MQTT : %v", err)
				return
			}

			switch cmd.Op {
			// Demande du serveur de zone de renvoyer l'état complet
			case "REQUEST_STATUS", "REFRESH_STATE":
				_ = result.PublishState(getLocalIP())

			// Demande du serveur de zone d'effacer un fragment stocké
			case "DELETE_OBJECT":
				if onDeleteRequested != nil && cmd.Tenant != "" && cmd.Object != "" {
					err := onDeleteRequested(cmd.Tenant, cmd.Object)
					statusStr := "SUCCESS"
					if err != nil {
						statusStr = "FAILED"
					}
					// Notification de l'événement en retour au serveur de zone
					_ = result.PublishEvent(cmd.ActionID, "DELETE_RESULT", map[string]interface{}{
						"tenant": cmd.Tenant,
						"object": cmd.Object,
						"status": statusStr,
					})
					// Mettre à jour l'espace libéré dans la zone
					_ = result.PublishState(getLocalIP())
				}
			}
		})
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

// PublishState collecte les métriques disque courantes et publie sur zone/{zone_id}/node/{node_id}/state
func (c *Client) PublishState(localIP string) error {
	status, err := collectMetrics(c.nodeID, c.zoneID, c.storageRoot)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]interface{}{
		"op":           "UPDATE",
		"node_id":      c.nodeID,
		"hostname":     status.EdgeID,
		"ip":           localIP,
		"status":       "ONLINE",
		"storage_max":  status.Storage.Total,
		"storage_used": status.Storage.Used,
	})
	if err != nil {
		return err
	}

	topic := fmt.Sprintf("zone/%s/node/%s/state", c.zoneID, c.nodeID)
	token := c.client.Publish(topic, 1, true, payload)
	token.Wait()

	return token.Error()
}

// PublishHeartbeat envoie un ping périodique sur zone/{zone_id}/node/{node_id}/heartbeat
func (c *Client) PublishHeartbeat() error {
	payload, _ := json.Marshal(map[string]interface{}{
		"op":        "HEARTBEAT",
		"node_id":   c.nodeID,
		"timestamp": time.Now().UTC().Unix(),
	})

	topic := fmt.Sprintf("zone/%s/node/%s/heartbeat", c.zoneID, c.nodeID)
	token := c.client.Publish(topic, 0, false, payload)
	token.Wait()

	return token.Error()
}

// PublishEvent publie un événement sur node/{node_id}/event
func (c *Client) PublishEvent(eventID string, op string, details map[string]interface{}) error {
	payloadMap := map[string]interface{}{
		"event_id": eventID,
		"node_id":  c.nodeID,
		"op":       op,
	}
	for k, v := range details {
		payloadMap[k] = v
	}

	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return err
	}

	topic := fmt.Sprintf("node/%s/event", c.nodeID)
	token := c.client.Publish(topic, 1, false, payload)
	token.Wait()

	return token.Error()
}

func (c *Client) Disconnect() {
	if c.client.IsConnected() {
		c.client.Disconnect(250)
	}
}
