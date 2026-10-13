package core

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const sessionResumedLabel = "session-resumed"

// resumeAttemptBoundaryText is injected into the worker prompt when a Pi
// session is resumed. It makes the model treat prior transcript claims as
// untrusted and forces re-verification of worktree state.
const resumeAttemptBoundaryText = `ATTEMPT BOUNDARY: you are a NEW attempt for this bead. A previous attempt's Pi session has been resumed for continuity, but its claims about files, tests, git state, and verification are NOT automatically true. Verify the current worktree state (git status, HEAD, changed files, test results) before trusting prior claims. Do not repeat the failed approach; diagnose the root cause first, then proceed.`

// SelfHealingConfig groups opt-in self-healing behaviours.
type SelfHealingConfig struct {
	SessionResume SessionResumeConfig `yaml:"session_resume"`
}

// SessionResumeConfig gates session-file resume on orphan respawn.
//
// The pilot defaults to disabled (Enabled=false) and dry-run=true so operators
// can measure first. When Enabled=true and DryRun=false, resumed beads that
// produce fix beads or verified-close rejections above the configured rate
// auto-disable resume.
type SessionResumeConfig struct {
	Enabled             bool          `yaml:"enabled"`
	DryRun              bool          `yaml:"dry_run"`
	InfraOnly           bool          `yaml:"infra_only"`
	HeadCheck           bool          `yaml:"head_check"`
	MaxSessionAge       time.Duration `yaml:"max_session_age"`
	MaxSessionSize      int64         `yaml:"max_session_size"`
	FixRateThreshold    float64       `yaml:"fix_rate_threshold"`
	RejectRateThreshold float64       `yaml:"reject_rate_threshold"`
	MinSamples          int           `yaml:"min_samples"`
}

// DefaultSessionResumeConfig returns safe defaults for the pilot.
func DefaultSessionResumeConfig() SessionResumeConfig {
	return SessionResumeConfig{
		DryRun:              true,
		InfraOnly:           true,
		HeadCheck:           true,
		MaxSessionAge:       time.Hour,
		MaxSessionSize:      50 * 1024 * 1024,
		FixRateThreshold:    0.5,
		RejectRateThreshold: 0.5,
		MinSamples:          3,
	}
}

// applyDefaults fills zero-valued thresholds with safe defaults. It is called
// after YAML parsing so an explicitly zeroed field gets a usable value.
func (c *SessionResumeConfig) applyDefaults() {
	if c.MaxSessionAge == 0 {
		c.MaxSessionAge = time.Hour
	}
	if c.MaxSessionSize == 0 {
		c.MaxSessionSize = 50 * 1024 * 1024
	}
	if c.FixRateThreshold == 0 {
		c.FixRateThreshold = 0.5
	}
	if c.RejectRateThreshold == 0 {
		c.RejectRateThreshold = 0.5
	}
	if c.MinSamples == 0 {
		c.MinSamples = 3
	}
}

// SessionResumePilot decides whether to resume a Pi session and tracks
// quality metrics for the pilot.
type SessionResumePilot struct {
	cfg     SessionResumeConfig
	enabled int32
	mu      sync.Mutex
	// resumed outcome counters
	resumedSuccess, resumedFix, resumedReject int
	// fresh outcome counters (for comparison logging)
	freshSuccess, freshFix, freshReject int
}

// NewSessionResumePilot creates a pilot from config.
func NewSessionResumePilot(cfg SessionResumeConfig) *SessionResumePilot {
	p := &SessionResumePilot{cfg: cfg}
	if cfg.Enabled {
		atomic.StoreInt32(&p.enabled, 1)
	}
	return p
}

// Enabled reports whether resume is currently allowed.
func (p *SessionResumePilot) Enabled() bool {
	return atomic.LoadInt32(&p.enabled) == 1
}

// SetEnabled turns resume on or off. Used by tests and auto-disable.
func (p *SessionResumePilot) SetEnabled(v bool) {
	var n int32
	if v {
		n = 1
	}
	atomic.StoreInt32(&p.enabled, n)
}

// ShouldResume applies the gated resume rules.
func (p *SessionResumePilot) ShouldResume(
	beadID string,
	retryCount int,
	lastInfra bool,
	sessionDir string,
	sessionAge time.Duration,
	sessionSize int64,
	currentHead, storedHead string,
) (bool, string) {
	if !p.Enabled() {
		return false, "session resume disabled"
	}
	if retryCount <= 0 {
		return false, "not a retry attempt"
	}
	if p.cfg.InfraOnly && !lastInfra {
		return false, "last failure was not infrastructure"
	}
	if sessionDir == "" {
		return false, "no session directory"
	}
	if st, err := os.Stat(sessionDir); err != nil || !st.IsDir() {
		return false, "session directory missing"
	}
	if p.cfg.MaxSessionAge > 0 && sessionAge > p.cfg.MaxSessionAge {
		return false, "session too old"
	}
	if p.cfg.MaxSessionSize > 0 && sessionSize > p.cfg.MaxSessionSize {
		return false, "session too large"
	}
	if p.cfg.HeadCheck {
		if storedHead == "" || currentHead == "" {
			return false, "HEAD check unavailable"
		}
		if currentHead != storedHead {
			return false, "worktree HEAD changed since prior attempt"
		}
	}
	return true, ""
}

// RecordSuccess records a completed close.
func (p *SessionResumePilot) RecordSuccess(resumed bool) {
	p.mu.Lock()
	if resumed {
		p.resumedSuccess++
	} else {
		p.freshSuccess++
	}
	p.mu.Unlock()
	p.maybeDisable()
}

// RecordFix records the creation of a fix bead after a failed close.
func (p *SessionResumePilot) RecordFix(resumed bool) {
	p.mu.Lock()
	if resumed {
		p.resumedFix++
	} else {
		p.freshFix++
	}
	p.mu.Unlock()
	p.maybeDisable()
}

// RecordReject records a verified-close rejection.
func (p *SessionResumePilot) RecordReject(resumed bool) {
	p.mu.Lock()
	if resumed {
		p.resumedReject++
	} else {
		p.freshReject++
	}
	p.mu.Unlock()
	p.maybeDisable()
}

// Stats returns current counters for logging/inspection.
func (p *SessionResumePilot) Stats() (resumedSuccess, resumedFix, resumedReject, freshSuccess, freshFix, freshReject int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumedSuccess, p.resumedFix, p.resumedReject, p.freshSuccess, p.freshFix, p.freshReject
}

func (p *SessionResumePilot) maybeDisable() {
	if p.cfg.DryRun {
		return
	}
	p.mu.Lock()
	total := p.resumedSuccess + p.resumedFix + p.resumedReject
	fixRate, rejectRate := 0.0, 0.0
	if total > 0 {
		fixRate = float64(p.resumedFix) / float64(total)
		rejectRate = float64(p.resumedReject) / float64(total)
	}
	p.mu.Unlock()
	if total < p.cfg.MinSamples || p.cfg.MinSamples <= 0 {
		return
	}
	if fixRate > p.cfg.FixRateThreshold || rejectRate > p.cfg.RejectRateThreshold {
		log.Printf("[SessionResumePilot] auto-disabling session resume: fixRate=%.2f rejectRate=%.2f over %d resumed beads",
			fixRate, rejectRate, total)
		p.SetEnabled(false)
	}
}

// PiSessionID returns a filesystem-safe session id derived from a bead id.
func PiSessionID(beadID string) string {
	sid := sanitizePiSessionID(beadID)
	if sid == "" {
		return "bead"
	}
	return sid
}

// PiSessionDir returns the deterministic Pi session directory for a bead.
func PiSessionDir(beadID, hydraHome string) string {
	if hydraHome == "" {
		return ""
	}
	return filepath.Join(hydraHome, "sessions", PiSessionID(beadID))
}

func sanitizePiSessionID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// SessionFileStats returns the age (since newest file) and total size of the
// session directory. Missing directories return exists=false.
func SessionFileStats(dir string) (age time.Duration, size int64, exists bool, err error) {
	st, err := os.Stat(dir)
	if err != nil {
		return 0, 0, false, err
	}
	if !st.IsDir() {
		return time.Since(st.ModTime()), st.Size(), true, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, true, err
	}
	var newest time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		size += info.Size()
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		newest = st.ModTime()
	}
	return time.Since(newest), size, true, nil
}
