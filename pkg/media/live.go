package media

// LiveEvent is covered by the existing owner IBS operation signature.
// Times/durations are integer milliseconds to keep canonical signatures stable.
type LiveEvent struct {
	Title      string `json:"title,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	ProgramMS  int64  `json:"program_unix_ms,omitempty"`
	InitBytes  uint64 `json:"init_bytes,omitempty"`
	InitSHA256 string `json:"init_sha256,omitempty"`
	PublishMS  int64  `json:"publish_ms,omitempty"`
	Reason     string `json:"reason,omitempty"`
}
type LiveChunk struct {
	Sequence uint64 `json:"sequence"`
	VideoID  string `json:"video_id"`
	LiveEvent
	CommittedMS int64 `json:"committed_unix_ms"`
}
type LiveState struct {
	ID       string      `json:"id"`
	Owner    string      `json:"owner_id"`
	Title    string      `json:"title"`
	Public   bool        `json:"public"`
	Ended    bool        `json:"ended"`
	Reason   string      `json:"end_reason,omitempty"`
	Revision uint64      `json:"revision"`
	Commit   string      `json:"commit"`
	Chunks   []LiveChunk `json:"chunks"`
}
