package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simplylimitless/cargobay/backend/pkg/database"
)

func TestVirtualTypeForAndBaseTypeOfVirtual(t *testing.T) {
	assert.Equal(t, "maven-virtual", VirtualTypeFor("maven"))

	base, ok := BaseTypeOfVirtual("maven-virtual")
	assert.True(t, ok)
	assert.Equal(t, "maven", base)

	_, ok = BaseTypeOfVirtual("maven")
	assert.False(t, ok)

	_, ok = BaseTypeOfVirtual(VirtualSuffix)
	assert.False(t, ok, "a bare suffix with no base type isn't a valid virtual type")
}

func TestIsVirtualRegistry(t *testing.T) {
	assert.False(t, IsVirtualRegistry(nil, "maven"))
	assert.False(t, IsVirtualRegistry(&database.RegistryConfig{Type: "maven"}, "maven"))
	assert.True(t, IsVirtualRegistry(&database.RegistryConfig{Type: "maven-virtual"}, "maven"))
	assert.False(t, IsVirtualRegistry(&database.RegistryConfig{Type: "npm-virtual"}, "maven"), "must not match a different member type's virtual")
}

func TestFetchFirstUpstreamStreamFirstCandidateSucceeds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("artifact bytes"))
	}))
	defer upstream.Close()

	cand := &database.RegistryConfig{ID: "reg1", URL: upstream.URL}
	resp, gotCand, err := FetchFirstUpstreamStream([]*database.RegistryConfig{cand}, func(c *database.RegistryConfig) (string, error) {
		return c.URL + "/artifact", nil
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	assert.Same(t, cand, gotCand)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "artifact bytes", string(body))
}

func TestFetchFirstUpstreamStreamFallsBackToSecondCandidate(t *testing.T) {
	badUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer badUpstream.Close()

	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("from the second candidate"))
	}))
	defer goodUpstream.Close()

	bad := &database.RegistryConfig{ID: "bad", URL: badUpstream.URL}
	good := &database.RegistryConfig{ID: "good", URL: goodUpstream.URL}

	resp, gotCand, err := FetchFirstUpstreamStream([]*database.RegistryConfig{bad, good}, func(c *database.RegistryConfig) (string, error) {
		return c.URL + "/artifact", nil
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	assert.Same(t, good, gotCand)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "from the second candidate", string(body))
}

func TestFetchFirstUpstreamStreamAllCandidatesFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	candidates := []*database.RegistryConfig{
		{ID: "reg1", URL: upstream.URL},
		{ID: "reg2", URL: upstream.URL},
	}

	resp, gotCand, err := FetchFirstUpstreamStream(candidates, func(c *database.RegistryConfig) (string, error) {
		return c.URL + "/artifact", nil
	})
	assert.Nil(t, resp)
	assert.Nil(t, gotCand)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reg2", "the error should name the last candidate that failed")
}

func TestFetchFirstUpstreamStreamNoCandidates(t *testing.T) {
	resp, gotCand, err := FetchFirstUpstreamStream(nil, func(c *database.RegistryConfig) (string, error) {
		return "", nil
	})
	assert.Nil(t, resp)
	assert.Nil(t, gotCand)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no upstream proxy configured")
}

func TestFetchFirstUpstreamStreamBuildURLErrorSkipsCandidate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	broken := &database.RegistryConfig{ID: "broken"}
	fine := &database.RegistryConfig{ID: "fine", URL: upstream.URL}

	resp, gotCand, err := FetchFirstUpstreamStream([]*database.RegistryConfig{broken, fine}, func(c *database.RegistryConfig) (string, error) {
		if c.ID == "broken" {
			return "", fmt.Errorf("cannot build URL for %s", c.ID)
		}
		return c.URL + "/artifact", nil
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	assert.Same(t, fine, gotCand)
}

func TestFetchFirstUpstreamStreamAppliesUpstreamAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cand := &database.RegistryConfig{
		ID:               "reg1",
		URL:              upstream.URL,
		UpstreamAuthType: "bearer",
		UpstreamSecret:   "s3cr3t",
	}

	resp, _, err := FetchFirstUpstreamStream([]*database.RegistryConfig{cand}, func(c *database.RegistryConfig) (string, error) {
		return c.URL + "/artifact", nil
	})
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer s3cr3t", gotAuth)
}
