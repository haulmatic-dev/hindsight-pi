package core

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ResourceMonitor tracks system resource usage using standard Unix commands
type ResourceMonitor struct {
	maxCPUUsage    float64 // Maximum allowed CPU usage (0-100)
	maxMemoryUsage float64 // Maximum allowed memory usage (0-100)
	checkInterval  time.Duration
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(maxCPU, maxMemory float64) *ResourceMonitor {
	return &ResourceMonitor{
		maxCPUUsage:    maxCPU,
		maxMemoryUsage: maxMemory,
		checkInterval:  5 * time.Second,
	}
}

// getCPUUsage returns current CPU usage percentage using standard Unix commands
func (rm *ResourceMonitor) getCPUUsage() (float64, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		// macOS: use top command
		cmd = exec.Command("sh", "-c", "top -l 1 -n 0 | grep 'CPU usage' | awk '{print $3}' | sed 's/%//'")
	} else {
		// Linux: use top command
		cmd = exec.Command("sh", "-c", "top -bn1 | grep 'Cpu(s)' | awk '{print $2}' | cut -d'%' -f1")
	}

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get CPU usage: %w", err)
	}

	cpuStr := strings.TrimSpace(string(output))
	if cpuStr == "" {
		return 0, fmt.Errorf("empty CPU usage output")
	}

	cpu, err := strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse CPU usage '%s': %w", cpuStr, err)
	}

	return cpu, nil
}

// getMemoryUsage returns current memory usage percentage using standard Unix commands
func (rm *ResourceMonitor) getMemoryUsage() (float64, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		// macOS: use vm_stat and sysctl
		cmd = exec.Command("sh", "-c", `
			page_size=$(vm_stat | grep 'page size' | awk '{print $8}')
			if [ -z "$page_size" ]; then page_size=4096; fi
			
			free_pages=$(vm_stat | grep 'Pages free' | awk '{print $3}' | sed 's/\.//')
			inactive_pages=$(vm_stat | grep 'Pages inactive' | awk '{print $3}' | sed 's/\.//')
			speculative_pages=$(vm_stat | grep 'Pages speculative' | awk '{print $3}' | sed 's/\.//' || echo 0)
			
			used=$(vm_stat | grep 'Pages wired down\|Pages active\|Pages occupied' | awk '{sum+=$3} END {print sum}' | sed 's/\.//')
			total=$(sysctl -n hw.memsize)
			total_pages=$((total / page_size))
			
			if [ -n "$used" ] && [ "$total_pages" -gt 0 ]; then
				echo "scale=2; ($used / $total_pages) * 100" | bc
			else
				echo "0"
			fi
		`)
	} else {
		// Linux: use free command
		cmd = exec.Command("sh", "-c", "free | grep Mem | awk '{printf \"%.2f\", ($3/$2) * 100.0}'")
	}

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get memory usage: %w", err)
	}

	memStr := strings.TrimSpace(string(output))
	if memStr == "" {
		return 0, fmt.Errorf("empty memory usage output")
	}

	mem, err := strconv.ParseFloat(memStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse memory usage '%s': %w", memStr, err)
	}

	return mem, nil
}

// CanSpawnWorker checks if system resources allow spawning a new worker
func (rm *ResourceMonitor) CanSpawnWorker() bool {
	// Check CPU usage
	cpuPercent, err := rm.getCPUUsage()
	if err != nil {
		log.Printf("[ResourceMonitor] Failed to get CPU usage: %v", err)
		return true // Allow spawn on error (fail open)
	}

	if cpuPercent > rm.maxCPUUsage {
		log.Printf("[ResourceMonitor] CPU usage %.1f%% exceeds limit %.1f%%, blocking worker spawn",
			cpuPercent, rm.maxCPUUsage)
		return false
	}

	// Check memory usage
	memPercent, err := rm.getMemoryUsage()
	if err != nil {
		log.Printf("[ResourceMonitor] Failed to get memory usage: %v", err)
		return true // Allow spawn on error (fail open)
	}

	if memPercent > rm.maxMemoryUsage {
		log.Printf("[ResourceMonitor] Memory usage %.1f%% exceeds limit %.1f%%, blocking worker spawn",
			memPercent, rm.maxMemoryUsage)
		return false
	}

	return true
}

// GetUsage returns current CPU and memory usage
func (rm *ResourceMonitor) GetUsage() (cpuPercent float64, memPercent float64, err error) {
	cpu, err := rm.getCPUUsage()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get CPU usage: %w", err)
	}

	mem, err := rm.getMemoryUsage()
	if err != nil {
		return cpu, 0, fmt.Errorf("failed to get memory usage: %w", err)
	}

	return cpu, mem, nil
}

// LogUsage logs current resource usage
func (rm *ResourceMonitor) LogUsage() {
	cpu, mem, err := rm.GetUsage()
	if err != nil {
		log.Printf("[ResourceMonitor] Error getting usage: %v", err)
		return
	}

	log.Printf("[ResourceMonitor] CPU: %.1f%%, Memory: %.1f%% (Limits: %.1f%%/%.1f%%)",
		cpu, mem, rm.maxCPUUsage, rm.maxMemoryUsage)
}

// ForceGC runs garbage collection to free memory
func (rm *ResourceMonitor) ForceGC() {
	log.Printf("[ResourceMonitor] Forcing garbage collection")
	runtime.GC()
}
