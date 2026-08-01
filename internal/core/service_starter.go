package core

import (
	"context"
	"log"
	"time"

	"github.com/haulmatic-dev/hydra/internal/pan"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

// ServiceStarter starts all overseer background services.
type ServiceStarter struct {
	overseer *Overseer
}

// NewServiceStarter creates a new service starter.
func NewServiceStarter(overseer *Overseer) *ServiceStarter {
	return &ServiceStarter{overseer: overseer}
}

// Start begins all background services.
func (s *ServiceStarter) Start(ctx context.Context) error {
	o := s.overseer

	// Start orphan process janitor
	if o.orphanJanitor != nil {
		o.orphanJanitor.Start()
		log.Println("🧹 Orphan janitor started")
	}

	// Open the worker registry (daemon-owned SQLite truth; PRD
	// worker-park-and-resume Part 4) and construct Pan, the shepherd.
	// Done here rather than in NewOverseer so tests constructing an Overseer
	// never touch the filesystem. Fail-open: if the registry cannot be
	// opened, the daemon runs without it exactly as before, and Pan (which
	// reads it) is not started either.
	if regStore, err := registry.Open(registryPath()); err != nil {
		log.Printf("[Overseer] Warning: registry unavailable, running without it: %v", err)
	} else {
		o.registryStore = regStore
		log.Printf("[Registry] Initialized")
		// Charon's stuck-worker sweep judges liveness from the registry
		// (heartbeat), not the wall clock: parked and actively-reporting
		// workers are never killed on time alone.
		if o.orphanJanitor != nil {
			o.orphanJanitor.SetRegistry(regStore)
		}
		panCfg := o.config.Shepherd.Pan
		o.panShepherd = pan.New(pan.Config{
			Enabled:          panCfg.Enabled,
			Interval:         time.Duration(panCfg.IntervalSeconds) * time.Second,
			HungAfter:        time.Duration(panCfg.HungAfterSeconds) * time.Second,
			ParkOverdueGrace: time.Duration(panCfg.ParkOverdueGraceSeconds) * time.Second,
			NudgeCap:         panCfg.NudgeCap,
			Provider:         panCfg.Provider,
			Model:            panCfg.Model,
			Thinking:         panCfg.Thinking,
			APIKeyName:       panCfg.APIKeyName,
		}, o.registryStore, nil)
	}

	// Start Pan, the shepherd (fail-open; nil when the registry is unavailable)
	if o.panShepherd != nil {
		o.panShepherd.Start()
		log.Println("🐐 Pan shepherd started")
	}

	// Run Pi ecosystem setup
	if err := o.runPiSetup(); err != nil {
		log.Printf("[Overseer] Pi setup warning: %v", err)
	}

	// Discover projects from system config
	projectScanner := NewProjectScanner(o.config)
	if err := projectScanner.Discover(); err != nil {
		return &ServiceError{Op: "discover projects", Err: err}
	}
	o.projects = projectScanner.Projects()
	log.Printf("📁 Discovered %d projects from config", len(o.projects))

	// Discover multi-user projects
	multiUserDiscovery := NewMultiUserDiscovery()
	if err := multiUserDiscovery.Discover(); err != nil {
		log.Printf("[MultiUser] Discovery error: %v", err)
	} else {
		userProjects := multiUserDiscovery.Projects()
		if len(userProjects) > 0 {
			o.projects = append(o.projects, userProjects...)
			log.Printf("👥 Discovered %d user projects from %d users", len(userProjects), len(multiUserDiscovery.Users()))
		}
		// Store user contexts on overseer and worker spawner for per-user attribution
		o.userContexts = multiUserDiscovery.Users()
		o.workerSpawner.SetUserContexts(o.userContexts)
	}

	// Initialize bead file reader and analyzer
	o.beadFileReader = NewBeadFileReader(o.projects)
	o.beadAnalyzer.SetProjects(o.projects)

	// Update bead discovery with discovered projects
	if o.beadDiscovery != nil {
		o.beadDiscovery.SetProjects(o.projects)
	}

	// Initialize chain registrar (depends on chainCompletionDetector and beadFileReader)
	if o.chainCompletionDetector != nil && o.beadFileReader != nil {
		o.chainRegistrar = NewChainRegistrar(o.chainCompletionDetector, o.beadFileReader)
	}

	// Start settled-state detection service if enabled
	if o.settledStateSvc != nil {
		if err := o.settledStateSvc.Start(ctx); err != nil {
			log.Printf("[SettledState] Failed to start service: %v", err)
		} else {
			log.Println("🔒 Settled-state detection service started")
		}
	}

	// Start skill updater if initialized
	if o.skillUpdater != nil {
		o.skillUpdater.Start()
		log.Println("📚 Skill updater started")
	}

	// Refresh each project's code graph. The graphify indexer has no background
	// worker to start: each refresh is a bounded one-shot subprocess.
	if o.intelligenceHub.GraphifyIndexer() != nil {
		o.intelligenceHub.QueueInitialIndexing(o.projects)
		log.Println("🕸️  Graphify code-graph refresh queued")
	}

	// Start epic scanner
	if o.epicScanner != nil {
		o.epicScanner.Start()
		log.Println("🏛️  Epic scanner started")
	}

	return nil
}

// Stop stops all background services.
func (s *ServiceStarter) Stop() {
	o := s.overseer

	if o.orphanJanitor != nil {
		o.orphanJanitor.Stop()
	}
	if o.settledStateSvc != nil {
		if err := o.settledStateSvc.Stop(); err != nil {
			log.Printf("[SettledState] Error stopping service: %v", err)
		} else {
			log.Println("🔒 Settled-state detection service stopped")
		}
	}
	if o.skillUpdater != nil {
		o.skillUpdater.Stop()
		log.Println("📚 Skill updater stopped")
	}

	if o.epicScanner != nil {
		o.epicScanner.Stop()
		log.Println("🏛️  Epic scanner stopped")
	}
}
