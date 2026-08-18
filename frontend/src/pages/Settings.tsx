import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { AuditLogs } from './AuditLogs'

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

interface Registry {
  id: string
  name: string
  url: string
  type: string
  enabled: boolean
  priority: number
  private: boolean
  proxy: boolean
  host: string
  upstreamAuthType: string
  upstreamUsername: string
  hasUpstreamSecret: boolean
}

interface RegistryAccessGrant {
  registryId: string
  userId: string
  canRead: boolean
  canPublish: boolean
  grantedAt: string
}

const EMPTY_REGISTRY_FORM = {
  id: '',
  name: '',
  url: '',
  type: 'docker',
  enabled: true,
  priority: 0,
  private: false,
  proxy: false,
  host: '',
  upstreamAuthType: 'none',
  upstreamUsername: '',
  upstreamSecret: '',
  hasUpstreamSecret: false,
}
const EMPTY_USER_FORM = { username: '', email: '', password: '', role: 'viewer' }
const AVAILABLE_ROLES = ['admin', 'publisher', 'viewer']

interface VulnDBSettings {
  autoUpdateEnabled: boolean
  updateIntervalHours: number
  lastCheckedAt: string | null
  lastUpdatedAt: string | null
  lastError: string
}

type SettingsTab = 'general' | 'registries' | 'users' | 'audit' | 'vulndb'

export function Settings() {
  const { currentUser, token, isAdmin } = useAuth()
  const navigate = useNavigate()
  const [config, setConfig] = useState<AdminConfig | null>(null)
  const [configError, setConfigError] = useState<string | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [usersError, setUsersError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [activeTab, setActiveTab] = useState<SettingsTab>('general')

  const [registries, setRegistries] = useState<Registry[]>([])
  const [registriesError, setRegistriesError] = useState<string | null>(null)
  const [registriesLoading, setRegistriesLoading] = useState(true)
  const [showRegistryForm, setShowRegistryForm] = useState(false)
  const [editingRegistry, setEditingRegistry] = useState(false)
  const [registryForm, setRegistryForm] = useState(EMPTY_REGISTRY_FORM)
  const [registrySaveError, setRegistrySaveError] = useState<string | null>(null)

  const [accessGrants, setAccessGrants] = useState<RegistryAccessGrant[]>([])
  const [accessError, setAccessError] = useState<string | null>(null)
  const [grantUserId, setGrantUserId] = useState('')
  const [grantCanRead, setGrantCanRead] = useState(true)
  const [grantCanPublish, setGrantCanPublish] = useState(false)

  const [showUserForm, setShowUserForm] = useState(false)
  const [userForm, setUserForm] = useState(EMPTY_USER_FORM)
  const [userSaveError, setUserSaveError] = useState<string | null>(null)
  const [userSaving, setUserSaving] = useState(false)

  const [vulnDBSettings, setVulnDBSettings] = useState<VulnDBSettings | null>(null)
  const [vulnDBLoading, setVulnDBLoading] = useState(true)
  const [vulnDBError, setVulnDBError] = useState<string | null>(null)
  const [vulnDBSaving, setVulnDBSaving] = useState(false)
  const [vulnDBUpdating, setVulnDBUpdating] = useState(false)
  const [vulnDBForm, setVulnDBForm] = useState({ autoUpdateEnabled: true, updateIntervalHours: 24 })

  const loadVulnDBSettings = () => {
    if (!token) return
    setVulnDBLoading(true)
    fetch('/api/v1/settings/vulnerability-db', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data: VulnDBSettings) => {
        setVulnDBSettings(data)
        setVulnDBForm({ autoUpdateEnabled: data.autoUpdateEnabled, updateIntervalHours: data.updateIntervalHours })
        setVulnDBError(null)
      })
      .catch((err) => setVulnDBError(err.message))
      .finally(() => setVulnDBLoading(false))
  }

  useEffect(() => {
    if (activeTab === 'vulndb') loadVulnDBSettings()
  }, [activeTab])

  const saveVulnDBSettings = async () => {
    if (!token) return
    setVulnDBSaving(true)
    try {
      const res = await fetch('/api/v1/settings/vulnerability-db', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(vulnDBForm),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: VulnDBSettings = await res.json()
      setVulnDBSettings(data)
      setVulnDBError(null)
    } catch (err: any) {
      setVulnDBError(err.message)
    } finally {
      setVulnDBSaving(false)
    }
  }

  const triggerVulnDBUpdate = async () => {
    if (!token) return
    setVulnDBUpdating(true)
    try {
      const res = await fetch('/api/v1/settings/vulnerability-db/update', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: VulnDBSettings = await res.json()
      setVulnDBSettings(data)
      setVulnDBError(null)
    } catch (err: any) {
      setVulnDBError(err.message)
    } finally {
      setVulnDBUpdating(false)
    }
  }

  const loadRegistries = () => {
    setRegistriesLoading(true)
    fetch('/api/v1/registries')
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => {
        setRegistries(data.registries || [])
        setRegistriesError(null)
      })
      .catch((err) => setRegistriesError(err.message))
      .finally(() => setRegistriesLoading(false))
  }

  useEffect(() => {
    if (activeTab === 'registries') loadRegistries()
  }, [activeTab])

  const openCreateRegistry = () => {
    setEditingRegistry(false)
    setRegistryForm(EMPTY_REGISTRY_FORM)
    setRegistrySaveError(null)
    setShowRegistryForm(true)
  }

  const openEditRegistry = (reg: Registry) => {
    setEditingRegistry(true)
    // upstreamSecret is never returned by the API (write-only) - leaving it
    // blank here means "keep whatever's already stored" on save.
    setRegistryForm({ ...reg, upstreamSecret: '' })
    setRegistrySaveError(null)
    setShowRegistryForm(true)
    setAccessGrants([])
    setAccessError(null)
    if (reg.private) loadAccessGrants(reg.id)
  }

  const saveRegistry = async () => {
    if (!token) return
    if (!registryForm.id || !registryForm.name || !registryForm.url) {
      setRegistrySaveError('id, name, and url are required')
      return
    }
    try {
      const res = await fetch('/api/v1/registries', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(registryForm),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setShowRegistryForm(false)
      loadRegistries()
    } catch (err: any) {
      setRegistrySaveError(err.message)
    }
  }

  const deleteRegistry = async (id: string) => {
    if (!token) return
    if (!confirm(`Delete registry "${id}"? This cannot be undone.`)) return
    try {
      const res = await fetch(`/api/v1/registries/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      loadRegistries()
    } catch (err: any) {
      setRegistriesError(err.message)
    }
  }

  const loadAccessGrants = (registryId: string) => {
    if (!token) return
    fetch(`/api/v1/registries/${encodeURIComponent(registryId)}/access`, {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => {
        setAccessGrants(data.grants || [])
        setAccessError(null)
      })
      .catch((err) => setAccessError(err.message))
  }

  const grantAccess = async () => {
    if (!token || !editingRegistry || !grantUserId) return
    try {
      const res = await fetch(`/api/v1/registries/${encodeURIComponent(registryForm.id)}/access`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ userId: grantUserId, canRead: grantCanRead, canPublish: grantCanPublish }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setGrantUserId('')
      setGrantCanRead(true)
      setGrantCanPublish(false)
      loadAccessGrants(registryForm.id)
    } catch (err: any) {
      setAccessError(err.message)
    }
  }

  const revokeAccess = async (userId: string) => {
    if (!token || !editingRegistry) return
    try {
      const res = await fetch(
        `/api/v1/registries/${encodeURIComponent(registryForm.id)}/access/${encodeURIComponent(userId)}`,
        { method: 'DELETE', headers: { Authorization: `Bearer ${token}` } },
      )
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      loadAccessGrants(registryForm.id)
    } catch (err: any) {
      setAccessError(err.message)
    }
  }

  const loadUsers = () => {
    if (!token) return
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
  }

  const openCreateUser = () => {
    setUserForm(EMPTY_USER_FORM)
    setUserSaveError(null)
    setShowUserForm(true)
  }

  const createUser = async () => {
    if (!token) return
    if (!userForm.username || !userForm.email || !userForm.password) {
      setUserSaveError('username, email, and password are required')
      return
    }
    if (userForm.password.length < 8) {
      setUserSaveError('password must be at least 8 characters')
      return
    }
    setUserSaving(true)
    setUserSaveError(null)
    try {
      const res = await fetch('/api/v1/users', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          username: userForm.username,
          email: userForm.email,
          password: userForm.password,
          roles: [userForm.role],
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setShowUserForm(false)
      loadUsers()
    } catch (err: any) {
      setUserSaveError(err.message)
    } finally {
      setUserSaving(false)
    }
  }

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

  const tabs: { id: SettingsTab; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'registries', label: 'Registries' },
    { id: 'users', label: 'Users' },
    { id: 'vulndb', label: 'Vulnerability DB' },
    { id: 'audit', label: 'Audit' },
  ]

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-3xl font-bold text-white mb-2">Settings</h1>
        <p className="text-gray-400">Manage system configuration, registries, users, and audit logs</p>
      </div>

      <div className="flex gap-2 border-b border-gray-800">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab.id
                ? 'border-blue-500 text-white'
                : 'border-transparent text-gray-400 hover:text-gray-200'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {activeTab === 'audit' && <AuditLogs />}

      {activeTab === 'registries' && (
        <div className="card">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold text-gray-100">Registries</h2>
            <button onClick={openCreateRegistry} className="btn btn-primary btn-sm">
              + Add Registry
            </button>
          </div>

          {registriesError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
              {registriesError}
            </div>
          )}

          {showRegistryForm && (
            <div className="mb-6 p-4 bg-gray-800/40 border border-gray-700 rounded-lg space-y-3">
              <h3 className="text-sm font-medium text-gray-300">
                {editingRegistry ? `Edit "${registryForm.id}"` : 'New Registry'}
              </h3>
              {registrySaveError && (
                <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-sm">
                  {registrySaveError}
                </div>
              )}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <label className="text-sm text-gray-400">
                  ID
                  <input
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100 disabled:opacity-50"
                    value={registryForm.id}
                    disabled={editingRegistry}
                    onChange={(e) => setRegistryForm({ ...registryForm, id: e.target.value })}
                    placeholder="e.g. my-npm-mirror"
                  />
                </label>
                <label className="text-sm text-gray-400">
                  Name
                  <input
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={registryForm.name}
                    onChange={(e) => setRegistryForm({ ...registryForm, name: e.target.value })}
                    placeholder="e.g. My NPM Mirror"
                  />
                </label>
                <label className="text-sm text-gray-400 md:col-span-2">
                  URL
                  <input
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100 font-mono"
                    value={registryForm.url}
                    onChange={(e) => setRegistryForm({ ...registryForm, url: e.target.value })}
                    placeholder="https://registry.npmjs.org"
                  />
                </label>
                <label className="text-sm text-gray-400">
                  Type
                  <select
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={registryForm.type}
                    onChange={(e) => setRegistryForm({ ...registryForm, type: e.target.value })}
                  >
                    {['docker', 'npm', 'maven', 'pypi', 'nuget', 'helm'].map((t) => (
                      <option key={t} value={t}>{t}</option>
                    ))}
                  </select>
                </label>
                <label className="text-sm text-gray-400">
                  Priority
                  <input
                    type="number"
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={registryForm.priority}
                    onChange={(e) => setRegistryForm({ ...registryForm, priority: parseInt(e.target.value, 10) || 0 })}
                  />
                </label>
                <label className="text-sm text-gray-400">
                  Host <span className="text-gray-600">(optional)</span>
                  <input
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100 font-mono"
                    value={registryForm.host}
                    onChange={(e) => setRegistryForm({ ...registryForm, host: e.target.value })}
                    placeholder="e.g. private.mycompany.com"
                  />
                </label>
                <div className="flex items-center gap-4">
                  <label className="flex items-center gap-2 text-sm text-gray-400">
                    <input
                      type="checkbox"
                      checked={registryForm.enabled}
                      onChange={(e) => setRegistryForm({ ...registryForm, enabled: e.target.checked })}
                    />
                    Enabled
                  </label>
                  <label className="flex items-center gap-2 text-sm text-gray-400">
                    <input
                      type="checkbox"
                      checked={registryForm.proxy}
                      onChange={(e) => setRegistryForm({ ...registryForm, proxy: e.target.checked })}
                    />
                    Proxy upstream URL
                  </label>
                  <label className="flex items-center gap-2 text-sm text-gray-400">
                    <input
                      type="checkbox"
                      checked={registryForm.private}
                      onChange={(e) => setRegistryForm({ ...registryForm, private: e.target.checked })}
                    />
                    Private
                  </label>
                </div>
              </div>

              {registryForm.proxy && (
                <div className="pt-3 border-t border-gray-700 space-y-3">
                  <h4 className="text-sm font-medium text-gray-300">
                    Upstream authentication
                    <span className="text-gray-600 font-normal"> — credentials cargobay presents when pulling through this proxy, if the upstream itself requires auth</span>
                  </h4>
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
                    <label className="text-sm text-gray-400">
                      Type
                      <select
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={registryForm.upstreamAuthType}
                        onChange={(e) => setRegistryForm({ ...registryForm, upstreamAuthType: e.target.value })}
                      >
                        <option value="none">None</option>
                        <option value="basic">Basic (Maven / PyPI / Docker Hub-style)</option>
                        <option value="bearer">Bearer token (npm / GitHub Packages-style)</option>
                      </select>
                    </label>
                    {registryForm.upstreamAuthType === 'basic' && (
                      <label className="text-sm text-gray-400">
                        Username
                        <input
                          className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                          value={registryForm.upstreamUsername}
                          onChange={(e) => setRegistryForm({ ...registryForm, upstreamUsername: e.target.value })}
                        />
                      </label>
                    )}
                    {registryForm.upstreamAuthType !== 'none' && (
                      <label className="text-sm text-gray-400">
                        {registryForm.upstreamAuthType === 'bearer' ? 'Token' : 'Password'}
                        <input
                          type="password"
                          className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                          value={registryForm.upstreamSecret}
                          onChange={(e) => setRegistryForm({ ...registryForm, upstreamSecret: e.target.value })}
                          placeholder={editingRegistry && registryForm.hasUpstreamSecret ? 'Leave blank to keep existing' : ''}
                        />
                      </label>
                    )}
                  </div>
                </div>
              )}

              <div className="flex gap-2">
                <button onClick={saveRegistry} className="btn btn-primary btn-sm">
                  {editingRegistry ? 'Save Changes' : 'Create Registry'}
                </button>
                <button onClick={() => setShowRegistryForm(false)} className="btn btn-secondary btn-sm">
                  Cancel
                </button>
              </div>

              {editingRegistry && registryForm.private && (
                <div className="pt-3 border-t border-gray-700 space-y-3">
                  <h4 className="text-sm font-medium text-gray-300">Access grants</h4>
                  {accessError && (
                    <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-sm">
                      {accessError}
                    </div>
                  )}
                  <table className="w-full text-left text-sm">
                    <thead className="text-gray-500">
                      <tr>
                        <th className="pr-4 py-1 font-medium">User</th>
                        <th className="pr-4 py-1 font-medium">Read</th>
                        <th className="pr-4 py-1 font-medium">Publish</th>
                        <th className="py-1 font-medium text-right">Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {accessGrants.map((g) => (
                        <tr key={g.userId} className="border-t border-gray-800">
                          <td className="pr-4 py-1 text-gray-300">
                            {users.find((u) => u.userId === g.userId)?.username || g.userId}
                          </td>
                          <td className="pr-4 py-1 text-gray-400">{g.canRead ? 'yes' : 'no'}</td>
                          <td className="pr-4 py-1 text-gray-400">{g.canPublish ? 'yes' : 'no'}</td>
                          <td className="py-1 text-right">
                            <button onClick={() => revokeAccess(g.userId)} className="text-red-400 hover:text-red-300 text-sm">
                              Revoke
                            </button>
                          </td>
                        </tr>
                      ))}
                      {accessGrants.length === 0 && (
                        <tr>
                          <td colSpan={4} className="py-2 text-center text-gray-500">
                            No users granted access yet.
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                  <div className="flex items-end gap-3">
                    <label className="text-sm text-gray-400 flex-1">
                      Grant to user
                      <select
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={grantUserId}
                        onChange={(e) => setGrantUserId(e.target.value)}
                      >
                        <option value="">Select a user...</option>
                        {users.map((u) => (
                          <option key={u.userId} value={u.userId}>{u.username}</option>
                        ))}
                      </select>
                    </label>
                    <label className="flex items-center gap-2 text-sm text-gray-400">
                      <input type="checkbox" checked={grantCanRead} onChange={(e) => setGrantCanRead(e.target.checked)} />
                      Read
                    </label>
                    <label className="flex items-center gap-2 text-sm text-gray-400">
                      <input type="checkbox" checked={grantCanPublish} onChange={(e) => setGrantCanPublish(e.target.checked)} />
                      Publish
                    </label>
                    <button onClick={grantAccess} disabled={!grantUserId} className="btn btn-primary btn-sm">
                      Grant
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}

          {registriesLoading ? (
            <div className="flex items-center justify-center h-32">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead className="text-gray-500">
                  <tr>
                    <th className="pr-4 py-2 font-medium">ID</th>
                    <th className="pr-4 py-2 font-medium">Name</th>
                    <th className="pr-4 py-2 font-medium">Type</th>
                    <th className="pr-4 py-2 font-medium">URL</th>
                    <th className="pr-4 py-2 font-medium">Priority</th>
                    <th className="pr-4 py-2 font-medium">Enabled</th>
                    <th className="pr-4 py-2 font-medium">Visibility</th>
                    <th className="py-2 font-medium text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {registries.map((reg) => (
                    <tr key={reg.id} className="border-t border-gray-800">
                      <td className="pr-4 py-2 font-mono text-blue-400">{reg.id}</td>
                      <td className="pr-4 py-2 text-gray-300">{reg.name}</td>
                      <td className="pr-4 py-2 text-gray-400">{reg.type}</td>
                      <td className="pr-4 py-2 text-gray-400 font-mono truncate max-w-xs">{reg.url}</td>
                      <td className="pr-4 py-2 text-gray-400">{reg.priority}</td>
                      <td className="pr-4 py-2">
                        <span className={`badge ${reg.enabled ? 'badge-info' : 'badge-warning'}`}>
                          {reg.enabled ? 'enabled' : 'disabled'}
                        </span>
                      </td>
                      <td className="pr-4 py-2">
                        <span className={`badge ${reg.private ? 'badge-warning' : 'badge-success'}`}>
                          {reg.private ? 'private' : 'public'}
                        </span>
                      </td>
                      <td className="py-2 text-right space-x-2">
                        <button onClick={() => openEditRegistry(reg)} className="text-blue-400 hover:text-blue-300 text-sm">
                          Edit
                        </button>
                        <button onClick={() => deleteRegistry(reg.id)} className="text-red-400 hover:text-red-300 text-sm">
                          Delete
                        </button>
                      </td>
                    </tr>
                  ))}
                  {registries.length === 0 && (
                    <tr>
                      <td colSpan={8} className="py-6 text-center text-gray-500">
                        No registries configured yet.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {activeTab === 'general' && (loading ? (
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
              </div>
            )}
          </div>
        </>
      ))}

      {activeTab === 'vulndb' && (
        <div className="card">
          <h2 className="text-xl font-semibold text-gray-100 mb-4">Vulnerability DB</h2>
          <p className="text-sm text-gray-400 mb-4">
            Controls how often cargobay refreshes Trivy's vulnerability database, used to scan artifacts for known CVEs.
          </p>

          {vulnDBError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
              {vulnDBError}
            </div>
          )}

          {vulnDBLoading ? (
            <div className="flex items-center justify-center h-32">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="flex items-center gap-4">
                <label className="flex items-center gap-2 text-sm text-gray-400">
                  <input
                    type="checkbox"
                    checked={vulnDBForm.autoUpdateEnabled}
                    onChange={(e) => setVulnDBForm({ ...vulnDBForm, autoUpdateEnabled: e.target.checked })}
                  />
                  Auto-update enabled
                </label>
                <label className="text-sm text-gray-400">
                  Update interval (hours)
                  <input
                    type="number"
                    min={1}
                    className="mt-1 w-32 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={vulnDBForm.updateIntervalHours}
                    onChange={(e) => setVulnDBForm({ ...vulnDBForm, updateIntervalHours: parseInt(e.target.value, 10) || 1 })}
                  />
                </label>
              </div>

              <div className="bg-gray-800/30 rounded-lg p-4">
                <dl className="text-sm space-y-1">
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last checked</dt>
                    <dd className="text-gray-200 font-mono">
                      {vulnDBSettings?.lastCheckedAt ? new Date(vulnDBSettings.lastCheckedAt).toLocaleString() : 'never'}
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last updated</dt>
                    <dd className="text-gray-200 font-mono">
                      {vulnDBSettings?.lastUpdatedAt ? new Date(vulnDBSettings.lastUpdatedAt).toLocaleString() : 'never'}
                    </dd>
                  </div>
                </dl>
                {vulnDBSettings?.lastError && (
                  <div className="mt-3 px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-xs">
                    Last error: {vulnDBSettings.lastError}
                  </div>
                )}
              </div>

              <div className="flex gap-2">
                <button onClick={saveVulnDBSettings} disabled={vulnDBSaving} className="btn btn-primary btn-sm">
                  {vulnDBSaving ? 'Saving...' : 'Save settings'}
                </button>
                <button onClick={triggerVulnDBUpdate} disabled={vulnDBUpdating} className="btn btn-secondary btn-sm">
                  {vulnDBUpdating ? 'Updating...' : 'Update now'}
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      {activeTab === 'users' && (
        <div className="card">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold text-gray-100">Users</h2>
            <button onClick={openCreateUser} className="btn btn-primary btn-sm">
              + Add User
            </button>
          </div>

          {usersError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
              Failed to load users: {usersError}
            </div>
          )}

          {showUserForm && (
            <div className="mb-6 p-4 bg-gray-800/40 border border-gray-700 rounded-lg space-y-3">
              <h3 className="text-sm font-medium text-gray-300">New User</h3>
              {userSaveError && (
                <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-sm">
                  {userSaveError}
                </div>
              )}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <label className="text-sm text-gray-400">
                  Username
                  <input
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={userForm.username}
                    onChange={(e) => setUserForm({ ...userForm, username: e.target.value })}
                    placeholder="e.g. jdoe"
                  />
                </label>
                <label className="text-sm text-gray-400">
                  Email
                  <input
                    type="email"
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={userForm.email}
                    onChange={(e) => setUserForm({ ...userForm, email: e.target.value })}
                    placeholder="jdoe@example.com"
                  />
                </label>
                <label className="text-sm text-gray-400 md:col-span-2">
                  Password
                  <input
                    type="password"
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={userForm.password}
                    onChange={(e) => setUserForm({ ...userForm, password: e.target.value })}
                    placeholder="At least 8 characters"
                  />
                </label>
                <label className="text-sm text-gray-400">
                  Role
                  <select
                    className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={userForm.role}
                    onChange={(e) => setUserForm({ ...userForm, role: e.target.value })}
                  >
                    {AVAILABLE_ROLES.map((role) => (
                      <option key={role} value={role}>{role}</option>
                    ))}
                  </select>
                </label>
              </div>
              <div className="flex gap-2">
                <button onClick={createUser} disabled={userSaving} className="btn btn-primary btn-sm">
                  {userSaving ? 'Creating...' : 'Create User'}
                </button>
                <button onClick={() => setShowUserForm(false)} className="btn btn-secondary btn-sm">
                  Cancel
                </button>
              </div>
            </div>
          )}

          <div className="overflow-x-auto">
            <table className="w-full text-left">
              <thead className="bg-gray-800/50 text-gray-400 text-sm">
                <tr>
                  <th className="px-4 py-3 rounded-l-lg font-medium">Username</th>
                  <th className="px-4 py-3 font-medium">Email</th>
                  <th className="px-4 py-3 font-medium">Status</th>
                  <th className="px-4 py-3 font-medium">Roles</th>
                  <th className="px-4 py-3 rounded-r-lg font-medium text-right">Actions</th>
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
                    <td className="px-4 py-3 text-gray-400 capitalize">{user.roles[0] || 'viewer'}</td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={() => navigate(`/settings/users/${user.userId}`)}
                        className="text-blue-400 hover:text-blue-300 text-sm"
                      >
                        Edit
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
