package api

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/ilkerispir/terrakubed/internal/api/database"
	"github.com/ilkerispir/terrakubed/internal/api/handler"
	"github.com/ilkerispir/terrakubed/internal/api/middleware"
	"github.com/ilkerispir/terrakubed/internal/api/registry"
	"github.com/ilkerispir/terrakubed/internal/api/repository"
	"github.com/ilkerispir/terrakubed/internal/storage"
)

// Config holds configuration for the API server.
type Config struct {
	DatabaseURL    string
	Port           int
	Hostname       string
	DexIssuerURI   string
	PatSecret      string
	InternalSecret string
	OwnerGroup     string
	UIURL          string
	StorageType    string
}

// Server is the main API server.
type Server struct {
	config  Config
	db      *database.Pool
	repo    *repository.GenericRepository
	handler http.Handler
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

	// Validate model columns against actual DB schema
	repo.ValidateColumns(ctx)

	// Create JSON:API handler
	jsonapiHandler := handler.NewJSONAPIHandler(repo)

	// Create custom handlers
	logsHandler := handler.NewLogsHandler(repo)
	outputHandler := handler.NewTerraformOutputHandler(repo)
	contextHandler := handler.NewContextHandler(repo)

	// Create storage service
	storageService, err := storage.NewStorageService(config.StorageType)
	if err != nil {
		log.Printf("Warning: storage service not available (%v), using nop", err)
		storageService = &storage.NopStorageService{}
	}

	// State & TFE handlers
	stateHandler := handler.NewTerraformStateHandler(db.Pool, config.Hostname, storageService)
	tfeHandler := handler.NewRemoteTFEHandler(db.Pool, config.Hostname)
	wellKnownHandler := handler.NewWellKnownHandler(config.Hostname)

	// Set up routes
	mux := http.NewServeMux()

	// GraphQL endpoint (Elide-compatible, used by the UI)
	graphqlHandler := handler.NewGraphQLHandler(repo)
	mux.Handle("/graphql/api/v1", graphqlHandler)

	// JSON:API CRUD endpoints
	mux.Handle("/api/v1/", jsonapiHandler)

	// Custom endpoints
	mux.HandleFunc("/logs/", logsHandler.AppendLogs)
	mux.HandleFunc("/tfoutput/v1/", outputHandler.GetOutput)
	mux.HandleFunc("/context/v1/", contextHandler.GetContext)

	// Token management endpoints (PAT + Team tokens)
	patHandler := handler.NewPatHandler(db.Pool, config.PatSecret)
	teamTokenHandler := handler.NewTeamTokenHandler(db.Pool, config.PatSecret, config.OwnerGroup)
	mux.Handle("/pat/v1", patHandler)
	mux.Handle("/pat/v1/", patHandler)
	mux.Handle("/access-token/v1/teams", teamTokenHandler)
	mux.Handle("/access-token/v1/teams/", teamTokenHandler)

	// State & TFE endpoints
	mux.Handle("/tfstate/v1/", stateHandler)
	mux.Handle("/remote/tfe/v2/", tfeHandler)
	mux.Handle("/.well-known/terraform.json", wellKnownHandler)

	// Health check — compatible with Spring Boot actuator probes
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"UP"}`))
	}
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/actuator/health", healthHandler)
	mux.HandleFunc("/actuator/health/readiness", healthHandler)
	mux.HandleFunc("/actuator/health/liveness", healthHandler)

	// Apply middleware chain: CORS → Auth → Router
	authConfig := middleware.AuthConfig{
		DexIssuerURI:   config.DexIssuerURI,
		PatSecret:      config.PatSecret,
		InternalSecret: config.InternalSecret,
		OwnerGroup:     config.OwnerGroup,
		UIURL:          config.UIURL,
	}

	var finalHandler http.Handler = mux
	finalHandler = middleware.AuthMiddleware(authConfig)(finalHandler)
	finalHandler = middleware.CORSMiddleware(config.UIURL)(finalHandler)

	return &Server{
		config:  config,
		db:      db,
		repo:    repo,
		handler: finalHandler,
	}, nil
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	log.Printf("API server starting on %s", addr)
	return http.ListenAndServe(addr, s.handler)
}

// Close closes the server and its resources.
func (s *Server) Close() {
	if s.db != nil {
		s.db.Close()
	}
}
