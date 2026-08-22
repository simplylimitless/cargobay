package proxy

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
)

// HostOnly strips a ":port" suffix (and lowercases) from an HTTP Host
// header, so registry-host lookups aren't thrown off by the port a client
// happened to connect on.
func HostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

// ResolveRegistry finds the target registry for a protocol-proxy request.
// If requestHost matches a registry's bound host, that registry is the
// explicit target — this is how downstream clients address a specific
// (often private) registry with zero change to repo/package paths: they
// just point at that registry's own hostname. Otherwise, falls back to
// the highest-priority enabled, non-private, proxy-enabled registry of
// artifactType. Both paths read live from the DB, so Settings edits take
// effect immediately — the registries param is unused for resolution but
// kept for the constructor call sites that still pass it. Returns nil if
// nothing matches (e.g. before any registry has been configured, or the
// matched registry has proxying disabled).
func ResolveRegistry(db *database.Database, registries []database.RegistryConfig, requestHost, artifactType string) *database.RegistryConfig {
	if host := HostOnly(requestHost); host != "" {
		if reg, err := db.GetRegistryByHost(host); err == nil && reg != nil && reg.Type == artifactType {
			return reg
		}
	}

	if reg, err := db.GetDefaultRegistry(artifactType); err == nil && reg != nil {
		return reg
	}
	return nil
}

// CheckAccess enforces CanReadRegistry/CanPublishRegistry for a protocol
// request. On denial it writes the appropriate status (401 for anonymous,
// 403 for an authenticated-but-unauthorized user) and returns false — the
// caller should stop handling the request in that case.
func CheckAccess(w http.ResponseWriter, rbacMgr *rbac.RBAC, user *middleware.User, reg *database.RegistryConfig, requirePublish bool) bool {
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	allowed := false
	if requirePublish {
		allowed = rbacMgr.CanPublishRegistry(userID, reg)
	} else {
		allowed = rbacMgr.CanReadRegistry(userID, reg)
	}
	if allowed {
		return true
	}

	status := http.StatusForbidden
	if userID == "" {
		status = http.StatusUnauthorized
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": "access denied for this registry"})
	return false
}

// Target bundles the registry resolved for a request (by Host header, or
// the default public proxy) with the label used to namespace its stored
// artifacts — every protocol proxy uses this same shape.
type Target struct {
	Reg   *database.RegistryConfig
	Label string
}

type targetCtxKey struct{}

// RequireReadAccess is chi middleware for read-only protocol proxies (npm,
// maven, pypi, nuget, helm): it resolves the registry addressed by the
// request's Host header (falling back to the default public proxy for
// artifactType), enforces read access, and stores the resolved Target in
// the request context for handlers to retrieve via TargetFromContext. On
// denial it writes 401/403 and the request never reaches the handler.
func RequireReadAccess(db *database.Database, rbacMgr *rbac.RBAC, registries []database.RegistryConfig, artifactType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reg := ResolveRegistry(db, registries, r.Host, artifactType)
			if !CheckAccess(w, rbacMgr, middleware.GetUser(r), reg, false) {
				return
			}
			label := artifactType
			if reg != nil {
				label = reg.ID
			}
			ctx := context.WithValue(r.Context(), targetCtxKey{}, Target{Reg: reg, Label: label})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TargetFromContext retrieves the Target stored by RequireReadAccess. Falls
// back to the bare artifact-type label (today's default behavior) if called
// outside that middleware.
func TargetFromContext(r *http.Request, artifactType string) Target {
	if t, ok := r.Context().Value(targetCtxKey{}).(Target); ok {
		return t
	}
	return Target{Label: artifactType}
}

// ApplyUpstreamAuth attaches the credentials configured for reg (if any) to
// an outgoing upstream request, using the standard scheme for each of the
// two supported auth types: HTTP Basic (the convention for private Maven,
// PyPI, and Docker-Hub-style registries) or a static Bearer token (the
// convention for npm, GitHub Packages, and other token-based registries,
// including another private cargobay instance configured with a bearer
// token). No-op if reg is nil or has no upstream auth configured.
func ApplyUpstreamAuth(req *http.Request, reg *database.RegistryConfig) {
	if reg == nil {
		return
	}
	switch reg.UpstreamAuthType {
	case "basic":
		if reg.UpstreamUsername != "" || reg.UpstreamSecret != "" {
			req.SetBasicAuth(reg.UpstreamUsername, reg.UpstreamSecret)
		}
	case "bearer":
		if reg.UpstreamSecret != "" {
			req.Header.Set("Authorization", "Bearer "+reg.UpstreamSecret)
		}
	}
}
