package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/internal/edgecontrol"
	"github.com/julesdavidou/projet_secu_cloud/internal/monitor"
	"github.com/julesdavidou/projet_secu_cloud/internal/mqttclient"
	"github.com/julesdavidou/projet_secu_cloud/internal/storage"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
)

type Config struct {
	EdgeID            string
	Zone              string
	MQTTBroker        string
	MQTTUsername      string
	MQTTPassword      string
	StorageRoot       string
	HeartbeatInterval time.Duration
}

func getEnv(name string, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	return value
}

func loadConfig() Config {
	interval, err := time.ParseDuration(getEnv("HEARTBEAT_INTERVAL", "5s"))
	if err != nil || interval <= 0 {
		log.Fatalf("invalid heartbeat interval: %v", err)
	}

	return Config{
		EdgeID:            getEnv("EDGE_ID", "edge-dev-01"),
		Zone:              getEnv("EDGE_ZONE", "dev"),
		MQTTBroker:        getEnv("MQTT_BROKER", ""),
		MQTTUsername:      getEnv("MQTT_USERNAME", ""),
		MQTTPassword:      getEnv("MQTT_PASSWORD", ""),
		StorageRoot:       getEnv("STORAGE_ROOT", "/data"),
		HeartbeatInterval: interval,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func main() {
	config := loadConfig()

	store, err := storage.New(config.StorageRoot)
	if tenant := os.Getenv("STORAGE_TENANT_ID"); tenant != "" {
		store, err = storage.NewTenant(config.StorageRoot, tenant)
	}
	if err != nil {
		log.Fatalf("storage initialization failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var mqttClient *mqttclient.Client

	managed := os.Getenv("EDGE_CONTROL_ENABLED") == "1"
	if config.MQTTBroker != "" && !managed {
		mqttClient = mqttclient.New(
			config.MQTTBroker,
			config.EdgeID,
			config.Zone,
			config.MQTTUsername,
			config.MQTTPassword,
			config.StorageRoot,
			func(tenant, object string) error {
				if os.Getenv("MQTT_ALLOW_DELETE") != "1" || tenant == "project" {
					return errors.New("managed deletion requires authoritative catalogue coordination")
				}
				return store.Delete(tenant, object)
			},
		)

		go func() {
			for {
				if err := mqttClient.Connect(); err == nil {
					_ = mqttClient.PublishState(getLocalIP())
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}()
	} else if !managed {
		log.Println("MQTT_BROKER not configured: MQTT disabled")
	}

	if managed {
		cfg, e := controllers.LoadConfig(controllers.Env("CONTROLLERS_CONFIG", "/config/config.json"))
		if e != nil {
			log.Fatal(e)
		}
		go func() {
			if e := edgecontrol.Run(ctx, cfg); e != nil && ctx.Err() == nil {
				log.Printf("managed Edge control stopped: %v", e)
				stop()
			}
		}()
		log.Print("managed MQTT control enabled; standalone telemetry publisher disabled")
	}
	mux := http.NewServeMux()
	registerVerifiedRoutes(mux, store)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"edge_id": config.EdgeID,
		})
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		status, err := monitor.Collect(config.EdgeID, config.Zone, config.StorageRoot)
		if err != nil {
			http.Error(w, "monitoring error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("PUT /v1/storage/{tenant}/{object}", func(w http.ResponseWriter, r *http.Request) {
		tenant := r.PathValue("tenant")
		object := r.PathValue("object")

		size, err := store.Put(tenant, object, r.Body)
		if errors.Is(err, storage.ErrInvalidID) {
			http.Error(w, "invalid identifier", http.StatusBadRequest)
			return
		}
		if err != nil {
			log.Printf("storage error: %v", err)
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"tenant": tenant,
			"object": object,
			"size":   size,
			"edge":   config.EdgeID,
		})

		if mqttClient != nil && mqttClient.IsConnected() {
			_ = mqttClient.PublishEvent("evt-"+object, "UPLOAD_COMPLETE", map[string]interface{}{
				"tenant": tenant,
				"object": object,
				"size":   size,
			})
			_ = mqttClient.PublishState(getLocalIP())
		}
	})

	mux.HandleFunc("GET /v1/storage/{tenant}/{object}", func(w http.ResponseWriter, r *http.Request) {
		path, err := store.Path(r.PathValue("tenant"), r.PathValue("object"))
		if errors.Is(err, storage.ErrInvalidID) {
			http.Error(w, "invalid identifier", http.StatusBadRequest)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "storage error", http.StatusInternalServerError)
			return
		}
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("DELETE /v1/storage/{tenant}/{object}", func(w http.ResponseWriter, r *http.Request) {
		tenant := r.PathValue("tenant")
		object := r.PathValue("object")

		if managed && tenant == "project" {
			http.Error(w, "managed deletion requires catalogue coordination", 409)
			return
		}
		err := store.Delete(tenant, object)
		if errors.Is(err, storage.ErrInvalidID) {
			http.Error(w, "invalid identifier", http.StatusBadRequest)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "delete error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)

		if mqttClient != nil && mqttClient.IsConnected() {
			_ = mqttClient.PublishEvent("evt-del-"+object, "DELETE_COMPLETE", map[string]interface{}{
				"tenant": tenant,
				"object": object,
			})
			_ = mqttClient.PublishState(getLocalIP())
		}
	})

	handler, guardErr := storageGuard(mux)
	if guardErr != nil {
		log.Fatal("HTTP authentication configuration invalid: ", guardErr)
	}
	server := &http.Server{
		Addr:              getEnv("STORAGE_HTTP_ADDR", ":8080"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("Edge %s listening on %s", config.EdgeID, server.Addr)
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(config.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if mqttClient == nil || !mqttClient.IsConnected() {
					continue
				}
				if err := mqttClient.PublishHeartbeat(); err != nil {
					log.Printf("heartbeat error: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()

	log.Println("stopping Edge Agent")
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = server.Shutdown(shutdownContext)

	if mqttClient != nil {
		mqttClient.Disconnect()
	}
}
