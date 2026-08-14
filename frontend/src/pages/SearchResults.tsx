import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

interface SearchResult {
  registry: string
  artifactType: string
  name: string
  namespace?: string
  description?: string
}

export function SearchResults() {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<Record<string, SearchResult[]>>({})
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const q = params.get('q')
    if (q) {
      setQuery(q)
      performSearch(q)
    }
  }, [])

  const performSearch = (q: string) => {
    setLoading(true)

    setTimeout(() => {
      const mockResults: Record<string, SearchResult[]> = {
        docker: [
          { registry: 'dockerhub', artifactType: 'docker', name: 'nginx', namespace: 'library', description: 'Official Nginx image' },
          { registry: 'dockerhub', artifactType: 'docker', name: 'redis', namespace: 'library', description: 'Official Redis image' },
          { registry: 'dockerhub', artifactType: 'docker', name: 'postgres', namespace: 'library', description: 'Official PostgreSQL image' },
          { registry: 'dockerhub', artifactType: 'docker', name: 'node', namespace: 'library', description: 'Official Node.js image' },
        ],
        maven: [
          { registry: 'maven-central', artifactType: 'maven', name: 'spring-boot-starter-web', namespace: 'org.springframework.boot', description: 'Spring Boot Web Starter' },
          { registry: 'maven-central', artifactType: 'maven', name: 'guava', namespace: 'com.google.guava', description: 'Google Core Libraries' },
        ],
        npm: [
          { registry: 'npm', artifactType: 'npm', name: 'react', description: 'A JavaScript library for building user interfaces' },
          { registry: 'npm', artifactType: 'npm', name: 'lodash', description: 'Lodash modular utilities' },
          { registry: 'npm', artifactType: 'npm', name: 'axios', description: 'Promise based HTTP client' },
        ],
      }

      setResults(mockResults)
      setLoading(false)
    }, 300)
  }

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (!query.trim()) return
    navigate(`/search?q=${encodeURIComponent(query.trim())}`)
    performSearch(query.trim())
  }

  const handleResultClick = (result: SearchResult) => {
    const path = result.namespace
      ? `/registries/${result.registry}/${result.artifactType}/${result.namespace}/${result.name}`
      : `/registries/${result.registry}/${result.artifactType}/${result.name}`
    navigate(path)
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
      <div>
        <h1 className="text-3xl font-bold text-white mb-2">Search Results</h1>
        <p className="text-gray-400">Search across all registries</p>
      </div>

      <div className="card">
        <form onSubmit={handleSearch} className="flex gap-4">
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search for containers, packages, artifacts..."
            className="flex-1 bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button
            type="submit"
            disabled={loading}
            className="bg-blue-600 hover:bg-blue-500 text-white px-6 py-3 rounded-lg font-medium transition-colors disabled:opacity-50 min-w-[120px]"
          >
            {loading ? 'Searching...' : 'Search'}
          </button>
        </form>
      </div>

      {Object.keys(results).length > 0 && (
        <div className="space-y-8">
          {Object.entries(results).map(([type, items]) => (
            items.length > 0 && (
              <div key={type}>
                <div className="flex items-center gap-3 mb-4">
                  <div className="text-2xl">{iconForType(type)}</div>
                  <h2 className="text-xl font-semibold text-gray-100 capitalize">{type} Results</h2>
                  <span className="px-2 py-1 bg-gray-800 rounded text-sm text-gray-400">
                    {items.length} found
                  </span>
                </div>
                <div className="space-y-3">
                  {items.map((result, idx) => (
                    <div
                      key={idx}
                      onClick={() => handleResultClick(result)}
                      className="card cursor-pointer hover:border-blue-500 hover:shadow-lg hover:shadow-blue-500/10 transition-all group"
                    >
                      <div className="flex items-start justify-between">
                        <div>
                          <div className="flex items-center gap-3 mb-1">
                            <h3 className="text-lg font-medium text-blue-400 font-mono group-hover:text-blue-300">
                              {result.name}
                            </h3>
                            <span className="px-2 py-0.5 bg-gray-800 rounded text-xs text-gray-500">
                              {result.artifactType.toUpperCase()}
                            </span>
                          </div>
                          <p className="text-sm text-gray-500">
                            Registry: {result.registry}
                            {result.namespace && <span> • Namespace: {result.namespace}</span>}
                          </p>
                          {result.description && (
                            <p className="text-sm text-gray-400 mt-1">{result.description}</p>
                          )}
                        </div>
                        <svg className="w-5 h-5 text-gray-500 group-hover:text-blue-400 transition-colors" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                        </svg>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )
          ))}
        </div>
      )}

      {loading && (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      )}

      {!loading && Object.keys(results).length === 0 && query && (
        <div className="text-center py-12 text-gray-500">
          <div className="mb-4 opacity-50">
            <svg className="w-16 h-16 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
            </svg>
          </div>
          <p>No results found for "{query}"</p>
        </div>
      )}

      {!loading && Object.keys(results).length === 0 && !query && (
        <div className="text-center py-12 text-gray-500">
          <div className="mb-4 opacity-50">
            <svg className="w-16 h-16 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </div>
          <p>Enter a search query to find artifacts across all registries</p>
        </div>
      )}
    </div>
  )
}
