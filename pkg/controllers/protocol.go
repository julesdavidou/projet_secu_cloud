package controllers

import (
	"errors"
	"github.com/julesdavidou/projet_secu_cloud/lab/placement/catalog"
	"github.com/julesdavidou/projet_secu_cloud/pkg/placement"
	"time"
)

type Content struct {
	ExtentID uint64 `json:"extent_id"`
	SHA256   string `json:"sha256"`
}
type Upload struct {
	OwnerID        string             `json:"owner_id,omitempty"`
	ManifestSHA256 string             `json:"manifest_sha256,omitempty"`
	Manifest       placement.Manifest `json:"manifest"`
	Target         uint64             `json:"target_size_bytes"`
	Contents       []Content          `json:"contents"`
}

func (u Upload) Validate() error {
	if (u.OwnerID != "" || u.ManifestSHA256 != "") && (!placement.ValidID(u.OwnerID) || !ValidHash(u.ManifestSHA256)) {
		return errors.New("invalid signed owner or manifest digest")
	}
	ext, e := placement.Extents(u.Manifest, u.Target)
	if e != nil {
		return e
	}
	if len(ext) != len(u.Contents) {
		return errors.New("one expected SHA256 required per extent")
	}
	for i, c := range u.Contents {
		if c.ExtentID != ext[i].ID || !ValidHash(c.SHA256) {
			return errors.New("invalid extent content digest")
		}
	}
	return nil
}

type Registered struct {
	Node     placement.Node `json:"node"`
	Endpoint string         `json:"endpoint"`
}
type Unit struct {
	OwnerID string `json:"owner_id,omitempty"`
	placement.Assignment
	SHA256   string `json:"sha256"`
	Object   string `json:"object"`
	Endpoint string `json:"endpoint"`
	State    string `json:"state"`
	Proof    string `json:"proof"`
}
type UploadRecord struct {
	Upload  Upload                    `json:"upload"`
	Plan    placement.DistributedPlan `json:"plan"`
	Expires time.Time                 `json:"expires_at"`
	Units   []Unit                    `json:"units"`
	State   string                    `json:"state"`
}
type PlanCommit struct {
	Upload  Upload                    `json:"upload"`
	Plan    placement.DistributedPlan `json:"plan"`
	Expires time.Time                 `json:"expires_at"`
}
type Receipt struct {
	VideoID       string `json:"video_id"`
	ExtentID      uint64 `json:"extent_id"`
	NodeID        string `json:"node_id"`
	ReservationID string `json:"reservation_id"`
	Size          uint64 `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	Proof         string `json:"proof"`
}
type Prepare struct {
	VideoID       string `json:"video_id"`
	ReservationID string `json:"reservation_id"`
	NodeID        string `json:"node_id,omitempty"`
}
type Key struct {
	ID string `json:"reservation_id"`
}
type LookupKey struct {
	RequestID string `json:"request_id"`
}
type NodeKey struct {
	NodeID string `json:"node_id"`
}
type Move struct {
	NodeID     string `json:"node_id"`
	ZoneID     string `json:"zone_id"`
	Generation uint64 `json:"generation"`
}
type NodeControl struct {
	State placement.State `json:"state"`
	Pause bool            `json:"pause"`
}
type Inspection struct {
	Catalog catalog.Snapshot        `json:"catalog"`
	Nodes   map[string]Registered   `json:"nodes"`
	Uploads map[string]UploadRecord `json:"uploads"`
}
