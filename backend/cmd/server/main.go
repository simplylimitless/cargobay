package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/api"
	"github.com/simplylimitless/cargobay/backend/pkg/backup"
	"github.com/simplylimitless/cargobay/backend/pkg/cache"
	"github.com/simplylimitless/cargobay/backend/pkg/config"
	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/alpine"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/cargo"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/cocoapods"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/composer"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/conan"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/conda"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/debian"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/docker"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/gomod"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/helm"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/maven"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/npm"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/nuget"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/pub"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/pypi"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/rpm"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/swift"
	"github.com/simplylimitless/cargobay/backend/pkg/proxy/terraform"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
	"github.com/simplylimitless/cargobay/backend/pkg/searchindex"
	"github.com/simplylimitless/cargobay/backend/pkg/storage"
	"github.com/simplylimitless/cargobay/backend/pkg/vulnerability"
	"github.com/simplylimitless/cargobay/backend/pkg/webui"
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

	// trackedStorage wraps storageAdapter to record bandwidth saved (bytes
	// served from local storage instead of an upstream fetch) for the Stats
	// page. Used everywhere artifacts are actually served to a client;
	// scanning/readiness checks keep the untracked adapter since those reads
	// don't represent bandwidth a client would otherwise have consumed.
	trackedStorage := storage.NewTrackingAdapter(storageAdapter, func(bytesServed int64) {
		if err := db.IncrementBandwidthSaved(bytesServed); err != nil {
			log.Printf("failed to record bandwidth saved: %v", err)
		}
	})

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
	proxyManager := proxy.New(db, trackedStorage, cacheClient, registries)

	// Initialize RBAC manager
	rbacMgr := rbac.New(db)

	// Initialize vulnerability scanner. trivyServerAddr/registryAddr point it
	// at the trivy server and cargobay's own Docker Registry v2 API so it can
	// run real Docker/OCI scans (see docker-compose.yml's trivy/backend services).
	trivyServerAddr := os.Getenv("TRIVY_SERVER_ADDR")
	if trivyServerAddr == "" {
		trivyServerAddr = "http://trivy:4954"
	}
	registryAddr := os.Getenv("REGISTRY_ADDR")
	if registryAddr == "" {
		registryAddr = "backend:4500"
	}
	scanner := vulnerability.NewScanner(db, storageAdapter, cacheClient, trivyServerAddr, registryAddr)

	// Initialize vulnerability DB updater. The cache dir must match the
	// volume mounted into the `trivy` server container (./data/app/trivy-cache
	// in docker-compose.yml) so both share the same downloaded DB.
	trivyCacheDir := os.Getenv("TRIVY_CACHE_DIR")
	if trivyCacheDir == "" {
		trivyCacheDir = "/app/trivy-cache"
	}
	vulnDBUpdater := vulnerability.NewDBUpdater(db, trivyCacheDir)

	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	defer cancelScheduler()
	go vulnDBUpdater.StartScheduler(schedulerCtx)

	// Initialize search index reindexer
	searchIndexReindexer := searchindex.NewReindexer(db)
	go searchIndexReindexer.StartScheduler(schedulerCtx)

	// Initialize vulnerability rescanner (periodic sweep of already-cached
	// docker/oci artifacts, complementing the scan-on-cache trigger in the
	// docker proxy which only fires once per push/pull).
	vulnRescanner := vulnerability.NewRescanner(db, scanner)
	go vulnRescanner.StartScheduler(schedulerCtx)

	// Initialize backup service (full logical DB dump/restore). defaultStorage
	// is used unless an admin has configured a dedicated backup destination
	// via Settings (backup_settings.storage_type/storage_config in the DB,
	// re-read on every backup/restore/list call — see backup.Backup.resolveStorage).
	backupSvc := backup.New(db, trackedStorage)
	go backupSvc.StartScheduler(schedulerCtx)

	// Initialize API server
	apiServer := api.NewServer(db, rbacMgr, scanner, trackedStorage, cacheClient, vulnDBUpdater, searchIndexReindexer, vulnRescanner, backupSvc)

	// Create router
	r := chi.NewRouter()

	// Standard middleware
	r.Use(middleware.NormalizePath)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Heartbeat("/health"))
	r.Use(chimw.Logger)
	r.Use(middleware.PrometheusMiddleware)

	middleware.RegisterCacheStatsProvider(func() (int64, int64, int64) {
		stats, _ := cacheClient.Stats()
		return stats.Hits, stats.Misses, stats.Errors
	})

	// Routes
	r.Get("/metrics", middleware.MetricsHandler)
	// /health (above) is a bare liveness ping; /healthz actually verifies
	// the database, cache, and storage backend are reachable.
	r.Get("/healthz", middleware.NewReadinessHandler(db, cacheClient, storageAdapter))

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
		r.Get("/artifacts/{namespace}/{artifactName}", proxyManager.GetArtifactInfo)
		r.Get("/artifacts/{namespace}/{artifactName}/{version}/download", proxyManager.DownloadArtifact)
	})

	// Proxy routes
	r.Route("/npm", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", npm.NewNPMProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/maven", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", maven.NewMavenProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	// Gradle and SBT both consume standard Maven-layout repositories — same
	// proxy implementation, distinct registry Type labels for resolution/RBAC.
	r.Route("/gradle", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", maven.NewMavenAliasProxy(db, trackedStorage, cacheClient, rbacMgr, registries, "gradle"))
	})

	r.Route("/sbt", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", maven.NewMavenAliasProxy(db, trackedStorage, cacheClient, rbacMgr, registries, "sbt"))
	})

	// Docker/OCI clients always request /v2/... — mounted like every other
	// proxy type, under its own prefix on the single shared port.
	r.Route("/v2", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", docker.NewDockerProxy(db, trackedStorage, cacheClient, rbacMgr, registries, scanner))
	})

	r.Route("/pypi", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", pypi.NewPyPIProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/nuget", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", nuget.NewNuGetProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/helm", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", helm.NewHelmProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/cargo", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", cargo.NewCargoProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/go", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", gomod.NewGoModProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/alpine", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", alpine.NewAlpineProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/debian", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", debian.NewDebianProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/rpm", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", rpm.NewRPMProxy(db, trackedStorage, cacheClient, rbacMgr, registries, "rpm"))
	})

	r.Route("/yum", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", rpm.NewRPMProxy(db, trackedStorage, cacheClient, rbacMgr, registries, "yum"))
	})

	r.Route("/conan", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", conan.NewConanProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/cocoapods", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", cocoapods.NewCocoaPodsProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/swift", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", swift.NewSwiftProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/dart", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", pub.NewPubProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/terraform", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", terraform.NewTerraformProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/composer", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", composer.NewComposerProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	r.Route("/conda", func(r chi.Router) {
		r.Use(middleware.NewAuthMiddleware(db, rbacMgr))
		r.Mount("/", conda.NewCondaProxy(db, trackedStorage, cacheClient, rbacMgr, registries))
	})

	// Frontend UI — serves the embedded React build for everything not
	// claimed by a route above, including client-side routes that need the
	// index.html SPA fallback.
	webUIHandler := webui.Handler()
	r.Get("/", webUIHandler.ServeHTTP)
	r.NotFound(webUIHandler.ServeHTTP)

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
