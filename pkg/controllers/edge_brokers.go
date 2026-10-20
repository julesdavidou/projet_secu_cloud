package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/mqttbus"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProvisionEdgeBrokers routes each Edge identity through its own broker. Every
// direction is disjoint; a broker can forward only its Edge's authenticated topics.
func ProvisionEdgeBrokers(cfg Config, out, hub string) error {
	if _, _, e := net.SplitHostPort(hub); e != nil || strings.ContainsAny(hub, "\r\n ") {
		return fmt.Errorf("invalid MQTT hub address")
	}
	base, e := os.ReadFile(filepath.Join(out, "broker", "acl"))
	if e != nil {
		return e
	}
	uid, _ := strconv.Atoi(Env("HOST_UID", "1000"))
	write := func(path string, data []byte, mode os.FileMode) error {
		if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return e
		}
		if e := os.WriteFile(path, data, mode); e != nil {
			return e
		}
		if os.Geteuid() == 0 {
			return os.Chown(path, uid, 10001)
		}
		return nil
	}
	actors := []string{"central", "admin"}
	for _, z := range cfg.Zones {
		actors = append(actors, "zone-"+z)
	}
	for _, n := range cfg.Nodes {
		actors = append(actors, "edge-"+n.ID)
	}
	for _, n := range cfg.Nodes {
		actor := "edge-" + n.ID
		dir := filepath.Join(out, "broker-"+actor)
		up := "uplink-" + n.ID
		password, e := os.ReadFile(filepath.Join(out, "identities", actor, actor+".password"))
		if e != nil {
			return e
		}
		localPath := filepath.Join(dir, up+".password")
		local, e := os.ReadFile(localPath)
		if os.IsNotExist(e) {
			random := make([]byte, 32)
			if _, e = rand.Read(random); e != nil {
				return e
			}
			local = []byte(hex.EncodeToString(random))
			if e = write(localPath, local, 0640); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if e = write(filepath.Join(dir, actor+".password"), password, 0640); e != nil {
			return e
		}
		start := strings.Index(string(base), "\nuser "+actor+"\n")
		if start < 0 {
			return fmt.Errorf("missing Edge ACL")
		}
		entry := string(base)[start+1:]
		if end := strings.Index(entry, "\nuser "); end >= 0 {
			entry = entry[:end]
		}
		outgoing := []string{mqttbus.Root + "presence/" + actor, mqttbus.Root + "zone/+/node/" + n.ID + "/+", mqttbus.Root + "central/request/" + actor}
		for _, z := range cfg.Zones {
			outgoing = append(outgoing, mqttbus.Root+"zone-"+z+"/request/"+actor)
		}
		for _, target := range actors {
			if target != actor {
				outgoing = append(outgoing, mqttbus.Root+target+"/reply/"+actor)
			}
		}
		incoming := []string{mqttbus.Root + actor + "/request/+", mqttbus.Root + actor + "/reply/+"}
		var acl strings.Builder
		acl.WriteString(entry)
		fmt.Fprintf(&acl, "\nuser %s\n", up)
		for _, topic := range outgoing {
			fmt.Fprintf(&acl, "topic read %s\n", topic)
		}
		for _, topic := range incoming {
			fmt.Fprintf(&acl, "topic write %s\n", topic)
		}
		if e = write(filepath.Join(dir, "acl"), []byte(acl.String()), 0644); e != nil {
			return e
		}
		conf := fmt.Sprintf("user mosquitto\nlistener 1883 0.0.0.0\nallow_anonymous false\npassword_file /tmp/auth/passwd\nacl_file /tmp/auth/acl\npersistence true\npersistence_location /mosquitto/data/\nlog_dest stdout\nmax_packet_size 1048576\nconnection %s\naddress %s\nremote_clientid %s\nremote_username %s\nremote_password %s\nlocal_username %s\nlocal_password %s\nbridge_protocol_version mqttv50\nnotifications false\ncleansession true\nrestart_timeout 1\n", up, hub, up, actor, strings.TrimSpace(string(password)), up, strings.TrimSpace(string(local)))
		for _, topic := range outgoing {
			conf += "topic " + topic + " out 1\n"
		}
		for _, topic := range incoming {
			conf += "topic " + topic + " in 1\n"
		}
		if e = write(filepath.Join(dir, "bridge.conf"), []byte(conf), 0640); e != nil {
			return e
		}
	}
	return nil
}
