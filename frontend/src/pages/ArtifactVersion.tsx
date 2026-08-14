import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

export function ArtifactVersion() {
  const { registryId, artifactType, namespace, artifactName, version } = useParams<{
    registryId: string
    artifactType: string
    namespace?: string
    artifactName: string
    version: string
  }>()
  const navigate = useNavigate()
  const { canManage } = useAuth()
  const [details, setDetails] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const mockDetails: Record<string, any> = {
      docker: {
        image: `${artifactName}:${version}`,
        digest: 'sha256:abc123def45678901234567890123456789012345678901234567890123456',
        size: 142000000,
        created: '2024-01-15T10:00:00Z',
        author: 'Docker Bot',
        layers: 7,
        environment: ['NODE_VERSION=20', 'DISTRO=bookworm', 'PATH=/usr/local/sbin:/usr/local/bin'],
        commands: ['CMD ["nginx", "-g", "daemon off;"]'],
        signatures: [
          { type: 'cosign', verified: true, timestamp: '2024-01-15T10:05:00Z' },
        ],
        uploadedBy: 'alice',
      },
      maven: {
        groupId: namespace,
        artifactId: artifactName,
        version: version,
        published: '2024-01-10',
        sha1: 'abc123def4567890123456789012345678901234',
        sha256: 'def4567890123456789012345678901234567890123456789012345678901234',
        md5: 'ghi78901234567890123456789012345',
        pomUrl: 'https://repo1.maven.org/maven2/...',
        dependencies: ['org.springframework:spring-core:6.1.0', 'com.google.guava:guava:32.1.2-jre'],
        uploadedBy: 'bob',
      },
      npm: {
        name: artifactName,
        version: version,
        published: '2024-01-12',
        license: 'MIT',
        sha1: 'abc123def4567890123456789012345678901234',
        sha256: 'def4567890123456789012345678901234567890123456789012345678901234',
        files: ['lib/', 'dist/', 'package.json', 'README.md', 'LICENSE'],
        dependencies: { react: '^18.0.0', lodash: '^4.17.0' },
        uploadedBy: 'alice',
      },
    }

    setDetails(mockDetails[artifactType] || {})
    setLoading(false)
  }, [artifactType, artifactName, version])

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
  }

  const formatTimestamp = (ts: string) => {
    return new Date(ts).toLocaleString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  }

  const handleDelete = () => {
    if (!confirm(`Delete version ${version}? This cannot be undone.`)) return
    navigate(`/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`)
  }

  if (loading || !details) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <button
        onClick={() => navigate(`/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`)}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to versions
      </button>

      <div className="card">
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <h1 className="text-3xl font-bold text-white">
                {namespace ? `${namespace}/${artifactName}` : artifactName}
              </h1>
              <span className="px-3 py-1.5 bg-blue-600 rounded-lg text-sm font-medium text-white">
                v{version}
              </span>
            </div>
            {artifactType === 'docker' && (
              <p className="text-gray-400 font-mono text-lg">{details.image}</p>
            )}
          </div>
          <div className="text-right">
            <div className="text-sm text-gray-500 mb-1">Registry</div>
            <div className="font-medium text-gray-300">{registryId}</div>
          </div>
        </div>
        {details.uploadedBy && (
          <div className="mt-4 pt-4 border-t border-gray-800 flex items-center justify-between">
            <span className="text-sm text-gray-500">
              Uploaded by <span className="text-gray-300 font-medium">{details.uploadedBy}</span>
            </span>
            {canManage(details.uploadedBy) && (
              <button onClick={handleDelete} className="btn btn-sm bg-red-600 hover:bg-red-500 text-white">
                Delete version
              </button>
            )}
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Security Section */}
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold text-gray-100 border-b border-gray-800 pb-2 flex items-center gap-2">
            <svg className="w-5 h-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
            Security & Integrity
          </h2>

          {artifactType === 'docker' && details.signatures && (
            <div className="space-y-3">
              <div className="flex items-center justify-between p-3 bg-green-500/10 border border-green-500/30 rounded-lg">
                <span className="text-green-500 font-medium flex items-center gap-2">
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  Signatures Verified
                </span>
                <span className="px-2 py-1 bg-green-500/20 rounded text-xs font-medium text-green-400">
                  {details.signatures.length} signatures
                </span>
              </div>
              {details.signatures.map((sig: any, idx: number) => (
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
                  <div className="text-xs text-gray-500 font-mono flex items-center gap-2">
                    <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                    </svg>
                    Verified: {formatTimestamp(sig.timestamp)}
                  </div>
                </div>
              ))}
            </div>
          )}

          <div>
            <label className="text-sm text-gray-500 mb-2 block">SHA-256 Digest</label>
            <div className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-400 break-all">
              {details.sha256 || details.digest}
            </div>
          </div>

          {details.sha1 && (
            <div>
              <label className="text-sm text-gray-500 mb-2 block">SHA-1</label>
              <div className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-500 break-all">
                {details.sha1}
              </div>
            </div>
          )}
        </div>

        {/* Metadata Section */}
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold text-gray-100 border-b border-gray-800 pb-2 flex items-center gap-2">
            <svg className="w-5 h-5 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            Metadata
          </h2>

          <div className="grid grid-cols-2 gap-4">
            {artifactType === 'docker' && (
              <>
                <div>
                  <div className="text-sm text-gray-500">Size</div>
                  <div className="font-medium text-white">{formatSize(details.size)}</div>
                </div>
                <div>
                  <div className="text-sm text-gray-500">Created</div>
                  <div className="font-medium text-white">{formatTimestamp(details.created)}</div>
                </div>
                <div>
                  <div className="text-sm text-gray-500">Layers</div>
                  <div className="font-medium text-white">{details.layers}</div>
                </div>
                <div>
                  <div className="text-sm text-gray-500">Author</div>
                  <div className="font-medium text-white">{details.author}</div>
                </div>
              </>
            )}

            {artifactType === 'maven' && (
              <>
                <div>
                  <div className="text-sm text-gray-500">Group ID</div>
                  <div className="font-mono font-medium text-white">{details.groupId}</div>
                </div>
                <div>
                  <div className="text-sm text-gray-500">Artifact ID</div>
                  <div className="font-mono font-medium text-white">{details.artifactId}</div>
                </div>
                <div>
                  <div className="text-sm text-gray-500">Published</div>
                  <div className="font-medium text-white">{details.published}</div>
                </div>
              </>
            )}

            {artifactType === 'npm' && (
              <>
                <div>
                  <div className="text-sm text-gray-500">License</div>
                  <div className="font-medium text-white">{details.license}</div>
                </div>
                <div>
                  <div className="text-sm text-gray-500">Published</div>
                  <div className="font-medium text-white">{details.published}</div>
                </div>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Details Section */}
      {artifactType === 'docker' && details.environment && (
        <div className="card space-y-4">
          <h2 className="text-lg font-semibold text-gray-100">Environment Variables</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
            {details.environment.map((env: string, idx: number) => (
              <div key={idx} className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-300 flex items-center gap-2">
                <svg className="w-4 h-4 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                </svg>
                {env}
              </div>
            ))}
          </div>
          {details.commands && (
            <div>
              <h3 className="text-sm font-medium text-gray-500 mb-2">Default Command</h3>
              <div className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-300">
                {details.commands[0]}
              </div>
            </div>
          )}
        </div>
      )}

      {artifactType === 'maven' && details.dependencies && (
        <div className="card">
          <h2 className="text-lg font-semibold text-gray-100 mb-3">Dependencies</h2>
          <div className="space-y-2">
            {details.dependencies.map((dep: string, idx: number) => (
              <div key={idx} className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-300 flex items-center justify-between">
                <span>{dep}</span>
                <span className="text-gray-500">compile</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {artifactType === 'npm' && details.dependencies && (
        <div className="card">
          <h2 className="text-lg font-semibold text-gray-100 mb-3">Dependencies</h2>
          <div className="space-y-2">
            {Object.entries(details.dependencies).map(([name, version]: [string, string], idx: number) => (
              <div key={idx} className="bg-gray-800/50 rounded-lg p-3 font-mono text-xs text-gray-300 flex items-center justify-between">
                <span>{name}</span>
                <span className="text-gray-500">{version}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Pull/Install Command */}
      <div className="card">
        <h2 className="text-lg font-semibold text-gray-100 mb-4">Pull/Install Command</h2>
        <div className="bg-gray-800 rounded-lg p-4 font-mono text-sm text-gray-300 flex items-center justify-between">
          {artifactType === 'docker' && (
            <>
              <span>docker pull {details.image}</span>
              <button className="text-blue-400 hover:text-blue-300 font-medium">Copy</button>
            </>
          )}
          {artifactType === 'npm' && (
            <>
              <span>npm install {artifactName}@{version}</span>
              <button className="text-blue-400 hover:text-blue-300 font-medium">Copy</button>
            </>
          )}
          {artifactType === 'maven' && (
            <>
              <span>&lt;dependency&gt;</span>
              <button className="text-blue-400 hover:text-blue-300 font-medium">Copy</button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
