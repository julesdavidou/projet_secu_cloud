package edgecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"io"
	"net/http"
	"os"
	"strings"
)

func (b *Bridge) unitRequest(ctx context.Context, u controllers.Unit, method string, verified bool, body io.Reader, length int64, digest string) (*http.Response, error) {
	tenant := "project"
	backend := b.backend
	token := b.token
	if u.OwnerID != "" && os.Getenv("STORAGE_BACKENDS_FILE") != "" {
		if !strings.HasPrefix(u.OwnerID, "client-") {
			return nil, errors.New("invalid tenant owner")
		}
		// Admin writes this node-local registry atomically. The Edge has only a
		// read-only mount and never receives access to the Docker engine.
		file, e := os.Open(controllers.Env("STORAGE_BACKENDS_FILE", ""))
		if e != nil {
			return nil, errors.New("owner storage not provisioned: run provision-tenant.ps1 for this node")
		}
		defer file.Close()
		type storageTarget struct {
			Owner    string `json:"owner_id"`
			Endpoint string `json:"endpoint"`
			Token    string `json:"token"`
		}
		var registry map[string]storageTarget
		if json.NewDecoder(io.LimitReader(file, 65536)).Decode(&registry) != nil {
			return nil, errors.New("invalid local tenant registry")
		}
		target := registry[u.OwnerID]
		if target.Owner != u.OwnerID || !strings.HasPrefix(target.Endpoint, "https://") || len(target.Token) < 24 {
			return nil, errors.New("invalid tenant storage receipt")
		}
		tenant = u.OwnerID
		backend = target.Endpoint
		token = target.Token
	}
	route := "/v1/storage/"
	if verified {
		route = "/v1/verified/"
	}
	q, e := http.NewRequestWithContext(ctx, method, backend+route+tenant+"/"+u.Object, body)
	if e != nil {
		return nil, e
	}
	q.ContentLength = length
	q.Header.Set("Authorization", "Bearer "+token)
	if digest != "" {
		q.Header.Set("X-Content-SHA256", digest)
	}
	return b.client.Do(q)
}
