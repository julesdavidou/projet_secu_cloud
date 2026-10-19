/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"main.go" démarre le serveur et initialise les composants
*/
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `{"status":"healthy"}`)
}

func main() {
	// Variables du .env
	port := os.Getenv("APP_PORT")
	if port == "" {
		log.Fatal("APP_PORT non défini dans .env")
	}

	// Initialisation
	initDatabase()
	initMQTT()

	// Vérifier l'état du serveur
	http.HandleFunc("/health", healthHandler)

	// Gestion d'erreur
	log.Println("Serveur de zone démarré sur :" + port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
