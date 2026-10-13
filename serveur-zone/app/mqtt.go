/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"mqtt.go", appelle les fonctions métiers dans "nodes.go"
*/
package main

import (
	"encoding/json"
	"log"
	"os"
)
import mqtt "github.com/eclipse/paho.mqtt.golang"

func initMQTT() {
	if os.Getenv("PLACEMENT_ENABLED") == "1" {
		initAllocator()
		return
	}
	if os.Getenv("LEGACY_COMMANDS_ENABLED") != "1" {
		log.Print("Legacy MQTT commands disabled; placement requires PLACEMENT_ENABLED=1")
		return
	}
	// Variables du .env
	broker := os.Getenv("MQTT_BROKER")
	if broker == "" {
		log.Fatal("MQTT_BROKER non défini dans .env")
	}
	clientID := os.Getenv("MQTT_CLIENT_ID")
	if clientID == "" {
		log.Fatal("MQTT_CLIENT_ID non défini dans .env")
	}

	// Initialisation de la connection entre client et Broker
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if token.Error() != nil {
		log.Fatal("MQTT connect error:", token.Error())
	}

	// Abonnements
	// TODO: créer les abonnements et les handlers associés dans nodes.go
	client.Subscribe("cariboutv/commands", 1, func(_ mqtt.Client, msg mqtt.Message) {
		var cmd Command
		if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
			log.Println("MQTT unmarshal error:", err)
			return
		}

		switch cmd.Action {
		case "allocate":
			handleAllocate(cmd)
		case "deallocate":
			handleDeallocate(cmd)
		}
	})
}
