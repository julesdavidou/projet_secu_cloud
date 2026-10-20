package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
)

// Provision generates per-actor secrets once and reuses them on subsequent runs.
// Configuration and exact allow-list ACLs are refreshed from the supplied topology.
func Provision(cfg Config, out string) error {
	if e := cfg.Validate(); e != nil {
		return e
	}
	ids := []string{"central", "admin"}
	for _, z := range cfg.Zones {
		ids = append(ids, "zone-"+z)
	}
	for _, n := range cfg.Nodes {
		ids = append(ids, "edge-"+n.ID)
	}
	sort.Strings(ids)
	uid, _ := strconv.Atoi(Env("HOST_UID", "1000"))
	write := func(path string, raw []byte, mode os.FileMode) error {
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return e
		}
		if e := os.WriteFile(path, raw, mode); e != nil {
			return e
		}
		if os.Geteuid() == 0 {
			_ = os.Chown(path, uid, 10001)
		}
		return nil
	}
	secret := func(path string) (string, error) {
		if raw, e := os.ReadFile(path); e == nil {
			return strings.TrimSpace(string(raw)), nil
		}
		bytes := make([]byte, 32)
		if _, e := rand.Read(bytes); e != nil {
			return "", e
		}
		value := hex.EncodeToString(bytes)
		return value, write(path, []byte(value+"\n"), 0640)
	}
	for _, id := range ids {
		value, e := secret(filepath.Join(out, "identities", id, id+".password"))
		if e != nil {
			return e
		}
		if e = write(filepath.Join(out, "broker", id+".password"), []byte(value+"\n"), 0640); e != nil {
			return e
		}
	}
	operator, e := secret(filepath.Join(out, "operator", "operator.token"))
	if e != nil {
		return e
	}
	if e = write(filepath.Join(out, "identities", "central", "operator.token"), []byte(operator+"\n"), 0640); e != nil {
		return e
	}
	for _, n := range cfg.Nodes {
		token, e := secret(filepath.Join(out, "operator", "edge-"+n.ID+".http-token"))
		if e != nil {
			return e
		}
		if e = write(filepath.Join(out, "identities", "edge-"+n.ID, "edge-"+n.ID+".http-token"), []byte(token+"\n"), 0640); e != nil {
			return e
		}
	}
	var acl strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&acl, "\nuser %s\ntopic read %s%s/request/+\ntopic read %s%s/reply/+\ntopic write %spresence/%s\n", id, mqttbus.Root, id, mqttbus.Root, id, mqttbus.Root, id)
		for _, receiver := range ids {
			fmt.Fprintf(&acl, "topic write %s%s/reply/%s\n", mqttbus.Root, receiver, id)
		}
		targets := []string{"central"}
		if id == "central" || id == "admin" {
			targets = ids
		} else if strings.HasPrefix(id, "zone-") {
			for _, z := range cfg.Zones {
				targets = append(targets, "zone-"+z)
			}
			for _, n := range cfg.Nodes {
				if Has(n.AllowedZones, strings.TrimPrefix(id, "zone-")) {
					targets = append(targets, "edge-"+n.ID)
				}
			}
		} else if strings.HasPrefix(id, "edge-") {
			n, _ := cfg.Node(strings.TrimPrefix(id, "edge-"))
			for _, z := range n.AllowedZones {
				targets = append(targets, "zone-"+z)
			}
		}
		if strings.HasPrefix(id, "edge-") {
			for _, peer := range cfg.Nodes {
				if "edge-"+peer.ID != id {
					targets = append(targets, "edge-"+peer.ID)
				}
			}
		}
		for _, target := range targets {
			fmt.Fprintf(&acl, "topic write %s%s/request/%s\n", mqttbus.Root, target, id)
		}
		if strings.HasPrefix(id, "zone-") {
			zone := strings.TrimPrefix(id, "zone-")
			fmt.Fprintf(&acl, "topic write %szone/%s/summary\n", mqttbus.Root, zone)
			for _, peer := range cfg.Zones {
				fmt.Fprintf(&acl, "topic read %szone/%s/summary\n", mqttbus.Root, peer)
			}
			for _, n := range cfg.Nodes {
				if Has(n.AllowedZones, zone) {
					for _, kind := range []string{"state", "heartbeat"} {
						fmt.Fprintf(&acl, "topic read %szone/%s/node/%s/%s\n", mqttbus.Root, zone, n.ID, kind)
					}
				}
			}
		}
		if strings.HasPrefix(id, "edge-") {
			n, _ := cfg.Node(strings.TrimPrefix(id, "edge-"))
			for _, zone := range n.AllowedZones {
				for _, kind := range []string{"state", "heartbeat"} {
					fmt.Fprintf(&acl, "topic write %szone/%s/node/%s/%s\n", mqttbus.Root, zone, n.ID, kind)
				}
			}
		}
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if e = write(filepath.Join(out, "config.json"), raw, 0644); e != nil {
		return e
	}
	return write(filepath.Join(out, "broker", "acl"), []byte(acl.String()), 0644)
}
