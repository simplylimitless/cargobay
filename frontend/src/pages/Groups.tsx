import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { useConfirm } from '../hooks/useConfirm'

interface Group {
  id: string
  name: string
  description: string
  createdAt: string
}

interface Member {
  userId: string
  username: string
  email: string
}

interface UserOption {
  userId: string
  username: string
}

export function Groups() {
  const { groupId } = useParams<{ groupId: string }>()
  const isCreating = groupId === 'new'
  const navigate = useNavigate()
  const { token, isAdmin } = useAuth()
  const { confirm, ConfirmDialog } = useConfirm()

  const [loading, setLoading] = useState(!isCreating)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [members, setMembers] = useState<Member[]>([])
  const [allUsers, setAllUsers] = useState<UserOption[]>([])
  const [selectedUserId, setSelectedUserId] = useState('')

  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loadGroup = () => {
    if (!token || !groupId || isCreating) return
    setLoading(true)
    fetch(`/api/v1/groups/${encodeURIComponent(groupId)}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json() as Promise<{ group: Group; members: Member[] }>
      })
      .then((data) => {
        setName(data.group.name)
        setDescription(data.group.description)
        setMembers(data.members || [])
        setLoadError(null)
      })
      .catch((err) => setLoadError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    if (!isAdmin) return
    loadGroup()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdmin, token, groupId, isCreating])

  useEffect(() => {
    if (!isAdmin || !token) return
    fetch('/api/v1/users', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => setAllUsers(data.users || []))
      .catch(() => {})
  }, [isAdmin, token])

  if (!isAdmin) {
    return (
      <div className="card max-w-md mx-auto text-center py-12">
        <h1 className="text-xl font-semibold text-white mb-2">Access denied</h1>
        <p className="text-gray-400">You need admin privileges to view this page.</p>
      </div>
    )
  }

  const createGroup = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    if (!name) {
      setError('name is required')
      return
    }
    setSaving(true)
    try {
      const res = await fetch('/api/v1/groups', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ name, description }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      navigate('/settings?tab=groups')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  const deleteGroup = async () => {
    if (!groupId) return
    const ok = await confirm({
      title: 'Delete group',
      message: `Delete group "${name}"? This removes all its members and registry access grants.`,
      confirmLabel: 'Delete',
      danger: true,
    })
    if (!ok) return
    try {
      const res = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      navigate('/settings?tab=groups')
    } catch (err: any) {
      setError(err.message)
    }
  }

  const addMember = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!groupId || !selectedUserId) return
    setError(null)
    try {
      const res = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/members`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ userId: selectedUserId }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setSelectedUserId('')
      loadGroup()
    } catch (err: any) {
      setError(err.message)
    }
  }

  const removeMember = async (userId: string) => {
    if (!groupId) return
    try {
      const res = await fetch(`/api/v1/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(userId)}`, {
        method: 'DELETE',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      loadGroup()
    } catch (err: any) {
      setError(err.message)
    }
  }

  const memberIds = new Set(members.map((m) => m.userId))
  const availableUsers = allUsers.filter((u) => !memberIds.has(u.userId))

  return (
    <div className="max-w-3xl mx-auto space-y-8">
      {ConfirmDialog}
      <button
        onClick={() => navigate('/settings?tab=groups')}
        className="flex items-center gap-2 text-gray-400 hover:text-gray-100 transition-colors"
      >
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
        </svg>
        Back to Settings
      </button>

      <div>
        <h1 className="text-3xl font-bold text-white mb-1">{isCreating ? 'New Group' : 'Edit Group'}</h1>
        <p className="text-gray-400">
          {isCreating
            ? 'Create a group to grant registry access to multiple users at once'
            : 'Update group details and manage members'}
        </p>
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
      ) : loadError ? (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          Failed to load group: {loadError}
        </div>
      ) : isCreating ? (
        <form onSubmit={createGroup} className="card max-w-md space-y-4">
          <h2 className="text-xl font-semibold text-gray-100">Group Details</h2>

          {error && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
              {error}
            </div>
          )}

          <div>
            <label className="block text-sm text-gray-400 mb-1">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="input w-full"
              placeholder="e.g. backend-team"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Description</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="input w-full"
              placeholder="Optional"
            />
          </div>

          <button type="submit" disabled={saving} className="btn btn-primary">
            {saving ? 'Creating...' : 'Create Group'}
          </button>
        </form>
      ) : (
        <div className="space-y-8">
          {error && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
              {error}
            </div>
          )}

          <div className="card space-y-4">
            <div className="flex items-center justify-between">
              <h2 className="text-xl font-semibold text-gray-100">Group Details</h2>
              <button onClick={deleteGroup} className="text-red-400 hover:text-red-300 text-sm">
                Delete Group
              </button>
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Name</label>
              <div className="text-gray-100">{name}</div>
            </div>
            <div>
              <label className="block text-sm text-gray-400 mb-1">Description</label>
              <div className="text-gray-400">{description || '—'}</div>
            </div>
          </div>

          <div className="card space-y-4">
            <h2 className="text-xl font-semibold text-gray-100">Members</h2>

            <form onSubmit={addMember} className="flex gap-2">
              <select
                className="input flex-1"
                value={selectedUserId}
                onChange={(e) => setSelectedUserId(e.target.value)}
              >
                <option value="">Select a user to add...</option>
                {availableUsers.map((u) => (
                  <option key={u.userId} value={u.userId}>{u.username}</option>
                ))}
              </select>
              <button type="submit" disabled={!selectedUserId} className="btn btn-primary btn-sm">
                Add
              </button>
            </form>

            <div className="overflow-x-auto">
              <table className="w-full text-left">
                <thead className="bg-gray-800/50 text-gray-400 text-sm">
                  <tr>
                    <th className="px-4 py-3 rounded-l-lg font-medium">Username</th>
                    <th className="px-4 py-3 font-medium">Email</th>
                    <th className="px-4 py-3 rounded-r-lg font-medium text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="text-sm">
                  {members.map((m) => (
                    <tr key={m.userId} className="border-b border-gray-800">
                      <td className="px-4 py-3 font-medium text-gray-100">{m.username}</td>
                      <td className="px-4 py-3 text-gray-400">{m.email}</td>
                      <td className="px-4 py-3 text-right">
                        <button
                          onClick={() => removeMember(m.userId)}
                          className="text-red-400 hover:text-red-300 text-sm"
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  ))}
                  {members.length === 0 && (
                    <tr>
                      <td colSpan={3} className="px-4 py-6 text-center text-gray-500">
                        No members yet
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
