package system

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func FreeMemoryMB() uint64 {
	if runtime.GOOS == "darwin" {
		if free := darwinFreeMemoryMB(); free > 0 {
			return free
		}
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Sys / 1024 / 1024
}

func darwinFreeMemoryMB() uint64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	pageSize := darwinPageSize()
	if pageSize == 0 {
		pageSize = 16 * 1024
	}

	var freePages uint64
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Pages free:") && !strings.HasPrefix(line, "Pages speculative:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		value := strings.TrimSuffix(parts[2], ".")
		pages, err := strconv.ParseUint(value, 10, 64)
		if err == nil {
			freePages += pages
		}
	}
	return freePages * pageSize / 1024 / 1024
}

func darwinPageSize() uint64 {
	out, err := exec.Command("sysctl", "-n", "hw.pagesize").Output()
	if err != nil {
		return 0
	}
	value := strings.TrimSpace(string(out))
	pageSize, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return pageSize
}
