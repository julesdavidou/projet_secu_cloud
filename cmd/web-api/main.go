package main

import (
	"github.com/julesdavidou/projet_secu_cloud/internal/webapp"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"log"
	"net/http"
	"time"
)

func main() {
	topology, err := controllers.LoadConfig(controllers.Env("CONTROLLERS_CONFIG", "/config/config.json"))
	if err != nil {
		log.Fatal(err)
	}
	s, e := webapp.New(webapp.Config{CentralURL: controllers.Env("CENTRAL_URL", "http://central:8088"), AuthURL: controllers.Env("AUTH_URL", "http://auth:8082"), SecretsDir: controllers.Env("WEB_SECRETS_DIR", "/run/web-secrets"), DataDir: controllers.Env("WEB_DATA_DIR", "/data"), StaticDir: controllers.Env("WEB_STATIC_DIR", "/app/frontend"), ConsoleDir: controllers.Env("WEB_CONSOLE_DIR", "/app/console"), VideoScript: controllers.Env("VIDEO_SCRIPT", "/app/video/segment_upload.py"), SourceNodeID: controllers.Env("UPLOAD_SOURCE_NODE", "A1"), NodeTTL: time.Duration(topology.NodeTTLSeconds) * time.Second, MaxUpload: 64 << 20})
	if e != nil {
		log.Fatal(e)
	}
	server := http.Server{Addr: ":8080", Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 180 * time.Second, WriteTimeout: 180 * time.Second, IdleTimeout: 30 * time.Second}
	log.Print("CaribouTV integration HTTP :8080")
	log.Fatal(server.ListenAndServe())
}
