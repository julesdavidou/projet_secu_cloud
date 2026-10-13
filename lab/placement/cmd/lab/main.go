package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) < 2 {
		log.Fatal("usage: placement-lab central|zone|mock|scenario|snapshot|upload manifest.json")
	}
	var err error
	switch os.Args[1] {
	case "central":
		err = service.Central(ctx)
	case "zone":
		err = service.Zone(ctx)
	case "mock":
		err = service.Edge(ctx)
	case "scenario":
		err = service.Scenario(ctx)
	case "snapshot":
		err = service.Snapshot(ctx)
	case "capacities":
		err = service.Capacities(ctx)
	case "reconcile":
		err = service.Reconcile(ctx)
	case "upload":
		if len(os.Args) != 3 {
			log.Fatal("upload requires a JSON manifest path")
		}
		err = service.UploadFile(ctx, os.Args[2])
	default:
		log.Fatal("unknown mode")
	}
	if err != nil {
		log.Fatal(err)
	}
}
