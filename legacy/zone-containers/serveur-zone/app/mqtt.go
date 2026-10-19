/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"mqtt.go", écoute les topics MQTT v5 de la zone et appelle les fonctions métiers de "nodes.go"
*/
package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"strings"

	"github.com/eclipse/paho.golang/paho"
)

var mqttClient *paho.Client
var zoneID string

// Structures de Payload conformes au schéma MQTT du projet
type NodeStatePayload struct {
	Op          string `json:"op"`
	Hostname    string `json:"hostname"`
	IP          string `json:"ip"`
	Status      string `json:"status"`
	StorageMax  int64  `json:"storage_max"`
	StorageUsed int64  `json:"storage_used"`
}

type UploadRequestPayload struct {
	Op       string `json:"op"`
	VideoID  string `json:"video_id"`
	Size     int64  `json:"size"`
	Segments int    `json:"segments"`
}

type PlacementReplyPayload struct {
	RequestID string          `json:"request_id"`
	Status    string          `json:"status"`
	Nodes     []AllocatedNode `json:"nodes,omitempty"`
}

type AllocatedNode struct {
	NodeID   string `json:"node_id"`
	Endpoint string `json:"endpoint"`
}

func initMQTT() {
	brokerAddr := os.Getenv("MQTT_BROKER")
	clientID := os.Getenv("MQTT_CLIENT_ID")
	zoneID = os.Getenv("ZONE_ID")
	username := os.Getenv("MQTT_USER")
	password := os.Getenv("MQTT_PASSWORD")

	if brokerAddr == "" || clientID == "" || zoneID == "" {
		log.Fatal("Variables MQTT manquantes dans l'environnement")
	}

	conn, err := net.Dial("tcp", brokerAddr)
	if err != nil {
		log.Fatalf("Impossible de se connecter au socket broker MQTT : %v", err)
	}

	mqttClient = paho.NewClient(paho.ClientConfig{
		ClientID: clientID,
		Conn:     conn,
		OnPublishReceived: []func(paho.PublishReceived) (bool, error){
			func(pr paho.PublishReceived) (bool, error) {
				handleIncomingPublish(pr.Packet)
				return true, nil
			},
		},
	})

	// Paquet de connexion MQTT v5 avec identifiants
	cp := &paho.Connect{
		ClientID:     clientID,
		Username:     username,
		Password:     []byte(password),
		UsernameFlag: username != "",
		PasswordFlag: password != "",
	}

	if _, err := mqttClient.Connect(context.Background(), cp); err != nil {
		log.Fatalf("Échec de l'authentification / connexion MQTT v5 : %v", err)
	}

	log.Printf("Connecté et authentifié auprès du Broker MQTT (Zone: %s)", zoneID)
	subscribeZoneTopics()
}

func subscribeZoneTopics() {
	stateTopic := "zone/" + zoneID + "/node/+/state"
	heartbeatTopic := "zone/" + zoneID + "/node/+/heartbeat"
	uploadReqTopic := "zone/" + zoneID + "/upload/request"

	_, err := mqttClient.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: stateTopic, QoS: 1},
			{Topic: heartbeatTopic, QoS: 0},
			{Topic: uploadReqTopic, QoS: 1},
		},
	})
	if err != nil {
		log.Fatalf("Erreur lors de la souscription aux topics de zone : %v", err)
	}

	log.Printf("Abonné aux topics MQTT de la zone %s", zoneID)
}

func handleIncomingPublish(msg *paho.Publish) {
	topic := msg.Topic

	switch {
	case strings.HasPrefix(topic, "zone/"+zoneID+"/node/") && strings.HasSuffix(topic, "/state"):
		nodeID := extractNodeIDFromTopic(topic)
		var statePayload NodeStatePayload
		if err := json.Unmarshal(msg.Payload, &statePayload); err == nil {
			node := Node{
				ID:          nodeID,
				Hostname:    statePayload.Hostname,
				IP:          statePayload.IP,
				Status:      statePayload.Status,
				StorageMax:  statePayload.StorageMax,
				StorageUsed: statePayload.StorageUsed,
			}
			if err := upsertNode(node); err != nil {
				log.Printf("Erreur mise à jour nœud %s : %v", nodeID, err)
			}
		}

	case strings.HasPrefix(topic, "zone/"+zoneID+"/node/") && strings.HasSuffix(topic, "/heartbeat"):
		nodeID := extractNodeIDFromTopic(topic)
		if err := touchNodeHeartbeat(nodeID); err != nil {
			log.Printf("Erreur heartbeat nœud %s : %v", nodeID, err)
		}

	case topic == "zone/"+zoneID+"/upload/request":
		handleUploadPlacementRequest(msg)
	}
}

func handleUploadPlacementRequest(msg *paho.Publish) {
	var req UploadRequestPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		log.Printf("Erreur format requête upload : %v", err)
		return
	}

	// Appel de la logique métier d'allocation (allocator.go)
	allocatedNodes, err := allocateStorage(req.Size, req.Segments)

	reply := PlacementReplyPayload{
		RequestID: string(msg.Properties.CorrelationData),
	}

	if err != nil {
		reply.Status = "FAILED"
	} else {
		reply.Status = "OK"
		reply.Nodes = allocatedNodes
	}

	replyBytes, _ := json.Marshal(reply)

	// Utilisation native du mécanisme MQTT 5 Request/Response
	if msg.Properties != nil && msg.Properties.ResponseTopic != "" {
		mqttClient.Publish(context.Background(), &paho.Publish{
			Topic:   msg.Properties.ResponseTopic,
			QoS:     1,
			Payload: replyBytes,
			Properties: &paho.PublishProperties{
				CorrelationData: msg.Properties.CorrelationData,
			},
		})
	}
}

func extractNodeIDFromTopic(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
