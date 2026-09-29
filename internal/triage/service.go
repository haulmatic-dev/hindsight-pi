package triage

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func runPi(ctx context.Context, cfg JudgeConfig, prompt string) (string, error) {
	var args []string
	args = append(args, "--no-extensions")
	if cfg.Provider != "" {
		args = append(args, "--provider", cfg.Provider)
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if cfg.Thinking != "" {
		args = append(args, "--thinking-level", cfg.Thinking)
	}
	piPath := cfg.PiPath
	if piPath == "" {
		piPath = "pi"
	}
	if env := os.Getenv("HYDRA_PI_PATH"); env != "" {
		piPath = env
	}
	c := exec.CommandContext(ctx, piPath, args...)
	c.Stdin = strings.NewReader(prompt)
	c.Env = os.Environ()
	if cfg.APIKeyName != "" {
		if key := os.Getenv(cfg.APIKeyName); key != "" {
			c.Env = append(c.Env, key+"="+os.Getenv(cfg.APIKeyName))
		}
	}
	var stdout, stderr strings.Builder
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		tail := stderr.String()
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		return "", err
	}
	return stdout.String(), nil
}

// commentWindow bounds how often one PR may receive a triage comment.
const commentWindow = 24 * time.Hour

// Service is the periodic PR-triage pass. Pan-adjacent: same fail-open
// discipline, but it only ever posts comments/labels — never merges, never
// closes.
type Service struct {
	interval time.Duration
	judge    JudgeConfig
	projects func() []Project
	run      GhRunner
	pi       PiRunner
	stateMu  chan struct{} // serializes state file access
	statePath string
	stopChan  chan struct{}
	isRunning bool
	// DryRun collects, groups, judges and logs the recommendation but posts
	// nothing — the operator's preview door before real comments go out.
	DryRun bool
}

// Project is one repo to scan.
type Project struct {
	Name string
	Path string
}

// commentRateKey scopes rate limiting per repo+PR: "path#12".
func commentRateKey(projectPath string, number int) string {
	return filepath.Clean(projectPath) + "#" + strconv.Itoa(number)
}

// New creates the service. Enabled is decided by the caller (config gate).
func New(interval time.Duration, judge JudgeConfig, projects func() []Project, statePath string) *Service {
	return &Service{
		interval:  interval,
		judge:     judge,
		projects:  projects,
		run:       DefaultGhRunner,
		pi:        DefaultPiRunner,
		stateMu:   make(chan struct{}, 1),
		statePath: statePath,
		stopChan:  make(chan struct{}),
	}
}

// SetRunner overrides the gh runner (tests).
func (s *Service) SetRunner(r GhRunner) { s.run = r }

// SetPiRunner overrides the judge runner (tests).
func (s *Service) SetPiRunner(r PiRunner) { s.pi = r }

// Start begins the periodic pass in a background goroutine.
func (s *Service) Start() {
	if s.isRunning {
		return
	}
	s.isRunning = true
	go func() {
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.RunOnce()
			case <-s.stopChan:
				return
			}
		}
	}()
	log.Printf("[Triage] PR-triage pass started (interval %v)", s.interval)
}

// Stop halts the pass.
func (s *Service) Stop() {
	if !s.isRunning {
		return
	}
	close(s.stopChan)
	s.isRunning = false
	log.Printf("[Triage] PR-triage pass stopped")
}

// RunOnce performs a single scan over all projects. Fail-open throughout:
// any per-project or per-group error is logged and the scan continues.
func (s *Service) RunOnce() {
	if s.projects == nil {
		return
	}
	for _, p := range s.projects() {
		s.triageProject(p)
	}
}

func (s *Service) triageProject(p Project) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	prs, err := CollectOpenDogfoodPRs(ctx, s.run, p.Path)
	if err != nil {
		log.Printf("[Triage] %s: collect PRs failed (skip): %v", p.Name, err)
		return
	}
	groups := GroupByOverlap(prs)
	if len(groups) == 0 {
		log.Printf("[Triage] %s: %d open dogfood PR(s), no overlapping groups", p.Name, len(prs))
		return
	}
	log.Printf("[Triage] %s: %d overlapping group(s) among %d PR(s)", p.Name, len(groups), len(prs))
	for _, g := range groups {
		s.triageGroup(ctx, p, g)
	}
}

func (s *Service) triageGroup(ctx context.Context, p Project, g Group) {
	// Rate limit before burning a judge call: every PR in the group must be
	// outside the 24h window, else the whole group is skipped this tick.
	for _, pr := range g.PRs {
		if s.commentedRecently(p.Path, pr.Number) {
			log.Printf("[Triage] %s #%d: triage comment posted within %v, skipping group", p.Name, pr.Number, commentWindow)
			return
		}
	}
	out, err := s.pi(ctx, s.judge, JudgePrompt(g))
	if err != nil {
		log.Printf("[Triage] %s: judge failed for group starting #%d (skip): %v", p.Name, g.PRs[0].Number, err)
		return
	}
	rec, err := ParseRecommendation(out)
	if err != nil {
		log.Printf("[Triage] %s: judge output unparseable for group starting #%d (skip): %v", p.Name, g.PRs[0].Number, err)
		return
	}
	body := CommentBody(g, rec)
	if s.DryRun {
		log.Printf("[Triage] DRY-RUN %s group starting #%d — would comment:\n%s", p.Name, g.PRs[0].Number, body)
		return
	}
	for _, pr := range g.PRs {
		if err := PostComment(ctx, s.run, p.Path, pr.Number, body); err != nil {
			log.Printf("[Triage] %s #%d: comment failed: %v", p.Name, pr.Number, err)
			continue
		}
		s.markCommented(p.Path, pr.Number)
		log.Printf("[Triage] %s #%d: triage recommendation posted (order=%v dups=%v human=%v)",
			p.Name, pr.Number, rec.MergeOrder, dupNums(rec), humanNums(rec))
	}
	// Labels: mechanical advisory tags. Fail-open — repos without the label
	// pre-created will error here and that is fine.
	for _, n := range rec.MergeOrder {
		if len(rec.MergeOrder) > 0 && n == rec.MergeOrder[0] {
			if err := AddLabel(ctx, s.run, p.Path, n, LabelMergeFirst); err != nil {
				log.Printf("[Triage] %s #%d: label %s failed (continuing): %v", p.Name, n, LabelMergeFirst, err)
			}
		}
	}
	for _, d := range rec.Duplicates {
		if err := AddLabel(ctx, s.run, p.Path, d.PR, LabelDuplicate); err != nil {
			log.Printf("[Triage] %s #%d: label %s failed (continuing): %v", p.Name, d.PR, LabelDuplicate, err)
		}
	}
}

func dupNums(r Recommendation) []int {
	ns := make([]int, 0, len(r.Duplicates))
	for _, d := range r.Duplicates {
		ns = append(ns, d.PR)
	}
	return ns
}

func humanNums(r Recommendation) []int {
	ns := make([]int, 0, len(r.NeedsHuman))
	for _, h := range r.NeedsHuman {
		ns = append(ns, h.PR)
	}
	return ns
}

// rateState is the persisted rate-limit map.
type rateState map[string]int64 // key → unix seconds of last comment

func (s *Service) loadState() rateState {
	st := make(rateState)
	b, err := os.ReadFile(s.statePath)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	return st
}

func (s *Service) saveState(st rateState) {
	if s.statePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.statePath, b, 0o644)
}

func (s *Service) commentedRecently(projectPath string, number int) bool {
	s.stateMu <- struct{}{}
	defer func() { <-s.stateMu }()
	st := s.loadState()
	at, ok := st[commentRateKey(projectPath, number)]
	return ok && time.Since(time.Unix(at, 0)) < commentWindow
}

func (s *Service) markCommented(projectPath string, number int) {
	s.stateMu <- struct{}{}
	defer func() { <-s.stateMu }()
	st := s.loadState()
	st[commentRateKey(projectPath, number)] = time.Now().Unix()
	s.saveState(st)
}
