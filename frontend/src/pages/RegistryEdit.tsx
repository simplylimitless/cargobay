import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

const REGISTRY_TYPE_GROUPS: [string, string[]][] = [
  ['Containers & Orchestration', ['docker', 'oci', 'helm']],
  ['Java & JVM', ['maven', 'maven-virtual', 'gradle', 'sbt']],
  ['JavaScript & Web', ['npm', 'bower']],
  ['Python', ['pypi', 'conda']],
  ['.NET', ['nuget']],
  ['System Packages', ['debian', 'rpm', 'alpine', 'yum']],
  ['Native & C/C++', ['conan', 'cocoapods']],
  ['Other Languages', ['go', 'cargo', 'composer', 'swift', 'dart']],
  ['Infrastructure & AI', ['terraform']],
]

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
  // Ordered member registry IDs for a "maven-virtual" registry (see
  // backend RegistryConfig.Members) - empty/absent for a normal registry.
  members?: string[]
}

interface RegistryAccessGrant {
  registryId: string
  userId: string
  canRead: boolean
  canPublish: boolean
  grantedAt: string
}

interface UserSummary {
  userId: string
  username: string
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
  members: [] as string[],
}

export function RegistryEdit() {
  const { registryId } = useParams<{ registryId: string }>()
  const isCreating = registryId === 'new'
  const navigate = useNavigate()
  const { token, isAdmin } = useAuth()

  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [registries, setRegistries] = useState<Registry[]>([])
  const [registryForm, setRegistryForm] = useState(EMPTY_REGISTRY_FORM)
  const [registrySaveError, setRegistrySaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const [users, setUsers] = useState<UserSummary[]>([])
  const [accessGrants, setAccessGrants] = useState<RegistryAccessGrant[]>([])
  const [accessError, setAccessError] = useState<string | null>(null)
  const [grantUserId, setGrantUserId] = useState('')
  const [grantCanRead, setGrantCanRead] = useState(true)
  const [grantCanPublish, setGrantCanPublish] = useState(false)

  const loadAccessGrants = (id: string) => {
    if (!token) return
    fetch(`/api/v1/registries/${encodeURIComponent(id)}/access`, {
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

  useEffect(() => {
    if (!token || !isAdmin || !registryId) return
    setLoading(true)

    const requests: Promise<any>[] = [
      fetch('/api/v1/registries?all=true', {
        headers: { Authorization: `Bearer ${token}` },
      }).then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      }),
      fetch('/api/v1/users', {
        headers: { Authorization: `Bearer ${token}` },
      }).then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      }),
    ]
    if (!isCreating) {
      requests.push(
        fetch(`/api/v1/registries/${encodeURIComponent(registryId)}`, {
          headers: { Authorization: `Bearer ${token}` },
        }).then((res) => {
          if (!res.ok) throw new Error(`Request failed: ${res.status}`)
          return res.json() as Promise<Registry>
        }),
      )
    }

    Promise.all(requests)
      .then(([registriesData, usersData, registry]) => {
        setRegistries(registriesData.registries || [])
        setUsers(usersData.users || [])
        if (registry) {
          // upstreamSecret is never returned by the API (write-only) -
          // leaving it blank here means "keep whatever's already stored"
          // on save.
          setRegistryForm({ ...registry, upstreamSecret: '', members: registry.members || [] })
          if (registry.private) loadAccessGrants(registry.id)
        } else {
          setRegistryForm(EMPTY_REGISTRY_FORM)
        }
        setLoadError(null)
      })
      .catch((err) => setLoadError(err.message))
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, isAdmin, registryId, isCreating])

  if (!isAdmin) {
    return (
      <div className="card max-w-md mx-auto text-center py-12">
        <h1 className="text-xl font-semibold text-white mb-2">Access denied</h1>
        <p className="text-gray-400">You need admin privileges to view this page.</p>
      </div>
    )
  }

  const saveRegistry = async () => {
    if (!token) return
    const isVirtual = registryForm.type === 'maven-virtual'
    if (!registryForm.id || !registryForm.name || (!isVirtual && !registryForm.url)) {
      setRegistrySaveError('id, name, and url are required')
      return
    }
    if (isVirtual && registryForm.members.length === 0) {
      setRegistrySaveError('A virtual Maven repository requires at least one member registry')
      return
    }
    // A newly created proxy registry that leaves Host blank, when another
    // registry of the same type already exists, would silently compete for
    // the same unbound default slot. Default it to the upstream hostname
    // instead, so it's reachable via a DNS/mirror override pointed at that
    // hostname rather than falling through to whichever registry already
    // holds the default.
    let host = registryForm.host
    if (isCreating && !host && registryForm.proxy && registries.some((r) => r.type === registryForm.type)) {
      try {
        host = new URL(registryForm.url).host
      } catch {
        // leave host blank if the URL isn't parseable yet; server-side
        // validation will surface the bad URL anyway
      }
    }
    setSaving(true)
    try {
      const res = await fetch('/api/v1/registries', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ ...registryForm, host }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      navigate('/settings?tab=registries')
    } catch (err: any) {
      setRegistrySaveError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const grantAccess = async () => {
    if (!token || isCreating || !grantUserId) return
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
    if (!token || isCreating) return
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

  return (
    <div className="max-w-3xl mx-auto space-y-8">
      <button
        onClick={() => navigate('/settings?tab=registries')}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to Settings
      </button>

      <div>
        <h1 className="text-3xl font-bold text-white mb-1">
          {isCreating ? 'New Registry' : `Edit "${registryForm.id}"`}
        </h1>
        <p className="text-gray-400">
          {isCreating ? 'Register a new artifact registry' : 'Update registry configuration'}
        </p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : loadError ? (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          Failed to load registry: {loadError}
        </div>
      ) : (
        <div className="card space-y-3">
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
                disabled={!isCreating}
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
            {registryForm.type !== 'maven-virtual' && (
              <label className="text-sm text-gray-400 md:col-span-2">
                {registryForm.proxy ? 'Upstream URL' : 'URL'}
                <input
                  className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100 font-mono"
                  value={registryForm.url}
                  onChange={(e) => setRegistryForm({ ...registryForm, url: e.target.value })}
                  placeholder="https://registry.npmjs.org"
                />
                {registryForm.proxy && (
                  <span className="text-xs text-gray-600 font-normal">Cargobay pulls from and caches this URL on demand.</span>
                )}
              </label>
            )}
            <label className="text-sm text-gray-400">
              Type
              <select
                className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                value={registryForm.type}
                onChange={(e) => setRegistryForm({ ...registryForm, type: e.target.value })}
              >
                {REGISTRY_TYPE_GROUPS.map(([label, types]) => (
                  <optgroup key={label} label={label}>
                    {types.map((t) => (
                      <option key={t} value={t}>{t}</option>
                    ))}
                  </optgroup>
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
              {registryForm.type !== 'maven-virtual' && (
                <label className="flex items-center gap-2 text-sm text-gray-400">
                  <input
                    type="checkbox"
                    checked={registryForm.proxy}
                    onChange={(e) => setRegistryForm({ ...registryForm, proxy: e.target.checked })}
                  />
                  Upstream Proxy
                </label>
              )}
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

          {registryForm.type === 'maven-virtual' && (
            <div className="pt-3 border-t border-gray-700 space-y-3">
              <h4 className="text-sm font-medium text-gray-300">
                Member Repositories
                <span className="text-gray-600 font-normal"> — tried in order; the first one with the artifact is used and cached</span>
              </h4>
              <div className="flex gap-2">
                <select
                  className="flex-1 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                  value=""
                  onChange={(e) => {
                    const id = e.target.value
                    if (id && !registryForm.members.includes(id)) {
                      setRegistryForm({ ...registryForm, members: [...registryForm.members, id] })
                    }
                  }}
                >
                  <option value="">Add member registry…</option>
                  {registries
                    .filter((r) => r.type === 'maven' && !registryForm.members.includes(r.id))
                    .map((r) => (
                      <option key={r.id} value={r.id}>{r.name} ({r.id})</option>
                    ))}
                </select>
              </div>
              {registryForm.members.length === 0 ? (
                <p className="text-sm text-gray-500">No members added yet.</p>
              ) : (
                <ul className="space-y-1">
                  {registryForm.members.map((memberId, idx) => (
                    <li
                      key={memberId}
                      className="flex items-center gap-2 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-sm text-gray-200"
                    >
                      <span className="text-gray-500 w-5">{idx + 1}.</span>
                      <span className="flex-1 font-mono">{memberId}</span>
                      <button
                        type="button"
                        className="text-gray-400 hover:text-white disabled:opacity-30"
                        disabled={idx === 0}
                        onClick={() => {
                          const members = [...registryForm.members]
                          ;[members[idx - 1], members[idx]] = [members[idx], members[idx - 1]]
                          setRegistryForm({ ...registryForm, members })
                        }}
                      >
                        ↑
                      </button>
                      <button
                        type="button"
                        className="text-gray-400 hover:text-white disabled:opacity-30"
                        disabled={idx === registryForm.members.length - 1}
                        onClick={() => {
                          const members = [...registryForm.members]
                          ;[members[idx + 1], members[idx]] = [members[idx], members[idx + 1]]
                          setRegistryForm({ ...registryForm, members })
                        }}
                      >
                        ↓
                      </button>
                      <button
                        type="button"
                        className="text-red-400 hover:text-red-300"
                        onClick={() => setRegistryForm({ ...registryForm, members: registryForm.members.filter((m) => m !== memberId) })}
                      >
                        Remove
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {registryForm.proxy && registryForm.type !== 'maven-virtual' && (
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
                      placeholder={!isCreating && registryForm.hasUpstreamSecret ? 'Leave blank to keep existing' : ''}
                    />
                  </label>
                )}
              </div>
            </div>
          )}

          <div className="flex gap-2">
            <button onClick={saveRegistry} disabled={saving} className="btn btn-primary btn-sm">
              {saving ? 'Saving...' : isCreating ? 'Create Registry' : 'Save Changes'}
            </button>
            <button onClick={() => navigate('/settings?tab=registries')} className="btn btn-secondary btn-sm">
              Cancel
            </button>
          </div>

          {!isCreating && registryForm.private && (
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
    </div>
  )
}
