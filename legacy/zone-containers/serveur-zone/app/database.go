/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"database.go" initialise la table "nodes", persistance des données avec le volume
*/
package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDatabase() {
	var err error

	db, err = sql.Open("sqlite", "/data/zone.db")
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL,
			ip TEXT NOT NULL,
			status TEXT NOT NULL,
			storage_max INTEGER NOT NULL,
			storage_used INTEGER NOT NULL,
			last_heartbeat DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Base de données SQLite de zone initialisée")
}
