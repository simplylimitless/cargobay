import { useEffect, useState } from 'react'
import { useAuth, useTimezone } from '../context/AuthContext'
import { useConfirm } from '../hooks/useConfirm'
import { formatDateTime, listTimezones } from '../lib/datetime'

interface AccessKeyMetadata {
  id: string
  name: string
  description: string
  scope: 'read' | 'read-write'
  createdAt: string
  lastUsed: string | null
  expiresAt: string | null
  isActive: boolean
}

const EMPTY_TOKEN_FORM = { name: '', description: '', scope: 'read' as 'read' | 'read-write', expiresAt: '' }

export function Profile() {
  const { currentUser, token, updateProfile } = useAuth()
  const displayTimezone = useTimezone()
  const { confirm, ConfirmDialog } = useConfirm()

  const [username, setUsername] = useState(currentUser?.username || '')
  const [email, setEmail] = useState(currentUser?.email || '')
  const [timezone, setTimezone] = useState(currentUser?.timezone || '')
  const [profileSaving, setProfileSaving] = useState(false)
  const [profileError, setProfileError] = useState<string | null>(null)
  const [profileSuccess, setProfileSuccess] = useState<string | null>(null)

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordSaving, setPasswordSaving] = useState(false)
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null)

  if (!currentUser) {
    return (
      <div className="text-center py-12">
        <p className="text-gray-400">You must be logged in to view this page.</p>
      </div>
    )
  }

  const saveProfile = async (e: React.FormEvent) => {
    e.preventDefault()
    setProfileError(null)
    setProfileSuccess(null)
    if (!username || !email) {
      setProfileError('Username and email are required')
      return
    }
    setProfileSaving(true)
    try {
      const res = await fetch('/api/v1/users/me', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ username, email, timezone }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      updateProfile(username, email, timezone)
      setProfileSuccess('Profile updated successfully')
    } catch (err: any) {
      setProfileError(err.message)
    } finally {
      setProfileSaving(false)
    }
  }

  const savePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    setPasswordError(null)
    setPasswordSuccess(null)
    if (!currentPassword || !newPassword || !confirmPassword) {
      setPasswordError('All password fields are required')
      return
    }
    if (newPassword !== confirmPassword) {
      setPasswordError('New password and confirmation do not match')
      return
    }
    if (newPassword.length < 8) {
      setPasswordError('New password must be at least 8 characters')
      return
    }
    setPasswordSaving(true)
    try {
      const res = await fetch('/api/v1/users/me/password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ currentPassword, newPassword }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setPasswordSuccess('Password updated successfully')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (err: any) {
      setPasswordError(err.message)
    } finally {
      setPasswordSaving(false)
    }
  }

  const [accessKeys, setAccessKeys] = useState<AccessKeyMetadata[]>([])
  const [accessKeysLoading, setAccessKeysLoading] = useState(true)
  const [accessKeysError, setAccessKeysError] = useState<string | null>(null)
  const [showTokenForm, setShowTokenForm] = useState(false)
  const [tokenForm, setTokenForm] = useState(EMPTY_TOKEN_FORM)
  const [tokenSaveError, setTokenSaveError] = useState<string | null>(null)
  const [tokenSaving, setTokenSaving] = useState(false)
  const [createdToken, setCreatedToken] = useState<string | null>(null)

  const loadAccessKeys = () => {
    if (!token) return
    setAccessKeysLoading(true)
    fetch('/api/v1/users/me/access-keys', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => {
        setAccessKeys(data.accessKeys || [])
        setAccessKeysError(null)
      })
      .catch((err) => setAccessKeysError(err.message))
      .finally(() => setAccessKeysLoading(false))
  }

  useEffect(() => {
    loadAccessKeys()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token])

  const openCreateToken = () => {
    setTokenForm(EMPTY_TOKEN_FORM)
    setTokenSaveError(null)
    setCreatedToken(null)
    setShowTokenForm(true)
  }

  const createToken = async () => {
    if (!token) return
    if (!tokenForm.name) {
      setTokenSaveError('name is required')
      return
    }
    setTokenSaving(true)
    setTokenSaveError(null)
    try {
      const res = await fetch('/api/v1/users/me/access-keys', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          name: tokenForm.name,
          description: tokenForm.description,
          scope: tokenForm.scope,
          expiresAt: tokenForm.expiresAt ? new Date(tokenForm.expiresAt).toISOString() : undefined,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data = await res.json()
      setCreatedToken(data.token)
      loadAccessKeys()
    } catch (err: any) {
      setTokenSaveError(err.message)
    } finally {
      setTokenSaving(false)
    }
  }

  const revokeToken = async (id: string) => {
    if (!token) return
    if (!(await confirm('Revoke this access token? Anything using it will immediately stop working.'))) return
    try {
      const res = await fetch(`/api/v1/users/me/access-keys/${encodeURIComponent(id)}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      loadAccessKeys()
    } catch (err: any) {
      setAccessKeysError(err.message)
    }
  }

  return (
    <div className="max-w-5xl mx-auto space-y-8">
      {ConfirmDialog}
      <div>
        <h1 className="text-3xl font-bold text-white mb-1">My Profile</h1>
        <p className="text-gray-400">Update your account details and password</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-8 items-start">
      <form onSubmit={saveProfile} className="card space-y-4">
        <h2 className="text-xl font-semibold text-gray-100">Account Details</h2>

        {profileError && (
          <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
            {profileError}
          </div>
        )}
        {profileSuccess && (
          <div className="px-4 py-3 bg-green-500/10 border border-green-500/30 rounded-lg text-green-400 text-sm">
            {profileSuccess}
          </div>
        )}

        <div>
          <label className="block text-sm text-gray-400 mb-1">Username</label>
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            className="input w-full"
          />
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Email</label>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input w-full"
          />
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Timezone</label>
          <select
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            className="input w-full"
          >
            <option value="">Browser default</option>
            {listTimezones().map((tz) => (
              <option key={tz} value={tz}>{tz}</option>
            ))}
          </select>
          <p className="text-xs text-gray-500 mt-1">Controls how timestamps are displayed to you. Storage is always UTC.</p>
        </div>

        <button type="submit" disabled={profileSaving} className="btn btn-primary">
          {profileSaving ? 'Saving...' : 'Save Changes'}
        </button>
      </form>

      <form onSubmit={savePassword} className="card space-y-4">
        <h2 className="text-xl font-semibold text-gray-100">Change Password</h2>

        {passwordError && (
          <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
            {passwordError}
          </div>
        )}
        {passwordSuccess && (
          <div className="px-4 py-3 bg-green-500/10 border border-green-500/30 rounded-lg text-green-400 text-sm">
            {passwordSuccess}
          </div>
        )}

        <div>
          <label className="block text-sm text-gray-400 mb-1">Current Password</label>
          <input
            type="password"
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
            className="input w-full"
          />
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">New Password</label>
          <input
            type="password"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            className="input w-full"
          />
        </div>
        <div>
          <label className="block text-sm text-gray-400 mb-1">Confirm New Password</label>
          <input
            type="password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            className="input w-full"
          />
        </div>

        <button type="submit" disabled={passwordSaving} className="btn btn-primary">
          {passwordSaving ? 'Saving...' : 'Change Password'}
        </button>
      </form>
      </div>

      <div className="card">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-xl font-semibold text-gray-100">API Tokens</h2>
            <p className="text-sm text-gray-400 mt-1">
              Personal access tokens for scripted pulls/pushes — use your username as the login and a token as the
              password (e.g. <code className="text-gray-300">docker login &lt;host&gt; -u {currentUser?.username} -p &lt;token&gt;</code>),
              so k3s <code className="text-gray-300">imagePullSecrets</code> and CI credentials don't depend on your account password.
            </p>
          </div>
          <button onClick={openCreateToken} className="btn btn-primary btn-sm shrink-0">
            + New Token
          </button>
        </div>

        {accessKeysError && (
          <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
            {accessKeysError}
          </div>
        )}

        {showTokenForm && (
          <div className="mb-6 p-4 bg-gray-800/40 border border-gray-700 rounded-lg space-y-3">
            <h3 className="text-sm font-medium text-gray-300">New Token</h3>
            {tokenSaveError && (
              <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-sm">
                {tokenSaveError}
              </div>
            )}

            {createdToken ? (
              <div className="space-y-3">
                <div className="px-3 py-2 bg-amber-500/10 border border-amber-500/30 rounded text-amber-300 text-sm">
                  Copy this token now — you won't be able to see it again.
                </div>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-gray-900 border border-gray-700 rounded px-3 py-2 text-gray-100 text-sm break-all">
                    {createdToken}
                  </code>
                  <button
                    onClick={() => navigator.clipboard.writeText(createdToken)}
                    className="btn btn-secondary btn-sm shrink-0"
                  >
                    Copy
                  </button>
                </div>
                <button onClick={() => setShowTokenForm(false)} className="btn btn-secondary btn-sm">
                  Done
                </button>
              </div>
            ) : (
              <>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <label className="text-sm text-gray-400">
                    Name
                    <input
                      className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                      value={tokenForm.name}
                      onChange={(e) => setTokenForm({ ...tokenForm, name: e.target.value })}
                      placeholder="e.g. k3s imagePullSecret"
                    />
                  </label>
                  <label className="text-sm text-gray-400">
                    Scope
                    <select
                      className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                      value={tokenForm.scope}
                      onChange={(e) => setTokenForm({ ...tokenForm, scope: e.target.value as 'read' | 'read-write' })}
                    >
                      <option value="read">Read-only</option>
                      <option value="read-write">Read/write</option>
                    </select>
                  </label>
                  <label className="text-sm text-gray-400">
                    Expires <span className="text-gray-600">(optional)</span>
                    <input
                      type="date"
                      className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                      value={tokenForm.expiresAt}
                      onChange={(e) => setTokenForm({ ...tokenForm, expiresAt: e.target.value })}
                    />
                  </label>
                  <label className="text-sm text-gray-400 md:col-span-2">
                    Description <span className="text-gray-600">(optional)</span>
                    <input
                      className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                      value={tokenForm.description}
                      onChange={(e) => setTokenForm({ ...tokenForm, description: e.target.value })}
                      placeholder="e.g. used by the prod k3s cluster to pull private images"
                    />
                  </label>
                </div>
                <div className="flex gap-2">
                  <button onClick={createToken} disabled={tokenSaving} className="btn btn-primary btn-sm">
                    {tokenSaving ? 'Creating...' : 'Create Token'}
                  </button>
                  <button onClick={() => setShowTokenForm(false)} className="btn btn-secondary btn-sm">
                    Cancel
                  </button>
                </div>
              </>
            )}
          </div>
        )}

        {accessKeysLoading ? (
          <div className="flex items-center justify-center h-32">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-gray-500">
                <tr>
                  <th className="pr-4 py-2 font-medium">Name</th>
                  <th className="pr-4 py-2 font-medium">Scope</th>
                  <th className="pr-4 py-2 font-medium">Created</th>
                  <th className="pr-4 py-2 font-medium">Last used</th>
                  <th className="pr-4 py-2 font-medium">Expires</th>
                  <th className="py-2 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {accessKeys.map((k) => (
                  <tr key={k.id} className="border-t border-gray-800">
                    <td className="pr-4 py-2 text-gray-300">
                      <div>{k.name}</div>
                      {k.description && <div className="text-xs text-gray-500">{k.description}</div>}
                    </td>
                    <td className="pr-4 py-2">
                      <span className={`badge ${k.scope === 'read-write' ? 'badge-warning' : 'badge-info'}`}>
                        {k.scope === 'read-write' ? 'read/write' : 'read-only'}
                      </span>
                    </td>
                    <td className="pr-4 py-2 text-gray-400">{formatDateTime(k.createdAt, displayTimezone)}</td>
                    <td className="pr-4 py-2 text-gray-400">{k.lastUsed ? formatDateTime(k.lastUsed, displayTimezone) : 'never'}</td>
                    <td className="pr-4 py-2 text-gray-400">{k.expiresAt ? formatDateTime(k.expiresAt, displayTimezone) : 'never'}</td>
                    <td className="py-2 text-right">
                      <button onClick={() => revokeToken(k.id)} className="text-red-400 hover:text-red-300 text-sm">
                        Revoke
                      </button>
                    </td>
                  </tr>
                ))}
                {accessKeys.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-6 text-center text-gray-500">
                      No access tokens yet.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
