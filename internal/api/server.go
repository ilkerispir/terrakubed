package api

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/ilkerispir/terrakubed/internal/api/database"
	"github.com/ilkerispir/terrakubed/internal/api/handler"
	"github.com/ilkerispir/terrakubed/internal/api/registry"
	"github.com/ilkerispir/terrakubed/internal/api/repository"
)

// Config holds configuration for the API server.
type Config struct {
	DatabaseURL string
	Port        int
}

// Server is the main API server.
type Server struct {
	config Config
	db     *database.Pool
	repo   *repository.GenericRepository
	mux    *http.ServeMux
}

// NewServer creates a new API server.
func NewServer(config Config) (*Server, error) {
	ctx := context.Background()

	// Connect to database
	db, err := database.New(ctx, config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create repository and register all resource types
	repo := repository.NewGenericRepository(db.Pool)
	registry.RegisterAll(repo)

	// Create JSON:API handler
	jsonapiHandler := handler.NewJSONAPIHandler(repo)

	// Create custom handlers
	logsHandler := handler.NewLogsHandler(repo)
	outputHandler := handler.NewTerraformOutputHandler(repo)
	contextHandler := handler.NewContextHandler(repo)

	// Set up routes
	mux := http.NewServeMux()

	// JSON:API CRUD endpoints
	mux.Handle("/api/v1/", jsonapiHandler)

	// Custom endpoints
	mux.HandleFunc("/logs/", logsHandler.AppendLogs)
	mux.HandleFunc("/tfoutput/v1/", outputHandler.GetOutput)
	mux.HandleFunc("/context/v1/", contextHandler.GetContext)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	return &Server{
		config: config,
		db:     db,
		repo:   repo,
		mux:    mux,
	}, nil
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	log.Printf("API server starting on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

// Close closes the server and its resources.
func (s *Server) Close() {
	if s.db != nil {
		s.db.Close()
	}
}
