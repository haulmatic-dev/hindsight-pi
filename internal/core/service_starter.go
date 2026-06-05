package core

import (
	"context"
	"log"
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
		// Store user contexts on overseer, worker spawner, and lifecycle for per-user git auth
		o.userContexts = multiUserDiscovery.Users()
		o.workerSpawner.SetUserContexts(o.userContexts)
		o.workerLifecycle.SetUserContexts(o.userContexts)
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

	// Start TLDR indexer if enabled
	if indexer := o.intelligenceHub.Indexer(); indexer != nil {
		indexer.Start()
		log.Println("📇 TLDR indexer started")
		o.intelligenceHub.QueueInitialIndexing(o.projects)
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
	if indexer := o.intelligenceHub.Indexer(); indexer != nil {
		indexer.Stop()
		log.Println("📇 TLDR indexer stopped")
	}
	if o.epicScanner != nil {
		o.epicScanner.Stop()
		log.Println("🏛️  Epic scanner stopped")
	}
}
