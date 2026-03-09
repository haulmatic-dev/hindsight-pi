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
// Calculates physical memory usage only (excluding swap and compressed pages)
func (rm *ResourceMonitor) getMemoryUsage() (float64, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		// macOS: Calculate physical memory usage only
		// Physical memory used = (wired + active) pages * page_size
		// Exclude: inactive, speculative, compressed (occupied by compressor), swap
		cmd = exec.Command("sh", "-c", `
			# Get page size (default to 16384 for Apple Silicon, 4096 for Intel)
			page_size=$(vm_stat | grep 'page size' | awk '{print $8}')
			if [ -z "$page_size" ]; then page_size=16384; fi
			
			# Get physical memory in bytes
			physical_mem=$(sysctl -n hw.memsize)
			
			# Get page counts
			wired_pages=$(vm_stat | grep 'Pages wired down' | awk '{print $4}' | sed 's/\.//')
			active_pages=$(vm_stat | grep 'Pages active' | awk '{print $3}' | sed 's/\.//')
			
			# Handle missing values
			if [ -z "$wired_pages" ]; then wired_pages=0; fi
			if [ -z "$active_pages" ]; then active_pages=0; fi
			
			# Calculate physical memory used (wired + active only)
			used_pages=$((wired_pages + active_pages))
			used_bytes=$((used_pages * page_size))
			
			# Calculate percentage
			if [ "$physical_mem" -gt 0 ]; then
				python3 -c "print(f'{(($used_bytes / $physical_mem) * 100):.2f}')" 2>/dev/null || \
				echo "scale=2; ($used_bytes / $physical_mem) * 100" | bc 2>/dev/null || \
				echo "0"
			else
				echo "0"
			fi
		`)
	} else {
		// Linux: Calculate physical memory usage (excluding buffers/cache)
		// Use MemAvailable from /proc/meminfo for accurate "used" calculation
		cmd = exec.Command("sh", "-c", `
			# Read memory info from /proc/meminfo
			mem_total=$(grep MemTotal /proc/meminfo | awk '{print $2}')
			mem_available=$(grep MemAvailable /proc/meminfo | awk '{print $2}')
			
			# If MemAvailable not present (older kernels), calculate manually
			if [ -z "$mem_available" ]; then
				mem_free=$(grep MemFree /proc/meminfo | awk '{print $2}')
				buffers=$(grep Buffers /proc/meminfo | awk '{print $2}')
				cached=$(grep "^Cached" /proc/meminfo | awk '{print $2}')
				mem_available=$((mem_free + buffers + cached))
			fi
			
			# Calculate used memory
			mem_used=$((mem_total - mem_available))
			
			# Calculate percentage
			if [ "$mem_total" -gt 0 ]; then
				printf "%.2f" $(echo "scale=4; ($mem_used / $mem_total) * 100" | bc)
			else
				echo "0"
			fi
		`)
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

	// Log when we're close to limits (within 10%)
	if cpuPercent > rm.maxCPUUsage*0.9 || memPercent > rm.maxMemoryUsage*0.9 {
		log.Printf("[ResourceMonitor] Warning: Resources high - CPU: %.1f%%, Memory: %.1f%% (Limits: %.1f%%/%.1f%%)",
			cpuPercent, memPercent, rm.maxCPUUsage, rm.maxMemoryUsage)
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
