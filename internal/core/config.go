package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the Hydra configuration
type Config struct {
	Overseer OverseerConfig  `yaml:"overseer"`
	Projects []ProjectConfig `yaml:"projects"`
	// AutoDiscoverExclude lists project directory names or paths that the
	// multi-user project scanner should skip. Applied before any repo
	// discovery or bv triage runs.
	AutoDiscoverExclude []string `yaml:"-"`
	// MultiUserDiscovery enables the multi-user project scan
	// (projects.multi_user_discovery, default true). Not decoded directly —
	// it lives inside the projects object and is resolved during parsing.
	MultiUserDiscovery bool                    `yaml:"-"`
	MCP                MCPConfig               `yaml:"mcp"`
	API                APIConfig               `yaml:"api"`
	Intelligence       IntelligenceConfig      `yaml:"intelligence"`
	Agents             AgentsConfig            `yaml:"agents"`
	Pi                 PiRootConfig            `yaml:"pi"`
	Output             OutputConfig            `yaml:"output"`
	Validation         ValidationConfig        `yaml:"validation"`
	ComplianceCycle    ComplianceCycleConfig   `yaml:"compliance_cycle"`
	Shepherd           ShepherdConfig          `yaml:"shepherd"`
	Worktree           WorktreeConfig          `yaml:"worktree"`
	Triage             TriageConfig            `yaml:"triage"`
	Status             StatusConfig            `yaml:"status"`
	IntegrationRetest  IntegrationRetestConfig `yaml:"integration_retest"`
	SelfHealing        SelfHealingConfig       `yaml:"self_healing"`
	// ContextPack configures the chain orientation pack (mechanical build +
	// agentic scout fallback). See docs/architecture/context-pack.md.
	ContextPack ContextPackConfig `yaml:"context_pack"`
	// Deploy configures the FR-031 tier-1 auto-merge gate and canary deploy
	// with automatic rollback. Disabled by default (NFR-004).
	Deploy DeployConfig `yaml:"deploy"`
}

// ContextPackConfig configures the chain context pack.
type ContextPackConfig struct {
	// Scout is the agentic fallback for requirements the mechanical pack
	// cannot map (confidence below threshold). Disabled by default.
	Scout ScoutConfig `yaml:"scout"`
}

// ShepherdConfig configures Pan, the shepherd that decides the ambiguous
// tail of worker states (PRD worker-park-and-resume-20260731 Part 6).
// Pan is fail-open: disabled or failing, deterministic rules still run.
type ShepherdConfig struct {
	Pan PanConfig `yaml:"pan"`
}

const DefaultPanFiledBeadsPerProject = 4

// PanConfig configures the Pan shepherd agent. Provider/Model/APIKeyName are
// deliberately separate from the worker fleet's: Pan must stay lucid during
// the congestion storms it manages.
type PanConfig struct {
	Enabled                 bool `yaml:"enabled"`
	IntervalSeconds         int  `yaml:"interval_seconds"`
	HungAfterSeconds        int  `yaml:"hung_after_seconds"`
	ParkOverdueGraceSeconds int  `yaml:"park_overdue_grace_seconds"`
	NudgeCap                int  `yaml:"nudge_cap"`
	// EscalatedWedgeGraceSeconds is how long an escalated bead that blocks
	// dependents may sit before Pan sees it as a wedge candidate. Leaf
	// escalations keep the 24h stale-escalation path; a chain-head escalation
	// stalls a campaign in minutes, so the wedge path must be much shorter.
	// Default: 3600 (1h).
	EscalatedWedgeGraceSeconds int `yaml:"escalated_wedge_grace_seconds"`
	// EscalatedLeafGraceSeconds is how long an escalated bead with NO
	// dependents may sit before Pan sees it as a stale-escalation candidate
	// (one fair retry via repair:clear-escalation; the escalation re-fires
	// on merit if the failure recurs). Default: 86400 (24h) — the historical
	// human window. Lower it when verified-close escalations of the
	// daemon-defect class (gate extraction errors, broken gate cwd) should
	// be auto-retried shortly after a fix deploys instead of waiting a day.
	EscalatedLeafGraceSeconds int    `yaml:"escalated_leaf_grace_seconds"`
	Provider                  string `yaml:"provider"`
	Model                     string `yaml:"model"`
	Thinking                  string `yaml:"thinking"`
	APIKeyName                string `yaml:"api_key_name"`
	// MaxFiledBeadsPerProject caps how many open pan-filed beads may exist
	// per project at once (FR-022). The default (0 → 4, see
	// DefaultPanFiledBeadsPerProject) leaves filing slots for distinct
	// findings during wedge storms; a cap hit emits a pan_filed_cap_hit
	// event and a Pan digest anomaly so suppression stays visible.
	MaxFiledBeadsPerProject int `yaml:"max_filed_beads_per_project"`
}

// AgentsConfig contains agent connector settings
type AgentsConfig struct {
	Enabled      bool     `yaml:"enabled"`
	DefaultAgent string   `yaml:"default_agent"`
	Pi           PiConfig `yaml:"pi"`
}

// PhaseModel is a per-phase provider/model override for Pi workers.
type PhaseModel struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// PiConfig contains Pi coding agent settings
type PiConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ThinkingLevel string `yaml:"thinking_level"`
	Timeout       string `yaml:"timeout"`
	ApiKeyName    string `yaml:"api_key_name"`

	// Mode controls whether the Pi connector uses RPC JSONL mode or the legacy
	// piped text mode. Recognised values: "rpc". Anything else, including the
	// empty default, selects text mode.
	Mode string `yaml:"mode"`

	// RPCAutoConfirm, when true, answers confirm UI asks with confirmed:true.
	// Default false keeps the deny-by-default policy for unattended headless runs.
	RPCAutoConfirm bool `yaml:"rpc_auto_confirm"`

	// FallbackProvider/FallbackModel are used when the primary provider's
	// circuit breaker opens after repeated rate-limit (429) failures.
	// Empty disables failover.
	FallbackProvider string `yaml:"fallback_provider"`
	FallbackModel    string `yaml:"fallback_model"`

	// PhaseModels maps a phase key (e.g. red, green, refactor) to an optional
	// provider/model override. Partial overrides are allowed: a non-empty
	// Provider or Model replaces the corresponding primary field, while empty
	// fields fall back to the primary.
	PhaseModels map[string]PhaseModel `yaml:"phase_models"`

	// Extensions is the allowlist of Pi extensions to load (rpc mode only —
	// text mode has no extension host). Names are package names or the short
	// aliases in cmd/worker's resolver, e.g. ["rtk", "caveman", "context-mode"].
	//
	// An allowlist rather than "load everything" because loading everything is
	// expensive: measured on one identical turn, all installed extensions cost
	// 48,315 tokens versus 12,059 for rtk alone — 4x, for extensions a headless
	// worker cannot use (themes, editor widgets, subagent UI). Empty disables
	// extensions entirely, which is the default and today's behaviour.
	Extensions []string `yaml:"extensions"`

	// PhaseExtensions overrides Extensions for a specific phase (red, green,
	// refactor, …), for cases where a phase needs something the others do not.
	// A phase with no entry uses Extensions.
	PhaseExtensions map[string][]string `yaml:"phase_extensions"`

	// TurnBudgetReminderCadence controls how often (in turns) the worker
	// injects a non-binding "you have used N of ~B turns" reminder into the
	// live prompt tail. Zero means "every turn" because the default is to
	// keep the budget visible.
	TurnBudgetReminderCadence int `yaml:"turn_budget_reminder_cadence"`

	// TurnBudgets maps TDD phase keys (red, green, refactor, …) to a soft
	// per-phase turn budget B. The budget is visible to the model as a
	// pacing hint, not a hard cap. Missing phases get fleet defaults set
	// above the observed p75 (red 150, green/refactor 300, others 200).
	TurnBudgets map[string]int `yaml:"turn_budgets"`
}

// defaultPiMode is the protocol workers use unless the config says otherwise.
//
// rpc rather than text because text mode initialises no extension host at all —
// verified: a marker extension loaded with -e in text mode is imported but
// nothing runs, and `pi --verbose` reports no extensions. Over rpc the same
// setup produces real work: "RTK rewrite: git status --short -> rtk git status
// --short". Set `mode: text` explicitly to opt back out.
const defaultPiMode = "rpc"

// defaultPiExtensions is the allowlist every phase gets unless overridden.
//
// rtk and context-mode, on each extension's own accounting rather than on a
// synthetic prompt.
//
// context-mode was briefly dropped from this list on the strength of a single
// trivial prompt, where it only added tokens. That was the wrong measurement: the
// prompt gave it nothing worth sandboxing. Its own stats across 155 worker
// sessions (24 of which used it) report 4,401,021 bytes sandboxed against 485,148
// returned, and 1,249,024 tokens saved. The saving is lumpy — median 3,385 per
// active session, one session 1,095,981 — so it earns its place by occasionally
// avoiding an enormous context load, not by shaving a steady percentage.
//
// rtk compacts tool OUTPUT and its effect is directly measurable: 77-81%
// reduction on high-output commands (an `rg` over internal/core went 111,165
// bytes to 21,073), and ~0% on already-terse ones. Bounded cost, saving that
// scales with how noisy the command is.
//
// caveman is deliberately NOT here. At its most aggressive setting (ultra,
// verified live) it produced 1,103 output tokens against 927 without it on a
// prose-heavy prompt — no saving, plus ~8,500 input tokens of its own
// instructions. Its mechanism compresses assistant prose while exempting code
// blocks, and a bead worker's output is mostly tool calls and code, so there is
// little for it to compress. Enable it per project with
// `extensions: [rtk, context-mode, caveman]` if a workload is prose-heavy.
var defaultPiExtensions = []string{"rtk", "context-mode"}

// applyPiDefaults fills in Pi settings the YAML did not specify.
//
// This runs AFTER the raw config is copied over, not in DefaultConfig, because
// LoadConfig assigns `config.Agents = raw.Agents` wholesale — anything
// DefaultConfig put in Agents.Pi is discarded the moment a config file contains
// an `agents:` block, which every real one does. Defaults set only in
// DefaultConfig would therefore be silently inert, the same failure already
// found with min_score, a self-referential fallback_provider and mcp.enabled.
//
// The distinction between "unset" and "explicitly empty" is what makes both
// opt-outs work: `mode: text` is a non-empty value, and `extensions: []`
// unmarshals to an empty-but-non-nil slice.
func (c *Config) applyPiDefaults() {
	if strings.TrimSpace(c.Agents.Pi.Mode) == "" {
		c.Agents.Pi.Mode = defaultPiMode
	}
	if c.Agents.Pi.Extensions == nil {
		c.Agents.Pi.Extensions = append([]string(nil), defaultPiExtensions...)
	}
}

// ExtensionsForPhase returns the extension allowlist for a phase, falling back
// to the default Extensions list when the phase has no override. Phase lookup is
// case-insensitive, matching ModelForPhase.
func (p PiConfig) ExtensionsForPhase(phase string) []string {
	if len(p.PhaseExtensions) > 0 {
		key := strings.ToLower(strings.TrimSpace(phase))
		if key != "" {
			if ext, ok := p.PhaseExtensions[key]; ok {
				return ext
			}
		}
	}
	return p.Extensions
}

// RPCEnabled reports whether the Pi connector should run in RPC JSONL mode.
// Only the exact, case-insensitive value "rpc" enables it; the empty default
// and any other value keep the existing text mode, preserving today's
// behaviour when agents.pi.mode is unset.
func (p PiConfig) RPCEnabled() bool {
	return strings.ToLower(strings.TrimSpace(p.Mode)) == "rpc"
}

// AutoConfirm returns the rpc_auto_confirm setting. When true, confirm asks
// are approved; when false (the default), they are denied.
func (p PiConfig) AutoConfirm() bool {
	return p.RPCAutoConfirm
}

// ReminderCadence returns the turn-budget reminder cadence in turns.
// Values <= 0 are treated as 1 (remind every turn) so the budget stays
// visible by default.
func (p PiConfig) ReminderCadence() int {
	if p.TurnBudgetReminderCadence > 0 {
		return p.TurnBudgetReminderCadence
	}
	return 1
}

// TurnBudgetForPhase returns the soft turn budget for a phase. Unset
// phases use defaults above the fleet p75 so the hint never binds early.
func (p PiConfig) TurnBudgetForPhase(phase string) int {
	key := strings.ToLower(strings.TrimSpace(phase))
	if key != "" {
		if v, ok := p.TurnBudgets[key]; ok && v > 0 {
			return v
		}
	}
	switch key {
	case "red":
		return 150
	case "green", "refactor":
		return 300
	default:
		return 200
	}
}

// ModelForPhase returns the provider and model for a phase, falling back to
// the primary provider/model when the phase has no override or the override
// field is empty. Phase lookup is case-insensitive.
func (p PiConfig) ModelForPhase(phase string) (string, string) {
	prov, model := p.Provider, p.Model
	key := strings.ToLower(strings.TrimSpace(phase))
	if override, ok := p.PhaseModels[key]; ok {
		if override.Provider != "" {
			prov = override.Provider
		}
		if override.Model != "" {
			model = override.Model
		}
	}
	return prov, model
}

// PiRootConfig contains Pi-specific configuration settings
type PiRootConfig struct {
	ConfigDir       string         `yaml:"config_dir"`
	PiConfigDir     string         `yaml:"pi_config_dir"`
	WorkerConfigDir string         `yaml:"worker_config_dir"`
	Skills          PiSkillsConfig `yaml:"skills"`
	StandardAgent   PiAgentConfig  `yaml:"standard-agent"`

	// ManageConfig grants the daemon permission to WRITE to ConfigDir —
	// creating the directory structure, installing Pi, installing LSP servers
	// and refreshing skills on a timer.
	//
	// Default false, because ConfigDir is shared: since 2026-07-31 the worker
	// fleet and every interactive shell resolve the same
	// /opt/dev-tools/config/pi (a git repo), so a daemon that reshapes it is
	// editing the team's environment as a side effect of starting up. Set true
	// only for a config directory hydra genuinely owns.
	ManageConfig bool `yaml:"manage_config"`
}

// PiAgentConfig contains a Pi agent sub-configuration (standard-agent, validation-agent)
type PiAgentConfig struct {
	LLM LLMConfig `yaml:"llm"`
}

// LLMConfig contains LLM provider settings
type LLMConfig struct {
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`
	ThinkingLevel string `yaml:"thinking_level"`
	Timeout       string `yaml:"timeout"`
	ApiKeyName    string `yaml:"api_key_name"`
}

// PiSkillsConfig contains Pi skills configuration
type PiSkillsConfig struct {
	UpdateInterval string `yaml:"update_interval"`
	AutoUpdate     bool   `yaml:"auto_update"`
}

// OutputConfig controls publishable output behaviour for a repository.
type OutputConfig struct {
	Push    bool   `yaml:"push"`
	Remote  string `yaml:"remote"`
	AutoPR  bool   `yaml:"auto_pr"`
	PRMerge bool   `yaml:"pr_merge"`
	// WritableBranchPrefixes lists extra branch prefixes that count as
	// hydra-owned for the publish-path ownership gate (in addition to the
	// built-in hydra/ and dogfood/). Branches outside these namespaces
	// never silently count as published.
	WritableBranchPrefixes []string `yaml:"writable_branch_prefixes"`
	// PRAutoMergeRepos lists absolute repo paths allowed to admin-merge their
	// integration PRs (tier-1 repos, e.g. the hydra self-repo). Repos not on
	// this list (product repos) stop at PR-open: the PR is created/left open
	// and the close is treated as waiting-on-human-merge, NOT an integration
	// failure. deploy.repo_path is implicitly on the list.
	PRAutoMergeRepos []string `yaml:"pr_auto_merge_repos"`
	// GithubTokenOwnerUser names the human user whose ~/.hydra/.github-token
	// is the daemon service account's GitHub credential (single source of
	// truth; 2026-10-01: a stale private ~/.git-credentials token grounded the
	// fleet for 7h). When set, daemon identity resolution reads that user's
	// PAT file and SKIPS the GH_TOKEN env and the daemon ~/.git-credentials
	// store entirely (fail-closed). Empty keeps the legacy fallback chain.
	GithubTokenOwnerUser string `yaml:"github_token_owner_user"`
}

// WorktreeConfig controls git-worktree behaviour used by the daemon.
type WorktreeConfig struct {
	// QuarantineAutoDrain enables automatic cleanup of hydra/quarantine/*
	// branches during the worktree reconciler tick. Default false (dry-run).
	QuarantineAutoDrain bool `yaml:"quarantine_auto_drain"`
}

// TriageConfig configures the periodic LLM PR-triage pass (hydra-muvh,
// FR-030): group overlapping dogfood/* PRs per product repo, ask the cheap
// judge for merge order / duplicate / needs-human, and post advisory
// comments + labels. Advice only: never merges, never closes. Provider/
// Model/APIKeyName are separate from the worker fleet's (GLM-class cheap
// model by design).
type TriageConfig struct {
	Enabled         bool   `yaml:"enabled"`
	IntervalSeconds int    `yaml:"interval_seconds"`
	Provider        string `yaml:"provider"`
	Model           string `yaml:"model"`
	Thinking        string `yaml:"thinking"`
	APIKeyName      string `yaml:"api_key_name"`
}

// IntegrationRetestConfig controls the integration-retest stage (Tier A of
// the integration-conflict plan): beads labelled integration-failed or
// integration-exhausted get a periodic mechanical recheck — merge-tree
// against the current base, gates in a scratch worktree, and landing via
// the existing dogfood PR path when provably clean. Tier B (agentic
// conflict resolution) is a separate bead and does not exist here.
type IntegrationRetestConfig struct {
	// Enabled turns the stage on. Default false: the selector matches only
	// beads the closer itself labelled, but a fresh enable should be a
	// deliberate operator decision, not an upgrade side effect.
	Enabled bool `yaml:"enabled"`
	// MaxAttempts caps retest attempts per bead (across conflict and
	// gates-failed outcomes). Reached → the bead stays parked and is never
	// retested again this daemon lifetime. Default 5.
	MaxAttempts int `yaml:"max_attempts"`
	// RetryInterval is the minimum gap between retest attempts on one bead,
	// as a Go duration string. Default "1h".
	RetryInterval string `yaml:"retry_interval"`
}

const (
	defaultIntegrationRetestMaxAttempts   = 5
	defaultIntegrationRetestRetryInterval = time.Hour
)

// applyDefaults fills zero values with the documented defaults.
func (c *IntegrationRetestConfig) applyDefaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = defaultIntegrationRetestMaxAttempts
	}
	if c.RetryInterval == "" {
		c.RetryInterval = defaultIntegrationRetestRetryInterval.String()
	}
}

// RetryIntervalDuration parses RetryInterval, falling back to the default
// on empty or unparsable values (fail-open: a bad knob must not panic the
// reconciler tick).
func (c *IntegrationRetestConfig) RetryIntervalDuration() time.Duration {
	if c == nil || c.RetryInterval == "" {
		return defaultIntegrationRetestRetryInterval
	}
	if d, err := time.ParseDuration(c.RetryInterval); err == nil && d > 0 {
		return d
	}
	return defaultIntegrationRetestRetryInterval
}

// StatusConfig configures the hydra status subcommand.
type StatusConfig struct {
	// LogPath is the daemon log to scan for recent verdicts.
	// Default: /var/log/hydra/hydra-error.log
	LogPath string `yaml:"log_path"`
}

// OutputPushEnabled reports whether the output/push feature is enabled.
func (c *Config) OutputPushEnabled() bool {
	if c == nil {
		return false
	}
	return c.Output.Push
}

// OutputPushRemote returns the configured push remote. When push is enabled
// but no remote is configured, it defaults to "origin".
func (c *Config) OutputPushRemote() string {
	if c == nil || !c.Output.Push {
		return ""
	}
	if c.Output.Remote == "" {
		return "origin"
	}
	return c.Output.Remote
}

// AutoPREnabled reports whether automatic PR creation for protected-branch
// push rejections is enabled.
func (c *Config) AutoPREnabled() bool {
	if c == nil {
		return false
	}
	return c.Output.AutoPR
}

// WritableBranchPrefixes returns the extra branch prefixes (beyond the
// built-in hydra/ and dogfood/) that count as hydra-owned for the
// publish-path ownership gate.
func (c *Config) WritableBranchPrefixes() []string {
	if c == nil {
		return nil
	}
	return c.Output.WritableBranchPrefixes
}

// IsPRAutoMergeRepo reports whether the repo at projectPath is allowed to
// admin-merge its integration PRs. Empty/nil config → false (fail safe:
// product default is stop-at-PR-open).
func (c *Config) IsPRAutoMergeRepo(projectPath string) bool {
	if c == nil || projectPath == "" {
		return false
	}
	clean := filepath.Clean(projectPath)
	if c.Deploy.RepoPath != "" && filepath.Clean(c.Deploy.RepoPath) == clean {
		return true
	}
	for _, p := range c.Output.PRAutoMergeRepos {
		if p != "" && filepath.Clean(p) == clean {
			return true
		}
	}
	return false
}

// PRMergeEnabled reports whether integrates must land via PR + gh pr merge
// instead of a direct push. Set this when the working branch has permanent
// branch protection: the direct push can never succeed, so the publisher
// goes straight to the dogfood/<id> feature branch + PR route.
func (c *Config) PRMergeEnabled() bool {
	if c == nil {
		return false
	}
	return c.Output.PRMerge
}

// rawConfig is used for YAML parsing to handle the nested projects structure
type rawConfig struct {
	Overseer          OverseerConfig          `yaml:"overseer"`
	Projects          yaml.Node               `yaml:"projects"` // Can be array or object
	MCP               MCPConfig               `yaml:"mcp"`
	API               APIConfig               `yaml:"api"`
	Intelligence      IntelligenceConfig      `yaml:"intelligence"`
	Agents            AgentsConfig            `yaml:"agents"`
	Pi                PiRootConfig            `yaml:"pi"`
	Output            OutputConfig            `yaml:"output"`
	Validation        ValidationConfig        `yaml:"validation"`
	ComplianceCycle   ComplianceCycleConfig   `yaml:"compliance_cycle"`
	Shepherd          ShepherdConfig          `yaml:"shepherd"`
	Worktree          WorktreeConfig          `yaml:"worktree"`
	Triage            TriageConfig            `yaml:"triage"`
	Status            StatusConfig            `yaml:"status"`
	IntegrationRetest IntegrationRetestConfig `yaml:"integration_retest"`
	SelfHealing       SelfHealingConfig       `yaml:"self_healing"`
	ContextPack       ContextPackConfig       `yaml:"context_pack"`
}

// projectsListConfig handles the case where projects is an object with 'list' field
type projectsListConfig struct {
	AutoDiscover        bool            `yaml:"auto_discover"`
	AutoDiscoverExclude []string        `yaml:"auto_discover_exclude"`
	MultiUserDiscovery  *bool           `yaml:"multi_user_discovery"`
	List                []ProjectConfig `yaml:"list"`
}

// IntelligenceConfig contains configuration for intelligence services
type IntelligenceConfig struct {
	Graphify              GraphifyConfig              `yaml:"graphify"`
	GPTCache              GPTCacheConfig              `yaml:"gptcache"`
	SettledState          SettledStateConfig          `yaml:"settled_state"`
	Patterns              PatternConfig               `yaml:"patterns"`
	CollectiveUnconscious CollectiveUnconsciousConfig `yaml:"collective_unconscious"`
	Hindsight             HindsightConfig             `yaml:"hindsight"`
}

// GraphifyConfig controls the code knowledge graph that replaces llm-tldr as the
// structural-intelligence layer.
//
// Graphify is reached two ways, deliberately split by cost:
//
//   - Agents query it through the `graphify` **skill**, which costs only its
//     frontmatter (~380 B) in the prompt because skill bodies load on demand.
//     Wiring it as an MCP server instead would put its tool definitions in every
//     bead's prompt — the extension route measured +8,325 tokens per turn.
//   - Hydra keeps the graph fresh by running `graphify update <project>`, which
//     is pure tree-sitter AST extraction with no model call. Measured on this
//     repo: 529 files, 9,720 nodes, 19,269 edges in 14.5s.
//
// Refresh is per project on an interval, never per bead. Beads run in throwaway
// worktrees, so a per-bead build would pay the extraction cost hundreds of times
// per run; and because every bead mutates code, a graph is stale the moment the
// next bead closes. Stale structure is worse than none — it tells a worker a
// symbol exists that an earlier bead deleted, which is the phantom-file failure
// class the compliance analyzer already exists to catch.
type GraphifyConfig struct {
	Enabled bool `yaml:"enabled"`

	// Binary is the graphify executable (default "graphify", resolved on PATH).
	Binary string `yaml:"binary"`

	// RefreshInterval is the minimum gap between graph rebuilds for one project.
	// Zero disables periodic refresh, leaving whatever graph is on disk.
	RefreshInterval time.Duration `yaml:"refresh_interval"`

	// Timeout bounds a single `graphify update` run. Upstream has an O(V x E)
	// pathology on very large monorepos (their issue #341: 114 minutes on a
	// 50k-file repo), so this is a hard stop rather than a suggestion.
	Timeout time.Duration `yaml:"timeout"`

	// MaxConcurrent caps simultaneous graph builds across projects.
	MaxConcurrent int `yaml:"max_concurrent"`

	// SkipClustering passes --no-cluster, writing the raw AST extraction without
	// community detection or the markdown report. Nodes and edges are all the
	// query/affected/god-nodes commands need, so this is the cheap mode.
	//
	// Note: --no-viz is NOT valid here. It belongs to `cluster-only`, and
	// passing it to `update` fails the build with "unknown update option" —
	// caught by running the real binary rather than trusting the help text.
	SkipClustering bool `yaml:"skip_clustering"`
}

// PatternConfig contains pattern extraction settings
type PatternConfig struct {
	Enabled             bool    `yaml:"enabled"`
	StoragePath         string  `yaml:"storage_path"`
	MinConfidence       float64 `yaml:"min_confidence"`
	MaxPatternsPerBead  int     `yaml:"max_patterns_per_bead"`
	ExtractCodeSnippets bool    `yaml:"extract_code_snippets"`
}

// CollectiveUnconsciousConfig contains Collective Unconscious settings
type CollectiveUnconsciousConfig struct {
	Enabled  bool             `yaml:"enabled"`
	Storage  CUStorageConfig  `yaml:"storage"`
	Matching CUMatchingConfig `yaml:"matching"`
	Review   CUReviewConfig   `yaml:"review"`
	Feedback CUFeedbackConfig `yaml:"feedback"`
	Security CUSecurityConfig `yaml:"security"`
}

// CUStorageConfig contains storage settings for Collective Unconscious
type CUStorageConfig struct {
	Backend  string        `yaml:"backend"` // "postgres", "hybrid"
	CacheTTL time.Duration `yaml:"cache_ttl"`
}

// CUMatchingConfig contains pattern matching settings
type CUMatchingConfig struct {
	Enabled                bool          `yaml:"enabled"`
	BlockOnFailure         bool          `yaml:"block_on_failure"`
	MaxPatternsInPrompt    int           `yaml:"max_patterns_in_prompt"`
	MinConfidenceThreshold float64       `yaml:"min_confidence_threshold"`
	CacheEnabled           bool          `yaml:"cache_enabled"`
	CacheTTL               time.Duration `yaml:"cache_ttl"`
	QueryTimeout           time.Duration `yaml:"query_timeout"`
}

// CUReviewConfig contains pattern review settings
type CUReviewConfig struct {
	Enabled              bool          `yaml:"enabled"`
	AutoApproveThreshold float64       `yaml:"auto_approve_threshold"`
	AutoDiscardThreshold float64       `yaml:"auto_discard_threshold"`
	RequireHumanReview   bool          `yaml:"require_human_review"`
	MaxPatternsInReview  int           `yaml:"max_patterns_in_review"`
	DuplicateThreshold   float64       `yaml:"duplicate_threshold"`
	ReviewTimeout        time.Duration `yaml:"review_timeout"`
}

// CUFeedbackConfig contains feedback loop settings
type CUFeedbackConfig struct {
	TrackOutcomes           bool    `yaml:"track_outcomes"`
	CollectExplicitFeedback bool    `yaml:"collect_explicit_feedback"`
	DecayRate               float64 `yaml:"decay_rate"` // Per day
}

// CUSecurityConfig contains security settings for patterns
type CUSecurityConfig struct {
	ScanForSecrets   bool     `yaml:"scan_for_secrets"`
	AllowedLicenses  []string `yaml:"allowed_licenses"`
	SanitizePatterns bool     `yaml:"sanitize_patterns"`
}

// GPTCacheConfig contains GPTCache service settings
type GPTCacheConfig struct {
	Enabled   bool   `yaml:"enabled"`
	ServerURL string `yaml:"server_url"`
}

// HindsightConfig contains Hindsight memory service settings
type HindsightConfig struct {
	Enabled bool          `yaml:"enabled"`
	BaseURL string        `yaml:"base_url"`
	Timeout time.Duration `yaml:"timeout"`
	Limit   int           `yaml:"limit"`

	// MaxTokens bounds the recalled context by token budget. This replaces the
	// former min_score, which the recall API does not accept and therefore
	// silently ignored — every recall returned the whole bank (~80KB) and the
	// client discarded most of it unfiltered. Measured: max_tokens=800 cuts the
	// recall response from ~80KB to ~16KB.
	MaxTokens int `yaml:"max_tokens"`
}

// OverseerConfig contains overseer-specific settings
type OverseerConfig struct {
	BeadDiscoveryInterval time.Duration            `yaml:"bead_discovery_interval"`
	MaxWorkers            int                      `yaml:"max_workers"`
	DefaultRetries        int                      `yaml:"default_retries"`
	MaxRetries            int                      `yaml:"max_retries"`
	WorkerTimeout         time.Duration            `yaml:"worker_timeout"`
	RetryDelay            time.Duration            `yaml:"retry_delay"`
	ResourceMonitoring    ResourceMonitoringConfig `yaml:"resource_monitoring"`
	ChainDetection        ChainDetectionConfig     `yaml:"chain_detection"`
	// ConcurrencyGovernor enables AIMD adaptive concurrency limiting.
	// Disabled by default.
	ConcurrencyGovernor GovernorConfig `yaml:"concurrency_governor"`

	// ProviderPool bounds concurrent provider requests across every consumer —
	// worker spawns, the adjudicator, and validation — so they stop refusing
	// each other's calls (Shepherd B3).
	ProviderPool PoolConfig `yaml:"provider_pool"`
	// EscalationTTL ages human escalations: a bead carrying the durable
	// `escalated` label older than this is demoted back to the ready queue
	// (label swapped for `escalation-expired`) with a structured event.
	// Escalations were previously never adjudicated, permanently wedging the
	// scheduler slot they hold. 0 disables the scanner.
	EscalationTTL time.Duration `yaml:"escalation_ttl"`
	// IntegrationExhaustedSweep ages integration-exhausted hard-parks: a
	// non-terminal bead carrying the durable `integration-exhausted` label
	// older than this gets `needs-human` added plus a typed
	// integration_exhausted_stale event, so abandoned integration attempts
	// surface for manual intervention or Pan retry-quarantine instead of
	// staying hidden forever. The hard-park itself is never cleared. 0
	// disables the sweeper.
	IntegrationExhaustedSweep time.Duration `yaml:"integration_exhausted_sweep"`
	// PostCloseSweep is the escape-rate regression budget (NFR-001): a
	// weekly scan that re-runs the declared verification command of beads
	// closed-completed in the last window. Any bead whose command now exits
	// nonzero is labelled `postclose-escape` and emits a typed
	// `postclose_escape` event — the escape-rate metric for the weekly query
	// (scripts/escape-rate-weekly.sh). Observe-and-report, fail-open. 0
	// disables the sweeper.
	PostCloseSweep bool `yaml:"postclose_sweep"`
	// PRAgingDays ages integration PRs left open waiting on a human merge:
	// a weekly per-owner digest (pr_aging_digest events) lists every PR still
	// open past this threshold, classified as stuck-auto-merge (repo is in
	// output.pr_auto_merge_repos / deploy.repo_path — the automated merge is
	// wedged) or t2-human-merge-queue (designed Tier-2 stop state — gentle
	// reminder that the human merge queue is aging). 0 disables the sweeper.
	PRAgingDays int `yaml:"pr_aging_days"`
	// MaxBeadTokens is the cumulative per-bead token ceiling enforced by
	// Charon: a working worker past the ceiling is killed and escalated via
	// the retry cap (0 = disabled). Non-converging grinders otherwise burn
	// 18-25M before the wall-clock timeout notices them.
	MaxBeadTokens int64 `yaml:"max_bead_tokens"`
	// WorktreeIsolation runs each chain in its own git worktree and commits
	// its work on verified-close, integrating back to the working branch on
	// chain completion. Off by default (opt-in).
	WorktreeIsolation bool `yaml:"worktree_isolation"`
}

type ChainDetectionConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
}

// ValidationConfig contains self-healing validation loop settings
type ValidationConfig struct {
	Enabled                  bool   `yaml:"enabled"`
	TDDValidationEnabled     bool   `yaml:"tdd_validation_enabled"`
	PRDValidationEnabled     bool   `yaml:"prd_validation_enabled"`
	MaxRounds                int    `yaml:"max_rounds"`
	AgentVerificationEnabled bool   `yaml:"agent_verification_enabled"`
	EnforceTargetScope       string `yaml:"enforce_target_scope"`

	// CloseBlockedOnFailed (default true) gates whether a bead that
	// closes with `close_reason=failed` (worker exit non-zero, retry
	// exhaustion) creates an auto-fix bead and keeps dependents
	// blocked. When false, the legacy behaviour is preserved: failed
	// closures propagate normally and dependents unblock. The default
	// is the strict one because the legacy behaviour let 57% of beads
	// in one audit close as `failed` while their dependents unblocked.
	CloseBlockedOnFailed bool `yaml:"close_blocked_on_failed"`

	// BlockStubImplementations (default true) rejects bead closures
	// whose target files contain stub patterns (empty bodies,
	// placeholder comments, _ = x discards). See
	// docs/operations/validation-stub-detection.md.
	BlockStubImplementations bool `yaml:"block_stub_implementations"`

	// RequireDiffOnClose (default true) rejects a completed bead close
	// when the worker left no uncommitted changes on disk for the bead's
	// target files (or the repo when none are declared). Workers do not
	// commit their work, so an empty diff means the "agent reported
	// success but edited nothing" case. Ops can disable this if a
	// workflow legitimately closes beads without file changes.
	RequireDiffOnClose bool `yaml:"require_diff_on_close"`

	// DisableFixSignatureClustering (default false = clustering on, PRD
	// FR-020) restores the legacy one-fix-bead-per-failed-close behaviour:
	// close-fail errors with the same normalized signature share one open
	// meta fix bead and later failures are parked against it instead of
	// spawning per-bead fix beads. Disable only to bisect clustering
	// regressions.
	DisableFixSignatureClustering bool `yaml:"disable_fix_signature_clustering"`

	// SemanticClustering (PRD FR-020 follow-up, bead hydra-n5qt) adds an
	// embedding-based pass on top of signature clustering: after an exact
	// signature miss, the failure cause is embedded and compared by cosine
	// similarity against open meta fix beads' recorded causes, merging
	// near-duplicate root causes into one cluster before fix-bead filing.
	// Inert unless Enabled; all embedding failures fail open to the string
	// path. See internal/core/semantic_cluster.go.
	SemanticClustering SemanticClusterConfig `yaml:"semantic_clustering"`

	// AgentVerifier is the semantic close-gate adversary (bounded veto):
	// after the deterministic gates pass, a pi-backed reviewer scores the
	// diff against the acceptance criteria. See docs/architecture/shepherd.md.
	AgentVerifier AgentVerifierConfig `yaml:"agent_verifier"`
}

// AgentVerifierConfig tunes the adversarial close gate. Bounded veto by
// construction: a close ships when the verifier reports zero BLOCKER
// findings AND score >= MinScore; NIT findings never block (recorded as
// events + closed-with-findings label). Rejections ride the standard
// verified-close rounds (Validation.MaxRounds); a verifier that produces
// NEW findings every round trips the churn guard and the close ships — an
// unconstrained adversary is an infinite nitpick generator, so the veto is
// deliberately finite.
type AgentVerifierConfig struct {
	// Enabled gates the whole gate (default false; opt-in per class).
	Enabled bool `yaml:"enabled"`
	// MinScore is the ship threshold (default 0.8). Crank toward 0.95 for
	// enhancement-hungry dogfood repos, down toward 0.5 for throughput.
	MinScore float64 `yaml:"min_score"`
	// Classes limits the gate to these bead types (default implement,
	// feature, refactor). Test/docs/syntax phases are execution-gated.
	Classes []string `yaml:"classes"`
	// MaxDiffBytes caps the diff fed to the reviewer (default 32KB).
	MaxDiffBytes int `yaml:"max_diff_bytes"`
	// TimeoutSeconds bounds one verifier call (default 300).
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// Provider/Model/PiPath mirror the Pan runner identity; empty = pi defaults.
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	PiPath   string `yaml:"pi_path"`
}

// agentVerifierWithDefaults normalizes zero-valued knobs.
func agentVerifierWithDefaults(c AgentVerifierConfig) AgentVerifierConfig {
	if c.MinScore <= 0 || c.MinScore > 1 {
		c.MinScore = 0.8
	}
	if len(c.Classes) == 0 {
		c.Classes = []string{"implement", "feature", "refactor"}
	}
	if c.MaxDiffBytes <= 0 {
		c.MaxDiffBytes = 32768
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 300
	}
	return c
}

// TargetScopeMode returns the normalized scope gate enforcement mode.
// Valid values are "off", "warn", and "block" (case-insensitive).
// Empty or unrecognized values fall back to "warn".
func (v ValidationConfig) TargetScopeMode() string {
	switch strings.ToLower(strings.TrimSpace(v.EnforceTargetScope)) {
	case "off":
		return "off"
	case "block":
		return "block"
	default:
		return "warn"
	}
}

// ResourceMonitoringConfig contains resource monitoring settings
type ResourceMonitoringConfig struct {
	Enabled          bool    `yaml:"enabled"`
	MaxCPUPercent    float64 `yaml:"max_cpu_percent"`
	MaxMemoryPercent float64 `yaml:"max_memory_percent"`
}

// ProjectConfig represents a project configuration
type ProjectConfig struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	Type string `yaml:"type"` // "single" or "multi"
	// IntegrationBranch pins the integration base for PR creation and
	// merge-base checks. Optional: when unset, the origin default branch is
	// used. The main checkout's current branch is NEVER used as a base.
	IntegrationBranch string       `yaml:"integration_branch,omitempty"`
	Repos             []RepoConfig `yaml:"repos,omitempty"`
	Gates             *GateSet     `yaml:"gates,omitempty"` // daemon-owned deterministic gates (FR-002); nil = legacy
}

// RepoConfig represents a repository within a multi-repo project
type RepoConfig struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	// IntegrationBranch pins the integration base for this repo (multi-repo
	// projects). Optional; see ProjectConfig.IntegrationBranch.
	IntegrationBranch string `yaml:"integration_branch,omitempty"`
}

// MCPConfig contains MCP (Model Context Protocol) Agent Mail settings
type MCPConfig struct {
	Enabled              bool          `yaml:"enabled"`
	URL                  string        `yaml:"url"`
	Port                 int           `yaml:"port"`
	BaseURL              string        `yaml:"base_url"`
	ReconnectInterval    time.Duration `yaml:"reconnect_interval"`
	MaxReconnectAttempts int           `yaml:"max_reconnect_attempts"`
	HeartbeatInterval    time.Duration `yaml:"heartbeat_interval"`
	FileReservationTTL   time.Duration `yaml:"file_reservation_ttl"`
}

// APIConfig contains API server settings
type APIConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		MultiUserDiscovery: true,
		Overseer: OverseerConfig{
			BeadDiscoveryInterval:     5 * time.Second,
			MaxWorkers:                2,
			DefaultRetries:            3,
			EscalationTTL:             72 * time.Hour,
			IntegrationExhaustedSweep: 48 * time.Hour,
			PostCloseSweep:            true,
			PRAgingDays:               PRAgingThresholdDays,
			WorkerTimeout:             30 * time.Minute,
			ChainDetection: ChainDetectionConfig{
				Enabled:  true,
				Interval: 10 * time.Second,
				Timeout:  10 * time.Minute,
			},
		},
		Validation: ValidationConfig{
			Enabled:                  true,
			TDDValidationEnabled:     true,
			PRDValidationEnabled:     true,
			MaxRounds:                3,
			AgentVerificationEnabled: true,
			EnforceTargetScope:       "warn",
			RequireDiffOnClose:       true,
		},
		SelfHealing: SelfHealingConfig{
			SessionResume: DefaultSessionResumeConfig(),
		},
		ComplianceCycle: ComplianceCycleConfig{
			Enabled:      false,
			MaxCycles:    2,
			PollInterval: 30 * time.Second,
		},
		MCP: MCPConfig{
			Enabled:              true,
			Port:                 8765,
			BaseURL:              "http://127.0.0.1:8765",
			URL:                  "ws://127.0.0.1:8765/ws",
			ReconnectInterval:    5 * time.Second,
			MaxReconnectAttempts: 10,
			HeartbeatInterval:    30 * time.Second,
			FileReservationTTL:   30 * time.Minute,
		},
		API: APIConfig{
			Enabled: true,
			Port:    8080,
		},
		Intelligence: IntelligenceConfig{
			// Graphify: the structural-intelligence layer. Enabled by default;
			// agents reach it through the `graphify` skill, and hydra only keeps
			// each project's graph fresh with an AST-only `graphify update`.
			Graphify: GraphifyConfig{
				Enabled:         true,
				Binary:          "graphify",
				RefreshInterval: 30 * time.Minute,
				Timeout:         10 * time.Minute,
				MaxConcurrent:   1,
			},
			GPTCache: GPTCacheConfig{
				Enabled:   true,
				ServerURL: "http://127.0.0.1:8788",
			},
			Hindsight: HindsightConfig{
				Enabled:   true,
				BaseURL:   "http://127.0.0.1:8000",
				Timeout:   10 * time.Second,
				Limit:     5,
				MaxTokens: 800,
			},
			SettledState: DefaultSettledStateConfig(),
			Patterns: PatternConfig{
				Enabled:             true,
				StoragePath:         "",
				MinConfidence:       0.7,
				MaxPatternsPerBead:  10,
				ExtractCodeSnippets: true,
			},
			CollectiveUnconscious: CollectiveUnconsciousConfig{
				Enabled: true,
				Storage: CUStorageConfig{
					Backend:  "cass_memory",
					CacheTTL: 5 * time.Minute,
				},
				Matching: CUMatchingConfig{
					Enabled:                true,
					BlockOnFailure:         true,
					MaxPatternsInPrompt:    3,
					MinConfidenceThreshold: 0.50, // Lowered from 0.70 for more pattern matches
					CacheEnabled:           true,
					CacheTTL:               5 * time.Minute,
					QueryTimeout:           5 * time.Second,
				},
				Review: CUReviewConfig{
					Enabled:              true,
					AutoApproveThreshold: 0.75, // Lowered from 0.90 for more auto-approvals
					AutoDiscardThreshold: 0.40, // Lowered from 0.50 to keep borderline patterns,
					RequireHumanReview:   false,
					MaxPatternsInReview:  100,
					DuplicateThreshold:   0.95,
					ReviewTimeout:        24 * time.Hour,
				},
				Feedback: CUFeedbackConfig{
					TrackOutcomes:           true,
					CollectExplicitFeedback: true,
					DecayRate:               0.01, // Per day
				},
				Security: CUSecurityConfig{
					ScanForSecrets:   true,
					AllowedLicenses:  []string{"MIT", "Apache-2.0", "BSD-3-Clause"},
					SanitizePatterns: true,
				},
			},
		},
		Agents: AgentsConfig{
			Enabled:      true,
			DefaultAgent: "pi",
			Pi: PiConfig{
				Enabled: true,
				// Provider/Model/ThinkingLevel/Timeout left empty so that
				// pi.standard-agent.llm in the YAML can override, or the
				// overseer fills in safe defaults.
			},
		},
		Pi: PiRootConfig{
			ConfigDir: "./pi-config",
			Skills: PiSkillsConfig{
				UpdateInterval: "24h",
				AutoUpdate:     true,
			},
		},
		Status: StatusConfig{
			LogPath: "/var/log/hydra/hydra-error.log",
		},
	}
}

// ConfigCache provides thread-safe caching for loaded configurations
type ConfigCache struct {
	mu          sync.RWMutex
	config      *Config
	path        string
	modTime     time.Time
	hits        uint64
	misses      uint64
	validations uint64
}

// Global cache instance
var globalCache = &ConfigCache{}

// CacheStats returns cache statistics
func (cc *ConfigCache) Stats() (hits, misses, validations uint64) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.hits, cc.misses, cc.validations
}

// ResetCache clears the global config cache
func ResetCache() {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()
	globalCache.config = nil
	globalCache.path = ""
	globalCache.modTime = time.Time{}
	globalCache.hits = 0
	globalCache.misses = 0
	globalCache.validations = 0
}

// getCachedConfig retrieves cached config if still valid
func (cc *ConfigCache) get(path string) (*Config, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	if cc.config == nil || cc.path != path {
		return nil, false
	}

	// Check if file has been modified
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}

	if info.ModTime() != cc.modTime {
		return nil, false
	}

	cc.hits++
	// Return a clone to prevent external modification
	return cc.config.Clone(), true
}

// set stores config in cache
func (cc *ConfigCache) set(path string, cfg *Config, modTime time.Time) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.config = cfg.Clone()
	cc.path = path
	cc.modTime = modTime
	cc.misses++
}

// DuplicateProjectNameError is returned when two configured projects share a
// name. Name-based resolution (GetProjectPath, findProjectPathForBeadID)
// picks the FIRST match, so duplicate names silently route chains to the
// wrong project (webapp zpf incident, 2026-09-16: 17/17 beads closed while
// the work sat unintegrated in another user's project).
type DuplicateProjectNameError struct {
	Name  string
	PathA string
	PathB string
}

func (e *DuplicateProjectNameError) Error() string {
	return fmt.Sprintf("duplicate project name %q: %s and %s — project names must be unique; name-based resolution picks the first match", e.Name, e.PathA, e.PathB)
}

// validateUniqueProjectNames rejects a project list where two entries share
// a name. Called from both load paths (daemon fails startup) and ValidateConfig
// (hot reload rejects the new config before applying it).
func validateUniqueProjectNames(projects []ProjectConfig) error {
	seen := make(map[string]string, len(projects))
	for _, p := range projects {
		if p.Name == "" {
			continue // caught by ValidateConfig's per-project checks
		}
		if prev, ok := seen[p.Name]; ok {
			return &DuplicateProjectNameError{Name: p.Name, PathA: prev, PathB: p.Path}
		}
		seen[p.Name] = p.Path
	}
	return nil
}

// LoadConfig loads configuration from a YAML file with caching
func LoadConfig(path string) (*Config, error) {
	// Try cache first
	if cfg, ok := globalCache.get(path); ok {
		return cfg, nil
	}

	// Load from file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{Op: "read", Err: err}
	}

	// Get file info for cache timestamp
	info, err := os.Stat(path)
	if err != nil {
		return nil, &ConfigError{Op: "stat", Err: err}
	}

	// Start with default config
	config := DefaultConfig()

	// Parse raw config to handle projects array vs object
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, &ConfigError{Op: "parse", Err: err}
	}

	// Copy non-project fields
	config.Overseer = raw.Overseer
	config.MCP = raw.MCP
	config.API = raw.API
	config.Intelligence = raw.Intelligence
	config.Agents = raw.Agents
	config.applyPiDefaults()
	config.Validation = raw.Validation
	config.ComplianceCycle = raw.ComplianceCycle
	config.Shepherd = raw.Shepherd
	config.Status = raw.Status
	config.Pi = PiRootConfig{
		ConfigDir:       raw.Pi.ConfigDir,
		PiConfigDir:     raw.Pi.PiConfigDir,
		WorkerConfigDir: raw.Pi.WorkerConfigDir,
		ManageConfig:    raw.Pi.ManageConfig,
		Skills:          raw.Pi.Skills,
		StandardAgent:   raw.Pi.StandardAgent,
	}
	config.Output = raw.Output
	config.Worktree = raw.Worktree
	config.Triage = raw.Triage
	config.IntegrationRetest = raw.IntegrationRetest
	config.IntegrationRetest.applyDefaults()
	config.SelfHealing = raw.SelfHealing
	config.ContextPack = raw.ContextPack
	config.SelfHealing.SessionResume.applyDefaults()
	// Handle projects field - can be:
	// 1. An array of ProjectConfig (direct list)
	// 2. An object with 'auto_discover' and 'list' fields
	// 3. Empty/null (use auto-discovery)
	if !isZeroNode(raw.Projects) {
		if raw.Projects.Kind == yaml.SequenceNode {
			// Projects is an array
			if err := raw.Projects.Decode(&config.Projects); err != nil {
				return nil, fmt.Errorf("failed to parse projects array: %w", err)
			}
		} else if raw.Projects.Kind == yaml.MappingNode {
			// Projects is an object - check if it's the {auto_discover, list} format
			var projectsObj projectsListConfig
			if err := raw.Projects.Decode(&projectsObj); err == nil && len(projectsObj.List) > 0 {
				// It's the object format with list
				config.Projects = projectsObj.List
				if projectsObj.MultiUserDiscovery != nil {
					config.MultiUserDiscovery = *projectsObj.MultiUserDiscovery
				}
				config.AutoDiscoverExclude = projectsObj.AutoDiscoverExclude
			} else {
				// Try parsing as direct array anyway
				if err := raw.Projects.Decode(&config.Projects); err != nil {
					return nil, fmt.Errorf("failed to parse projects: %w", err)
				}
			}
		}
	}

	if err := validateUniqueProjectNames(config.Projects); err != nil {
		return nil, err
	}

	// Cache the loaded config
	globalCache.set(path, config, info.ModTime())

	return config, nil
}

// LoadConfigUncached loads configuration without using cache (for testing)
func LoadConfigUncached(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{Op: "read", Err: err}
	}

	// Start with default config
	config := DefaultConfig()

	// Parse raw config to handle projects array vs object
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, &ConfigError{Op: "parse", Err: err}
	}

	// Copy non-project fields
	config.Overseer = raw.Overseer
	config.MCP = raw.MCP
	config.API = raw.API
	config.Intelligence = raw.Intelligence
	config.Agents = raw.Agents
	config.applyPiDefaults()
	config.Pi = PiRootConfig{
		ConfigDir:       raw.Pi.ConfigDir,
		PiConfigDir:     raw.Pi.PiConfigDir,
		WorkerConfigDir: raw.Pi.WorkerConfigDir,
		ManageConfig:    raw.Pi.ManageConfig,
		Skills:          raw.Pi.Skills,
		StandardAgent:   raw.Pi.StandardAgent,
	}
	config.Output = raw.Output
	config.Worktree = raw.Worktree
	config.Triage = raw.Triage
	config.IntegrationRetest = raw.IntegrationRetest
	config.IntegrationRetest.applyDefaults()
	config.SelfHealing = raw.SelfHealing
	config.ContextPack = raw.ContextPack
	config.SelfHealing.SessionResume.applyDefaults()
	config.Validation = raw.Validation
	config.ComplianceCycle = raw.ComplianceCycle
	config.Shepherd = raw.Shepherd
	config.Status = raw.Status
	// Handle projects field
	if !isZeroNode(raw.Projects) {
		if raw.Projects.Kind == yaml.SequenceNode {
			if err := raw.Projects.Decode(&config.Projects); err != nil {
				return nil, fmt.Errorf("failed to parse projects array: %w", err)
			}
		} else if raw.Projects.Kind == yaml.MappingNode {
			var projectsObj projectsListConfig
			if err := raw.Projects.Decode(&projectsObj); err == nil && len(projectsObj.List) > 0 {
				config.Projects = projectsObj.List
				if projectsObj.MultiUserDiscovery != nil {
					config.MultiUserDiscovery = *projectsObj.MultiUserDiscovery
				}
				config.AutoDiscoverExclude = projectsObj.AutoDiscoverExclude
			} else {
				if err := raw.Projects.Decode(&config.Projects); err != nil {
					return nil, fmt.Errorf("failed to parse projects: %w", err)
				}
			}
		}
	}

	if err := validateUniqueProjectNames(config.Projects); err != nil {
		return nil, err
	}

	return config, nil
}

// isZeroNode checks if a yaml.Node is empty/zero value
func isZeroNode(n yaml.Node) bool {
	return n.Kind == 0 && n.Tag == "" && len(n.Content) == 0
}

// Clone creates a deep copy of the Config
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}

	cloned := &Config{
		Overseer:        c.Overseer,
		MCP:             c.MCP,
		API:             c.API,
		Intelligence:    c.Intelligence,
		Agents:          c.Agents,
		Pi:              c.Pi,
		Output:          c.Output,
		Validation:      c.Validation,
		ComplianceCycle: c.ComplianceCycle,
		Shepherd:        c.Shepherd,
		Worktree:        c.Worktree,
		Status:          c.Status,
	}

	// Deep copy Projects slice
	if c.Projects != nil {
		cloned.Projects = make([]ProjectConfig, len(c.Projects))
		copy(cloned.Projects, c.Projects)
	}
	if c.AutoDiscoverExclude != nil {
		cloned.AutoDiscoverExclude = make([]string, len(c.AutoDiscoverExclude))
		copy(cloned.AutoDiscoverExclude, c.AutoDiscoverExclude)
	}

	// Deep copy Agents.Pi.PhaseModels map so mutations of the returned
	// config do not affect the cached config.
	if c.Agents.Pi.PhaseModels != nil {
		cloned.Agents.Pi.PhaseModels = make(map[string]PhaseModel, len(c.Agents.Pi.PhaseModels))
		for k, v := range c.Agents.Pi.PhaseModels {
			cloned.Agents.Pi.PhaseModels[k] = v
		}
	}

	return cloned
}

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string
	Message string
}

func (v ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", v.Field, v.Message)
}

// ValidationErrors holds multiple validation errors
type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	if len(v) == 0 {
		return "no validation errors"
	}
	if len(v) == 1 {
		return v[0].Error()
	}

	// Pre-allocate with exact capacity needed
	messages := make([]string, 0, len(v))
	for _, err := range v {
		messages = append(messages, err.Error())
	}
	return fmt.Sprintf("%d validation errors:\n  - %s", len(v), strings.Join(messages, "\n  - "))
}

// IsValid returns true if there are no validation errors
func (v ValidationErrors) IsValid() bool {
	return len(v) == 0
}

// Pre-allocated constants for validation to reduce allocations
const (
	fieldProjects                  = "projects"
	fieldOverseerMaxWorkers        = "overseer.max_workers"
	fieldOverseerBeadDiscovery     = "overseer.bead_discovery_interval"
	fieldPiConfigDir               = "pi.config_dir"
	fieldPiSkillsUpdateInterval    = "pi.skills.update_interval"
	msgNoProjects                  = "no projects configured"
	msgMaxWorkersNegative          = "max_workers cannot be negative (use 0 for unlimited)"
	msgBeadDiscoveryIntervalTooLow = "bead_discovery_interval must be at least 1 second"
	msgProjectPathRequired         = "project path is required"
	msgProjectNameRequired         = "project name is required"
	msgConfigDirInvalidChars       = "config_dir contains invalid characters"
	msgUpdateIntervalInvalid       = "update_interval must be a valid duration (e.g., '24h', '30m'): %v"
	msgUpdateIntervalTooShort      = "update_interval must be at least 1 minute"
	msgUpdateIntervalTooLong       = "update_interval should not exceed 30 days"
	msgSkillNameRequired           = "skill name is required"
	msgSkillRepoInvalid            = "repo must be a valid git URL (https:// or git@)"
)

// ValidateConfig validates the config and returns all validation errors
func ValidateConfig(cfg *Config) ValidationErrors {
	if cfg == nil {
		return ValidationErrors{{Field: "config", Message: "config is nil"}}
	}

	var errors ValidationErrors
	// Pre-allocate with reasonable capacity (typical configs have < 10 errors)
	errors = make(ValidationErrors, 0, 8)

	// Check if we have any projects
	if len(cfg.Projects) == 0 {
		errors = append(errors, ValidationError{
			Field:   fieldProjects,
			Message: msgNoProjects,
		})
	}

	// Validate overseer settings - fast checks first
	if cfg.Overseer.MaxWorkers < 0 {
		errors = append(errors, ValidationError{
			Field:   fieldOverseerMaxWorkers,
			Message: msgMaxWorkersNegative,
		})
	}

	if cfg.Overseer.BeadDiscoveryInterval < time.Second {
		errors = append(errors, ValidationError{
			Field:   fieldOverseerBeadDiscovery,
			Message: msgBeadDiscoveryIntervalTooLow,
		})
	}

	// Validate project paths and reject duplicate names (name-based
	// resolution picks the first match — duplicates integrate into the
	// wrong project silently).
	seenNames := make(map[string]string, len(cfg.Projects))
	for i, proj := range cfg.Projects {
		if proj.Path == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].path", i),
				Message: msgProjectPathRequired,
			})
		}
		if proj.Name == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].name", i),
				Message: msgProjectNameRequired,
			})
		} else if prev, ok := seenNames[proj.Name]; ok {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("projects[%d].name", i),
				Message: (&DuplicateProjectNameError{Name: proj.Name, PathA: prev, PathB: proj.Path}).Error(),
			})
		} else {
			seenNames[proj.Name] = proj.Path
		}
	}

	// Validate PI configuration
	if piErrors := validatePiConfigFast(&cfg.Pi); len(piErrors) > 0 {
		errors = append(errors, piErrors...)
	}

	return errors
}

// validatePiConfigFast is an optimized version of validatePiConfig
func validatePiConfigFast(cfg *PiRootConfig) ValidationErrors {
	if cfg == nil {
		return nil
	}

	var errors ValidationErrors
	errors = make(ValidationErrors, 0, 4)

	// Fast path: skip empty config_dir check
	if cfg.ConfigDir != "" {
		// Check for null bytes only (most common invalid char)
		if strings.Contains(cfg.ConfigDir, "\x00") {
			errors = append(errors, ValidationError{
				Field:   fieldPiConfigDir,
				Message: msgConfigDirInvalidChars,
			})
		}
	}

	// Validate update_interval
	if cfg.Skills.UpdateInterval != "" {
		if duration, err := time.ParseDuration(cfg.Skills.UpdateInterval); err != nil {
			errors = append(errors, ValidationError{
				Field:   fieldPiSkillsUpdateInterval,
				Message: fmt.Sprintf(msgUpdateIntervalInvalid, err),
			})
		} else {
			// Single comparison using constants
			const minDuration = time.Minute
			const maxDuration = 30 * 24 * time.Hour

			if duration < minDuration {
				errors = append(errors, ValidationError{
					Field:   fieldPiSkillsUpdateInterval,
					Message: msgUpdateIntervalTooShort,
				})
			} else if duration > maxDuration {
				errors = append(errors, ValidationError{
					Field:   fieldPiSkillsUpdateInterval,
					Message: msgUpdateIntervalTooLong,
				})
			}
		}
	}

	return errors
}

// ValidateConfigSingleError validates the config and returns a single error if any validation fails.
func ValidateConfigSingleError(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	errors := ValidateConfig(cfg)
	if !errors.IsValid() {
		return errors
	}
	return nil
}

// ConfigValidator validates configuration files (deprecated: use ValidateConfig or ExtendedValidator)
type ConfigValidator struct{}

// NewConfigValidator creates a new config validator (deprecated: use ValidateConfig or ExtendedValidator)
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate checks if the config is valid (deprecated: use ValidateConfig or ExtendedValidator.Validate)
func (v *ConfigValidator) Validate(cfg *Config) error {
	return ValidateConfigSingleError(cfg)
}

// validatePiConfig validates PI-specific configuration settings (kept for backward compatibility)
func validatePiConfig(cfg *PiRootConfig) ValidationErrors {
	return validatePiConfigFast(cfg)
}
