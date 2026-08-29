import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth, useTimezone } from '../context/AuthContext'
import { formatDateTime, formatTime } from '../lib/datetime'

interface AuditLog {
  id: string
  userId: string
  username?: string
  email?: string
  action: string
  resourceType: string
  resourceId: string
  details: string
  createdAt: string
}

const CATEGORIES = ['all', 'user', 'registry', 'artifact']
const ACTIONS = [
  'all',
  'user.create',
  'user.update',
  'user.deactivate',
  'user.role_grant',
  'user.role_revoke',
  'user.login',
  'user.login_failed',
  'registry.upsert',
  'registry.delete',
  'registry.access_grant',
  'registry.access_revoke',
  'artifact.create',
  'artifact.delete',
  'artifact.scan',
]

const ITEMS_PER_PAGE = 20

export function AuditLogs() {
  const { currentUser, isAdmin, token } = useAuth()
  const timezone = useTimezone()
  const navigate = useNavigate()
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [filterCategory, setFilterCategory] = useState('all')
  const [filterAction, setFilterAction] = useState('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null)
  const [page, setPage] = useState(1)

  useEffect(() => {
    if (!isAdmin || !token) {
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    const params = new URLSearchParams()
    if (filterCategory !== 'all') params.set('category', filterCategory)
    if (filterAction !== 'all') params.set('action', filterAction)
    if (searchQuery) params.set('search', searchQuery)
    params.set('limit', String(ITEMS_PER_PAGE))
    params.set('offset', String((page - 1) * ITEMS_PER_PAGE))

    fetch(`/api/v1/audit/logs?${params.toString()}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then(async (res) => {
        if (!res.ok) {
          const data = await res.json().catch(() => ({}))
          throw new Error(data.error || `Request failed: ${res.status}`)
        }
        return res.json() as Promise<{ logs: AuditLog[]; total: number }>
      })
      .then((data) => {
        setLogs(data.logs || [])
        setTotal(data.total || 0)
      })
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }, [isAdmin, token, filterCategory, filterAction, searchQuery, page])

  useEffect(() => {
    setPage(1)
  }, [filterCategory, filterAction, searchQuery])

  const getCategoryColor = (category: string): string => {
    switch (category) {
      case 'artifact': return 'bg-blue-500/20 text-blue-400 border-blue-500/30'
      case 'user': return 'bg-green-500/20 text-green-400 border-green-500/30'
      case 'registry': return 'bg-purple-500/20 text-purple-400 border-purple-500/30'
      default: return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
    }
  }

  const isFailed = (action: string) => action.endsWith('_failed')

  const getResultColor = (action: string): string =>
    isFailed(action)
      ? 'bg-red-500/20 text-red-400 border-red-500/30'
      : 'bg-green-500/20 text-green-400 border-green-500/30'

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
      .replace(/_/g, ' ')
  }

  const parseDetails = (details: string): Record<string, unknown> | string => {
    try {
      return JSON.parse(details)
    } catch {
      return details
    }
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

  if (!isAdmin) {
    return (
      <div className="card max-w-md mx-auto text-center py-12">
        <h1 className="text-xl font-semibold text-white mb-2">Access denied</h1>
        <p className="text-gray-400">You need admin privileges to view audit logs.</p>
      </div>
    )
  }

  const totalPages = Math.max(1, Math.ceil(total / ITEMS_PER_PAGE))
  const failedLogins = logs.filter(l => l.action === 'user.login_failed').length
  const userActions = logs.filter(l => l.resourceType === 'user').length
  const registryActions = logs.filter(l => l.resourceType === 'registry').length

  return (
    <div>
      {/* Stats */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
        <div className="card p-4">
          <div className="text-sm text-gray-400">Total Events</div>
          <div className="text-2xl font-bold text-white mt-1">{total}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Failed Logins (page)</div>
          <div className="text-2xl font-bold text-red-400 mt-1">{failedLogins}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">User Actions (page)</div>
          <div className="text-2xl font-bold text-green-500 mt-1">{userActions}</div>
        </div>
        <div className="card p-4">
          <div className="text-sm text-gray-400">Registry Actions (page)</div>
          <div className="text-2xl font-bold text-purple-500 mt-1">{registryActions}</div>
        </div>
      </div>

      {/* Filters */}
      <div className="card p-6 mb-6">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div>
            <label className="block text-sm font-medium text-gray-300 mb-2">Category</label>
            <select
              value={filterCategory}
              onChange={(e) => setFilterCategory(e.target.value)}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              {CATEGORIES.map(c => (
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
              {ACTIONS.map(a => (
                <option key={a} value={a}>{a}</option>
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
      ) : error ? (
        <div className="card text-center py-12">
          <h3 className="text-lg font-medium text-white mb-2">Failed to load audit logs</h3>
          <p className="text-gray-400">{error}</p>
        </div>
      ) : logs.length === 0 ? (
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
                  <th className="text-left px-6 py-3 text-sm font-medium text-gray-300">Result</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-700">
                {logs.map(log => (
                  <tr
                    key={log.id}
                    className="hover:bg-gray-800/50 transition-colors cursor-pointer"
                    onClick={() => setSelectedLog(log)}
                  >
                    <td className="px-6 py-4 text-sm text-gray-300">
                      {formatRelativeTime(log.createdAt)}
                      <div className="text-xs text-gray-500">
                        {formatTime(log.createdAt, timezone)}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="text-white font-medium">{getActionLabel(log.action)}</div>
                    </td>
                    <td className="px-6 py-4">
                      <span className={`px-2 py-1 text-xs rounded-full border ${getCategoryColor(log.resourceType)}`}>
                        {log.resourceType}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <div className="text-white">{log.username || log.userId}</div>
                      {log.email && <div className="text-xs text-gray-500">{log.email}</div>}
                    </td>
                    <td className="px-6 py-4">
                      <span className={`px-2 py-1 text-xs rounded-full border ${getResultColor(log.action)}`}>
                        {isFailed(log.action) ? 'failure' : 'success'}
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
                Showing {(page - 1) * ITEMS_PER_PAGE + 1}-{Math.min(page * ITEMS_PER_PAGE, total)} of {total} entries
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
                    <span className={`px-3 py-1 rounded-full text-xs ${getCategoryColor(selectedLog.resourceType)}`}>
                      {selectedLog.resourceType}
                    </span>
                    <span className={`px-3 py-1 rounded-full text-xs ${getResultColor(selectedLog.action)}`}>
                      {isFailed(selectedLog.action) ? 'failure' : 'success'}
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
                    {formatDateTime(selectedLog.createdAt, timezone)}
                  </div>
                </div>
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">Event ID</h3>
                  <div className="text-gray-400 font-mono text-sm">{selectedLog.id}</div>
                </div>
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">User</h3>
                  <div className="text-white">{selectedLog.username || selectedLog.userId}</div>
                  {selectedLog.email && <div className="text-gray-400 text-sm">{selectedLog.email}</div>}
                </div>
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">Resource</h3>
                  <div className="text-white">{selectedLog.resourceType}</div>
                  <div className="text-gray-400 text-sm font-mono">{selectedLog.resourceId}</div>
                </div>
              </div>

              <div>
                <h3 className="text-sm font-medium text-gray-300 mb-2">Details</h3>
                <pre className="bg-gray-800 p-4 rounded-lg overflow-x-auto text-sm text-gray-300">
                  {JSON.stringify(parseDetails(selectedLog.details), null, 2)}
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
