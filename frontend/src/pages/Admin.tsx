import { useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'

interface User {
  userId: string
  username: string
  email: string
  createdAt: string
  lastLogin: string | null
  isActive: boolean
  roles: string[]
}

interface AdminConfig {
  server: { port: number; host: string }
  storage: { type: string; config: Record<string, string> }
  database: { type: string; dsn: string }
  cache: { type: string; url: string; ttl: string; maxSize: number }
  registries: { id: string; name: string; url: string; type: string; enabled: boolean; priority: number }[] | null
  editable: boolean
  note: string
}

const MOCK_ARTIFACTS = [
  { registryId: 'dockerhub', artifactType: 'docker', name: 'library/nginx', version: 'latest', uploadedBy: 'alice' },
  { registryId: 'dockerhub', artifactType: 'docker', name: 'library/nginx', version: '1.24.0', uploadedBy: 'bob' },
  { registryId: 'maven-central', artifactType: 'maven', name: 'com.google.guava/guava', version: '3.12.0', uploadedBy: 'alice' },
  { registryId: 'npm', artifactType: 'npm', name: 'react', version: '18.19.0', uploadedBy: 'alice' },
  { registryId: 'npm', artifactType: 'npm', name: 'lodash', version: '18.18.0', uploadedBy: 'bob' },
]

export function Admin() {
  const { currentUser, token, isAdmin } = useAuth()
  const [config, setConfig] = useState<AdminConfig | null>(null)
  const [configError, setConfigError] = useState<string | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [usersError, setUsersError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!isAdmin || !currentUser || !token) return

    setLoading(true)

    // Fetch config
    fetch('/api/v1/admin/config', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then(setConfig)
      .catch((err) => setConfigError(err.message))

    // Fetch users
    fetch('/api/v1/users', {
      headers: {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => {
        setUsers(data.users || [])
        setUsersError(null)
      })
      .catch((err) => setUsersError(err.message))
      .finally(() => setLoading(false))
  }, [isAdmin, currentUser, token])

  if (!isAdmin) {
    return (
      <div className="card max-w-md mx-auto text-center py-12">
        <h1 className="text-xl font-semibold text-white mb-2">Access denied</h1>
        <p className="text-gray-400">You need admin privileges to view this page.</p>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-3xl font-bold text-white mb-2">Admin</h1>
        <p className="text-gray-400">Manage users and artifacts</p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : (
        <>
          <div className="card">
            <h2 className="text-xl font-semibold text-gray-100 mb-4">Settings</h2>

            {configError && (
              <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
                Failed to load configuration: {configError}
              </div>
            )}

            {config && (
              <div className="space-y-4">
                <div className="px-4 py-3 bg-gray-800/50 border border-gray-700 rounded-lg text-gray-400 text-sm">
                  {config.note}
                </div>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div className="bg-gray-800/30 rounded-lg p-4">
                    <h3 className="text-sm font-medium text-gray-300 mb-2">Server</h3>
                    <dl className="text-sm space-y-1">
                      <div className="flex justify-between">
                        <dt className="text-gray-500">Host</dt>
                        <dd className="text-gray-200 font-mono">{config.server.host}</dd>
                      </div>
                      <div className="flex justify-between">
                        <dt className="text-gray-500">Port</dt>
                        <dd className="text-gray-200 font-mono">{config.server.port}</dd>
                      </div>
                    </dl>
                  </div>

                  <div className="bg-gray-800/30 rounded-lg p-4">
                    <h3 className="text-sm font-medium text-gray-300 mb-2">Storage</h3>
                    <dl className="text-sm space-y-1">
                      <div className="flex justify-between">
                        <dt className="text-gray-500">Backend</dt>
                        <dd className="text-gray-200 font-mono">{config.storage.type}</dd>
                      </div>
                      {Object.entries(config.storage.config ?? {}).map(([key, value]) => (
                        <div key={key} className="flex justify-between">
                          <dt className="text-gray-500">{key}</dt>
                          <dd className="text-gray-200 font-mono">{value}</dd>
                        </div>
                      ))}
                    </dl>
                  </div>

                  <div className="bg-gray-800/30 rounded-lg p-4">
                    <h3 className="text-sm font-medium text-gray-300 mb-2">Database</h3>
                    <dl className="text-sm space-y-1">
                      <div className="flex justify-between">
                        <dt className="text-gray-500">Type</dt>
                        <dd className="text-gray-200 font-mono">{config.database.type}</dd>
                      </div>
                      <div className="flex justify-between gap-4">
                        <dt className="text-gray-500 flex-shrink-0">DSN</dt>
                        <dd className="text-gray-200 font-mono truncate">{config.database.dsn}</dd>
                      </div>
                    </dl>
                  </div>

                  <div className="bg-gray-800/30 rounded-lg p-4">
                    <h3 className="text-sm font-medium text-gray-300 mb-2">Cache</h3>
                    <dl className="text-sm space-y-1">
                      <div className="flex justify-between">
                        <dt className="text-gray-500">Type</dt>
                        <dd className="text-gray-200 font-mono">{config.cache.type}</dd>
                      </div>
                      <div className="flex justify-between gap-4">
                        <dt className="text-gray-500 flex-shrink-0">URL</dt>
                        <dd className="text-gray-200 font-mono truncate">{config.cache.url}</dd>
                      </div>
                      <div className="flex justify-between">
                        <dt className="text-gray-500">TTL</dt>
                        <dd className="text-gray-200 font-mono">{config.cache.ttl}</dd>
                      </div>
                    </dl>
                  </div>
                </div>

                {config.registries && config.registries.length > 0 && (
                  <div className="bg-gray-800/30 rounded-lg p-4">
                    <h3 className="text-sm font-medium text-gray-300 mb-3">Registries</h3>
                    <div className="overflow-x-auto">
                      <table className="w-full text-left text-sm">
                        <thead className="text-gray-500">
                          <tr>
                            <th className="pr-4 py-1 font-medium">ID</th>
                            <th className="pr-4 py-1 font-medium">Name</th>
                            <th className="pr-4 py-1 font-medium">Type</th>
                            <th className="pr-4 py-1 font-medium">URL</th>
                            <th className="py-1 font-medium">Enabled</th>
                          </tr>
                        </thead>
                        <tbody>
                          {config.registries.map((reg) => (
                            <tr key={reg.id} className="border-t border-gray-800">
                              <td className="pr-4 py-2 font-mono text-blue-400">{reg.id}</td>
                              <td className="pr-4 py-2 text-gray-300">{reg.name}</td>
                              <td className="pr-4 py-2 text-gray-400">{reg.type}</td>
                              <td className="pr-4 py-2 text-gray-400 font-mono">{reg.url}</td>
                              <td className="py-2">
                                <span className={`badge ${reg.enabled ? 'badge-info' : 'badge-warning'}`}>
                                  {reg.enabled ? 'enabled' : 'disabled'}
                                </span>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>

          <div className="card">
            <h2 className="text-xl font-semibold text-gray-100 mb-4">Users</h2>

            {usersError && (
              <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
                Failed to load users: {usersError}
              </div>
            )}

            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead className="bg-gray-800/50 text-gray-400 text-sm">
                  <tr>
                    <th className="px-4 py-3 rounded-l-lg font-medium">Username</th>
                    <th className="px-4 py-3 font-medium">Email</th>
                    <th className="px-4 py-3 font-medium">Status</th>
                    <th className="px-4 py-3 rounded-r-lg font-medium">Roles</th>
                  </tr>
                </thead>
                <tbody className="text-sm">
                  {users.map((user) => (
                    <tr key={user.userId} className="border-b border-gray-800">
                      <td className="px-4 py-3 font-medium text-gray-100">{user.username}</td>
                      <td className="px-4 py-3 text-gray-400">{user.email}</td>
                      <td className="px-4 py-3">
                        <span className={`badge ${user.isActive ? 'badge-success' : 'badge-error'}`}>
                          {user.isActive ? 'active' : 'inactive'}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-1">
                          {user.roles.map((role) => (
                            <span
                              key={role}
                              className={`badge ${
                                role === 'admin' ? 'badge-warning' : role === 'developer' ? 'badge-blue' : role === 'publisher' ? 'badge-green' : role === 'auditor' ? 'badge-purple' : 'badge-info'
                              }`}
                            >
                              {role}
                            </span>
                          ))}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <div className="card">
            <h2 className="text-xl font-semibold text-gray-100 mb-4">Artifacts</h2>
            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead className="bg-gray-800/50 text-gray-400 text-sm">
                  <tr>
                    <th className="px-4 py-3 rounded-l-lg font-medium">Artifact</th>
                    <th className="px-4 py-3 font-medium">Registry</th>
                    <th className="px-4 py-3 font-medium">Version</th>
                    <th className="px-4 py-3 rounded-r-lg font-medium">Uploaded By</th>
                  </tr>
                </thead>
                <tbody className="text-sm">
                  {MOCK_ARTIFACTS.map((artifact, idx) => (
                    <tr key={idx} className="border-b border-gray-800">
                      <td className="px-4 py-3 font-mono text-blue-400">{artifact.name}</td>
                      <td className="px-4 py-3 text-gray-400">{artifact.registryId}</td>
                      <td className="px-4 py-3 text-gray-300">{artifact.version}</td>
                      <td className="px-4 py-3 text-gray-300">{artifact.uploadedBy}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
