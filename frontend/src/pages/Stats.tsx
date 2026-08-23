import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { isHiddenDockerLibraryNamespace } from '../lib/artifactTypes'

interface TopArtifact {
  ID: string
  RegistryID: string
  ArtifactType: string
  Namespace: string
  ArtifactName: string
  Version: string
  Downloads: number
}

interface StatsResponse {
  bandwidthSavedBytes: number
  totalPulls: number
  topArtifacts: TopArtifact[] | null
}

const formatBytes = (bytes: number): string => {
  if (bytes >= 1024 * 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024 / 1024).toFixed(2)} TB`
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(2)} KB`
  return `${bytes} B`
}

const formatCount = (n: number): string => n.toLocaleString()

export function Stats() {
  const navigate = useNavigate()
  const [stats, setStats] = useState<StatsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    fetch('/api/v1/stats?limit=10')
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => setStats(data))
      .catch((err) => setError(err.message || 'Failed to load stats'))
      .finally(() => setLoading(false))
  }, [])

  const handleArtifactClick = (artifact: TopArtifact) => {
    navigate(
      `/registries/${artifact.RegistryID}/${artifact.ArtifactType}/${encodeURIComponent(artifact.Namespace)}/${encodeURIComponent(artifact.ArtifactName)}`
    )
  }

  const topArtifacts = stats?.topArtifacts ?? []

  return (
    <div className="space-y-8">
      <div className="card">
        <h1 className="text-2xl font-bold text-gray-100">Stats</h1>
        <p className="text-gray-400">Bandwidth saved by caching and the most-pulled artifacts</p>
      </div>

      {error && (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
            <div className="card">
              <p className="text-sm text-gray-400 mb-1">Bandwidth Saved</p>
              <p className="text-3xl font-bold text-blue-400">{formatBytes(stats?.bandwidthSavedBytes ?? 0)}</p>
              <p className="text-xs text-gray-500 mt-2">Served from local cache instead of the upstream registry</p>
            </div>
            <div className="card">
              <p className="text-sm text-gray-400 mb-1">Total Pulls</p>
              <p className="text-3xl font-bold text-purple-400">{formatCount(stats?.totalPulls ?? 0)}</p>
              <p className="text-xs text-gray-500 mt-2">Downloads across every artifact and registry</p>
            </div>
          </div>

          <div className="card">
            <h2 className="text-lg font-semibold text-gray-100 mb-4">Top Pulled Artifacts</h2>
            {topArtifacts.length === 0 ? (
              <div className="text-center py-12 text-gray-500">No pull activity yet</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left">
                  <thead>
                    <tr className="text-xs text-gray-500 uppercase border-b border-gray-800">
                      <th className="px-3 py-2">#</th>
                      <th className="px-3 py-2">Artifact</th>
                      <th className="px-3 py-2">Registry</th>
                      <th className="px-3 py-2 text-right">Pulls</th>
                    </tr>
                  </thead>
                  <tbody>
                    {topArtifacts.map((artifact, index) => (
                      <tr
                        key={artifact.ID}
                        onClick={() => handleArtifactClick(artifact)}
                        className="border-t border-gray-800 cursor-pointer hover:bg-gray-800/50 transition-colors"
                      >
                        <td className="px-3 py-3 text-gray-500">{index + 1}</td>
                        <td className="px-3 py-3">
                          <span className="font-mono text-blue-400">
                            {artifact.Namespace && !isHiddenDockerLibraryNamespace(artifact.ArtifactType, artifact.Namespace)
                              ? `${artifact.Namespace}/${artifact.ArtifactName}`
                              : artifact.ArtifactName}
                          </span>
                          <span className="ml-2 text-xs text-gray-500">{artifact.Version}</span>
                        </td>
                        <td className="px-3 py-3">
                          <span className="badge badge-info">{artifact.ArtifactType}</span>
                        </td>
                        <td className="px-3 py-3 text-right text-gray-200 font-medium">{formatCount(artifact.Downloads)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
