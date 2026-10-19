/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"nodes.go" gère la logique métier des nœuds Edge
*/
package main

import (
	"time"
)

// ----------------------------------------------------------------------------------------------------
// Modèle
// ----------------------------------------------------------------------------------------------------
type Node struct {
	ID            string    `json:"node_id"`
	Hostname      string    `json:"hostname"`
	IP            string    `json:"ip"`
	Status        string    `json:"status"`
	StorageMax    int64     `json:"storage_max"`
	StorageUsed   int64     `json:"storage_used"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// ----------------------------------------------------------------------------------------------------
// Logique métier
// ----------------------------------------------------------------------------------------------------

// Upsert (Création ou Mise à jour) d'un nœud à la réception de zone/{zone_id}/node/{node_id}/state
func upsertNode(node Node) error {
	_, err := db.Exec(`
		INSERT INTO nodes (id, hostname, ip, status, storage_max, storage_used, last_heartbeat)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			hostname = excluded.hostname,
			ip = excluded.ip,
			status = excluded.status,
			storage_max = excluded.storage_max,
			storage_used = excluded.storage_used,
			last_heartbeat = CURRENT_TIMESTAMP;
	`, node.ID, node.Hostname, node.IP, node.Status, node.StorageMax, node.StorageUsed)

	return err
}

// Enregistre un nouveau nœud
func registerNode(node Node) error {
	return upsertNode(node)
}

// Rafraîchit le heartbeat d'un nœud à la réception de zone/{zone_id}/node/{node_id}/heartbeat
func touchNodeHeartbeat(id string) error {
	_, err := db.Exec(`
		UPDATE nodes
		SET last_heartbeat = CURRENT_TIMESTAMP, status = 'ONLINE'
		WHERE id = ?
	`, id)
	return err
}

// Met à jour le statut d'un nœud
func updateNodeStatus(id string, status string) error {
	_, err := db.Exec(`
		UPDATE nodes
		SET status = ?
		WHERE id = ?
	`, status, id)

	return err
}

// Met à jour le stockage utilisé d'un nœud
func updateNodeStorage(id string, storageUsed int64) error {
	_, err := db.Exec(`
		UPDATE nodes
		SET storage_used = ?
		WHERE id = ?
	`, storageUsed, id)

	return err
}

// Supprime un nœud (ex: après un désenregistrement explicite)
func removeNode(id string) error {
	_, err := db.Exec(`
		DELETE FROM nodes
		WHERE id = ?
	`, id)

	return err
}

// Récupère tous les nœuds de la zone
func getNodes() ([]Node, error) {
	rows, err := db.Query(`
		SELECT id, hostname, ip, status, storage_max, storage_used, last_heartbeat
		FROM nodes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var node Node
		var lastHB string

		err := rows.Scan(
			&node.ID,
			&node.Hostname,
			&node.IP,
			&node.Status,
			&node.StorageMax,
			&node.StorageUsed,
			&lastHB,
		)
		if err != nil {
			return nil, err
		}
		node.LastHeartbeat, _ = time.Parse("2006-01-02 15:04:05", lastHB)
		nodes = append(nodes, node)
	}

	return nodes, rows.Err()
}

// Récupère un nœud par son ID
func getNode(id string) (Node, error) {
	var node Node
	var lastHB string

	err := db.QueryRow(`
		SELECT id, hostname, ip, status, storage_max, storage_used, last_heartbeat
		FROM nodes
		WHERE id = ?
	`, id).Scan(
		&node.ID,
		&node.Hostname,
		&node.IP,
		&node.Status,
		&node.StorageMax,
		&node.StorageUsed,
		&lastHB,
	)

	if err == nil {
		node.LastHeartbeat, _ = time.Parse("2006-01-02 15:04:05", lastHB)
	}

	return node, err
}

// Sélectionne les nœuds éligibles pour le stockage (pour allocator.go)
func getEligibleNodes(minFreeSpace int64) ([]Node, error) {
	rows, err := db.Query(`
		SELECT id, hostname, ip, status, storage_max, storage_used, last_heartbeat
		FROM nodes
		WHERE status = 'ONLINE' AND (storage_max - storage_used) >= ?
		ORDER BY (storage_max - storage_used) DESC
	`, minFreeSpace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var node Node
		var lastHB string
		if err := rows.Scan(&node.ID, &node.Hostname, &node.IP, &node.Status, &node.StorageMax, &node.StorageUsed, &lastHB); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}
