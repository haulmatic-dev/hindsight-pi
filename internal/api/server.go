package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/haulmatic-dev/hydra/graph"
	"github.com/haulmatic-dev/hydra/internal/core"
)

// Server represents the HTTP API server
type Server struct {
	router   *gin.Engine
	config   *core.Config
	overseer *core.Overseer
	server   *http.Server
}

// NewServer creates a new API server instance
func NewServer(config *core.Config, overseer *core.Overseer) *Server {
	if !config.API.Enabled {
		return nil
	}

	// Set Gin mode
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())

	s := &Server{
		router:   router,
		config:   config,
		overseer: overseer,
	}

	s.setupRoutes()
	s.setupGraphQL()

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// Health check
	s.router.GET("/health", s.HealthHandler)

	// API v1 routes
	v1 := s.router.Group("/api/v1")
	{
		// Stats
		v1.GET("/stats", s.GetStatsHandler)

		// Projects
		v1.GET("/projects", s.GetProjectsHandler)

		// Workers
		v1.GET("/workers", s.GetWorkersHandler)

		// Overseer control
		v1.POST("/overseer/pause", s.PauseOverseerHandler)
		v1.POST("/overseer/resume", s.ResumeOverseerHandler)
		v1.POST("/overseer/max-workers", s.SetMaxWorkersHandler)

		// DAG endpoints
		v1.GET("/dag/stats", s.GetDAGStatsHandler)
		v1.GET("/dag/ready", s.GetReadyBeadsHandler)

		// MCP endpoints
		v1.GET("/mcp/status", s.GetMCPStatusHandler)
		v1.GET("/mcp/reservations", s.GetReservationsHandler)

		// Graph Metrics endpoints (FR-006)
		v1.GET("/graph/metrics", s.GetGraphMetricsHandler)
		v1.GET("/graph/bottlenecks", s.GetGraphBottlenecksHandler)
	}

	// TODO: Add GraphQL endpoint in Phase 1
	// TODO: Add WebSocket endpoint for real-time updates
	// TODO: Add SSE endpoint for log streaming
}

// setupGraphQL configures GraphQL endpoints
func (s *Server) setupGraphQL() {
	// Create resolver with overseer reference
	resolver := &graph.Resolver{
		Overseer: s.overseer,
	}

	// Create GraphQL server
	srv := handler.NewDefaultServer(
		graph.NewExecutableSchema(graph.Config{
			Resolvers: resolver,
		}),
	)

	// Add WebSocket transport for subscriptions
	srv.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Configure for production
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	})

	// GraphQL endpoint
	s.router.POST("/graphql", gin.WrapH(srv))
	s.router.GET("/graphql", gin.WrapH(srv))

	// GraphQL Playground (development)
	s.router.GET("/playground", gin.WrapH(
		playground.Handler("Hydra GraphQL", "/graphql"),
	))
}

// Router returns the underlying gin router for testing purposes
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Start starts the API server
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}

	addr := fmt.Sprintf(":%d", s.config.API.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	log.Printf("🌐 Starting API server on %s", addr)

	// Start server in goroutine
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("API server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// corsMiddleware handles CORS
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
