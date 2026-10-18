/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"nodes.go" gère la logique métier
*/
package main

// ----------------------------------------------------------------------------------------------------
// Modèle
// ----------------------------------------------------------------------------------------------------
type Node struct {
	ID          string
	Hostname    string
	IP          string
	Status      string
	StorageMax  int64
	StorageUsed int64
}

// ----------------------------------------------------------------------------------------------------
// Logique métier
// ----------------------------------------------------------------------------------------------------

// Enregistre un nouveau nœud.
func registerNode(node Node) error {
	_, err := db.Exec(`
		INSERT INTO nodes
		(id, hostname, ip, status, storage_max, storage_used)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		node.ID,
		node.Hostname,
		node.IP,
		node.Status,
		node.StorageMax,
		node.StorageUsed,
	)

	return err
}

// Met à jour le statut d'un nœud.
func updateNodeStatus(id string, status string) error {
	_, err := db.Exec(`
		UPDATE nodes
		SET status = ?
		WHERE id = ?
	`, status, id)

	return err
}

// Met à jour le stockage utilisé d'un nœud.
func updateNodeStorage(id string, storageUsed int64) error {
	_, err := db.Exec(`
		UPDATE nodes
		SET storage_used = ?
		WHERE id = ?
	`, storageUsed, id)

	return err
}

// Supprime un nœud.
func removeNode(id string) error {
	_, err := db.Exec(`
		DELETE FROM nodes
		WHERE id = ?
	`, id)

	return err
}

// Récupère tous les nœuds.
func getNodes() ([]Node, error) {
	rows, err := db.Query(`
		SELECT id, hostname, ip, status, storage_max, storage_used
		FROM nodes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var node Node

		err := rows.Scan(
			&node.ID,
			&node.Hostname,
			&node.IP,
			&node.Status,
			&node.StorageMax,
			&node.StorageUsed,
		)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	return nodes, rows.Err()
}

// Récupère un nœud par son ID.
func getNode(id string) (Node, error) {
	var node Node

	err := db.QueryRow(`
		SELECT id, hostname, ip, status, storage_max, storage_used
		FROM nodes
		WHERE id = ?
	`, id).Scan(&node.ID, &node.Hostname, &node.IP, &node.Status, &node.StorageMax, &node.StorageUsed)

	return node, err
}
