import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

interface Registry {
  id: string
  name: string
  type: string
  url: string
  status: string
  lastSync: string
  syncStatus: 'syncing' | 'completed' | 'failed' | 'pending'
  upstreamUrl: string
  syncInterval: string
  lastError?: string
  artifactCount: number
  totalSize: number
  syncProgress: number
}

export function RegistryStatus() {
  const { currentUser } = useAuth()
  const navigate = useNavigate()
  const [registries, setRegistries] = useState<Registry[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState('all')
  const [sortBy, setSortBy] = useState<'name' | 'status' | 'lastSync'>('name')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [lastRefresh, setLastRefresh] = useState<Date>(new Date())

  useEffect(() => {
    const fetchRegistries = async () => {
      try {
        const mockRegistries: Registry[] = [
          {
            id: 'dockerhub',
            name: 'Docker Hub',
            type: 'docker',
            url: 'https://proxy.cargobay.io/docker',
            status: 'active',
            lastSync: new Date(Date.now() - 300000).toISOString(),
            syncStatus: 'completed',
            upstreamUrl: 'https://registry.hub.docker.com',
            syncInterval: '5 minutes',
            artifactCount: 12500,
            totalSize: 450000000000,
            syncProgress: 100,
          },
          {
            id: 'npm',
            name: 'NPM Registry',
            type: 'npm',
            url: 'https://proxy.cargobay.io/npm',
            status: 'active',
            lastSync: new Date(Date.now() - 60000).toISOString(),
            syncStatus: 'completed',
            upstreamUrl: 'https://registry.npmjs.org',
            syncInterval: '1 minute',
            artifactCount: 2500000,
            totalSize: 150000000000,
            syncProgress: 100,
          },
          {
            id: 'maven-central',
            name: 'Maven Central',
            type: 'maven',
            url: 'https://proxy.cargobay.io/maven',
            status: 'active',
            lastSync: new Date(Date.now() - 3600000).toISOString(),
            syncStatus: 'completed',
            upstreamUrl: 'https://repo.maven.apache.org',
            syncInterval: '1 hour',
            artifactCount: 500000,
            totalSize: 800000000000,
            syncProgress: 100,
          },
          {
            id: 'pypi',
            name: 'PyPI',
            type: 'pypi',
            url: 'https://proxy.cargobay.io/pypi',
            status: 'active',
            lastSync: new Date(Date.now() - 900000).toISOString(),
            syncStatus: 'syncing',
            upstreamUrl: 'https://pypi.org',
            syncInterval: '15 minutes',
            artifactCount: 450000,
            totalSize: 250000000000,
            syncProgress: 65,
          },
          {
            id: 'nuget',
            name: 'NuGet Gallery',
            type: 'nuget',
            url: 'https://proxy.cargobay.io/nuget',
            status: 'warning',
            lastSync: new Date(Date.now() - 86400000).toISOString(),
            syncStatus: 'failed',
            upstreamUrl: 'https://api.nuget.org',
            syncInterval: '24 hours',
            artifactCount: 180000,
            totalSize: 50000000000,
            syncProgress: 0,
            lastError: 'Connection timeout - upstream service unavailable',
          },
        ]
        setRegistries(mockRegistries)
        setLoading(false)
        setLastRefresh(new Date())
      } catch (err) {
        setLoading(false)
      }
    }

    fetchRegistries()

    if (autoRefresh) {
      const interval = setInterval(fetchRegistries, 30000)
      return () => clearInterval(interval)
    }
  }, [autoRefresh])

  const handleSort = (field: typeof sortBy) => {
    if (sortBy === field) {
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc')
    } else {
      setSortBy(field)
      setSortOrder('asc')
    }
  }

  const sortedRegistries = [...registries].sort((a, b) => {
    let comparison = 0
    switch (sortBy) {
      case 'name':
        comparison = a.name.localeCompare(b.name)
        break
      case 'status':
        comparison = a.status.localeCompare(b.status)
        break
      case 'lastSync':
        comparison = new Date(a.lastSync).getTime() - new Date(b.lastSync).getTime()
        break
    }
    return sortOrder === 'asc' ? comparison : -comparison
  })

  const filteredRegistries = filter === 'all'
    ? sortedRegistries
    : sortedRegistries.filter(r => {
        if (filter === 'active') return r.status === 'active'
        if (filter === 'warning') return r.status === 'warning'
        if (filter === 'error') return r.status === 'error'
        if (filter === 'syncing') return r.syncStatus === 'syncing'
        if (filter === 'failed') return r.syncStatus === 'failed'
        return true
      })

  const formatSize = (bytes: number): string => {
    if (bytes >= 1024 * 1024 * 1024 * 1024) {
      return `${(bytes / 1024 / 1024 / 1024 / 1024).toFixed(2)} TB`
    }
    if (bytes >= 1024 * 1024 * 1024) {
      return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
    }
    return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  }

  const formatRelativeTime = (dateString: string): string => {
    const date = new Date(dateString)
    const now = new Date()
    const diff = now.getTime() - date.getTime()
    const minutes = Math.floor(diff / (1000 * 60))
    const hours = Math.floor(diff / (1000 * 60 * 60))
    const days = Math.floor(diff / (1000 * 60 * 60 * 24))
    if (minutes < 1) return 'Just now'
    if (minutes < 60) return `${minutes}m ago`
    if (hours < 24) return `${hours}h ago`
    if (days < 7) return `${days}d ago`
    return date.toLocaleDateString()
  }

  const getStatusColor = (status: string): string => {
    switch (status) {
      case 'active': return 'bg-green-500/20 text-green-400 border-green-500/30'
      case 'warning': return 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30'
      case 'error': return 'bg-red-500/20 text-red-400 border-red-500/30'
      default: return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
    }
  }

  const getSyncStatusColor = (status: string): string => {
    switch (status) {
      case 'syncing': return 'bg-blue-500/20 text-blue-400 border-blue-500/30 animate-pulse'
      case 'completed': return 'bg-green-500/20 text-green-400 border-green-500/30'
      case 'failed': return 'bg-red-500/20 text-red-400 border-red-500/30'
      case 'pending': return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
      default: return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
    }
  }

  if (!currentUser) {
    return (
      <div className="max-w-4xl mx-auto">
        <div className="card text-center py-12">
          <h2 className="text-2xl font-bold text-white mb-4">Please Log In</h2>
          <p className="text-gray-400 mb-6">You need to be logged in to view registry status.</p>
          <button onClick={() => navigate('/login')} className="btn btn-primary">Log In</button>
        </div>
      </div>
    )
  }

  return (
    <div className="max-w-7xl mx-auto">
      <div className="flex flex-col md:flex-row md:items-center justify-between mb-8 gap-4">
        <div>
          <h1 className="text-3xl font-bold text-white mb-2">Registry Proxy Status</h1>
          <p className="text-gray-400">Monitor upstream registry synchronization and health</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className={`px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
              autoRefresh
                ? 'bg-blue-500/20 text-blue-400 border border-blue-500/30'
                : 'bg-gray-800 text-gray-400 border border-gray-700 hover:bg-gray-700'
            }`}
          >
            {autoRefresh ? 'Auto-refresh: ON' : 'Auto-refresh: OFF'}
          </button>
          <span className="text-sm text-gray-400">
            Last refresh: {formatRelativeTime(lastRefresh.toISOString())}
          </span>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Registries</div>
          <div className="text-2xl font-bold text-white mt-1">{registries.length}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Active Registries</div>
          <div className="text-2xl font-bold text-green-400 mt-1">
            {registries.filter(r => r.status === 'active').length}
          </div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Artifacts</div>
          <div className="text-2xl font-bold text-white mt-1">
            {registries.reduce((sum, r) => sum + r.artifactCount, 0).toLocaleString()}
          </div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Storage</div>
          <div className="text-2xl font-bold text-white mt-1">
            {formatSize(registries.reduce((sum, r) => sum + r.totalSize, 0))}
          </div>
        </div>
      </div>

      {/* Filters */}
      <div className="card p-6 mb-6">
        <div className="flex flex-wrap gap-2">
          {['all', 'active', 'warning', 'error', 'syncing', 'failed'].map(f => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={`px-4 py-2 rounded-full text-sm font-medium capitalize transition-colors ${
                filter === f
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
              }`}
            >
              {f}
            </button>
          ))}
        </div>
      </div>

      {/* Registries List */}
      {loading ? (
        <div className="card text-center py-12">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500 mx-auto mb-4"></div>
          <p className="text-gray-400">Loading registry status...</p>
        </div>
      ) : filteredRegistries.length === 0 ? (
        <div className="card text-center py-12">
          <div className="w-16 h-16 bg-gray-700 text-gray-400 rounded-full flex items-center justify-center mx-auto mb-4">
            <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.172 16.172a4 4 0 015.656 0M9 10h.01M15 10h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-white mb-2">No registries found</h3>
          <p className="text-gray-400">Try adjusting your filters</p>
        </div>
      ) : (
        <div className="space-y-4">
          {filteredRegistries.map(registry => (
            <div key={registry.id} className="card p-6">
              <div className="flex flex-col md:flex-row md:items-start justify-between gap-4">
                {/* Registry Info */}
                <div className="flex-1">
                  <div className="flex items-center gap-3 mb-2">
                    <h3 className="text-xl font-semibold text-white">{registry.name}</h3>
                    <span className={`px-3 py-1 text-xs rounded-full border ${getStatusColor(registry.status)}`}>
                      {registry.status}
                    </span>
                    <span className={`px-3 py-1 text-xs rounded-full border ${getSyncStatusColor(registry.syncStatus)}`}>
                      {registry.syncStatus}
                    </span>
                  </div>
                  <div className="text-gray-400 text-sm mb-4">
                    <span className="mr-4">
                      <span className="text-gray-500">Type:</span> {registry.type}
                    </span>
                    <span className="mr-4">
                      <span className="text-gray-500">Upstream:</span> {registry.upstreamUrl}
                    </span>
                    <span>
                      <span className="text-gray-500">Sync Interval:</span> {registry.syncInterval}
                    </span>
                  </div>

                  {/* Sync Progress */}
                  {registry.syncStatus === 'syncing' && (
                    <div className="mb-4">
                      <div className="flex justify-between text-sm mb-1">
                        <span className="text-blue-400">Syncing...</span>
                        <span className="text-blue-400">{registry.syncProgress}%</span>
                      </div>
                      <div className="w-full bg-gray-700 rounded-full h-2">
                        <div
                          className="bg-blue-600 h-2 rounded-full transition-all duration-500"
                          style={{ width: `${registry.syncProgress}%` }}
                        ></div>
                      </div>
                    </div>
                  )}

                  {/* Error Message */}
                  {registry.lastError && (
                    <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg mb-4">
                      <p className="text-red-400 text-sm flex items-center gap-2">
                        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                        {registry.lastError}
                      </p>
                    </div>
                  )}

                  {/* Stats */}
                  <div className="flex flex-wrap gap-4 text-sm">
                    <div className="flex items-center gap-2">
                      <span className="text-gray-500">Artifacts:</span>
                      <span className="text-white font-medium">{registry.artifactCount.toLocaleString()}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="text-gray-500">Total Size:</span>
                      <span className="text-white font-medium">{formatSize(registry.totalSize)}</span>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="text-gray-500">Last Sync:</span>
                      <span className={`font-medium ${registry.syncStatus === 'failed' ? 'text-red-400' : 'text-white'}`}>
                        {formatRelativeTime(registry.lastSync)}
                      </span>
                    </div>
                  </div>
                </div>

                {/* Actions */}
                <div className="flex flex-col gap-2 min-w-[140px]">
                  <button
                    onClick={() => navigate(`/registries/${registry.id}`)}
                    className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium transition-colors"
                  >
                    View Details
                  </button>
                  <div className="text-xs text-gray-500 px-2">
                    Artifacts auto-sync on request
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
