package main

import (
	"context"
	"github.com/julesdavidou/projet_secu_cloud/internal/edgecontrol"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, e := controllers.LoadConfig(controllers.Env("CONTROLLERS_CONFIG", "/config/config.json"))
	if e != nil {
		log.Fatal(e)
	}
	log.Printf("starting edge-bridge; configured zones=%d nodes=%d", len(cfg.Zones), len(cfg.Nodes))
	if e = edgecontrol.Run(ctx, cfg); e != nil {
		log.Fatal(e)
	}
}
