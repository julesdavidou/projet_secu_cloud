package main

import (
	"encoding/json"
	"flag"
	"github.com/julesdavidou/projet_secu_cloud/pkg/controllers"
	"log"
	"os"
)

func main() {
	path := flag.String("config", "/input/topology.json", "public configuration")
	out := flag.String("out", "/output", "provisioning output")
	mode := flag.String("mode", "real-http", "real-http or mock")
	flag.Parse()
	raw, e := os.ReadFile(*path)
	if e != nil {
		log.Fatal(e)
	}
	var cfg controllers.Config
	if e = json.Unmarshal(raw, &cfg); e != nil {
		log.Fatal(e)
	}
	for i := range cfg.Nodes {
		cfg.Nodes[i].Mode = *mode
	}
	if e = controllers.Provision(cfg, *out); e != nil {
		log.Fatal(e)
	}
	log.Print("configuration and distinct identities provisioned; secret values omitted")
}
