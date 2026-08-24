import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { getArtifactTypeConfig } from '../lib/artifactTypes'

interface Artifact {
  ID: string
  RegistryID: string
  ArtifactType: string
  Namespace: string
  ArtifactName: string
  Version: string
  Size: number
  TotalSize: number
  Tags: string[] | null
}

interface GroupedArtifact {
  namespace: string
  artifactName: string
  latestVersion: string
  size: number
  versionCount: number
}

export function ArtifactList() {
  const { registryId, artifactType, namespace } = useParams<{ registryId: string; artifactType: string; namespace?: string }>()
  const navigate = useNavigate()
  const [searchQuery, setSearchQuery] = useState('')
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    const params = new URLSearchParams({ registryId: registryId ?? '', artifactType: artifactType ?? '', limit: '200' })
    if (namespace) params.set('namespace', namespace)

    fetch(`/api/v1/artifacts?${params.toString()}`)
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => setArtifacts(data.artifacts || []))
      .catch((err) => setError(err.message || 'Failed to load artifacts'))
      .finally(() => setLoading(false))
  }, [registryId, artifactType, namespace])

  // Docker official images (namespace "library") are referenced with no
  // namespace prefix at all, e.g. `docker pull nginx` — not `library/nginx`.
  const isDockerLibrary = (ns: string) => artifactType === 'docker' && ns === 'library'

  const grouped: GroupedArtifact[] = (() => {
    const byKey = new Map<string, Artifact[]>()
    for (const a of artifacts) {
      const key = `${a.Namespace}/${a.ArtifactName}`
      const list = byKey.get(key) ?? []
      list.push(a)
      byKey.set(key, list)
    }
    return Array.from(byKey.entries()).map(([, versions]) => {
      const sorted = [...versions].sort((a, b) => b.Version.localeCompare(a.Version))
      const latest = sorted[0]
      return {
        namespace: latest.Namespace,
        artifactName: latest.ArtifactName,
        latestVersion: latest.Version,
        size: latest.TotalSize || latest.Size,
        versionCount: versions.length,
      }
    })
  })()

  const filteredArtifacts = grouped.filter((artifact) =>
    artifact.artifactName.toLowerCase().includes(searchQuery.toLowerCase()) ||
    artifact.namespace.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const handleArtifactClick = (artifact: GroupedArtifact) => {
    navigate(`/registries/${registryId}/${artifactType}/${encodeURIComponent(artifact.namespace)}/${encodeURIComponent(artifact.artifactName)}`)
  }

  const formatSize = (bytes: number): string => {
    if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
    if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
    return `${(bytes / 1024).toFixed(2)} KB`
  }

  return (
    <div className="space-y-8">
      <div className="card flex items-center gap-4">
        <div className="text-4xl">{getArtifactTypeConfig(artifactType).icon}</div>
        <div>
          <h1 className="text-2xl font-bold text-gray-100 capitalize">{artifactType} Artifacts</h1>
          <p className="text-gray-400">
            {namespace ? `Namespace: ${namespace}` : 'All namespaces'}
          </p>
        </div>
      </div>

      {error && (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      <div className="card">
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={`Search ${artifactType} artifacts...`}
          className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-gray-100 placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 mb-6"
        />

        <div className="space-y-3">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            </div>
          ) : filteredArtifacts.length === 0 ? (
            <div className="text-center py-12 text-gray-500">
              <div className="mb-4 opacity-50">
                <svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
                </svg>
              </div>
              No artifacts found
            </div>
          ) : (
            filteredArtifacts.map((artifact) => (
              <div
                key={`${artifact.namespace}/${artifact.artifactName}`}
                onClick={() => handleArtifactClick(artifact)}
                className="card cursor-pointer hover:border-blue-500 hover:shadow-md hover:bg-gray-800/50 transition-all group"
              >
                <div className="flex items-start justify-between">
                  <div>
                    <div className="flex items-center gap-3 mb-1">
                      <h3 className="text-lg font-medium text-blue-400 font-mono group-hover:text-blue-300 transition-colors">
                        {artifact.namespace && !isDockerLibrary(artifact.namespace) ? `${artifact.namespace}/${artifact.artifactName}` : artifact.artifactName}
                      </h3>
                      <span className="px-2 py-0.5 bg-gray-800 rounded text-xs text-gray-500">
                        {artifactType?.toUpperCase()}
                      </span>
                    </div>
                    <p className="text-sm text-gray-400">
                      {artifact.versionCount} version{artifact.versionCount !== 1 ? 's' : ''} · latest {artifact.latestVersion} · {formatSize(artifact.size)}
                    </p>
                  </div>
                  <svg className="w-5 h-5 text-gray-500 group-hover:text-blue-400 transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                  </svg>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
