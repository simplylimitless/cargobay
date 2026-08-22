import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { useConfirm } from '../hooks/useConfirm'
import { getArtifactTypeConfig, isContainerType } from '../lib/artifactTypes'

interface Artifact {
  ID: string
  RegistryID: string
  ArtifactType: string
  Namespace: string
  ArtifactName: string
  Version: string
  Digest: string
  DigestAlgorithm: string
  Size: number
  Created: string
  Tags: string[] | null
  Signatures: { type: string; verified: boolean }[] | null
  Metadata: Record<string, any> | null
  Downloads: number
}

interface VulnerabilitySummary {
  severity: 'critical' | 'high' | 'medium' | 'low' | 'none'
  vulnerabilities: { severity: 'critical' | 'high' | 'medium' | 'low' }[] | null
  scan_time: string
}

const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low'] as const

function severityCounts(summary: VulnerabilitySummary): Record<(typeof SEVERITY_ORDER)[number], number> {
  const counts = { critical: 0, high: 0, medium: 0, low: 0 }
  for (const v of summary.vulnerabilities || []) {
    if (v.severity in counts) counts[v.severity]++
  }
  return counts
}

// Formats a pull/download count per the agreed display scheme: exact below
// 1M, "1M+" from 1M up to (not including) 10M, then "10M+"/"15M+"/... in 5M
// increments above that (rounded down), so the leaderboard-scale numbers
// stay readable without claiming false precision.
function formatPulls(n: number): string {
  if (n < 1_000_000) return n.toLocaleString()
  if (n < 10_000_000) return '1M+'
  const bucket = Math.floor(n / 5_000_000) * 5
  return `${bucket}M+`
}

export function ArtifactDetail() {
  const { registryId, artifactType, namespace, artifactName } = useParams<{
    registryId: string
    artifactType: string
    namespace?: string
    artifactName: string
  }>()
  const navigate = useNavigate()
  const { canManage, token } = useAuth()
  const { confirm, ConfirmDialog } = useConfirm()
  const [versions, setVersions] = useState<Artifact[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [registryHost, setRegistryHost] = useState<string | null>(null)
  const [vulnSummaries, setVulnSummaries] = useState<Record<string, VulnerabilitySummary>>({})

  useEffect(() => {
    if (!registryId) return
    fetch(`/api/v1/registries/${encodeURIComponent(registryId)}`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setRegistryHost(data?.host || null))
      .catch(() => setRegistryHost(null))
  }, [registryId])

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
      .then((data) => {
        const all: Artifact[] = data.artifacts || []
        const matching = all
          .filter((a) => a.ArtifactName === artifactName)
          .sort((a, b) => b.Version.localeCompare(a.Version))
        setVersions(matching)
      })
      .catch((err) => setError(err.message || 'Failed to load artifact'))
      .finally(() => setLoading(false))
  }, [registryId, artifactType, namespace, artifactName])

  useEffect(() => {
    if (!isContainerType(artifactType)) return
    if (versions.length === 0) {
      setVulnSummaries({})
      return
    }
    const ids = versions.map((v) => v.ID).join(',')
    fetch(`/api/v1/artifacts/vulnerability-summary?ids=${encodeURIComponent(ids)}`)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setVulnSummaries(data?.summaries || {}))
      .catch(() => setVulnSummaries({}))
  }, [versions, artifactType])

  const handleVersionClick = (version: string) => {
    navigate(`/registries/${registryId}/${artifactType}/${encodeURIComponent(namespace ?? '')}/${encodeURIComponent(artifactName ?? '')}/${encodeURIComponent(version)}`)
  }

  const handleDelete = async (e: React.MouseEvent, v: Artifact) => {
    e.stopPropagation()
    if (!(await confirm(`Delete version ${v.Version}? This cannot be undone.`))) return
    setDeletingId(v.ID)
    try {
      const res = await fetch(`/api/v1/artifacts/${encodeURIComponent(v.ID)}`, {
        method: 'DELETE',
        headers: token ? { Authorization: `Bearer ${token}` } : undefined,
      })
      if (!res.ok) throw new Error(`Request failed: ${res.status}`)
      setVersions((prev) => prev.filter((x) => x.ID !== v.ID))
    } catch (err: any) {
      setError(err.message || 'Failed to delete version')
    } finally {
      setDeletingId(null)
    }
  }

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
  }

  // Empty host means the registry is the default/unbound proxy for its type,
  // reachable at the current origin; a bound host is only reachable there
  // (see proxy.ResolveRegistry in the backend).
  const origin = `${window.location.protocol}//${registryHost || window.location.host}`

  const typeConfig = getArtifactTypeConfig(artifactType)

  const pullCommand = (v: Artifact) =>
    typeConfig.pullCommand({
      origin,
      host: registryHost || window.location.host,
      registryHost,
      namespace: namespace ?? '',
      artifactName: artifactName ?? '',
      version: v.Version,
    })

  const childManifests = (v: Artifact): Record<string, any>[] => {
    const list = v.Metadata?.childManifests
    if (!Array.isArray(list)) return []
    // Buildx-produced multi-arch images attach extra "attestation" manifests
    // (SBOM/provenance) alongside the real platform images; these always
    // report os/arch as "unknown" and aren't a pullable platform, so they're
    // filtered out here the same way Docker Hub's own UI hides them.
    return list.filter((m) => m.os !== 'unknown' && m.arch !== 'unknown')
  }

  const timeAgo = (ts: string) => {
    const seconds = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
    if (seconds < 60) return 'a few seconds ago'
    const minutes = Math.floor(seconds / 60)
    if (minutes < 60) return `${minutes} minute${minutes !== 1 ? 's' : ''} ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours} hour${hours !== 1 ? 's' : ''} ago`
    const days = Math.floor(hours / 24)
    if (days < 30) return `${days} day${days !== 1 ? 's' : ''} ago`
    const months = Math.floor(days / 30)
    if (months < 12) return `${months} month${months !== 1 ? 's' : ''} ago`
    const years = Math.floor(months / 12)
    return `${years} year${years !== 1 ? 's' : ''} ago`
  }

  const getSeverityColor = (severity: string): string => {
    switch (severity) {
      case 'critical': return 'bg-red-600 text-white'
      case 'high': return 'bg-orange-500 text-white'
      case 'medium': return 'bg-yellow-500 text-white'
      case 'low': return 'bg-blue-500 text-white'
      case 'none': return 'bg-green-500 text-white'
      default: return 'bg-gray-500 text-white'
    }
  }

  // Mirrors Docker Scout's per-tag health chips (hub.docker.com tag pages):
  // a breakdown by severity rather than a single "top severity + count".
  const vulnBadge = (v: Artifact) => {
    if (!isContainerType(artifactType)) return null
    const summary = vulnSummaries[v.ID]
    if (!summary) {
      return (
        <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-gray-600 text-white">
          Not scanned
        </span>
      )
    }
    const counts = severityCounts(summary)
    const total = counts.critical + counts.high + counts.medium + counts.low
    if (total === 0) {
      return (
        <span className="px-2 py-0.5 rounded-full text-xs font-medium bg-green-500 text-white">
          No issues
        </span>
      )
    }
    return (
      <span className="flex items-center gap-1" title={`${total} vulnerabilit${total !== 1 ? 'ies' : 'y'} found`}>
        {SEVERITY_ORDER.filter((s) => counts[s] > 0).map((s) => (
          <span key={s} className={`px-1.5 py-0.5 rounded text-xs font-semibold ${getSeverityColor(s)}`}>
            {counts[s]} {s}
          </span>
        ))}
      </span>
    )
  }

  return (
    <div className="space-y-8">
      {ConfirmDialog}
      <div className="card">
        <div className="flex items-start gap-6">
          <div className="w-20 h-20 bg-gradient-to-br from-blue-600 to-purple-600 rounded-2xl flex items-center justify-center flex-shrink-0 text-4xl shadow-lg shadow-blue-600/20">
            {typeConfig.icon}
          </div>
          <div className="flex-1">
            <div className="flex items-center gap-3 mb-2">
              <h1 className="text-3xl font-bold text-white">
                {namespace && namespace !== 'library' ? `${namespace}/${artifactName}` : artifactName}
              </h1>
              <span className="px-3 py-1 bg-gray-800 rounded-lg text-sm text-gray-400">
                {artifactType?.toUpperCase()}
              </span>
            </div>
            <div className="flex flex-wrap gap-3 text-sm text-gray-500">
              <span className="flex items-center gap-1.5">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                </svg>
                Registry: {registryId}
              </span>
              <span>•</span>
              <span>{versions.length} version{versions.length !== 1 ? 's' : ''}</span>
              <span>•</span>
              <span>{formatPulls(versions.reduce((sum, v) => sum + (v.Downloads || 0), 0))} pulls</span>
            </div>
          </div>
        </div>
      </div>

      {error && (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-gray-100">
          Tags <span className="text-gray-500 font-normal">({versions.length})</span>
        </h2>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : versions.length === 0 ? (
        <div className="card text-center py-12 text-gray-500">No versions found</div>
      ) : (
        <div className="space-y-4">
          {versions.map((v) => {
            const cmd = pullCommand(v)
            return (
              <div key={v.ID} className="card">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <button
                        onClick={() => handleVersionClick(v.Version)}
                        className="font-mono text-blue-400 hover:text-blue-300 font-semibold text-base"
                      >
                        {v.Version}
                      </button>
                      {v.Signatures && v.Signatures.length > 0 ? (
                        <span className="flex items-center gap-1 text-green-500 text-xs font-medium">
                          <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                          </svg>
                          {v.Signatures.length} signature{v.Signatures.length !== 1 ? 's' : ''}
                        </span>
                      ) : (
                        <span className="text-gray-500 text-xs">Unsigned</span>
                      )}
                      {vulnBadge(v)}
                    </div>
                    <div className="text-sm text-gray-500">
                      Last pushed {timeAgo(v.Created)} · {formatSize(v.Size)} · {formatPulls(v.Downloads || 0)} pulls
                    </div>
                  </div>
                  {canManage(v.Metadata?.uploadedBy) && (
                    <button
                      onClick={(e) => handleDelete(e, v)}
                      disabled={deletingId === v.ID}
                      className="text-xs text-red-400 hover:text-red-300 disabled:opacity-50 flex-shrink-0"
                    >
                      {deletingId === v.ID ? 'Deleting...' : 'Delete'}
                    </button>
                  )}
                </div>

                <div className="mt-3 bg-gray-800 rounded-lg px-4 py-2.5 font-mono text-sm text-gray-300 flex items-center justify-between gap-3">
                  <span className="truncate">{cmd}</span>
                  <button
                    onClick={() => navigator.clipboard.writeText(cmd)}
                    className="text-blue-400 hover:text-blue-300 flex-shrink-0"
                    title="Copy"
                  >
                    <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                    </svg>
                  </button>
                </div>

                {isContainerType(artifactType) ? (
                  <div className="mt-3 overflow-x-auto">
                    <table className="w-full text-left text-xs">
                      <thead className="text-gray-500">
                        <tr>
                          <th className="py-1.5 pr-4 font-medium">Digest</th>
                          {childManifests(v).length > 0 && <th className="py-1.5 pr-4 font-medium">OS/ARCH</th>}
                          <th className="py-1.5 pr-4 font-medium">Compressed Size</th>
                        </tr>
                      </thead>
                      <tbody>
                        {childManifests(v).length > 0 ? (
                          childManifests(v).map((m, idx) => (
                            <tr key={idx} className="border-t border-gray-800">
                              <td className="py-1.5 pr-4 font-mono text-gray-400 truncate max-w-xs">
                                sha256:{String(m.digest).replace(/^sha256:/, '').slice(-12)}
                              </td>
                              <td className="py-1.5 pr-4 text-gray-400">{m.os}/{m.arch}</td>
                              <td className="py-1.5 pr-4 text-gray-400">{formatSize(Number(m.size))}</td>
                            </tr>
                          ))
                        ) : (
                          <tr className="border-t border-gray-800">
                            <td className="py-1.5 pr-4 font-mono text-gray-400 truncate max-w-xs">
                              {v.DigestAlgorithm || 'sha256'}:{v.Digest.replace(/^sha256:/, '').slice(-12)}
                            </td>
                            <td className="py-1.5 pr-4 text-gray-400">{formatSize(v.Size)}</td>
                          </tr>
                        )}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  typeConfig.usage && (
                    <div className="mt-3 text-xs">
                      <div className="text-gray-500 mb-1">{typeConfig.usage.title}</div>
                      <pre className="bg-gray-800/50 rounded-lg p-3 font-mono text-gray-400 whitespace-pre-wrap break-all">
                        {typeConfig.usage.code({
                          origin,
                          host: registryHost || window.location.host,
                          registryHost,
                          namespace: namespace ?? '',
                          artifactName: artifactName ?? '',
                          version: v.Version,
                        })}
                      </pre>
                    </div>
                  )
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
