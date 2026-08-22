import { useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { listTimezones } from '../lib/datetime'

export function Profile() {
  const { currentUser, token, updateProfile } = useAuth()

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

  return (
    <div className="max-w-5xl mx-auto space-y-8">
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
    </div>
  )
}
