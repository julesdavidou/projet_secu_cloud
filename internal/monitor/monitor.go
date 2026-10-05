package monitor

import (
	"runtime"
	"time"

	"github.com/julesdavidou/projet_secu_cloud/pkg/protocol"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

func Collect(
	edgeID string,
	zone string,
	storageRoot string,
) (protocol.NodeStatus, error) {

	cpuValues, err := cpu.Percent(
		0,
		false,
	)

	if err != nil {
		return protocol.NodeStatus{}, err
	}

	cpuPercent := 0.0

	if len(cpuValues) > 0 {
		cpuPercent = cpuValues[0]
	}

	memory, err := mem.VirtualMemory()

	if err != nil {
		return protocol.NodeStatus{}, err
	}

	storage, err := disk.Usage(storageRoot)

	if err != nil {
		return protocol.NodeStatus{}, err
	}

	return protocol.NodeStatus{
		EdgeID:       edgeID,
		Zone:         zone,
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
		CPUPercent:   cpuPercent,

		Memory: protocol.MemoryStatus{
			Total: memory.Total,
			Used:  memory.Used,
			Free:  memory.Available,
		},

		Storage: protocol.StorageStatus{
			Total: storage.Total,
			Used:  storage.Used,
			Free:  storage.Free,
		},

		Timestamp: time.Now().UTC(),
	}, nil
}
