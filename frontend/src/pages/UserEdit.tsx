import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

interface User {
  userId: string
  username: string
  email: string
  createdAt: string
  lastLogin: string | null
  isActive: boolean
}

const AVAILABLE_ROLES = ['admin', 'publisher', 'viewer']

export function UserEdit() {
  const { userId } = useParams<{ userId: string }>()
  const isCreating = userId === 'new'
  const navigate = useNavigate()
  const { token, isAdmin } = useAuth()

  const [loading, setLoading] = useState(!isCreating)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [isActive, setIsActive] = useState(true)
  const [role, setRole] = useState('viewer')
  const [initialRole, setInitialRole] = useState('viewer')
  const [password, setPassword] = useState('')

  const [profileSaving, setProfileSaving] = useState(false)
  const [profileError, setProfileError] = useState<string | null>(null)
  const [profileSuccess, setProfileSuccess] = useState<string | null>(null)

  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [passwordSaving, setPasswordSaving] = useState(false)
  const [passwordError, setPasswordError] = useState<string | null>(null)
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null)

  useEffect(() => {
    if (!isAdmin || !token || !userId || isCreating) return
    setLoading(true)
    Promise.all([
      fetch(`/api/v1/users/${encodeURIComponent(userId)}`, {
        headers: { Authorization: `Bearer ${token}` },
      }).then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json() as Promise<User>
      }),
      fetch(`/api/v1/users/${encodeURIComponent(userId)}/roles`, {
        headers: { Authorization: `Bearer ${token}` },
      }).then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json() as Promise<{ roles: string[] }>
      }),
    ])
      .then(([user, rolesData]) => {
        setUsername(user.username)
        setEmail(user.email)
        setIsActive(user.isActive)
        const currentRole = rolesData.roles?.[0] || 'viewer'
        setRole(currentRole)
        setInitialRole(currentRole)
        setLoadError(null)
      })
      .catch((err) => setLoadError(err.message))
      .finally(() => setLoading(false))
  }, [isAdmin, token, userId, isCreating])

  if (!isAdmin) {
    return (
      <div className="card max-w-md mx-auto text-center py-12">
        <h1 className="text-xl font-semibold text-white mb-2">Access denied</h1>
        <p className="text-gray-400">You need admin privileges to view this page.</p>
      </div>
    )
  }

  const createUser = async (e: React.FormEvent) => {
    e.preventDefault()
    setProfileError(null)
    setProfileSuccess(null)
    if (!username || !email || !password) {
      setProfileError('username, email, and password are required')
      return
    }
    if (password.length < 8) {
      setProfileError('password must be at least 8 characters')
      return
    }
    setProfileSaving(true)
    try {
      const res = await fetch('/api/v1/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ username, email, password, roles: [role] }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      navigate('/settings?tab=users')
    } catch (err: any) {
      setProfileError(err.message)
    } finally {
      setProfileSaving(false)
    }
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
      const res = await fetch(`/api/v1/users/${encodeURIComponent(userId!)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ username, email, isActive }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }

      if (role !== initialRole) {
        const roleRes = await fetch(`/api/v1/users/${encodeURIComponent(userId!)}/roles`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({ roleId: role }),
        })
        if (!roleRes.ok) {
          const data = await roleRes.json().catch(() => ({}))
          throw new Error(data.error || `Request failed: ${roleRes.status}`)
        }
        setInitialRole(role)
      }

      setProfileSuccess('User updated successfully')
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
    if (!newPassword || !confirmPassword) {
      setPasswordError('Both password fields are required')
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
      const res = await fetch(`/api/v1/users/${encodeURIComponent(userId!)}/password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ newPassword }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setPasswordSuccess('Password reset successfully')
      setNewPassword('')
      setConfirmPassword('')
    } catch (err: any) {
      setPasswordError(err.message)
    } finally {
      setPasswordSaving(false)
    }
  }

  return (
    <div className="max-w-5xl mx-auto space-y-8">
      <button
        onClick={() => navigate('/settings?tab=users')}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to Settings
      </button>

      <div>
        <h1 className="text-3xl font-bold text-white mb-1">{isCreating ? 'New User' : 'Edit User'}</h1>
        <p className="text-gray-400">
          {isCreating ? 'Create a new account' : 'Update account details, role, and password'}
        </p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : loadError ? (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          Failed to load user: {loadError}
        </div>
      ) : isCreating ? (
        <form onSubmit={createUser} className="card max-w-md space-y-4">
          <h2 className="text-xl font-semibold text-gray-100">Account Details</h2>

          {profileError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
              {profileError}
            </div>
          )}

          <div>
            <label className="block text-sm text-gray-400 mb-1">Username</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="input w-full"
              placeholder="e.g. jdoe"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Email</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="input w-full"
              placeholder="jdoe@example.com"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Password</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="input w-full"
              placeholder="At least 8 characters"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Role</label>
            <select
              className="input w-full"
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              {AVAILABLE_ROLES.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
          </div>

          <button type="submit" disabled={profileSaving} className="btn btn-primary">
            {profileSaving ? 'Creating...' : 'Create User'}
          </button>
        </form>
      ) : (
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
              <label className="block text-sm text-gray-400 mb-1">Role</label>
              <select
                className="input w-full"
                value={role}
                onChange={(e) => setRole(e.target.value)}
              >
                {AVAILABLE_ROLES.map((r) => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
            </div>
            <label className="flex items-center gap-2 text-sm text-gray-400">
              <input
                type="checkbox"
                checked={isActive}
                onChange={(e) => setIsActive(e.target.checked)}
              />
              Active
            </label>

            <button type="submit" disabled={profileSaving} className="btn btn-primary">
              {profileSaving ? 'Saving...' : 'Save Changes'}
            </button>
          </form>

          <form onSubmit={savePassword} className="card space-y-4">
            <h2 className="text-xl font-semibold text-gray-100">Reset Password</h2>
            <p className="text-sm text-gray-500">Sets a new password for this user directly, without needing their current one.</p>

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
              <label className="block text-sm text-gray-400 mb-1">New Password</label>
              <input
                type="password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                className="input w-full"
                placeholder="At least 8 characters"
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
              {passwordSaving ? 'Saving...' : 'Reset Password'}
            </button>
          </form>
        </div>
      )}
    </div>
  )
}
