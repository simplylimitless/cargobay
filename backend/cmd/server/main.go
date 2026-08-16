package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/anthropics/cargobay/backend/pkg/api"
	"github.com/anthropics/cargobay/backend/pkg/cache"
	"github.com/anthropics/cargobay/backend/pkg/config"
	"github.com/anthropics/cargobay/backend/pkg/database"
	"github.com/anthropics/cargobay/backend/pkg/middleware"
	"github.com/anthropics/cargobay/backend/pkg/proxy"
	"github.com/anthropics/cargobay/backend/pkg/proxy/docker"
	"github.com/anthropics/cargobay/backend/pkg/proxy/helm"
	"github.com/anthropics/cargobay/backend/pkg/proxy/maven"
	"github.com/anthropics/cargobay/backend/pkg/proxy/nuget"
	"github.com/anthropics/cargobay/backend/pkg/proxy/npm"
	"github.com/anthropics/cargobay/backend/pkg/proxy/pypi"
	"github.com/anthropics/cargobay/backend/pkg/rbac"
	"github.com/anthropics/cargobay/backend/pkg/storage"
	"github.com/anthropics/cargobay/backend/pkg/vulnerability"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

var startTime = time.Now()

func main() {
	// Load configuration
	cfg := config.LoadConfig()
	log.Printf("Starting cargobay server on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Initialize cache
	cacheClient, err := cache.New(cfg.Cache.Type, cfg.Cache.URL)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	defer cacheClient.Close()

	// Initialize database
	db := database.New(cfg.Database.DSN)
	// Enable caching for stateless performance
	db.SetCache(cacheClient)
	if err := db.Connect(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Disconnect()

	// Initialize storage
	storageAdapter, err := storage.New(cfg.Storage.Type, cfg.Storage.Config)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

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

	// Initialize RBAC manager
	rbacMgr := rbac.New(db)

	// Initialize vulnerability scanner
	scanner := vulnerability.NewScanner(db, storageAdapter, cacheClient)

	// Initialize API server
	apiServer := api.NewServer(db, rbacMgr, scanner, storageAdapter, cacheClient)

	// Create router
	r := chi.NewRouter()

	// Standard middleware
	r.Use(chimw.Recoverer)
	r.Use(chimw.Heartbeat("/health"))
	r.Use(chimw.Logger)
	r.Use(middleware.PrometheusMiddleware)

	// Routes
	r.Get("/metrics", middleware.MetricsHandler)

	// API routes (v1)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", apiServer)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Get("/admin/config", handleAdminConfig(cfg))
		})
	})

	// Legacy API routes (using proxyManager directly)
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
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
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", docker.NewDockerProxy(db, storageAdapter, cacheClient, registries))
	})

	r.Route("/pypi", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", pypi.NewPyPIProxy(db, storageAdapter, cacheClient, registries))
	})

	r.Route("/nuget", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", nuget.NewNuGetProxy(db, storageAdapter, cacheClient, registries))
	})

	r.Route("/helm", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", helm.NewHelmProxy(db, storageAdapter, cacheClient, registries))
	})

	// Server setup
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout.Std(),
		WriteTimeout: cfg.Server.WriteTimeout.Std(),
		IdleTimeout:  cfg.Server.IdleTimeout.Std(),
	}

	log.Printf("Server starting on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}

// handleAdminConfig returns the server's effective configuration (as loaded
// from config.yaml / environment variables at startup), with credentials
// redacted. Config is process-level and applied at startup only — there is
// no hot-reload path, so this endpoint is read-only.
func handleAdminConfig(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"server": map[string]interface{}{
				"port": cfg.Server.Port,
				"host": cfg.Server.Host,
			},
			"storage": map[string]interface{}{
				"type":   cfg.Storage.Type,
				"config": cfg.Storage.Config,
			},
			"database": map[string]interface{}{
				"type": cfg.Database.Type,
				"dsn":  redactDSN(cfg.Database.DSN),
			},
			"cache": map[string]interface{}{
				"type":    cfg.Cache.Type,
				"url":     redactDSN(cfg.Cache.URL),
				"ttl":     cfg.Cache.TTL.String(),
				"maxSize": cfg.Cache.MaxSize,
			},
			"registries": cfg.Registries,
			"editable":   false,
			"note":       "Configuration is loaded from config.yaml and environment variables at startup; edit those and restart the server to change it.",
		})
	}
}

// redactDSN masks the userinfo (username/password) portion of a connection
// string so secrets never reach the client. Falls back to returning the
// input unchanged if it isn't a parseable URL.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	if username := u.User.Username(); username != "" {
		u.User = url.UserPassword(username, "***")
	} else {
		u.User = url.User("***")
	}
	return u.String()
}
