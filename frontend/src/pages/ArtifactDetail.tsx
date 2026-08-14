import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'

const MOCK_README = `# Quick reference

- Maintained by: [the cargobay community]
- Where to get help: the \`#cargobay\` channel, Stack Overflow, or GitHub Discussions

## How to use this image

Start an instance:

\`\`\`bash
docker run -d -p 8080:80 --name my-app <image>:<tag>
\`\`\`

### Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| \`APP_ENV\` | \`production\` | Runtime environment |
| \`APP_PORT\` | \`80\` | Port the app listens on |

### Persisting data

\`\`\`bash
docker run -d \\
  -v app-data:/var/lib/app \\
  --name my-app <image>:<tag>
\`\`\`

## License

View the [license information](https://example.com/license) for this image.
`

export function ArtifactDetail() {
  const { registryId, artifactType, namespace, artifactName } = useParams<{
    registryId: string
    artifactType: string
    namespace?: string
    artifactName: string
  }>()
  const navigate = useNavigate()
  const [versions, setVersions] = useState<any[]>([])
  const [readme, setReadme] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const mockVersions: Record<string, any[]> = {
      docker: [
        { tag: 'latest', digest: 'sha256:abc123def456789...', size: 142000000, created: '2024-01-15T10:00:00Z', signed: true, verified: true, uploadedBy: 'alice' },
        { tag: '1.25.0', digest: 'sha256:def456ghi789abc...', size: 141000000, created: '2024-01-10T08:30:00Z', signed: true, verified: true, uploadedBy: 'alice' },
        { tag: '1.24.0', digest: 'sha256:ghi789jkl012def...', size: 140000000, created: '2023-12-01T14:20:00Z', signed: false, verified: false, uploadedBy: 'bob' },
        { tag: '1.23.0', digest: 'sha256:jkl012mno345ghi...', size: 139000000, created: '2023-11-15T09:45:00Z', signed: true, verified: false, uploadedBy: 'bob' },
      ],
      maven: [
        { version: '3.12.0', released: '2024-01-10', size: '2.3 MB', type: 'jar', uploadedBy: 'alice' },
        { version: '3.11.0', released: '2023-12-15', size: '2.2 MB', type: 'jar', uploadedBy: 'bob' },
        { version: '3.10.0', released: '2023-11-20', size: '2.1 MB', type: 'jar', uploadedBy: 'bob' },
      ],
      npm: [
        { version: '18.19.0', published: '2024-01-12', size: '3.2 MB', type: 'tgz', uploadedBy: 'alice' },
        { version: '18.18.0', published: '2024-01-05', size: '3.1 MB', type: 'tgz', uploadedBy: 'bob' },
        { version: '18.17.0', published: '2023-12-20', size: '3.0 MB', type: 'tgz', uploadedBy: 'bob' },
      ],
    }

    setVersions(mockVersions[artifactType] || [])
    setReadme(artifactType === 'docker' ? MOCK_README : null)
    setLoading(false)
  }, [artifactType])

  const handleVersionClick = (version: string) => {
    navigate(`/registries/${registryId}/${artifactType}/${namespace}/${artifactName}/${version}`)
  }

  const formatSize = (bytes: number | string) => {
    if (typeof bytes === 'string') return bytes
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
  }

  const iconForType = (type: string) => {
    switch (type) {
      case 'docker': return '🐳'
      case 'maven': return '☕'
      case 'npm': return '📦'
      default: return '📦'
    }
  }

  return (
    <div className="space-y-8">
      <button
        onClick={() => navigate(`/registries/${registryId}/${artifactType}`)}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to {artifactType} list
      </button>

      <div className="card">
        <div className="flex items-start gap-6">
          <div className="w-20 h-20 bg-gradient-to-br from-blue-600 to-purple-600 rounded-2xl flex items-center justify-center flex-shrink-0 text-4xl shadow-lg shadow-blue-600/20">
            {iconForType(artifactType)}
          </div>
          <div className="flex-1">
            <div className="flex items-center gap-3 mb-2">
              <h1 className="text-3xl font-bold text-white">
                {namespace ? `${namespace}/${artifactName}` : artifactName}
              </h1>
              <span className="px-3 py-1 bg-gray-800 rounded-lg text-sm text-gray-400">
                {artifactType.toUpperCase()}
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
              <span>{versions.length} versions</span>
            </div>
          </div>
        </div>
      </div>

      {readme && (
        <div className="card">
          <h2 className="text-xl font-semibold text-gray-100 mb-4 flex items-center gap-2">
            <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
            </svg>
            README
          </h2>
          <div className="markdown-body">
            <ReactMarkdown>{readme}</ReactMarkdown>
          </div>
        </div>
      )}

      <div className="card">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-xl font-semibold text-gray-100">Versions</h2>
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1.5 text-xs text-gray-500">
              <div className="w-2.5 h-2.5 bg-green-500 rounded-full"></div>
              <span>Verified</span>
            </div>
            <div className="flex items-center gap-1.5 text-xs text-gray-500">
              <div className="w-2.5 h-2.5 bg-yellow-500 rounded-full"></div>
              <span>Unsigned</span>
            </div>
          </div>
        </div>

        {loading ? (
          <div className="flex items-center justify-center h-32">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left">
              <thead className="bg-gray-800/50 text-gray-400 text-sm">
                <tr>
                  <th className="px-4 py-3 rounded-l-lg font-medium">Version</th>
                  {artifactType === 'docker' && (
                    <>
                      <th className="px-4 py-3 font-medium">Digest (SHA-256)</th>
                      <th className="px-4 py-3 font-medium">Size</th>
                    </>
                  )}
                  {artifactType !== 'docker' && (
                    <th className="px-4 py-3 font-medium">Released/Published</th>
                  )}
                  {artifactType === 'docker' && <th className="px-4 py-3 rounded-r-lg font-medium">Signature</th>}
                </tr>
              </thead>
              <tbody className="text-sm">
                {versions.map((version, idx) => (
                  <tr
                    key={idx}
                    onClick={() => handleVersionClick(version.tag || version.version)}
                    className="border-b border-gray-800 hover:bg-gray-800/50 cursor-pointer transition-colors group"
                  >
                    <td className="px-4 py-3 font-mono text-blue-400 font-medium group-hover:text-blue-300">
                      {version.tag || version.version}
                    </td>
                    {artifactType === 'docker' && (
                      <>
                        <td className="px-4 py-3 font-mono text-xs text-gray-500 truncate max-w-xs">
                          {version.digest}
                        </td>
                        <td className="px-4 py-3 text-gray-300">{formatSize(version.size)}</td>
                      </>
                    )}
                    {artifactType !== 'docker' && (
                      <td className="px-4 py-3 text-gray-400">
                        {version.released || version.published}
                      </td>
                    )}
                    {artifactType === 'docker' && (
                      <td className="px-4 py-3">
                        {version.signed ? (
                          version.verified ? (
                            <span className="flex items-center gap-1.5 text-green-500 text-xs font-medium">
                              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
                              </svg>
                              Verified
                            </span>
                          ) : (
                            <span className="flex items-center gap-1.5 text-yellow-500 text-xs font-medium">
                              <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                              </svg>
                              Unsigned
                            </span>
                          )
                        ) : (
                          <span className="text-gray-500 text-xs">Unknown</span>
                        )}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
