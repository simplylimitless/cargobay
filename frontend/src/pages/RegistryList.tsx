import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

export function RegistryList() {
  const navigate = useNavigate()
  const [registries] = useState([
    { id: 'dockerhub', name: 'Docker Hub', url: 'https://registry-1.docker.io', type: 'docker', icon: '🐳' },
    { id: 'ghcr', name: 'GitHub Container Registry', url: 'https://ghcr.io', type: 'docker', icon: '🐙' },
    { id: 'quay', name: 'Quay.io', url: 'https://quay.io', type: 'docker', icon: '🛰️' },
    { id: 'npm', name: 'NPM Registry', url: 'https://registry.npmjs.org', type: 'npm', icon: '📦' },
    { id: 'maven-central', name: 'Maven Central', url: 'https://repo1.maven.org', type: 'maven', icon: '☕' },
    { id: 'artifactory', name: 'Artifactory', url: 'https://artifactory.example.com', type: 'artifactory', icon: '🏗️' },
  ])

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

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="card flex items-center gap-4">
          <div className="w-12 h-12 bg-blue-600/20 rounded-lg flex items-center justify-center text-blue-400">
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
            </svg>
          </div>
          <div>
            <div className="text-2xl font-bold text-white">Multi-Registry</div>
            <div className="text-sm text-gray-500">Docker, NPM, Maven, and more</div>
          </div>
        </div>
        <div className="card flex items-center gap-4">
          <div className="w-12 h-12 bg-green-600/20 rounded-lg flex items-center justify-center text-green-400">
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <div>
            <div className="text-2xl font-bold text-white">Verified</div>
            <div className="text-sm text-gray-500">SHA-256 signature verification</div>
          </div>
        </div>
        <div className="card flex items-center gap-4">
          <div className="w-12 h-12 bg-purple-600/20 rounded-lg flex items-center justify-center text-purple-400">
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div>
            <div className="text-2xl font-bold text-white">Fast</div>
            <div className="text-sm text-gray-500">Caching proxy for quick access</div>
          </div>
        </div>
      </div>

      {/* Registry Grid */}
      <div>
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-2xl font-semibold text-gray-100">Configured Registries</h2>
          <span className="px-3 py-1 bg-gray-800 rounded-full text-sm text-gray-400">
            {registries.length} registries
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {registries.map((registry) => (
            <div
              key={registry.id}
              onClick={() => handleRegistryClick(registry.id)}
              className="card cursor-pointer hover:border-blue-500/50 hover:shadow-xl hover:shadow-blue-500/10 hover:-translate-y-1 transition-all group"
            >
              <div className="flex items-start justify-between mb-4">
                <div className="w-14 h-14 bg-gray-800 rounded-xl flex items-center justify-center text-3xl group-hover:bg-blue-600/20 group-hover:text-blue-400 transition-colors">
                  {registry.icon}
                </div>
                <span className="px-3 py-1 bg-gray-800 rounded-full text-xs font-medium text-gray-400 uppercase tracking-wide">
                  {registry.type}
                </span>
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

          <div className="card border-dashed border-gray-700 hover:border-gray-600 cursor-pointer flex flex-col items-center justify-center py-12 group">
            <div className="w-12 h-12 bg-gray-800 rounded-lg flex items-center justify-center mb-4 group-hover:bg-blue-600/20 transition-colors">
              <svg className="w-6 h-6 text-gray-400 group-hover:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
            </div>
            <span className="text-gray-400 group-hover:text-gray-200 font-medium">Add New Registry</span>
            <span className="text-xs text-gray-600 mt-2">Configure in config.yaml</span>
          </div>
        </div>
      </div>
    </div>
  )
}
