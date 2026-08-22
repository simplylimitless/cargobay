import { useState, useEffect } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

// Shape returned by GET /api/v1/artifacts — database.ArtifactMetadata has
// no json tags, so it serializes using its Go field names verbatim.
interface Artifact {
  ID: string
  RegistryID: string
  ArtifactType: string
  Namespace: string
  ArtifactName: string
  Version: string
  Digest: string
  Size: number
  Created: string
  Tags: string[] | null
}

interface Registry {
  id: string
  name: string
  type: string
  url: string
  private: boolean
}

export function ArtifactBrowse() {
  const { registryId, artifactType, namespace } = useParams()
  const navigate = useNavigate()
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [registries, setRegistries] = useState<Registry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedRegistry, setSelectedRegistry] = useState(registryId || '')
  const [selectedType, setSelectedType] = useState(artifactType || 'all')
  const [selectedNamespace, setSelectedNamespace] = useState(namespace || '')
  const [searchQuery, setSearchQuery] = useState('')
  const [filteredArtifacts, setFilteredArtifacts] = useState<Artifact[]>([])

  const artifactTypes = ['all', 'docker', 'npm', 'maven', 'pypi', 'nuget', 'helm', 'generic']

  useEffect(() => {
    const fetchRegistries = async () => {
      try {
        const res = await fetch('/api/v1/registries')
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        const data = await res.json()
        const loaded: Registry[] = data.registries || []
        setRegistries(loaded)
        if (!selectedRegistry && loaded.length > 0) {
          setSelectedRegistry(loaded[0].id)
        }
      } catch (err: any) {
        setError(err.message || 'Failed to load registries')
      }
    }

    const fetchArtifacts = async () => {
      try {
        const res = await fetch('/api/v1/artifacts?limit=200')
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        const data = await res.json()
        setArtifacts(data.artifacts || [])
      } catch (err: any) {
        setError(err.message || 'Failed to load artifacts')
      } finally {
        setLoading(false)
      }
    }

    fetchRegistries()
    fetchArtifacts()
  }, [])

  useEffect(() => {
    let result = artifacts

    if (selectedRegistry) {
      result = result.filter(a => a.RegistryID === selectedRegistry)
    }
    if (selectedType !== 'all') {
      result = result.filter(a => a.ArtifactType === selectedType)
    }
    if (selectedNamespace) {
      result = result.filter(a => a.Namespace === selectedNamespace)
    }
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      result = result.filter(a =>
        a.ArtifactName.toLowerCase().includes(query) ||
        a.Version.toLowerCase().includes(query) ||
        (a.Tags || []).some(t => t.toLowerCase().includes(query))
      )
    }

    setFilteredArtifacts(result)
  }, [artifacts, selectedRegistry, selectedType, selectedNamespace, searchQuery])

  const formatSize = (bytes: number): string => {
    if (bytes >= 1024 * 1024 * 1024) {
      return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
    }
    if (bytes >= 1024 * 1024) {
      return `${(bytes / 1024 / 1024).toFixed(2)} MB`
    }
    return `${(bytes / 1024).toFixed(2)} KB`
  }

  const formatRelativeTime = (dateString: string): string => {
    const date = new Date(dateString)
    const now = new Date()
    const diff = now.getTime() - date.getTime()
    const days = Math.floor(diff / (1000 * 60 * 60 * 24))
    if (days === 0) return 'Today'
    if (days === 1) return '1 day ago'
    if (days < 7) return `${days} days ago`
    return date.toLocaleDateString()
  }

  const getNamespaceList = (type: string): string[] => {
    const namespaces = new Set<string>()
    artifacts
      .filter(a => a.ArtifactType === type)
      .forEach(a => {
        if (a.Namespace) namespaces.add(a.Namespace)
      })
    return Array.from(namespaces).sort()
  }

  return (
    <div className="max-w-7xl mx-auto">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-white mb-2">Browse Artifacts</h1>
        <p className="text-gray-400">Explore and search artifacts from configured registries</p>
      </div>

      {error && (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
          {error}
        </div>
      )}

      {/* Filters */}
      <div className="card p-6 mb-6 space-y-6">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Registry</label>
            <select
              value={selectedRegistry}
              onChange={(e) => setSelectedRegistry(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              {registries.map(r => (
                <option key={r.id} value={r.id}>{r.name}</option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Artifact Type</label>
            <select
              value={selectedType}
              onChange={(e) => setSelectedType(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              {artifactTypes.map(t => (
                <option key={t} value={t}>{t.charAt(0).toUpperCase() + t.slice(1)}</option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Namespace</label>
            <select
              value={selectedNamespace}
              onChange={(e) => setSelectedNamespace(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              <option value="">All</option>
              {getNamespaceList(selectedType === 'all' ? 'docker' : selectedType).map(ns => (
                <option key={ns} value={ns}>{ns}</option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Search</label>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search by name, version, tag..."
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            />
          </div>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Artifacts</div>
          <div className="text-2xl font-bold text-white mt-1">{filteredArtifacts.length}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Size</div>
          <div className="text-2xl font-bold text-white mt-1">
            {formatSize(filteredArtifacts.reduce((sum, a) => sum + a.Size, 0))}
          </div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Namespaces</div>
          <div className="text-2xl font-bold text-white mt-1">
            {new Set(filteredArtifacts.map(a => a.Namespace).filter(Boolean)).size}
          </div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Active Registries</div>
          <div className="text-2xl font-bold text-white mt-1">
            {registries.length}
          </div>
        </div>
      </div>

      {/* Artifacts Table */}
      {loading ? (
        <div className="card text-center py-12">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500 mx-auto mb-4"></div>
          <p className="text-gray-400">Loading artifacts...</p>
        </div>
      ) : filteredArtifacts.length === 0 ? (
        <div className="card text-center py-12">
          <div className="w-16 h-16 bg-gray-700 text-gray-400 rounded-full flex items-center justify-center mx-auto mb-4">
            <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.172 16.172a4 4 0 015.656 0M9 10h.01M15 10h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-white mb-2">No artifacts found</h3>
          <p className="text-gray-400">Try adjusting your filters or search query</p>
        </div>
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b border-gray-700">
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Artifact</th>
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Type</th>
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Version</th>
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Namespace</th>
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Size</th>
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Tags</th>
                <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Cached</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-700">
              {filteredArtifacts.map(artifact => (
                <tr
                  key={artifact.ID}
                  className="hover:bg-gray-800/50 transition-colors"
                  onClick={() => navigate(`/artifacts/${artifact.RegistryID}/${artifact.ArtifactType}/${artifact.Namespace}/${artifact.ArtifactName}/${artifact.Version}`)}
                >
                  <td className="px-6 py-4">
                    <div className="font-medium text-white">{artifact.ArtifactName}</div>
                  </td>
                  <td className="px-6 py-4">
                    <span className="px-2 py-1 text-xs rounded-full bg-blue-500/20 text-blue-300 capitalize">
                      {artifact.ArtifactType}
                    </span>
                  </td>
                  <td className="px-6 py-4 text-gray-300">{artifact.Version}</td>
                  <td className="px-6 py-4 text-gray-300">{artifact.Namespace || '-'}</td>
                  <td className="px-6 py-4 text-gray-300">{formatSize(artifact.Size)}</td>
                  <td className="px-6 py-4">
                    <div className="flex flex-wrap gap-1">
                      {(artifact.Tags || []).map((tag, idx) => (
                        <span key={idx} className="px-2 py-0.5 text-xs rounded-full bg-gray-700 text-gray-300">
                          {tag}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-6 py-4 text-gray-300 text-sm">{formatRelativeTime(artifact.Created)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
