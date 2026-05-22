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
	maxProcPercent float64 // Maximum allowed process count percentage (0-100)
	checkInterval  time.Duration
}

// NewResourceMonitor creates a new resource monitor
// maxProcPercent: maximum process usage percentage (0 = unlimited, default 90%)
func NewResourceMonitor(maxCPU, maxMemory float64, maxProcPercent ...float64) *ResourceMonitor {
	procPercent := 90.0 // Default: block when >90% of process limit reached
	if len(maxProcPercent) > 0 {
		procPercent = maxProcPercent[0]
	}
	return &ResourceMonitor{
		maxCPUUsage:    maxCPU,
		maxMemoryUsage: maxMemory,
		maxProcPercent: procPercent,
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

// getProcessCount returns the current number of processes running
func (rm *ResourceMonitor) getProcessCount() (int, error) {
	var cmd *exec.Cmd

	if runtime.GOOS == "darwin" {
		// macOS: count processes using ps
		cmd = exec.Command("sh", "-c", "ps ax | wc -l")
	} else {
		// Linux: count processes in /proc
		cmd = exec.Command("sh", "-c", "ls /proc | grep -E '^[0-9]+$' | wc -l")
	}

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get process count: %w", err)
	}

	countStr := strings.TrimSpace(string(output))
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse process count '%s': %w", countStr, err)
	}

	return count, nil
}

// getProcessLimit returns the maximum number of processes allowed (ulimit -u)
func (rm *ResourceMonitor) getProcessLimit() (int, error) {
	// ulimit is a shell builtin — must invoke via bash (sh/dash may not support -u)
	cmd := exec.Command("bash", "-c", "ulimit -u")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get process limit: %w", err)
	}

	limitStr := strings.TrimSpace(string(output))
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse process limit '%s': %w", limitStr, err)
	}

	return limit, nil
}

// CanSpawnWorker checks if system resources allow spawning a new worker
// Returns true if CPU, Memory, and Process count are below their respective limits
// Limit of 0 means unlimited (no check performed)
func (rm *ResourceMonitor) CanSpawnWorker() bool {
	// Check CPU usage (skip if limit is 0)
	if rm.maxCPUUsage > 0 {
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

		// Log when we're close to limits (within 10%)
		if cpuPercent > rm.maxCPUUsage*0.9 {
			log.Printf("[ResourceMonitor] Warning: CPU high - %.1f%% (Limit: %.1f%%)",
				cpuPercent, rm.maxCPUUsage)
		}
	}

	// Check memory usage (skip if limit is 0)
	if rm.maxMemoryUsage > 0 {
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
		if memPercent > rm.maxMemoryUsage*0.9 {
			log.Printf("[ResourceMonitor] Warning: Memory high - %.1f%% (Limit: %.1f%%)",
				memPercent, rm.maxMemoryUsage)
		}
	}

	// Check process count against limit (skip if limit is 0)
	if rm.maxProcPercent > 0 {
		procLimit, err := rm.getProcessLimit()
		if err != nil {
			log.Printf("[ResourceMonitor] Failed to get process limit: %v", err)
			// Continue anyway (fail open for this check)
		} else {
			procCount, err := rm.getProcessCount()
			if err != nil {
				log.Printf("[ResourceMonitor] Failed to get process count: %v", err)
				return true // Allow spawn on error (fail open)
			}

			procPercent := float64(procCount) / float64(procLimit) * 100
			if procPercent > rm.maxProcPercent {
				log.Printf("[ResourceMonitor] Process count %d/%d (%.1f%%) exceeds %.1f%%, blocking worker spawn",
					procCount, procLimit, procPercent, rm.maxProcPercent)
				return false
			}

			// Log when we're close to limits (within 10%)
			if procPercent > rm.maxProcPercent*0.9 {
				log.Printf("[ResourceMonitor] Warning: Process count high - %d/%d (%.1f%%) (Limit: %.1f%%)",
					procCount, procLimit, procPercent, rm.maxProcPercent)
			}
		}
	}

	return true
}

// GetUsage returns current CPU and memory usage, and optionally process count info
func (rm *ResourceMonitor) GetUsage() (cpuPercent float64, memPercent float64, procPercent float64, procCount int, procLimit int, err error) {
	cpu, err := rm.getCPUUsage()
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("failed to get CPU usage: %w", err)
	}

	mem, err := rm.getMemoryUsage()
	if err != nil {
		return cpu, 0, 0, 0, 0, fmt.Errorf("failed to get memory usage: %w", err)
	}

	procLimit, err = rm.getProcessLimit()
	if err != nil {
		// Non-fatal: just return without process info
		return cpu, mem, 0, 0, 0, nil
	}

	procCount, err = rm.getProcessCount()
	if err != nil {
		// Non-fatal: just return without process info
		return cpu, mem, 0, 0, procLimit, nil
	}

	procPercent = float64(procCount) / float64(procLimit) * 100

	return cpu, mem, procPercent, procCount, procLimit, nil
}

// LogUsage logs current resource usage
func (rm *ResourceMonitor) LogUsage() {
	cpu, mem, procPercent, procCount, procLimit, err := rm.GetUsage()
	if err != nil {
		log.Printf("[ResourceMonitor] Error getting usage: %v", err)
		return
	}

	log.Printf("[ResourceMonitor] CPU: %.1f%%, Memory: %.1f%%, Processes: %d/%d (%.1f%%)",
		cpu, mem, procCount, procLimit, procPercent)
}

// ForceGC runs garbage collection to free memory
func (rm *ResourceMonitor) ForceGC() {
	log.Printf("[ResourceMonitor] Forcing garbage collection")
	runtime.GC()
}
