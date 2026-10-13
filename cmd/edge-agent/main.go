package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/internal/monitor"
	"github.com/julesdavidou/projet_secu_cloud/internal/mqttclient"
	"github.com/julesdavidou/projet_secu_cloud/internal/storage"
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

func getEnv(
	name string,
	defaultValue string,
) string {

	value := os.Getenv(name)

	if value == "" {
		return defaultValue
	}

	return value
}

func loadConfig() Config {
	interval, err := time.ParseDuration(
		getEnv(
			"HEARTBEAT_INTERVAL",
			"5s",
		),
	)

	if err != nil {
		log.Fatalf(
			"invalid heartbeat interval: %v",
			err,
		)
	}

	return Config{
		EdgeID: getEnv(
			"EDGE_ID",
			"edge-dev-01",
		),

		Zone: getEnv(
			"EDGE_ZONE",
			"dev",
		),

		MQTTBroker: getEnv(
			"MQTT_BROKER",
			"",
		),

		MQTTUsername: getEnv(
			"MQTT_USERNAME",
			"",
		),

		MQTTPassword: getEnv(
			"MQTT_PASSWORD",
			"",
		),

		StorageRoot: getEnv(
			"STORAGE_ROOT",
			"/data",
		),

		HeartbeatInterval: interval,
	}
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

func main() {

	config := loadConfig()

	store, err := storage.New(
		config.StorageRoot,
	)

	if err != nil {
		log.Fatalf(
			"storage initialization failed: %v",
			err,
		)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)

	defer stop()

	var mqttClient *mqttclient.Client

	if config.MQTTBroker != "" {
		mqttClient = mqttclient.New(
			config.MQTTBroker,
			config.EdgeID,
			config.MQTTUsername,
			config.MQTTPassword,
		)

		go func() {
			for {
				err := mqttClient.Connect()

				if err == nil {
					return
				}

				log.Printf(
					"MQTT connection failed: %v",
					err,
				)

				select {

				case <-ctx.Done():
					return

				case <-time.After(
					2 * time.Second,
				):
				}
			}
		}()
	} else {
		log.Println("MQTT_BROKER not configured: MQTT disabled")
	}

	getStatus := func() (any, error) {
		return monitor.Collect(
			config.EdgeID,
			config.Zone,
			config.StorageRoot,
		)
	}

	mux := http.NewServeMux()

	mux.HandleFunc(
		"GET /health",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			writeJSON(
				w,
				http.StatusOK,
				map[string]string{
					"status":  "ok",
					"edge_id": config.EdgeID,
				},
			)
		},
	)

	mux.HandleFunc(
		"GET /status",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			status, err := getStatus()

			if err != nil {
				http.Error(
					w,
					"monitoring error",
					http.StatusInternalServerError,
				)

				return
			}

			writeJSON(
				w,
				http.StatusOK,
				status,
			)
		},
	)

	mux.HandleFunc(
		"PUT /v1/storage/{tenant}/{object}",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			tenant := r.PathValue("tenant")
			object := r.PathValue("object")

			size, err := store.Put(
				tenant,
				object,
				r.Body,
			)

			if errors.Is(
				err,
				storage.ErrInvalidID,
			) {
				http.Error(
					w,
					"invalid identifier",
					http.StatusBadRequest,
				)

				return
			}

			if err != nil {
				log.Printf(
					"storage error: %v",
					err,
				)

				http.Error(
					w,
					"storage error",
					http.StatusInternalServerError,
				)

				return
			}

			writeJSON(
				w,
				http.StatusCreated,
				map[string]any{
					"tenant": tenant,
					"object": object,
					"size":   size,
					"edge":   config.EdgeID,
				},
			)
		},
	)

	mux.HandleFunc(
		"GET /v1/storage/{tenant}/{object}",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			path, err := store.Path(
				r.PathValue("tenant"),
				r.PathValue("object"),
			)

			if errors.Is(
				err,
				storage.ErrInvalidID,
			) {
				http.Error(
					w,
					"invalid identifier",
					http.StatusBadRequest,
				)

				return
			}

			if errors.Is(
				err,
				storage.ErrNotFound,
			) {
				http.Error(
					w,
					"not found",
					http.StatusNotFound,
				)

				return
			}

			if err != nil {
				http.Error(
					w,
					"storage error",
					http.StatusInternalServerError,
				)

				return
			}

			http.ServeFile(
				w,
				r,
				path,
			)
		},
	)

	mux.HandleFunc(
		"DELETE /v1/storage/{tenant}/{object}",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			err := store.Delete(
				r.PathValue("tenant"),
				r.PathValue("object"),
			)

			if errors.Is(
				err,
				storage.ErrInvalidID,
			) {
				http.Error(
					w,
					"invalid identifier",
					http.StatusBadRequest,
				)

				return
			}

			if errors.Is(
				err,
				storage.ErrNotFound,
			) {
				http.Error(
					w,
					"not found",
					http.StatusNotFound,
				)

				return
			}

			if err != nil {
				http.Error(
					w,
					"delete error",
					http.StatusInternalServerError,
				)

				return
			}

			w.WriteHeader(
				http.StatusNoContent,
			)
		},
	)

	server := &http.Server{
		Addr: ":8080",

		Handler: mux,

		ReadHeaderTimeout: 5 * time.Second,

		IdleTimeout: 60 * time.Second,
	}

	go func() {
		log.Printf(
			"Edge %s listening on :8080",
			config.EdgeID,
		)

		err := server.ListenAndServe()

		if err != nil &&
			!errors.Is(
				err,
				http.ErrServerClosed,
			) {

			log.Fatalf(
				"HTTP server failed: %v",
				err,
			)
		}
	}()

	go func() {
		ticker := time.NewTicker(
			config.HeartbeatInterval,
		)

		defer ticker.Stop()

		for {
			select {

			case <-ticker.C:

				if mqttClient == nil || !mqttClient.IsConnected() {
					continue
				}

				status, err := monitor.Collect(
					config.EdgeID,
					config.Zone,
					config.StorageRoot,
				)

				if err != nil {
					log.Printf(
						"monitor error: %v",
						err,
					)

					continue
				}

				if err := mqttClient.PublishHeartbeat(
					status,
				); err != nil {
					log.Printf(
						"heartbeat error: %v",
						err,
					)
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()

	log.Println(
		"stopping Edge Agent",
	)

	shutdownContext, cancel :=
		context.WithTimeout(
			context.Background(),
			5*time.Second,
		)

	defer cancel()

	_ = server.Shutdown(
		shutdownContext,
	)

	if mqttClient != nil {
		mqttClient.Disconnect()
	}
}
