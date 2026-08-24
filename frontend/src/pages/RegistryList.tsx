import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { getRegistryLogo, REGISTRY_EMOJI } from '../lib/registryLogos'

interface Registry {
  id: string
  name: string
  url: string
  type: string
  enabled: boolean
  priority: number
  private: boolean
}

// A logo image that falls back to the classic emoji icon (and then 📁) on
// load failure — kept as its own component so each card tracks its own
// failed-load state independently.
function RegistryIcon({ registry }: { registry: Registry }) {
  const [failed, setFailed] = useState(false)
  const logo = failed ? null : getRegistryLogo(registry)

  if (logo) {
    return (
      <div className="w-14 h-14 bg-white rounded-xl flex items-center justify-center p-2.5 group-hover:ring-2 group-hover:ring-blue-500/50 transition-all">
        <img
          src={logo}
          alt={`${registry.name} logo`}
          className="w-full h-full object-contain"
          onError={() => setFailed(true)}
        />
      </div>
    )
  }

  return (
    <div className="w-14 h-14 bg-gray-800 rounded-xl flex items-center justify-center text-3xl group-hover:bg-blue-600/20 group-hover:text-blue-400 transition-colors">
      {REGISTRY_EMOJI[registry.type] || '📁'}
    </div>
  )
}

export function RegistryList() {
  const navigate = useNavigate()
  const [registries, setRegistries] = useState<Registry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/v1/registries')
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => setRegistries(data.registries || []))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [])

  const handleRegistryClick = (registryId: string) => {
    navigate(`/registries/${registryId}`)
  }

  return (
    <div className="space-y-10">
      {/* Hero Section */}
      <div className="text-center py-12">
        <h1 className="text-4xl md:text-5xl font-bold text-white mb-4 tracking-tight">
          Universal Artifact Registry
        </h1>
        <p className="text-xl text-gray-400 max-w-2xl mx-auto">
          Browse, search, and cache container images, npm packages, Maven artifacts, and more from a single interface.
        </p>
      </div>

      {/* Registry Grid */}
      <div>
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-2xl font-semibold text-gray-100">Browse Registry</h2>
          <span className="px-3 py-1 bg-gray-800 rounded-full text-sm text-gray-400">
            {registries.length} registries
          </span>
        </div>

        {error && (
          <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
            Failed to load registries: {error}
          </div>
        )}

        {loading ? (
          <div className="flex items-center justify-center h-32">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {registries.map((registry) => (
              <div
                key={registry.id}
                onClick={() => handleRegistryClick(registry.id)}
                className="card cursor-pointer hover:border-blue-500/50 hover:shadow-xl hover:shadow-blue-500/10 hover:-translate-y-1 transition-all group"
              >
                <div className="flex items-start justify-between mb-4">
                  <RegistryIcon registry={registry} />
                  <div className="flex flex-col items-end gap-1">
                    <span className="px-3 py-1 bg-gray-800 rounded-full text-xs font-medium text-gray-400 uppercase tracking-wide">
                      {registry.type}
                    </span>
                    <span
                      className={`px-3 py-1 rounded-full text-xs font-medium uppercase tracking-wide ${
                        registry.private
                          ? 'bg-amber-500/10 text-amber-400'
                          : 'bg-green-500/10 text-green-400'
                      }`}
                    >
                      {registry.private ? 'Private' : 'Public'}
                    </span>
                  </div>
                </div>
                <h3 className="text-xl font-semibold text-gray-100 mb-1 group-hover:text-blue-400 transition-colors">
                  {registry.name}
                </h3>
                <p className="text-sm text-gray-500 font-mono truncate mb-4">{registry.url}</p>
                <div className="flex items-center text-sm text-gray-500">
                  <svg className="w-4 h-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
                  </svg>
                  <span>Click to browse</span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
