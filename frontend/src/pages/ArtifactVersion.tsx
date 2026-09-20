import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useAuth, useTimezone } from '../context/AuthContext'
import { useConfirm } from '../hooks/useConfirm'
import { getArtifactTypeConfig, isContainerType } from '../lib/artifactTypes'
import { formatDateTime } from '../lib/datetime'

// Field names here match the backend's plain Go json tags verbatim
// (encoding/json does no camelCase conversion) — see
// vulnerability.Vulnerability / vulnerability.ScanResult in scanner.go.
interface ApiCVE {
  id: string
  package: string
  version: string
  pkg_type: string
  severity: 'critical' | 'high' | 'medium' | 'low' | 'none'
  cvss: number
  installed: string
  fixed: string
  description: string
  references: string[]
  discovery: string
}

interface ApiScanResult {
  artifact_id: string
  artifact_type: string
  registry_id: string
  namespace: string
  artifact_name: string
  version: string
  scan_time: string
  severity: 'critical' | 'high' | 'medium' | 'low' | 'none'
  vulnerabilities: ApiCVE[]
  scanned_by: string
}

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
  TotalSize: number
  Created: string
  Updated: string
  Tags: string[] | null
  Signatures: { type: string; verified: boolean; timestamp?: string }[] | null
  Metadata: Record<string, any> | null
  Downloads: number
}

// Formats a pull/download count per the agreed display scheme: exact below
// 1M, "1M+" from 1M up to (not including) 10M, then "10M+"/"15M+"/... in 5M
// increments above that (rounded down).
function formatPulls(n: number): string {
  if (n < 1_000_000) return n.toLocaleString()
  if (n < 10_000_000) return '1M+'
  const bucket = Math.floor(n / 5_000_000) * 5
  return `${bucket}M+`
}

export function ArtifactVersion() {
  const { registryId, artifactType, namespace, artifactName, version } = useParams<{
    registryId: string
    artifactType: string
    namespace?: string
    artifactName: string
    version: string
  }>()
  const navigate = useNavigate()
  const { canManage, token } = useAuth()
  const timezone = useTimezone()
  const { confirm, ConfirmDialog } = useConfirm()
  const [artifact, setArtifact] = useState<Artifact | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [registryHost, setRegistryHost] = useState<string | null>(null)
  const [scanResults, setScanResults] = useState<ApiScanResult[]>([])

  useEffect(() => {
    if (!registryId) return
    fetch(`/api/v1/registries/${encodeURIComponent(registryId)}`, token ? { headers: { Authorization: `Bearer ${token}` } } : undefined)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setRegistryHost(data?.host || null))
      .catch(() => setRegistryHost(null))
  }, [registryId, token])

  useEffect(() => {
    setLoading(true)
    setError(null)
    const params = new URLSearchParams({ registryId: registryId ?? '', artifactType: artifactType ?? '', limit: '200' })
    if (namespace) params.set('namespace', namespace)

    fetch(`/api/v1/artifacts?${params.toString()}`, token ? { headers: { Authorization: `Bearer ${token}` } } : undefined)
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => {
        const all: Artifact[] = data.artifacts || []
        const match = all.find((a) => a.ArtifactName === artifactName && a.Version === version)
        setArtifact(match ?? null)
      })
      .catch((err) => setError(err.message || 'Failed to load artifact version'))
      .finally(() => setLoading(false))
  }, [registryId, artifactType, namespace, artifactName, version, token])

  useEffect(() => {
    if (!artifact) return
    fetch(`/api/v1/vulnerability-scans?artifactId=${encodeURIComponent(artifact.ID)}`, token ? { headers: { Authorization: `Bearer ${token}` } } : undefined)
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setScanResults(data?.results || []))
      .catch(() => setScanResults([]))
  }, [artifact, token])

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
  }

  const formatTimestamp = (ts: string) => {
    return formatDateTime(ts, timezone, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
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

  const handleDelete = async () => {
    if (!artifact) return
    if (!(await confirm(`Delete version ${version}? This cannot be undone.`))) return
    setDeleting(true)
    try {
      const res = await fetch(`/api/v1/artifacts/${encodeURIComponent(artifact.ID)}`, {
        method: 'DELETE',
        headers: token ? { Authorization: `Bearer ${token}` } : undefined,
      })
      if (!res.ok) throw new Error(`Request failed: ${res.status}`)
      navigate(`/registries/${registryId}/${artifactType}/${encodeURIComponent(namespace ?? '')}/${encodeURIComponent(artifactName ?? '')}`)
    } catch (err: any) {
      setError(err.message || 'Failed to delete version')
      setDeleting(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
        {error}
      </div>
    )
  }

  if (!artifact) {
    return (
      <div className="text-center py-12 text-gray-500">Version not found</div>
    )
  }

  // Empty host means the registry is the default/unbound proxy for its type,
  // reachable at the current origin; a bound host is only reachable there
  // (see proxy.ResolveRegistry in the backend).
  const origin = `${window.location.protocol}//${registryHost || window.location.host}`
  const ns = namespace ?? ''
  // Docker official images (namespace "library") are referenced with no
  // namespace prefix at all, e.g. `docker pull nginx:latest` — not `library/nginx`.
  const isDockerLibrary = artifactType === 'docker' && ns === 'library'

  // Buildx-produced multi-arch images attach extra "attestation" manifests
  // (SBOM/provenance) alongside the real platform images; these always
  // report os/arch as "unknown" and aren't a pullable platform, so they're
  // filtered out here the same way Docker Hub's own UI hides them.
  const childManifestsRaw = artifact.Metadata?.childManifests
  const childManifests: Record<string, any>[] = Array.isArray(childManifestsRaw)
    ? childManifestsRaw.filter((m) => m.os !== 'unknown' && m.arch !== 'unknown')
    : []

  const typeConfig = getArtifactTypeConfig(artifactType)
  const pullParams = {
    origin,
    host: registryHost || window.location.host,
    registryHost,
    namespace: ns,
    artifactName: artifactName ?? '',
    version: version ?? '',
  }
  const pullCommand = typeConfig.pullCommand(pullParams)

  return (
    <div className="space-y-8">
      {ConfirmDialog}
      <div className="card">
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <h1 className="text-3xl font-bold text-white">
                {namespace && !isDockerLibrary ? `${namespace}/${artifactName}` : artifactName}
              </h1>
              <span className="px-3 py-1.5 bg-blue-600 rounded-lg text-sm font-medium text-white">
                {isContainerType(artifactType) ? version : `v${version}`}
              </span>
            </div>
          </div>
          <div className="text-right">
            <div className="text-sm text-gray-500 mb-1">Registry</div>
            <div className="font-medium text-gray-300">{registryId}</div>
          </div>
        </div>
        {canManage(artifact.Metadata?.uploadedBy) && (
          <div className="mt-4 pt-4 border-t border-gray-800 flex items-center justify-end">
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="btn btn-sm bg-red-600 hover:bg-red-500 text-white disabled:opacity-50"
            >
              {deleting ? 'Deleting...' : 'Delete version'}
            </button>
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold text-gray-100 border-b border-gray-800 pb-2 flex items-center gap-2">
            <svg className="w-5 h-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
            Security & Integrity
          </h2>

          {artifact.Signatures && artifact.Signatures.length > 0 ? (
            <div className="space-y-3">
              <div className="flex items-center justify-between p-3 bg-green-500/10 border border-green-500/30 rounded-lg">
                <span className="text-green-500 font-medium flex items-center gap-2">
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  Signatures Present
                </span>
                <span className="px-2 py-1 bg-green-500/20 rounded text-xs font-medium text-green-400">
                  {artifact.Signatures.length} signatures
                </span>
              </div>
              {artifact.Signatures.map((sig, idx) => (
                <div key={idx} className="bg-gray-800/50 rounded-lg p-3">
                  <div className="flex items-center justify-between mb-2">
                    <span className="font-medium text-gray-300">{sig.type}</span>
                    {sig.verified ? (
                      <span className="flex items-center gap-1 text-green-500 text-xs font-medium">
                        <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                        Valid
                      </span>
                    ) : (
                      <span className="text-yellow-500 text-xs">Invalid</span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="p-3 bg-gray-800/50 rounded-lg text-gray-500 text-sm">Unsigned</div>
          )}

          <div>
            <label className="text-sm text-gray-500 mb-2 block">
              {artifact.DigestAlgorithm || 'Digest'}
            </label>
            <div className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-400 break-all">
              {artifact.Digest}
            </div>
          </div>
        </div>

        <div className="card space-y-4">
          <h2 className="text-lg font-semibold text-gray-100 border-b border-gray-800 pb-2 flex items-center gap-2">
            <svg className="w-5 h-5 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            Metadata
          </h2>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <div className="text-sm text-gray-500">Size</div>
              <div className="font-medium text-white">{formatSize(artifact.TotalSize || artifact.Size)}</div>
            </div>
            <div>
              <div className="text-sm text-gray-500">Created</div>
              <div className="font-medium text-white">{formatTimestamp(artifact.Created)}</div>
            </div>
            {artifact.Updated && (
              <div>
                <div className="text-sm text-gray-500">Cached</div>
                <div className="font-medium text-white">{formatTimestamp(artifact.Updated)}</div>
              </div>
            )}
            <div>
              <div className="text-sm text-gray-500">Pulls</div>
              <div className="font-medium text-white">{formatPulls(artifact.Downloads || 0)}</div>
            </div>
          </div>

          {artifact.Tags && artifact.Tags.length > 0 && (
            <div>
              <div className="text-sm text-gray-500 mb-2">Tags</div>
              <div className="flex flex-wrap gap-1">
                {artifact.Tags.map((tag, idx) => (
                  <span key={idx} className="px-2 py-0.5 text-xs rounded-full bg-gray-700 text-gray-300">
                    {tag}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      {scanResults.length > 0 && (
        <div className="card">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-gray-100">Vulnerabilities</h2>
            <a
              href={`/vulnerabilities/${encodeURIComponent(artifact.ID)}`}
              className="text-sm text-blue-400 hover:text-blue-300 font-medium"
            >
              View full scan history
            </a>
          </div>
          {(() => {
            const latestScan = scanResults
              .slice()
              .sort((a, b) => new Date(b.scan_time).getTime() - new Date(a.scan_time).getTime())[0]
            const cves = (latestScan.vulnerabilities || [])
              .slice()
              .sort((a, b) => b.cvss - a.cvss)
            return (
              <>
                <div className="text-sm text-gray-500 mb-4">
                  Last scanned {formatTimestamp(latestScan.scan_time)}
                  {latestScan.scanned_by ? ` by ${latestScan.scanned_by}` : ''}
                </div>
                {cves.length === 0 ? (
                  <div className="p-3 bg-green-500/10 border border-green-500/30 rounded-lg text-green-400 text-sm">
                    No known vulnerabilities found
                  </div>
                ) : (
                  <div className="overflow-x-auto">
                    <table className="w-full text-left text-sm">
                      <thead className="text-gray-500">
                        <tr>
                          <th className="py-1.5 pr-4 font-medium">Severity</th>
                          <th className="py-1.5 pr-4 font-medium">CVE</th>
                          <th className="py-1.5 pr-4 font-medium">Package</th>
                          <th className="py-1.5 pr-4 font-medium">Installed</th>
                          <th className="py-1.5 pr-4 font-medium">Fixed</th>
                          <th className="py-1.5 pr-4 font-medium">Score</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-gray-800">
                        {cves.map((cve) => (
                          <tr key={cve.id}>
                            <td className="py-2 pr-4">
                              <span className={`px-2 py-0.5 text-xs rounded-full font-medium ${getSeverityColor(cve.severity)}`}>
                                {cve.severity}
                              </span>
                            </td>
                            <td className="py-2 pr-4 text-blue-400 font-medium">{cve.id}</td>
                            <td className="py-2 pr-4 text-gray-300">{cve.package}</td>
                            <td className="py-2 pr-4 text-gray-400 font-mono text-xs">{cve.installed}</td>
                            <td className="py-2 pr-4 text-gray-400 font-mono text-xs">{cve.fixed || 'no fix'}</td>
                            <td className="py-2 pr-4 text-gray-300">{cve.cvss}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </>
            )
          })()}
        </div>
      )}

      <div className="card">
        <h2 className="text-lg font-semibold text-gray-100 mb-4">Pull/Install Command</h2>
        <div className="bg-gray-800 rounded-lg p-4 font-mono text-sm text-gray-300 flex items-center justify-between gap-3">
          <span className="break-all">{pullCommand}</span>
          <button
            onClick={() => navigator.clipboard.writeText(pullCommand)}
            className="text-blue-400 hover:text-blue-300 font-medium flex-shrink-0"
          >
            Copy
          </button>
        </div>

        {typeConfig.usage && (
          <div className="mt-4 pt-4 border-t border-gray-800">
            <div className="text-sm text-gray-500 mb-2">{typeConfig.usage.title}</div>
            <div className="bg-gray-800 rounded-lg p-4 font-mono text-sm text-gray-300 flex items-start justify-between gap-3">
              <pre className="whitespace-pre-wrap break-all">{typeConfig.usage.code(pullParams)}</pre>
              <button
                onClick={() => navigator.clipboard.writeText(typeConfig.usage!.code(pullParams))}
                className="text-blue-400 hover:text-blue-300 font-medium flex-shrink-0"
              >
                Copy
              </button>
            </div>
          </div>
        )}
      </div>

      {isContainerType(artifactType) && childManifests.length > 0 && (
        <div className="card">
          <h2 className="text-lg font-semibold text-gray-100 mb-4">Platforms</h2>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="text-gray-500">
                <tr>
                  <th className="py-1.5 pr-4 font-medium">Digest</th>
                  <th className="py-1.5 pr-4 font-medium">OS/ARCH</th>
                  <th className="py-1.5 pr-4 font-medium">Compressed Size</th>
                </tr>
              </thead>
              <tbody>
                {childManifests.map((m, idx) => (
                  <tr key={idx} className="border-t border-gray-800">
                    <td className="py-1.5 pr-4 font-mono text-gray-400 truncate max-w-xs">
                      sha256:{String(m.digest).replace(/^sha256:/, '').slice(-12)}
                    </td>
                    <td className="py-1.5 pr-4 text-gray-400">{m.os}/{m.arch}</td>
                    <td className="py-1.5 pr-4 text-gray-400">{formatSize(Number(m.size))}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
