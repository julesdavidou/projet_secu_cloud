package main

import (
	"encoding/json"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
)

// The project snapshot is distinct from Dan's legacy physical-disk fields.
// It is a local cache; central.db remains the only placement authority.
func cacheProjectNode(n controllers.Registered) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS project_nodes(id TEXT PRIMARY KEY, snapshot TEXT NOT NULL)`); err != nil {
		return err
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if _, err = db.Exec(`INSERT INTO project_nodes(id,snapshot) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET snapshot=excluded.snapshot`, n.Node.ID, string(raw)); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO nodes(id,hostname,ip,status,storage_max,storage_used) VALUES(?,?,?,?,0,0) ON CONFLICT(id) DO UPDATE SET ip=excluded.ip,status=excluded.status`, n.Node.ID, n.Node.ID, n.Endpoint, string(n.Node.State))
	return err
}
