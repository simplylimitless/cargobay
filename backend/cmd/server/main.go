package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/anthropics/cargobay/pkg/cache"
	"github.com/anthropics/cargobay/pkg/config"
	"github.com/anthropics/cargobay/pkg/database"
	"github.com/anthropics/cargobay/pkg/middleware"
	"github.com/anthropics/cargobay/pkg/proxy"
	"github.com/anthropics/cargobay/pkg/proxy/docker"
	"github.com/anthropics/cargobay/pkg/proxy/maven"
	"github.com/anthropics/cargobay/pkg/proxy/npm"
	"github.com/anthropics/cargobay/pkg/storage"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

var startTime = time.Now()

func main() {
	// Load configuration
	cfg := config.LoadConfig()
	log.Printf("Starting cargobay server on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Initialize database
	db := database.New(cfg.Database.DSN)
	if err := db.Connect(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Disconnect()

	// Initialize storage
	storageAdapter, err := storage.New(cfg.Storage.Type, cfg.Storage.Config)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Initialize cache
	cacheClient, err := cache.New(cfg.Cache.Type, cfg.Cache.URL)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cacheClient.Close()

	// Convert registry config
	registries := make([]database.RegistryConfig, len(cfg.Registries))
	for i, reg := range cfg.Registries {
		registries[i] = database.RegistryConfig{
			ID:       reg.ID,
			Name:     reg.Name,
			URL:      reg.URL,
			Type:     reg.Type,
			Proxy:    reg.Proxy,
			Enabled:  reg.Enabled,
			Priority: reg.Priority,
		}
	}

	// Initialize proxy manager
	proxyManager := proxy.New(db, storageAdapter, cacheClient, registries)

	// Create router
	r := chi.NewRouter()

	// Standard middleware
	r.Use(chimw.Recoverer)
	r.Use(chimw.Heartbeat("/health"))
	r.Use(chimw.Logger)
	r.Use(middleware.PrometheusMiddleware)

	// Routes
	r.Get("/metrics", middleware.MetricsHandler)

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.WithUser)
		r.Get("/registries", proxyManager.ListRegistries)
		r.Get("/search", proxyManager.SearchArtifacts)
		r.Get("/search/upstream", proxyManager.SearchUpstream)
		r.Get("/artifacts/{namespace}/{artifactName}", proxyManager.GetArtifactInfo)
		r.Get("/artifacts/{namespace}/{artifactName}/{version}/download", proxyManager.DownloadArtifact)
	})

	// Proxy routes
	r.Route("/npm", func(r chi.Router) {
		r.Mount("/", npm.NewNPMProxy(db, storageAdapter, cacheClient, registries))
	})

	r.Route("/maven", func(r chi.Router) {
		r.Mount("/", maven.NewMavenProxy(db, storageAdapter, cacheClient, registries))
	})

	r.Route("/docker", func(r chi.Router) {
		r.Use(middleware.WithUser)
		r.Mount("/", docker.NewDockerProxy(db, storageAdapter, cacheClient, registries))
	})

	// Server setup
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	log.Printf("Server starting on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
