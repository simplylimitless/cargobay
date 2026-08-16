import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

interface AuditLog {
  id: string
  timestamp: string
  action: string
  category: 'artifact' | 'user' | 'registry' | 'system' | 'security'
  user: string
  email: string
  ip: string
  details: Record<string, unknown>
  result: 'success' | 'failure'
}

export function AuditLogs() {
  const { currentUser, isAdmin } = useAuth()
  const navigate = useNavigate()
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [filterCategory, setFilterCategory] = useState('all')
  const [filterAction, setFilterAction] = useState('all')
  const [filterResult, setFilterResult] = useState('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null)
  const [page, setPage] = useState(1)
  const itemsPerPage = 20

  useEffect(() => {
    const fetchLogs = async () => {
      try {
        const mockLogs: AuditLog[] = [
          { id: '1', timestamp: '2024-02-15T14:30:00Z', action: 'artifact.upload', category: 'artifact', user: 'john.doe', email: 'john@example.com', ip: '192.168.1.100', details: { artifactName: 'nginx', version: '1.25.3', type: 'docker', size: 145000000 }, result: 'success' },
          { id: '2', timestamp: '2024-02-15T14:25:00Z', action: 'artifact.delete', category: 'artifact', user: 'jane.smith', email: 'jane@example.com', ip: '192.168.1.101', details: { artifactName: 'redis', version: '7.0.0', type: 'docker' }, result: 'success' },
          { id: '3', timestamp: '2024-02-15T14:20:00Z', action: 'user.create', category: 'user', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { username: 'new.user', email: 'new@example.com', role: 'developer' }, result: 'success' },
          { id: '4', timestamp: '2024-02-15T14:15:00Z', action: 'artifact.download', category: 'artifact', user: 'alice.jones', email: 'alice@example.com', ip: '192.168.1.102', details: { artifactName: 'nginx', version: '1.25.3', type: 'docker', size: 145000000 }, result: 'success' },
          { id: '5', timestamp: '2024-02-15T14:10:00Z', action: 'artifact.download', category: 'artifact', user: 'bob.wilson', email: 'bob@example.com', ip: '192.168.1.102', details: { artifactName: 'express', version: '4.18.2', type: 'npm', size: 50000 }, result: 'success' },
          { id: '6', timestamp: '2024-02-15T14:05:00Z', action: 'user.login', category: 'security', user: 'alice.jones', email: 'alice@example.com', ip: '203.0.113.50', details: { method: 'password', success: true }, result: 'failure' },
          { id: '7', timestamp: '2024-02-15T14:00:00Z', action: 'artifact.scan', category: 'artifact', user: 'system', email: 'system@cargobay.io', ip: '127.0.0.1', details: { artifactName: 'lodash', version: '4.17.21', vulnerabilitiesFound: 2 }, result: 'success' },
          { id: '8', timestamp: '2024-02-15T13:55:00Z', action: 'registry.update', category: 'registry', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { registryId: 'npm', url: 'https://registry.npmjs.org', enabled: true }, result: 'success' },
          { id: '9', timestamp: '2024-02-15T13:50:00Z', action: 'user.delete', category: 'user', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { username: 'former.employee', email: 'former@example.com' }, result: 'success' },
          { id: '10', timestamp: '2024-02-15T13:45:00Z', action: 'artifact.upload', category: 'artifact', user: 'charlie.brown', email: 'charlie@example.com', ip: '192.168.1.103', details: { artifactName: 'spring-core', version: '6.1.3', type: 'maven', size: 600000 }, result: 'success' },
          { id: '11', timestamp: '2024-02-15T13:40:00Z', action: 'security.policy.check', category: 'security', user: 'system', email: 'system@cargobay.io', ip: '127.0.0.1', details: { artifactName: 'requests', version: '2.31.0', policyViolations: 0 }, result: 'success' },
          { id: '12', timestamp: '2024-02-15T13:35:00Z', action: 'artifact.tag', category: 'artifact', user: 'dave.lee', email: 'dave@example.com', ip: '192.168.1.104', details: { artifactName: 'nginx', version: '1.25.3', tag: 'stable', added: true }, result: 'success' },
          { id: '13', timestamp: '2024-02-15T13:30:00Z', action: 'user.login', category: 'security', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { method: 'api-key', success: true }, result: 'success' },
          { id: '14', timestamp: '2024-02-15T13:25:00Z', action: 'artifact.delete', category: 'artifact', user: 'eve.taylor', email: 'eve@example.com', ip: '192.168.1.105', details: { artifactName: 'tmp-artifact', version: '0.0.1', type: 'generic' }, result: 'success' },
          { id: '15', timestamp: '2024-02-15T13:20:00Z', action: 'system.config.update', category: 'system', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { setting: 'cache.ttl', oldValue: 3600, newValue: 7200 }, result: 'success' },
          { id: '16', timestamp: '2024-02-15T13:15:00Z', action: 'artifact.download', category: 'artifact', user: 'frank.miller', email: 'frank@example.com', ip: '192.168.1.106', details: { artifactName: 'nuget-package', version: '1.0.0', type: 'nuget', size: 800000 }, result: 'success' },
          { id: '17', timestamp: '2024-02-15T13:10:00Z', action: 'user.update', category: 'user', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { username: 'jane.smith', roleChange: 'viewer', oldValue: 'developer' }, result: 'success' },
          { id: '18', timestamp: '2024-02-15T13:05:00Z', action: 'artifact.upload', category: 'artifact', user: 'grace.wilson', email: 'grace@example.com', ip: '192.168.1.107', details: { artifactName: 'helm-chart', version: '1.2.0', type: 'helm', size: 25000 }, result: 'success' },
          { id: '19', timestamp: '2024-02-15T13:00:00Z', action: 'registry.test', category: 'registry', user: 'admin', email: 'admin@example.com', ip: '10.0.0.1', details: { registryId: 'pypi', connection: true, status: 'healthy' }, result: 'success' },
          { id: '20', timestamp: '2024-02-15T12:55:00Z', action: 'artifact.scan', category: 'artifact', user: 'system', email: 'system@cargobay.io', ip: '127.0.0.1', details: { artifactName: 'nginx', version: '1.25.3', vulnerabilitiesFound: 0 }, result: 'success' },
        ]
        setLogs(mockLogs)
        setLoading(false)
      } catch (err) {
        setLoading(false)
      }
    }

    fetchLogs()
  }, [])

  const filteredLogs = logs.filter(log => {
    if (filterCategory !== 'all' && log.category !== filterCategory) return false
    if (filterAction !== 'all' && log.action !== filterAction) return false
    if (filterResult !== 'all' && log.result !== filterResult) return false
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      return (
        log.user.toLowerCase().includes(query) ||
        log.email.toLowerCase().includes(query) ||
        log.action.toLowerCase().includes(query) ||
        JSON.stringify(log.details).toLowerCase().includes(query)
      )
    }
    return true
  })

  const paginatedLogs = filteredLogs.slice((page - 1) * itemsPerPage, page * itemsPerPage)
  const totalPages = Math.ceil(filteredLogs.length / itemsPerPage)

  const getCategoryColor = (category: string): string => {
    switch (category) {
      case 'artifact': return 'bg-blue-500/20 text-blue-400 border-blue-500/30'
      case 'user': return 'bg-green-500/20 text-green-400 border-green-500/30'
      case 'registry': return 'bg-purple-500/20 text-purple-400 border-purple-500/30'
      case 'system': return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
      case 'security': return 'bg-orange-500/20 text-orange-400 border-orange-500/30'
      default: return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
    }
  }

  const getResultColor = (result: string): string => {
    switch (result) {
      case 'success': return 'bg-green-500/20 text-green-400 border-green-500/30'
      case 'failure': return 'bg-red-500/20 text-red-400 border-red-500/30'
      default: return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
    }
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
    return `${days}d ago`
  }

  const getActionLabel = (action: string): string => {
    return action
      .split('.')
      .map(part => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ')
  }

  if (!currentUser) {
    return (
      <div className="max-w-4xl mx-auto">
        <div className="card text-center py-12">
          <h2 className="text-2xl font-bold text-white mb-4">Please Log In</h2>
          <p className="text-gray-400 mb-6">You need to be logged in to view audit logs.</p>
          <button onClick={() => navigate('/login')} className="btn btn-primary">Log In</button>
        </div>
      </div>
    )
  }

  return (
    <div className="max-w-7xl mx-auto">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-white mb-2">Audit Logs</h1>
        <p className="text-gray-400">Track all user actions and system events</p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Events</div>
          <div className="text-2xl font-bold text-white mt-1">{filteredLogs.length}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Success Rate</div>
          <div className="text-2xl font-bold text-green-400 mt-1">
            {Math.round((filteredLogs.filter(l => l.result === 'success').length / Math.max(1, filteredLogs.length)) * 100)}%
          </div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Security Events</div>
          <div className="text-2xl font-bold text-orange-500 mt-1">
            {filteredLogs.filter(l => l.category === 'security').length}
          </div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Artifact Actions</div>
          <div className="text-2xl font-bold text-blue-500 mt-1">
            {filteredLogs.filter(l => l.category === 'artifact').length}
          </div>
        </div>
      </div>

      {/* Filters */}
      <div className="card p-6 mb-6">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Category</label>
            <select
              value={filterCategory}
              onChange={(e) => setFilterCategory(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              {['all', 'artifact', 'user', 'registry', 'system', 'security'].map(c => (
                <option key={c} value={c}>{c.charAt(0).toUpperCase() + c.slice(1)}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Action</label>
            <select
              value={filterAction}
              onChange={(e) => setFilterAction(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              {['all', 'artifact.upload', 'artifact.download', 'artifact.delete', 'artifact.scan', 'artifact.tag', 'user.create', 'user.update', 'user.delete', 'user.login', 'registry.update', 'registry.test', 'system.config.update', 'security.policy.check'].map(a => (
                <option key={a} value={a}>{a}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Result</label>
            <select
              value={filterResult}
              onChange={(e) => setFilterResult(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              {['all', 'success', 'failure'].map(r => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Search</label>
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search logs..."
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            />
          </div>
        </div>
      </div>

      {/* Logs Table */}
      {loading ? (
        <div className="card text-center py-12">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500 mx-auto mb-4"></div>
          <p className="text-gray-400">Loading audit logs...</p>
        </div>
      ) : filteredLogs.length === 0 ? (
        <div className="card text-center py-12">
          <div className="w-16 h-16 bg-gray-700 text-gray-400 rounded-full flex items-center justify-center mx-auto mb-4">
            <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
          </div>
          <h3 className="text-lg font-medium text-white mb-2">No audit logs found</h3>
          <p className="text-gray-400">Try adjusting your filters</p>
        </div>
      ) : (
        <>
          <div className="card overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-gray-700">
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Time</th>
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Action</th>
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Category</th>
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">User</th>
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">IP</th>
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Result</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-700">
                {paginatedLogs.map(log => (
                  <tr
                    key={log.id}
                    className="hover:bg-gray-800/50 transition-colors cursor-pointer"
                    onClick={() => setSelectedLog(log)}
                  >
                    <td className="px-6 py-4 text-sm text-gray-300">
                      {formatRelativeTime(log.timestamp)}
                      <div className="text-xs text-gray-500">
                        {new Date(log.timestamp).toLocaleTimeString()}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="text-white font-medium">{getActionLabel(log.action)}</div>
                    </td>
                    <td className="px-6 py-4">
                      <span className={`px-2 py-1 text-xs rounded-full border ${getCategoryColor(log.category)}`}>
                        {log.category}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <div className="text-white">{log.user}</div>
                      <div className="text-xs text-gray-500">{log.email}</div>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-300">{log.ip}</td>
                    <td className="px-6 py-4">
                      <span className={`px-2 py-1 text-xs rounded-full border ${getResultColor(log.result)}`}>
                        {log.result}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-6 px-6">
              <div className="text-gray-400 text-sm">
                Showing {(page - 1) * itemsPerPage + 1}-{Math.min(page * itemsPerPage, filteredLogs.length)} of {filteredLogs.length} entries
              </div>
              <div className="flex gap-2">
                <button
                  onClick={() => setPage(Math.max(1, page - 1))}
                  disabled={page === 1}
                  className="px-3 py-1 bg-gray-800 hover:bg-gray-700 text-white rounded-lg text-sm disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Previous
                </button>
                <button
                  onClick={() => setPage(Math.min(totalPages, page + 1))}
                  disabled={page === totalPages}
                  className="px-3 py-1 bg-gray-800 hover:bg-gray-700 text-white rounded-lg text-sm disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </>
      )}

      {/* Log Details Modal */}
      {selectedLog && (
        <div className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4 z-50">
          <div className="bg-gray-900 rounded-xl max-w-2xl w-full max-h-[90vh] overflow-y-auto border border-gray-700 shadow-2xl">
            <div className="p-6 border-b border-gray-700">
              <div className="flex items-start justify-between">
                <div>
                  <h2 className="text-xl font-bold text-white mb-2">{getActionLabel(selectedLog.action)}</h2>
                  <div className="flex items-center gap-3">
                    <span className={`px-3 py-1 rounded-full text-xs ${getCategoryColor(selectedLog.category)}`}>
                      {selectedLog.category}
                    </span>
                    <span className={`px-3 py-1 rounded-full text-xs ${getResultColor(selectedLog.result)}`}>
                      {selectedLog.result}
                    </span>
                  </div>
                </div>
                <button
                  onClick={() => setSelectedLog(null)}
                  className="text-gray-400 hover:text-white"
                >
                  <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="p-6 space-y-6">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">Timestamp</h3>
                  <div className="text-gray-400">
                    {new Date(selectedLog.timestamp).toLocaleString()}
                  </div>
                </div>
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">Event ID</h3>
                  <div className="text-gray-400 font-mono text-sm">{selectedLog.id}</div>
                </div>
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">User</h3>
                  <div className="text-white">{selectedLog.user}</div>
                  <div className="text-gray-400 text-sm">{selectedLog.email}</div>
                </div>
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">IP Address</h3>
                  <div className="text-white">{selectedLog.ip}</div>
                </div>
              </div>

              <div>
                <h3 className="text-sm font-medium text-gray-300 mb-2">Details</h3>
                <pre className="bg-gray-800 p-4 rounded-lg overflow-x-auto text-sm text-gray-300">
                  {JSON.stringify(selectedLog.details, null, 2)}
                </pre>
              </div>
            </div>

            <div className="p-6 border-t border-gray-700">
              <button
                onClick={() => setSelectedLog(null)}
                className="w-full px-4 py-2 bg-gray-800 hover:bg-gray-700 text-white rounded-lg text-sm font-medium transition-colors"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
