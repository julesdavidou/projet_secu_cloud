package controllers

import (
	"testing"
	"time"
)

func TestDiscoveryIsNotCapacityAdmission(t *testing.T) {
	cfg := Config{Zones: []string{"A", "B"}, Nodes: []NodeSpec{{ID: "PI1", Zone: "A", Mode: "real-http", Endpoint: "https://edge-PI1:8443", AllowedZones: []string{"A", "B"}}}}
	now := time.Now()
	candidate := DiscoveryCandidate{NodeID: "PI1", IP: "192.168.1.5", HTTPPort: 8443, BrokerPort: 8883, WorkerPort: 9443}
	if e := validCandidate(cfg, candidate); e != nil {
		t.Fatal(e)
	}
	for _, ip := range []string{"127.0.0.1", "8.8.8.8", "224.0.0.251", "::1"} {
		q := candidate
		q.IP = ip
		if validCandidate(cfg, q) == nil {
			t.Fatal("invalid LAN destination accepted", ip)
		}
	}
	d := Discovery{DiscoveryCandidate: candidate, ZoneID: "B", Generation: 2, Observed: now, Expires: now.Add(30 * time.Second)}
	routes := discoveryRoutes(cfg, map[string]Discovery{"PI1": d}, now)
	if len(routes) != 3 || routes["edge-pi1:8443"].Address != "192.168.1.5:8443" || routes["reconstructor-pi1:8443"].Address != "192.168.1.5:9443" {
		t.Fatalf("wrong routes %+v", routes)
	}
	if len(discoveryRoutes(cfg, map[string]Discovery{"PI1": d}, now.Add(31*time.Second))) != 0 {
		t.Fatal("expired directory route accepted")
	}
	// Config and ownership remain unchanged: a route to a peer in B does not
	// insert a B node into the A allocator or move the provisioned device.
	n, _ := cfg.Node("PI1")
	if n.Zone != "A" {
		t.Fatal("network discovery changed placement ownership")
	}
}
