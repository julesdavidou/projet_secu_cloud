/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"database.go" initialise la table "node", persistance des données avec le volume
*/
package main

import (
	"database/sql"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDatabase() {
	var err error

	path := os.Getenv("ZONE_DB_PATH")
	if path == "" {
		path = "/data/zone.db"
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL,
			ip TEXT NOT NULL,
			status TEXT NOT NULL,
			storage_max INTEGER NOT NULL,
			storage_used INTEGER NOT NULL
		);
	`)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Base de données initialisée")
}
