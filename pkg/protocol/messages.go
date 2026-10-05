package protocol

import "time"

type StorageStatus struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

type MemoryStatus struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

type NodeStatus struct {
	EdgeID       string        `json:"edge_id"`
	Zone         string        `json:"zone"`
	Architecture string        `json:"architecture"`
	OS           string        `json:"os"`
	CPUPercent   float64       `json:"cpu_percent"`
	Memory       MemoryStatus  `json:"memory"`
	Storage      StorageStatus `json:"storage"`
	Timestamp    time.Time     `json:"timestamp"`
}
