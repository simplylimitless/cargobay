import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'

const mockData: Record<string, { name: string; description: string }[]> = {
  docker: [
    { name: 'nginx', description: 'Official Nginx image' },
    { name: 'redis', description: 'Official Redis image' },
    { name: 'postgres', description: 'Official PostgreSQL image' },
    { name: 'node', description: 'Official Node.js image' },
  ],
  maven: [
    { name: 'org.springframework.boot:spring-boot-starter-web', description: 'Spring Boot Web Starter' },
    { name: 'com.google.guava:guava', description: 'Google Core Libraries' },
    { name: 'org.apache.commons:commons-lang3', description: 'Apache Commons Lang' },
    { name: 'junit:junit', description: 'JUnit Testing Framework' },
  ],
  npm: [
    { name: 'react', description: 'A JavaScript library for building user interfaces' },
    { name: 'lodash', description: 'Lodash modular utilities' },
    { name: 'axios', description: 'Promise based HTTP client' },
    { name: 'typescript', description: 'TypeScript is a language for application scale JavaScript' },
  ],
}

export function ArtifactList() {
  const { registryId, artifactType, namespace } = useParams<{ registryId: string; artifactType: string; namespace?: string }>()
  const navigate = useNavigate()
  const [searchQuery, setSearchQuery] = useState('')

  const staticArtifacts = mockData[artifactType ?? ''] ?? []
  const filteredStaticArtifacts = staticArtifacts.filter((artifact) =>
    artifact.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    artifact.description?.toLowerCase().includes(searchQuery.toLowerCase())
  )

  const handleArtifactClick = (artifactName: string) => {
    if (namespace) {
      navigate(`/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`)
    } else {
      navigate(`/registries/${registryId}/${artifactType}/${artifactName}`)
    }
  }

  const iconForType = (type: string) => {
    switch (type) {
      case 'docker': return '🐳'
      case 'maven': return '☕'
      case 'npm': return '📦'
      case 'helm': return '⚓'
      default: return '📦'
    }
  }

  return (
    <div className="space-y-8">
      <button
        onClick={() => navigate(`/registries/${registryId}`)}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to {registryId}
      </button>

      <div className="card flex items-center gap-4">
        <div className="text-4xl">{iconForType(artifactType ?? '')}</div>
        <div>
          <h1 className="text-2xl font-bold text-gray-100 capitalize">{artifactType} Artifacts</h1>
          <p className="text-gray-400">
            {namespace ? `Namespace: ${namespace}` : 'All namespaces'}
          </p>
        </div>
      </div>

      <div className="card">
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder={`Search ${artifactType} artifacts...`}
          className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-gray-100 placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 mb-6"
        />

        <div className="space-y-3">
            {filteredStaticArtifacts.length === 0 ? (
              <div className="text-center py-12 text-gray-500">
                <div className="mb-4 opacity-50">
                  <svg className="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
                  </svg>
                </div>
                No artifacts found
              </div>
            ) : (
              filteredStaticArtifacts.map((artifact) => (
                <div
                  key={artifact.name}
                  onClick={() => handleArtifactClick(artifact.name)}
                  className="card cursor-pointer hover:border-blue-500 hover:shadow-md hover:bg-gray-800/50 transition-all group"
                >
                  <div className="flex items-start justify-between">
                    <div>
                      <div className="flex items-center gap-3 mb-1">
                        <h3 className="text-lg font-medium text-blue-400 font-mono group-hover:text-blue-300 transition-colors">
                          {artifact.name}
                        </h3>
                        <span className="px-2 py-0.5 bg-gray-800 rounded text-xs text-gray-500">
                          {artifactType?.toUpperCase()}
                        </span>
                      </div>
                      {artifact.description && (
                        <p className="text-sm text-gray-400">{artifact.description}</p>
                      )}
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
