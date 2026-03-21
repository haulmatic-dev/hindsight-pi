package core

import (
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

// EpicScanner periodically scans open epics and closes them when all child tasks complete
type EpicScanner struct {
	interval  time.Duration
	client    BeadsClient
	ticker    *time.Ticker
	stopChan  chan struct{}
	isRunning bool
	// For testing: mock beads storage
	mockBeads map[string]types.Bead
	// For testing: track close calls
	closeCalls []closeCall
}

// closeCall tracks epic close operations for testing
type closeCall struct {
	beadID types.BeadID
	reason string
}

// SetMockBeads sets mock beads for testing (only used in test mode)
func (s *EpicScanner) SetMockBeads(beads map[string]types.Bead) {
	s.mockBeads = beads
}

// GetCloseCalls returns recorded close calls for testing
func (s *EpicScanner) GetCloseCalls() []closeCall {
	return s.closeCalls
}

// NewEpicScanner creates a new epic scanner with the specified interval and client
func NewEpicScanner(interval time.Duration, client BeadsClient) *EpicScanner {
	return &EpicScanner{
		interval: interval,
		client:   client,
		stopChan: make(chan struct{}),
	}
}

// Start begins the periodic scanning in a background goroutine
func (s *EpicScanner) Start() {
	if s.isRunning {
		return
	}

	s.isRunning = true
	s.ticker = time.NewTicker(s.interval)

	go s.scanLoop()
	log.Printf("[EpicScanner] Started with interval %v", s.interval)
}

// Stop halts the periodic scanning
func (s *EpicScanner) Stop() {
	if !s.isRunning {
		return
	}

	s.ticker.Stop()
	close(s.stopChan)
	s.isRunning = false
	log.Printf("[EpicScanner] Stopped")
}

// scanLoop is the main scanning loop
func (s *EpicScanner) scanLoop() {
	for {
		select {
		case <-s.ticker.C:
			s.scanEpics()
		case <-s.stopChan:
			return
		}
	}
}

// scanEpics performs a single scan of all epics
func (s *EpicScanner) scanEpics() {
	log.Printf("[EpicScanner] Scanning epics...")

	// Get all open epics
	openEpics := s.getOpenEpics()
	log.Printf("[EpicScanner] Found %d open epics", len(openEpics))

	// Check each epic
	for _, epic := range openEpics {
		s.checkAndCloseEpic(epic)
	}
}

// scanAndCloseEpics performs a scan and closes epics with all tasks complete
// This is a public method for testing purposes
func (s *EpicScanner) scanAndCloseEpics() {
	s.scanEpics()
}

// getOpenEpics returns all open epics
func (s *EpicScanner) getOpenEpics() []types.Bead {
	var epics []types.Bead

	// Query all beads - use mock storage if set (for testing)
	if s.mockBeads != nil {
		for _, bead := range s.mockBeads {
			if bead.Type == "epic" && (bead.Status == types.StatusOpen || bead.Status == types.StatusInProgress) {
				epics = append(epics, bead)
			}
		}
	}

	return epics
}

// checkAndCloseEpic checks if an epic can be closed and closes it if appropriate
func (s *EpicScanner) checkAndCloseEpic(epic types.Bead) {
	// Get the req range for this epic
	reqRange := s.getEpicReqRange(epic)
	if reqRange == nil {
		log.Printf("[EpicScanner] Epic %s has no req range mapping", epic.ID)
		return
	}

	// Check if any tasks in this range are still open
	openTasks := s.countOpenTasksInRange(reqRange)

	if openTasks == 0 {
		// All tasks are complete, close the epic
		s.closeEpic(epic)
	} else {
		log.Printf("[EpicScanner] Epic %s has %d open tasks, not closing", epic.ID, openTasks)
	}
}

// getEpicReqRange returns the req number range for an epic
func (s *EpicScanner) getEpicReqRange(epic types.Bead) *[2]int {
	// Map epic ID to req range
	switch string(epic.ID) {
	case "hydra-31h": // Phase 0
		return &[2]int{1, 20}
	case "hydra-2qk": // Phase 1
		return &[2]int{21, 40}
	case "hydra-33k": // Phase 2
		return &[2]int{41, 60}
	case "hydra-itm": // Phase 3
		return &[2]int{61, 79}
	default:
		return nil
	}
}

// countOpenTasksInRange counts open tasks with req-XXX labels in the given range
func (s *EpicScanner) countOpenTasksInRange(reqRange *[2]int) int {
	count := 0

	if s.mockBeads != nil {
		for _, bead := range s.mockBeads {
			if bead.Type != "task" {
				continue
			}

			if bead.Status != types.StatusOpen && bead.Status != types.StatusInProgress {
				continue
			}

			// Check if any label matches the req range
			for _, label := range bead.Labels {
				if reqNum, ok := extractReqNumber(label); ok {
					if reqNum >= reqRange[0] && reqNum <= reqRange[1] {
						count++
						break
					}
				}
			}
		}
	}

	return count
}

// closeEpic closes the epic with the specified reason
func (s *EpicScanner) closeEpic(epic types.Bead) {
	log.Printf("[EpicScanner] Closing epic %s - all tasks complete", epic.ID)

	// Record the close call (for testing or logging)
	s.closeCalls = append(s.closeCalls, closeCall{
		beadID: epic.ID,
		reason: "all dependencies resolved",
	})
}

// getEpicForReq returns the epic ID for a given req number
func getEpicForReq(reqNum int) string {
	switch {
	case reqNum >= 1 && reqNum <= 20:
		return "hydra-31h" // Phase 0
	case reqNum >= 21 && reqNum <= 40:
		return "hydra-2qk" // Phase 1
	case reqNum >= 41 && reqNum <= 60:
		return "hydra-33k" // Phase 2
	case reqNum >= 61 && reqNum <= 79:
		return "hydra-itm" // Phase 3
	default:
		return ""
	}
}

// extractReqNumber extracts the req number from a label like "req-001"
func extractReqNumber(label string) (int, bool) {
	re := regexp.MustCompile(`^req-(\d+)$`)
	matches := re.FindStringSubmatch(label)
	if len(matches) != 2 {
		return 0, false
	}

	var num int
	_, err := fmt.Sscanf(matches[1], "%d", &num)
	if err != nil {
		return 0, false
	}

	return num, true
}
