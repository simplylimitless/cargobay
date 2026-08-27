import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { getRegistryLogo, REGISTRY_EMOJI } from '../lib/registryLogos'
import { useAuth } from '../context/AuthContext'

interface Registry {
  id: string
  name: string
  url: string
  type: string
  enabled: boolean
  priority: number
  private: boolean
  proxy: boolean
}

export function RegistryDetail() {
  const { registryId } = useParams<{ registryId: string }>()
  const navigate = useNavigate()
  const { token } = useAuth()
  const [registry, setRegistry] = useState<Registry | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [logoFailed, setLogoFailed] = useState(false)
  const [prefetchRef, setPrefetchRef] = useState('')
  const [prefetching, setPrefetching] = useState(false)
  const [prefetchResult, setPrefetchResult] = useState<{ ok: boolean; message: string } | null>(null)

  useEffect(() => {
    setLoading(true)
    fetch(`/api/v1/registries/${encodeURIComponent(registryId!)}`)
      .then((res) => {
        if (!res.ok) throw new Error(res.status === 404 ? 'Registry not found' : `Request failed: ${res.status}`)
        return res.json() as Promise<Registry>
      })
      .then((data) => {
        setRegistry(data)
        setError(null)
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [registryId])

  const getArtifactTypes = (registryType: string) => {
    switch (registryType) {
      case 'docker':
        return [
          { id: 'docker', name: 'Container Images', icon: '🐳', desc: 'Browse Docker images' },
          { id: 'helm', name: 'Helm Charts', icon: '⚓', desc: 'Kubernetes charts' },
        ]
      case 'npm':
        return [{ id: 'npm', name: 'NPM Packages', icon: '📦', desc: 'Node.js packages' }]
      case 'maven':
        return [{ id: 'maven', name: 'Maven Artifacts', icon: '☕', desc: 'Java libraries' }]
      case 'pypi':
        return [{ id: 'pypi', name: 'Python Packages', icon: '🐍', desc: 'PyPI packages' }]
      case 'nuget':
        return [{ id: 'nuget', name: 'NuGet Packages', icon: '🔨', desc: '.NET packages' }]
      case 'helm':
        return [{ id: 'helm', name: 'Helm Charts', icon: '⚓', desc: 'Kubernetes charts' }]
      default:
        return [{ id: registryType, name: 'Artifacts', icon: '📦', desc: 'Browse artifacts' }]
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  if (error || !registry) {
    return (
      <div className="text-center py-12">
        <div className="text-red-500 mb-4">
          <svg className="w-16 h-16 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <h2 className="text-2xl font-bold text-gray-200 mb-2">Registry Not Found</h2>
        <p className="text-gray-500">{error || `The registry "${registryId}" does not exist.`}</p>
      </div>
    )
  }

  const artifactTypes = getArtifactTypes(registry.type)
  const logo = logoFailed ? null : getRegistryLogo(registry)

  const handlePrefetch = async () => {
    const reference = prefetchRef.trim()
    if (!reference || prefetching) return
    if (!token) {
      setPrefetchResult({ ok: false, message: 'Please log in to force-cache artifacts.' })
      return
    }

    setPrefetching(true)
    setPrefetchResult(null)
    try {
      const res = await fetch(`/api/v1/registries/${encodeURIComponent(registryId!)}/prefetch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ reference }),
      })
      const text = await res.text()
      let data: { error?: string; alreadyCached?: boolean } = {}
      try {
        data = text ? JSON.parse(text) : {}
      } catch {
        // Non-JSON body (e.g. a plain-text 401 from auth middleware) — fall through
        // to the raw text/status below instead of throwing.
      }
      if (!res.ok) {
        setPrefetchResult({
          ok: false,
          message: res.status === 401 ? 'Please log in to force-cache artifacts.' : data.error || text || `Request failed: ${res.status}`,
        })
        return
      }
      setPrefetchResult({
        ok: true,
        message: data.alreadyCached
          ? `${reference} is already cached.`
          : `${reference} fetched from upstream and cached.`,
      })
      setPrefetchRef('')
    } catch (err) {
      setPrefetchResult({ ok: false, message: err instanceof Error ? err.message : 'Prefetch failed' })
    } finally {
      setPrefetching(false)
    }
  }

  return (
    <div className="space-y-8">
      <div className="card">
        <div className="flex items-start gap-6">
          {logo ? (
            <div className="w-20 h-20 bg-white rounded-2xl flex items-center justify-center flex-shrink-0 p-3 shadow-lg shadow-blue-600/20">
              <img
                src={logo}
                alt={`${registry.name} logo`}
                className="w-full h-full object-contain"
                onError={() => setLogoFailed(true)}
              />
            </div>
          ) : REGISTRY_EMOJI[registry.type] ? (
            <div className="w-20 h-20 bg-gray-800 rounded-2xl flex items-center justify-center flex-shrink-0 text-5xl shadow-lg shadow-blue-600/20">
              {REGISTRY_EMOJI[registry.type]}
            </div>
          ) : (
            <div className="w-20 h-20 bg-gradient-to-br from-blue-600 to-purple-600 rounded-2xl flex items-center justify-center flex-shrink-0 text-5xl shadow-lg shadow-blue-600/20">
              {registry.name.charAt(0)}
            </div>
          )}
          <div className="flex-1">
            <div className="flex flex-wrap items-center gap-3 mb-2">
              <h1 className="text-3xl font-bold text-white">{registry.name}</h1>
              <span
                className={`px-3 py-1 text-xs rounded-full border ${
                  registry.enabled
                    ? 'bg-green-500/20 text-green-400 border-green-500/30'
                    : 'bg-gray-500/20 text-gray-400 border-gray-500/30'
                }`}
              >
                {registry.enabled ? 'enabled' : 'disabled'}
              </span>
              <span
                className={`px-3 py-1 text-xs rounded-full border ${
                  registry.private
                    ? 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30'
                    : 'bg-blue-500/20 text-blue-400 border-blue-500/30'
                }`}
              >
                {registry.private ? 'private' : 'public'}
              </span>
              {registry.proxy && (
                <span className="px-3 py-1 text-xs rounded-full border bg-purple-500/20 text-purple-400 border-purple-500/30">
                  proxy
                </span>
              )}
            </div>
            <p className="text-gray-400 font-mono text-lg mb-4">{registry.url}</p>
            <div className="flex flex-wrap gap-3">
              <span className="px-4 py-2 bg-blue-600/20 border border-blue-500/30 rounded-lg text-blue-400 font-medium">
                Type: {registry.type}
              </span>
              <span className="px-4 py-2 bg-gray-800 rounded-lg text-gray-300">
                {artifactTypes.length} Artifact Types Available
              </span>
              {registry.proxy && (
                <span className="px-4 py-2 bg-gray-800 rounded-lg text-gray-300">
                  Pull-through cache from upstream, fetched on demand
                </span>
              )}
            </div>
          </div>
        </div>
      </div>

      {registry.type === 'docker' && registry.proxy && (
        <div className="card">
          <h2 className="text-lg font-semibold text-gray-100 mb-2">Force Cache an Artifact</h2>
          <p className="text-sm text-gray-500 mb-4">
            Not cached yet? Enter an image reference (e.g. <code className="text-gray-400">ubuntu:latest</code>) to
            fetch it from upstream right now.
          </p>
          <div className="flex flex-col sm:flex-row gap-3">
            <input
              type="text"
              value={prefetchRef}
              onChange={(e) => setPrefetchRef(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handlePrefetch()}
              placeholder="ubuntu:latest"
              className="flex-1 px-4 py-2 bg-gray-800 border border-gray-700 rounded-lg text-gray-100 font-mono placeholder-gray-600 focus:outline-none focus:border-blue-500"
              disabled={prefetching}
            />
            <button
              onClick={handlePrefetch}
              disabled={prefetching || !prefetchRef.trim()}
              className="px-6 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-lg font-medium transition-colors"
            >
              {prefetching ? 'Fetching...' : 'Fetch & Cache'}
            </button>
          </div>
          {prefetchResult && (
            <div
              className={`mt-3 px-4 py-2 rounded-lg text-sm ${
                prefetchResult.ok
                  ? 'bg-green-500/10 border border-green-500/30 text-green-400'
                  : 'bg-red-500/10 border border-red-500/30 text-red-400'
              }`}
            >
              {prefetchResult.message}
            </div>
          )}
        </div>
      )}

      <div>
        <h2 className="text-2xl font-semibold text-gray-100 mb-6">Available Artifact Types</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {artifactTypes.map((artifact) => (
            <div
              key={artifact.id}
              onClick={() => navigate(`/registries/${registryId}/${artifact.id}`)}
              className="card cursor-pointer hover:border-blue-500 hover:shadow-lg hover:shadow-blue-500/10 transition-all group"
            >
              <div className="flex items-start gap-4">
                <div className="w-14 h-14 bg-gray-800 rounded-xl flex items-center justify-center text-3xl group-hover:bg-blue-600/20 group-hover:text-blue-400 transition-colors">
                  {artifact.icon}
                </div>
                <div className="flex-1">
                  <h3 className="text-lg font-medium text-gray-100 mb-1">{artifact.name}</h3>
                  <p className="text-sm text-gray-500">{artifact.desc}</p>
                  <div className="mt-3 flex items-center text-sm text-blue-400 font-medium group-hover:translate-x-1 transition-transform">
                    <span>Explore</span>
                    <svg className="w-4 h-4 ml-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
