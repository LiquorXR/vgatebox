package main

import (
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Capacity estimates are deliberately conservative product guidance, not a
// hard limit. The operator's -max remains authoritative; this only makes a
// mismatch visible before a large target exhausts fds or memory.
const (
	capacityFDReserve      = 256
	capacityFDPerSlot      = 64
	capacityMemoryPerSlot  = 32 << 20
	capacityReasonFD       = "文件描述符上限"
	capacityReasonMemory   = "可用内存"
	capacityReasonPorts    = "端口区间"
	capacityReasonHardware = "实例地址空间上限"
)

type capacityInput struct {
	fdLimit         uint64
	memoryAvailable uint64
	portFirst       int
	portLast        int
	hardwareMax     int
}

type capacityEstimate struct {
	Recommended int
	Reason      string
	ByFD        int
	ByMemory    int
	ByPorts     int
}

func estimateCapacityFrom(in capacityInput) capacityEstimate {
	rec := in.hardwareMax
	reason := capacityReasonHardware
	if rec < 1 {
		rec = 1
	}

	byFD := 0
	if in.fdLimit > capacityFDReserve {
		byFD = int((in.fdLimit - capacityFDReserve) / capacityFDPerSlot)
		if byFD < rec {
			rec, reason = byFD, capacityReasonFD
		}
	}

	byMemory := 0
	if in.memoryAvailable > 0 {
		byMemory = int(in.memoryAvailable / capacityMemoryPerSlot)
		if byMemory < rec {
			rec, reason = byMemory, capacityReasonMemory
		}
	}

	byPorts := 0
	if in.portLast >= in.portFirst && in.portFirst > 0 {
		byPorts = in.portLast - in.portFirst + 1
		if byPorts < rec {
			rec, reason = byPorts, capacityReasonPorts
		}
	}

	if rec < 1 {
		rec = 1
	}
	return capacityEstimate{Recommended: rec, Reason: reason, ByFD: byFD, ByMemory: byMemory, ByPorts: byPorts}
}

func estimateCapacity() capacityEstimate {
	in := capacityInput{
		hardwareMax: maximumSlots,
		portFirst:   randPortMin,
		portLast:    randPortMax,
	}
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err == nil && limit.Cur != ^uint64(0) {
		in.fdLimit = limit.Cur
	}
	in.memoryAvailable = readMemAvailable()
	if base, fixed, err := fixedSlotPort(1); fixed && err == nil {
		in.portFirst = base
		in.portLast = 65535
	}
	return estimateCapacityFrom(in)
}

func readMemAvailable() uint64 {
	blob, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(blob), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}

// logCapacityEstimate only reports the estimate; it never changes -max or
// rejects a target. A mismatch is an operator decision, visible in the log.
func logCapacityEstimate(configured int) {
	est := estimateCapacity()
	log.Printf("容量预检: 本机建议最多 %d 个出口（%s；fd=%d、内存=%d MiB、端口=%d）",
		est.Recommended, est.Reason, est.ByFD, est.ByMemory, est.ByPorts)
	if configured > est.Recommended {
		log.Printf("容量预检: 当前 -max=%d 高于建议值 %d；实际仍以 -max 为准", configured, est.Recommended)
	}
}
