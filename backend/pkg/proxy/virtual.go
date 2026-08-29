package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
	"github.com/simplylimitless/cargobay/backend/pkg/middleware"
	"github.com/simplylimitless/cargobay/backend/pkg/rbac"
)

// VirtualSuffix is the naming convention for an aggregating "virtual"
// registry type: a registry of type "<memberType>-virtual" has no upstream
// URL of its own and instead resolves artifacts by trying its Members (all
// of type memberType) in order, Artifactory-style. Any protocol proxy can
// opt into this by using ResolveMemberCandidates/FetchFirstUpstream below.
const VirtualSuffix = "-virtual"

// VirtualTypeFor returns the virtual registry type for a base protocol type,
// e.g. "maven" -> "maven-virtual".
func VirtualTypeFor(memberType string) string {
	return memberType + VirtualSuffix
}

// BaseTypeOfVirtual returns the member type a virtual registry type
// aggregates and true, e.g. "maven-virtual" -> ("maven", true). Returns
// ("", false) if registryType isn't a virtual type.
func BaseTypeOfVirtual(registryType string) (string, bool) {
	base, ok := strings.CutSuffix(registryType, VirtualSuffix)
	if !ok || base == "" {
		return "", false
	}
	return base, true
}

// IsVirtualRegistry reports whether reg is a "<memberType>-virtual"
// aggregator for memberType specifically.
func IsVirtualRegistry(reg *database.RegistryConfig, memberType string) bool {
	return reg != nil && reg.Type == VirtualTypeFor(memberType)
}

// ResolveMemberCandidates returns the ordered registries to try upstream for
// reg. A plain registry (not a "<memberType>-virtual" aggregator) is just
// itself. A virtual registry has no upstream URL of its own -- it resolves
// each of its Members by ID, skipping any that are missing, disabled, not of
// memberType (no nested virtuals), or unreadable by user -- so a public
// virtual repo can't be used to route around a private member's own access
// grants.
func ResolveMemberCandidates(db *database.Database, rbacMgr *rbac.RBAC, user *middleware.User, reg *database.RegistryConfig, memberType string) []*database.RegistryConfig {
	if reg == nil {
		return nil
	}
	if !IsVirtualRegistry(reg, memberType) {
		return []*database.RegistryConfig{reg}
	}

	candidates := make([]*database.RegistryConfig, 0, len(reg.Members))
	for _, memberID := range reg.Members {
		member, err := db.GetRegistry(memberID)
		if err != nil || member == nil || !member.Enabled || member.Type != memberType {
			continue
		}
		if !rbacMgr.CanReadRegistry(user, member) {
			continue
		}
		candidates = append(candidates, member)
	}
	return candidates
}

// FetchFirstUpstream tries each candidate in order, building the request URL
// via buildURL and applying the candidate's own upstream auth, and returns
// the first successful (HTTP 200) response body along with the candidate
// that answered (useful for a follow-up request, e.g. npm's tarball fetch,
// that must reuse the same member's credentials/host). Returns an error
// naming the last failure if every candidate fails or none are eligible.
func FetchFirstUpstream(candidates []*database.RegistryConfig, buildURL func(*database.RegistryConfig) (string, error)) ([]byte, *database.RegistryConfig, error) {
	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("no upstream proxy configured for this registry")
	}

	var lastErr error
	for _, cand := range candidates {
		upstreamURL, err := buildURL(cand)
		if err != nil {
			lastErr = err
			continue
		}
		req, err := http.NewRequest(http.MethodGet, upstreamURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		ApplyUpstreamAuth(req, cand)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream %s returned status %d", cand.ID, resp.StatusCode)
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return data, cand, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no upstream candidate available")
	}
	return nil, nil, lastErr
}
