// Package media defines transport-neutral, versioned block reconstruction contracts.
// It never chooses storage destinations or changes catalogue state.
package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Block struct {
	Index          uint64 `json:"index"`
	ExtentID       uint64 `json:"extent_id"`
	Size           uint64 `json:"size_bytes"`
	SHA256         string `json:"sha256"`
	NodeID         string `json:"node_id,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	ReadCapability string `json:"read_capability,omitempty"`
}
type Asset struct {
	Name      string  `json:"name"`
	Size      uint64  `json:"size_bytes"`
	SHA256    string  `json:"sha256"`
	BlockSize uint64  `json:"block_size"`
	Blocks    []Block `json:"blocks"`
}
type Job struct {
	Version     int             `json:"version"`
	VideoID     string          `json:"video_id"`
	OwnerID     string          `json:"owner_id"`
	Asset       Asset           `json:"asset"`
	Security    json.RawMessage `json:"security,omitempty"`
	Signature   json.RawMessage `json:"signature,omitempty"`
	TrustAnchor json.RawMessage `json:"trust_anchor,omitempty"`
}
type readGrant struct {
	Node    string `json:"node"`
	Video   string `json:"video"`
	Extent  uint64 `json:"extent"`
	Hash    string `json:"sha256"`
	Expires int64  `json:"expires"`
}

func MintRead(token, node, video string, extent uint64, hash string, expires time.Time) (string, error) {
	if len(token) < 24 || len(hash) != 64 {
		return "", errors.New("invalid read grant")
	}
	raw, e := json.Marshal(readGrant{node, video, extent, hash, expires.Unix()})
	if e != nil {
		return "", e
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(token))
	fmt.Fprint(mac, "caribou-read-v1\x00", body)
	return body + "." + hex.EncodeToString(mac.Sum(nil)), nil
}
func VerifyRead(grant, token, node, video string, extent uint64, hash string, now time.Time) bool {
	parts := strings.Split(grant, ".")
	if len(parts) != 2 || len(grant) > 2048 {
		return false
	}
	sig, e := hex.DecodeString(parts[1])
	if e != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(token))
	fmt.Fprint(mac, "caribou-read-v1\x00", parts[0])
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	raw, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return false
	}
	var g readGrant
	if json.Unmarshal(raw, &g) != nil {
		return false
	}
	return g.Node == node && g.Video == video && g.Extent == extent && g.Hash == hash && g.Expires > now.Unix() && g.Expires <= now.Add(2*time.Minute).Unix()
}
