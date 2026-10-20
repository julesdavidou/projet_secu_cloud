package media

import "encoding/json"

// Operations change the logical media head, never the placement algorithm.
// Content versions remain immutable and their bytes are retained explicitly.
type Operation struct {
	OwnerID          string          `json:"owner_id"`
	VideoID          string          `json:"video_id"`
	Nonce            string          `json:"nonce"`
	Kind             string          `json:"kind"`
	ExpectedRevision uint64          `json:"expected_revision"`
	Previous         string          `json:"previous"`
	InitialPublic    bool            `json:"initial_public"`
	NewVideoID       string          `json:"new_video_id,omitempty"`
	GrantID          string          `json:"grant_id,omitempty"`
	Certificate      json.RawMessage `json:"certificate,omitempty"`
}
type OperationProof struct {
	Suite      string          `json:"suite"`
	Operation  Operation       `json:"operation"`
	Signature  json.RawMessage `json:"signature"`
	Zones      json.RawMessage `json:"zones"`
	Delegation json.RawMessage `json:"delegation,omitempty"`
}
type Grant struct {
	ID              string   `json:"grant_id"`
	OwnerID         string   `json:"owner_id"`
	VideoID         string   `json:"video_id"`
	DelegateID      string   `json:"delegate_id"`
	Operations      []string `json:"operations"`
	NotBefore       int64    `json:"not_before"`
	Expires         int64    `json:"expires"`
	MaxRevision     uint64   `json:"max_revision"`
	MaxBytes        uint64   `json:"max_bytes"`
	MaxBudget       uint64   `json:"max_budget_microcredits"`
	CertificateHash string   `json:"certificate_sha256"`
	Revoked         bool     `json:"revoked"`
}
type OperationVerification struct {
	Verified        bool   `json:"verified"`
	OwnerID         string `json:"owner_id"`
	DelegateGrantID string `json:"delegate_grant_id,omitempty"`
	CertificateHash string `json:"certificate_sha256,omitempty"`
	NewGrant        *Grant `json:"new_grant,omitempty"`
}
type OperationState struct {
	RootID    string `json:"root_id"`
	OwnerID   string `json:"owner_id"`
	CurrentID string `json:"current_video_id"`
	Revision  uint64 `json:"revision"`
	Commit    string `json:"commit"`
	Public    bool   `json:"public"`
	Deleted   bool   `json:"deleted"`
}
