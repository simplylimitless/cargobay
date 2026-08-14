import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'

export function RegistryDetail() {
  const { registryId } = useParams<{ registryId: string }>()
  const navigate = useNavigate()
  const [registry, setRegistry] = useState<any>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const registries = [
      { id: 'dockerhub', name: 'Docker Hub', url: 'https://registry-1.docker.io', type: 'docker' },
      { id: 'ghcr', name: 'GitHub Container Registry', url: 'https://ghcr.io', type: 'docker' },
      { id: 'quay', name: 'Quay.io', url: 'https://quay.io', type: 'docker' },
      { id: 'npm', name: 'NPM Registry', url: 'https://registry.npmjs.org', type: 'npm' },
      { id: 'maven-central', name: 'Maven Central', url: 'https://repo1.maven.org', type: 'maven' },
      { id: 'artifactory', name: 'Artifactory', url: 'https://artifactory.example.com', type: 'artifactory' },
    ]
    const reg = registries.find((r) => r.id === registryId)
    setRegistry(reg || null)
    setLoading(false)
  }, [registryId])

  const getArtifactTypes = (registryType: string) => {
    switch (registryType) {
      case 'docker':
        return [
          { id: 'docker', name: 'Container Images', icon: '🐳', desc: 'Browse Docker images' },
          { id: 'helm', name: 'Helm Charts', icon: '⚓', desc: 'Kubernetes charts' },
        ]
      case 'npm':
        return [
          { id: 'npm', name: 'NPM Packages', icon: '📦', desc: 'Node.js packages' },
        ]
      case 'maven':
        return [
          { id: 'maven', name: 'Maven Artifacts', icon: '☕', desc: 'Java libraries' },
          { id: 'gradle', name: 'Gradle Archives', icon: '⚛️', desc: 'Gradle dependencies' },
        ]
      case 'artifactory':
        return [
          { id: 'docker', name: 'Container Images', icon: '🐳', desc: 'Docker images' },
          { id: 'maven', name: 'Maven Artifacts', icon: '☕', desc: 'Java libraries' },
          { id: 'npm', name: 'NPM Packages', icon: '📦', desc: 'Node.js packages' },
          { id: 'pypi', name: 'Python Packages', icon: '🐍', desc: 'PyPI packages' },
          { id: 'nuget', name: 'NuGet Packages', icon: '🔨', desc: '.NET packages' },
        ]
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

  if (!registry) {
    return (
      <div className="text-center py-12">
        <div className="text-red-500 mb-4">
          <svg className="w-16 h-16 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
        </div>
        <h2 className="text-2xl font-bold text-gray-200 mb-2">Registry Not Found</h2>
        <p className="text-gray-500">The registry "{registryId}" does not exist.</p>
      </div>
    )
  }

  const artifactTypes = getArtifactTypes(registry.type)

  return (
    <div className="space-y-8">
      <button
        onClick={() => navigate('/')}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to registries
      </button>

      <div className="card">
        <div className="flex items-start gap-6">
          <div className="w-20 h-20 bg-gradient-to-br from-blue-600 to-purple-600 rounded-2xl flex items-center justify-center flex-shrink-0 text-5xl shadow-lg shadow-blue-600/20">
            {registry.name.charAt(0)}
          </div>
          <div className="flex-1">
            <h1 className="text-3xl font-bold text-white mb-2">{registry.name}</h1>
            <p className="text-gray-400 font-mono text-lg mb-4">{registry.url}</p>
            <div className="flex flex-wrap gap-3">
              <span className="px-4 py-2 bg-blue-600/20 border border-blue-500/30 rounded-lg text-blue-400 font-medium">
                Type: {registry.type}
              </span>
              <span className="px-4 py-2 bg-gray-800 rounded-lg text-gray-300">
                {artifactTypes.length} Artifact Types Available
              </span>
            </div>
          </div>
        </div>
      </div>

      <div>
        <h2 className="text-2xl font-semibold text-gray-100 mb-6">Available Artifact Types</h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {artifactTypes.map((artifact: any) => (
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
