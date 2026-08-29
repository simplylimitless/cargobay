import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth, useTimezone } from '../context/AuthContext'
import { useConfirm } from '../hooks/useConfirm'
import { AuditLogs } from './AuditLogs'
import { formatDateTime, listTimezones } from '../lib/datetime'

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

interface VulnDBSettings {
  autoUpdateEnabled: boolean
  updateIntervalHours: number
  lastCheckedAt: string | null
  lastUpdatedAt: string | null
  lastError: string
}

interface VulnScanSettings {
  autoScanEnabled: boolean
  scanIntervalHours: number
  lastCheckedAt: string | null
  lastScanAt: string | null
  lastError: string
}

interface SearchIndexSettings {
  autoReindexEnabled: boolean
  reindexIntervalHours: number
  lastCheckedAt: string | null
  lastReindexedAt: string | null
  lastArtifactCount: number
  lastError: string
}

interface BackupSettings {
  autoBackupEnabled: boolean
  backupIntervalHours: number
  lastCheckedAt: string | null
  lastBackupAt: string | null
  lastBackupPath: string
  lastError: string
  storageType: string
  storageConfig: Record<string, string>
}

const MASKED_SECRET_VALUE = '••••••••'

interface BackupInfo {
  path: string
  createdAt: string
}

type SettingsTab = 'general' | 'registries' | 'users' | 'audit' | 'vulndb' | 'searchindex' | 'backup'

const SETTINGS_TABS: SettingsTab[] = ['general', 'registries', 'users', 'vulndb', 'searchindex', 'backup', 'audit']

export function Settings() {
  const { currentUser, token, isAdmin, updateProfile } = useAuth()
  const timezone = useTimezone()
  const navigate = useNavigate()
  const { confirm, ConfirmDialog } = useConfirm()
  const [config, setConfig] = useState<AdminConfig | null>(null)
  const [configError, setConfigError] = useState<string | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [usersError, setUsersError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [searchParams, setSearchParams] = useSearchParams()
  const tabParam = searchParams.get('tab') as SettingsTab | null
  const activeTab: SettingsTab =
    tabParam && SETTINGS_TABS.includes(tabParam) ? tabParam : 'general'
  const setActiveTab = (tab: SettingsTab) => setSearchParams(tab === 'general' ? {} : { tab }, { replace: false })

  const [registries, setRegistries] = useState<Registry[]>([])
  const [registriesError, setRegistriesError] = useState<string | null>(null)
  const [registriesLoading, setRegistriesLoading] = useState(true)

  const [vulnDBSettings, setVulnDBSettings] = useState<VulnDBSettings | null>(null)
  const [vulnDBLoading, setVulnDBLoading] = useState(true)
  const [vulnDBError, setVulnDBError] = useState<string | null>(null)
  const [vulnDBSaving, setVulnDBSaving] = useState(false)
  const [vulnDBSaved, setVulnDBSaved] = useState(false)
  const [vulnDBUpdating, setVulnDBUpdating] = useState(false)
  const [vulnDBForm, setVulnDBForm] = useState({ autoUpdateEnabled: true, updateIntervalHours: 24 })
  // Set right before a server-driven form update, so the autosave effect below
  // can tell "form changed because we just loaded it" apart from a real edit.
  const vulnDBSkipAutosave = useRef(true)

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
        vulnDBSkipAutosave.current = true
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
      setVulnDBSaved(true)
    } catch (err: any) {
      setVulnDBError(err.message)
    } finally {
      setVulnDBSaving(false)
    }
  }

  // Autosave: debounce edits so they persist without a manual "Save" click,
  // without firing on the form update that loadVulnDBSettings itself performs.
  useEffect(() => {
    if (vulnDBSkipAutosave.current) {
      vulnDBSkipAutosave.current = false
      return
    }
    setVulnDBSaved(false)
    const timer = setTimeout(() => {
      saveVulnDBSettings()
    }, 800)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vulnDBForm])

  const [timezoneForm, setTimezoneForm] = useState(currentUser?.timezone ?? '')
  const [timezoneSaving, setTimezoneSaving] = useState(false)
  const [timezoneSaved, setTimezoneSaved] = useState(false)
  const [timezoneError, setTimezoneError] = useState<string | null>(null)
  // Set right before syncing the form from currentUser, so the autosave
  // effect below can tell "form changed because we just loaded it" apart
  // from a real edit.
  const timezoneSkipAutosave = useRef(true)

  useEffect(() => {
    timezoneSkipAutosave.current = true
    setTimezoneForm(currentUser?.timezone ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentUser?.id])

  const saveTimezone = async (tz: string) => {
    if (!token || !currentUser) return
    setTimezoneSaving(true)
    try {
      const res = await fetch('/api/v1/users/me', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ username: currentUser.username, email: currentUser.email, timezone: tz }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      updateProfile(currentUser.username, currentUser.email, tz)
      setTimezoneError(null)
      setTimezoneSaved(true)
    } catch (err: any) {
      setTimezoneError(err.message)
    } finally {
      setTimezoneSaving(false)
    }
  }

  useEffect(() => {
    if (timezoneSkipAutosave.current) {
      timezoneSkipAutosave.current = false
      return
    }
    setTimezoneSaved(false)
    const timer = setTimeout(() => {
      saveTimezone(timezoneForm)
    }, 800)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [timezoneForm])

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

  const [vulnScanSettings, setVulnScanSettings] = useState<VulnScanSettings | null>(null)
  const [vulnScanLoading, setVulnScanLoading] = useState(true)
  const [vulnScanError, setVulnScanError] = useState<string | null>(null)
  const [vulnScanSaving, setVulnScanSaving] = useState(false)
  const [vulnScanSaved, setVulnScanSaved] = useState(false)
  const [vulnScanScanning, setVulnScanScanning] = useState(false)
  const [vulnScanForm, setVulnScanForm] = useState({ autoScanEnabled: true, scanIntervalHours: 24 })
  // Set right before a server-driven form update, so the autosave effect below
  // can tell "form changed because we just loaded it" apart from a real edit.
  const vulnScanSkipAutosave = useRef(true)

  const loadVulnScanSettings = () => {
    if (!token) return
    setVulnScanLoading(true)
    fetch('/api/v1/settings/vulnerability-scan', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data: VulnScanSettings) => {
        vulnScanSkipAutosave.current = true
        setVulnScanSettings(data)
        setVulnScanForm({ autoScanEnabled: data.autoScanEnabled, scanIntervalHours: data.scanIntervalHours })
        setVulnScanError(null)
      })
      .catch((err) => setVulnScanError(err.message))
      .finally(() => setVulnScanLoading(false))
  }

  useEffect(() => {
    if (activeTab === 'vulndb') loadVulnScanSettings()
  }, [activeTab])

  const saveVulnScanSettings = async () => {
    if (!token) return
    setVulnScanSaving(true)
    try {
      const res = await fetch('/api/v1/settings/vulnerability-scan', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(vulnScanForm),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: VulnScanSettings = await res.json()
      setVulnScanSettings(data)
      setVulnScanError(null)
      setVulnScanSaved(true)
    } catch (err: any) {
      setVulnScanError(err.message)
    } finally {
      setVulnScanSaving(false)
    }
  }

  // Autosave: debounce edits to the auto-scan toggle/interval so they persist
  // without a manual "Save" click, without firing on the form update that
  // loadVulnScanSettings itself performs.
  useEffect(() => {
    if (vulnScanSkipAutosave.current) {
      vulnScanSkipAutosave.current = false
      return
    }
    setVulnScanSaved(false)
    const timer = setTimeout(() => {
      saveVulnScanSettings()
    }, 800)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vulnScanForm])

  const triggerVulnScan = async () => {
    if (!token) return
    setVulnScanScanning(true)
    try {
      const res = await fetch('/api/v1/settings/vulnerability-scan/scan-now', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: VulnScanSettings = await res.json()
      setVulnScanSettings(data)
      setVulnScanError(null)
    } catch (err: any) {
      setVulnScanError(err.message)
    } finally {
      setVulnScanScanning(false)
    }
  }

  const [searchIndexSettings, setSearchIndexSettings] = useState<SearchIndexSettings | null>(null)
  const [searchIndexLoading, setSearchIndexLoading] = useState(true)
  const [searchIndexError, setSearchIndexError] = useState<string | null>(null)
  const [searchIndexSaving, setSearchIndexSaving] = useState(false)
  const [searchIndexSaved, setSearchIndexSaved] = useState(false)
  const [searchIndexUpdating, setSearchIndexUpdating] = useState(false)
  const [searchIndexForm, setSearchIndexForm] = useState({ autoReindexEnabled: true, reindexIntervalHours: 24 })
  const searchIndexSkipAutosave = useRef(true)

  const loadSearchIndexSettings = () => {
    if (!token) return
    setSearchIndexLoading(true)
    fetch('/api/v1/settings/search-index', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data: SearchIndexSettings) => {
        setSearchIndexSettings(data)
        searchIndexSkipAutosave.current = true
        setSearchIndexForm({ autoReindexEnabled: data.autoReindexEnabled, reindexIntervalHours: data.reindexIntervalHours })
        setSearchIndexError(null)
      })
      .catch((err) => setSearchIndexError(err.message))
      .finally(() => setSearchIndexLoading(false))
  }

  useEffect(() => {
    if (activeTab === 'searchindex') loadSearchIndexSettings()
  }, [activeTab])

  const saveSearchIndexSettings = async () => {
    if (!token) return
    setSearchIndexSaving(true)
    try {
      const res = await fetch('/api/v1/settings/search-index', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(searchIndexForm),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: SearchIndexSettings = await res.json()
      setSearchIndexSettings(data)
      setSearchIndexError(null)
      setSearchIndexSaved(true)
    } catch (err: any) {
      setSearchIndexError(err.message)
    } finally {
      setSearchIndexSaving(false)
    }
  }

  useEffect(() => {
    if (searchIndexSkipAutosave.current) {
      searchIndexSkipAutosave.current = false
      return
    }
    setSearchIndexSaved(false)
    const timer = setTimeout(() => {
      saveSearchIndexSettings()
    }, 800)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchIndexForm])

  const triggerSearchIndexReindex = async () => {
    if (!token) return
    setSearchIndexUpdating(true)
    try {
      const res = await fetch('/api/v1/settings/search-index/reindex', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: SearchIndexSettings = await res.json()
      setSearchIndexSettings(data)
      setSearchIndexError(null)
    } catch (err: any) {
      setSearchIndexError(err.message)
    } finally {
      setSearchIndexUpdating(false)
    }
  }

  const [backupSettings, setBackupSettings] = useState<BackupSettings | null>(null)
  const [backupLoading, setBackupLoading] = useState(true)
  const [backupError, setBackupError] = useState<string | null>(null)
  const [backupSaving, setBackupSaving] = useState(false)
  const [backupSaved, setBackupSaved] = useState(false)
  const [backupRunning, setBackupRunning] = useState(false)
  const [backupForm, setBackupForm] = useState<{
    autoBackupEnabled: boolean
    backupIntervalHours: number
    storageType: string
    storageConfig: Record<string, string>
  }>({ autoBackupEnabled: false, backupIntervalHours: 24, storageType: '', storageConfig: {} })
  const backupSkipAutosave = useRef(true)

  const setStorageConfigField = (key: string, value: string) =>
    setBackupForm((prev) => ({ ...prev, storageConfig: { ...prev.storageConfig, [key]: value } }))

  const [backups, setBackups] = useState<BackupInfo[]>([])
  const [backupsError, setBackupsError] = useState<string | null>(null)
  const [restoringPath, setRestoringPath] = useState<string | null>(null)

  const loadBackupSettings = () => {
    if (!token) return
    setBackupLoading(true)
    fetch('/api/v1/settings/backup', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data: BackupSettings) => {
        backupSkipAutosave.current = true
        setBackupSettings(data)
        setBackupForm({
          autoBackupEnabled: data.autoBackupEnabled,
          backupIntervalHours: data.backupIntervalHours,
          storageType: data.storageType,
          storageConfig: data.storageConfig ?? {},
        })
        setBackupError(null)
      })
      .catch((err) => setBackupError(err.message))
      .finally(() => setBackupLoading(false))
  }

  const loadBackups = () => {
    if (!token) return
    fetch('/api/v1/settings/backup/list', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Request failed: ${res.status}`)
        return res.json()
      })
      .then((data) => {
        setBackups(data.backups || [])
        setBackupsError(null)
      })
      .catch((err) => setBackupsError(err.message))
  }

  useEffect(() => {
    if (activeTab === 'backup') {
      loadBackupSettings()
      loadBackups()
    }
  }, [activeTab])

  const saveBackupSettings = async () => {
    if (!token) return
    setBackupSaving(true)
    try {
      const res = await fetch('/api/v1/settings/backup', {
        method: 'PUT',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify(backupForm),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: BackupSettings = await res.json()
      setBackupSettings(data)
      setBackupError(null)
      setBackupSaved(true)
    } catch (err: any) {
      setBackupError(err.message)
    } finally {
      setBackupSaving(false)
    }
  }

  // Autosave: debounce edits so they persist without a manual "Save" click,
  // without firing on the form update that loadBackupSettings itself performs.
  useEffect(() => {
    if (backupSkipAutosave.current) {
      backupSkipAutosave.current = false
      return
    }
    setBackupSaved(false)
    const timer = setTimeout(() => {
      saveBackupSettings()
    }, 800)
    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [backupForm])

  const triggerBackup = async () => {
    if (!token) return
    setBackupRunning(true)
    try {
      const res = await fetch('/api/v1/settings/backup/backup-now', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      const data: BackupSettings = await res.json()
      setBackupSettings(data)
      setBackupError(null)
      loadBackups()
    } catch (err: any) {
      setBackupError(err.message)
    } finally {
      setBackupRunning(false)
    }
  }

  const restoreBackup = async (path: string) => {
    if (!token) return
    if (!(await confirm(`Restore backup "${path}"? This replaces the entire current database and cannot be undone.`))) return
    setRestoringPath(path)
    try {
      const res = await fetch('/api/v1/settings/backup/restore', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ path }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || `Request failed: ${res.status}`)
      }
      setBackupsError(null)
    } catch (err: any) {
      setBackupsError(err.message)
    } finally {
      setRestoringPath(null)
    }
  }

  const loadRegistries = () => {
    if (!token) return
    setRegistriesLoading(true)
    fetch('/api/v1/registries?all=true', {
      headers: { Authorization: `Bearer ${token}` },
    })
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

  const deleteRegistry = async (id: string) => {
    if (!token) return
    if (!(await confirm(`Delete registry "${id}"? This cannot be undone.`))) return
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
    { id: 'vulndb', label: 'Vulnerability Scanner' },
    { id: 'searchindex', label: 'Search Index' },
    { id: 'backup', label: 'Backup & Restore' },
    { id: 'audit', label: 'Audit' },
  ]

  return (
    <div className="space-y-8">
      {ConfirmDialog}
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
            <button onClick={() => navigate('/settings/registries/new')} className="btn btn-primary btn-sm">
              + Add Registry
            </button>
          </div>

          {registriesError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
              {registriesError}
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
                        <button onClick={() => navigate(`/settings/registries/${reg.id}`)} className="text-blue-400 hover:text-blue-300 text-sm">
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
          <div className="card mb-6">
            <h2 className="text-xl font-semibold text-gray-100 mb-4">Display Preferences</h2>
            <div className="max-w-sm">
              <label className="block text-sm font-medium text-gray-400 mb-1">Timezone</label>
              <select
                className="input w-full"
                value={timezoneForm}
                onChange={(e) => setTimezoneForm(e.target.value)}
              >
                <option value="">Browser default</option>
                {listTimezones().map((tz) => (
                  <option key={tz} value={tz}>{tz}</option>
                ))}
              </select>
              <p className="text-xs text-gray-500 mt-1">
                Controls how timestamps are displayed to you. Storage is always UTC.
              </p>
              {timezoneError && (
                <p className="text-xs text-red-400 mt-1">Failed to save: {timezoneError}</p>
              )}
              {timezoneSaving && <p className="text-xs text-gray-500 mt-1">Saving...</p>}
              {!timezoneSaving && timezoneSaved && <p className="text-xs text-green-400 mt-1">Saved</p>}
            </div>
          </div>

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
        <div className="space-y-6">
          <div className="card">
            <h2 className="text-xl font-semibold text-gray-100 mb-4">Artifact Scanning</h2>
            <p className="text-sm text-gray-400 mb-4">
              Controls automatic Trivy vulnerability scanning of cached docker/OCI artifacts.
            </p>

            {vulnScanError && (
              <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
                {vulnScanError}
              </div>
            )}

            {vulnScanLoading ? (
              <div className="flex items-center justify-center h-32">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="flex items-center gap-4">
                  <label className="flex items-center gap-2 text-sm text-gray-400">
                    <input
                      type="checkbox"
                      checked={vulnScanForm.autoScanEnabled}
                      onChange={(e) => setVulnScanForm({ ...vulnScanForm, autoScanEnabled: e.target.checked })}
                    />
                    Auto-scan enabled
                  </label>
                  <label className="text-sm text-gray-400">
                    Scan interval (hours)
                    <input
                      type="number"
                      min={1}
                      className="mt-1 w-32 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                      value={vulnScanForm.scanIntervalHours}
                      onChange={(e) => setVulnScanForm({ ...vulnScanForm, scanIntervalHours: parseInt(e.target.value, 10) || 1 })}
                    />
                  </label>
                </div>

                <div className="bg-gray-800/30 rounded-lg p-4">
                  <dl className="text-sm space-y-1">
                    <div className="flex justify-between">
                      <dt className="text-gray-500">Last checked</dt>
                      <dd className="text-gray-200 font-mono">
                        {vulnScanSettings?.lastCheckedAt ? formatDateTime(vulnScanSettings.lastCheckedAt, timezone) : 'never'}
                      </dd>
                    </div>
                    <div className="flex justify-between">
                      <dt className="text-gray-500">Last scanned</dt>
                      <dd className="text-gray-200 font-mono">
                        {vulnScanSettings?.lastScanAt ? formatDateTime(vulnScanSettings.lastScanAt, timezone) : 'never'}
                      </dd>
                    </div>
                  </dl>
                  {vulnScanSettings?.lastError && (
                    <div className="mt-3 px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-xs">
                      Last error: {vulnScanSettings.lastError}
                    </div>
                  )}
                </div>

                <div className="flex items-center gap-3">
                  <button onClick={triggerVulnScan} disabled={vulnScanScanning} className="btn btn-secondary btn-sm">
                    {vulnScanScanning ? 'Scanning...' : 'Scan Now'}
                  </button>
                  <span className="text-xs text-gray-500">
                    {vulnScanSaving ? 'Saving…' : vulnScanSaved ? 'Saved' : ''}
                  </span>
                </div>
              </div>
            )}
          </div>

          <div className="card">
          <h2 className="text-xl font-semibold text-gray-100 mb-4">Vulnerability Database</h2>
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
                      {vulnDBSettings?.lastCheckedAt ? formatDateTime(vulnDBSettings.lastCheckedAt, timezone) : 'never'}
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last updated</dt>
                    <dd className="text-gray-200 font-mono">
                      {vulnDBSettings?.lastUpdatedAt ? formatDateTime(vulnDBSettings.lastUpdatedAt, timezone) : 'never'}
                    </dd>
                  </div>
                </dl>
                {vulnDBSettings?.lastError && (
                  <div className="mt-3 px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-xs">
                    Last error: {vulnDBSettings.lastError}
                  </div>
                )}
              </div>

              <div className="flex items-center gap-3">
                <button onClick={triggerVulnDBUpdate} disabled={vulnDBUpdating} className="btn btn-secondary btn-sm">
                  {vulnDBUpdating ? 'Updating...' : 'Update now'}
                </button>
                <span className="text-xs text-gray-500">
                  {vulnDBSaving ? 'Saving…' : vulnDBSaved ? 'Saved' : ''}
                </span>
              </div>
            </div>
          )}
          </div>
        </div>
      )}

      {activeTab === 'searchindex' && (
        <div className="card">
          <h2 className="text-xl font-semibold text-gray-100 mb-4">Search Index</h2>
          <p className="text-sm text-gray-400 mb-4">
            Controls how often cargobay rebuilds its full-text search index over artifacts. Rebuild manually if search results look stale.
          </p>

          {searchIndexError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
              {searchIndexError}
            </div>
          )}

          {searchIndexLoading ? (
            <div className="flex items-center justify-center h-32">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="flex items-center gap-4">
                <label className="flex items-center gap-2 text-sm text-gray-400">
                  <input
                    type="checkbox"
                    checked={searchIndexForm.autoReindexEnabled}
                    onChange={(e) => setSearchIndexForm({ ...searchIndexForm, autoReindexEnabled: e.target.checked })}
                  />
                  Auto-reindex enabled
                </label>
                <label className="text-sm text-gray-400">
                  Reindex interval (hours)
                  <input
                    type="number"
                    min={1}
                    className="mt-1 w-32 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={searchIndexForm.reindexIntervalHours}
                    onChange={(e) => setSearchIndexForm({ ...searchIndexForm, reindexIntervalHours: parseInt(e.target.value, 10) || 1 })}
                  />
                </label>
              </div>

              <div className="bg-gray-800/30 rounded-lg p-4">
                <dl className="text-sm space-y-1">
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last checked</dt>
                    <dd className="text-gray-200 font-mono">
                      {searchIndexSettings?.lastCheckedAt ? formatDateTime(searchIndexSettings.lastCheckedAt, timezone) : 'never'}
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last reindexed</dt>
                    <dd className="text-gray-200 font-mono">
                      {searchIndexSettings?.lastReindexedAt ? formatDateTime(searchIndexSettings.lastReindexedAt, timezone) : 'never'}
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Artifacts indexed</dt>
                    <dd className="text-gray-200 font-mono">{searchIndexSettings?.lastArtifactCount ?? 0}</dd>
                  </div>
                </dl>
                {searchIndexSettings?.lastError && (
                  <div className="mt-3 px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-xs">
                    Last error: {searchIndexSettings.lastError}
                  </div>
                )}
              </div>

              <div className="flex items-center gap-3">
                <button onClick={triggerSearchIndexReindex} disabled={searchIndexUpdating} className="btn btn-secondary btn-sm">
                  {searchIndexUpdating ? 'Reindexing...' : 'Reindex now'}
                </button>
                <span className="text-xs text-gray-500">
                  {searchIndexSaving ? 'Saving…' : searchIndexSaved ? 'Saved' : ''}
                </span>
              </div>
            </div>
          )}
        </div>
      )}

      {activeTab === 'backup' && (
        <div className="card">
          <h2 className="text-xl font-semibold text-gray-100 mb-4">Backup & Restore</h2>
          <p className="text-sm text-gray-400 mb-4">
            Full database backups, written through the same pluggable storage backend used for artifact blobs.
            Restoring replaces the entire database and cannot be undone.
          </p>

          {backupError && (
            <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm mb-4">
              {backupError}
            </div>
          )}

          {backupLoading ? (
            <div className="flex items-center justify-center h-32">
              <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
            </div>
          ) : (
            <div className="space-y-4">
              <div className="flex items-center gap-4">
                <label className="flex items-center gap-2 text-sm text-gray-400">
                  <input
                    type="checkbox"
                    checked={backupForm.autoBackupEnabled}
                    onChange={(e) => setBackupForm({ ...backupForm, autoBackupEnabled: e.target.checked })}
                  />
                  Scheduled backups enabled
                </label>
                <label className="text-sm text-gray-400">
                  Backup interval (hours)
                  <input
                    type="number"
                    min={1}
                    className="mt-1 w-32 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={backupForm.backupIntervalHours}
                    onChange={(e) => setBackupForm({ ...backupForm, backupIntervalHours: parseInt(e.target.value, 10) || 1 })}
                  />
                </label>
              </div>

              <div className="bg-gray-800/30 rounded-lg p-4 space-y-3">
                <div>
                  <h3 className="text-sm font-medium text-gray-300 mb-1">Backup location</h3>
                  <p className="text-xs text-gray-500 mb-3">
                    Where scheduled and manual backups are written. Leave a secret field showing{' '}
                    <code className="text-gray-400">{MASKED_SECRET_VALUE}</code> unchanged to keep the stored value.
                  </p>
                </div>

                <label className="block text-sm text-gray-400">
                  Storage type
                  <select
                    className="mt-1 w-full sm:w-72 bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                    value={backupForm.storageType}
                    onChange={(e) => setBackupForm((prev) => ({ ...prev, storageType: e.target.value }))}
                  >
                    <option value="">Shared with artifact storage</option>
                    <option value="local">Local path</option>
                    <option value="s3">S3-compatible</option>
                    <option value="gcs">Google Cloud Storage</option>
                    <option value="azure">Azure Blob Storage</option>
                  </select>
                </label>

                {backupForm.storageType === 'local' && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <label className="text-sm text-gray-400">
                      Path
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.path || ''}
                        onChange={(e) => setStorageConfigField('path', e.target.value)}
                      />
                    </label>
                  </div>
                )}

                {backupForm.storageType === 's3' && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <label className="text-sm text-gray-400">
                      Bucket *
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.bucket || ''}
                        onChange={(e) => setStorageConfigField('bucket', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Region
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.region || ''}
                        onChange={(e) => setStorageConfigField('region', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Endpoint
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.endpoint || ''}
                        onChange={(e) => setStorageConfigField('endpoint', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Prefix
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.prefix || ''}
                        onChange={(e) => setStorageConfigField('prefix', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Access key
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.access_key || ''}
                        onChange={(e) => setStorageConfigField('access_key', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Secret key
                      <input
                        type="password"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.secret_key || ''}
                        onChange={(e) => setStorageConfigField('secret_key', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Session token
                      <input
                        type="password"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.session_token || ''}
                        onChange={(e) => setStorageConfigField('session_token', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Storage tiering
                      <select
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.tiering || ''}
                        onChange={(e) => setStorageConfigField('tiering', e.target.value)}
                      >
                        <option value="">STANDARD</option>
                        <option value="glacier">Glacier</option>
                        <option value="deep_archive">Glacier Deep Archive</option>
                      </select>
                    </label>
                    <label className="flex items-center gap-2 text-sm text-gray-400 mt-6">
                      <input
                        type="checkbox"
                        checked={backupForm.storageConfig.ssl !== 'false'}
                        onChange={(e) => setStorageConfigField('ssl', e.target.checked ? 'true' : 'false')}
                      />
                      Use SSL
                    </label>
                  </div>
                )}

                {backupForm.storageType === 'gcs' && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <label className="text-sm text-gray-400">
                      Bucket *
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.bucket || ''}
                        onChange={(e) => setStorageConfigField('bucket', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Prefix
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.prefix || ''}
                        onChange={(e) => setStorageConfigField('prefix', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400 sm:col-span-2">
                      Service account JSON key
                      <textarea
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100 font-mono text-xs"
                        rows={3}
                        value={backupForm.storageConfig.json_key || ''}
                        onChange={(e) => setStorageConfigField('json_key', e.target.value)}
                      />
                    </label>
                  </div>
                )}

                {backupForm.storageType === 'azure' && (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <label className="text-sm text-gray-400">
                      Container *
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.container || ''}
                        onChange={(e) => setStorageConfigField('container', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Prefix
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.prefix || ''}
                        onChange={(e) => setStorageConfigField('prefix', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Storage tiering
                      <select
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.tiering || ''}
                        onChange={(e) => setStorageConfigField('tiering', e.target.value)}
                      >
                        <option value="">Hot</option>
                        <option value="Cool">Cool</option>
                        <option value="Archive">Archive</option>
                      </select>
                    </label>
                    <label className="text-sm text-gray-400">
                      Connection string
                      <input
                        type="password"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.connection_string || ''}
                        onChange={(e) => setStorageConfigField('connection_string', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Account name
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.account_name || ''}
                        onChange={(e) => setStorageConfigField('account_name', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Account key
                      <input
                        type="password"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.account_key || ''}
                        onChange={(e) => setStorageConfigField('account_key', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      SAS token
                      <input
                        type="password"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.sas_token || ''}
                        onChange={(e) => setStorageConfigField('sas_token', e.target.value)}
                      />
                    </label>
                    <label className="text-sm text-gray-400">
                      Account URL
                      <input
                        type="text"
                        className="mt-1 w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5 text-gray-100"
                        value={backupForm.storageConfig.account_url || ''}
                        onChange={(e) => setStorageConfigField('account_url', e.target.value)}
                      />
                    </label>
                  </div>
                )}
              </div>

              <div className="bg-gray-800/30 rounded-lg p-4">
                <dl className="text-sm space-y-1">
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last checked</dt>
                    <dd className="text-gray-200 font-mono">
                      {backupSettings?.lastCheckedAt ? formatDateTime(backupSettings.lastCheckedAt, timezone) : 'never'}
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last backup</dt>
                    <dd className="text-gray-200 font-mono">
                      {backupSettings?.lastBackupAt ? formatDateTime(backupSettings.lastBackupAt, timezone) : 'never'}
                    </dd>
                  </div>
                  <div className="flex justify-between">
                    <dt className="text-gray-500">Last backup path</dt>
                    <dd className="text-gray-200 font-mono">{backupSettings?.lastBackupPath || '-'}</dd>
                  </div>
                </dl>
                {backupSettings?.lastError && (
                  <div className="mt-3 px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-xs">
                    Last error: {backupSettings.lastError}
                  </div>
                )}
              </div>

              <div className="flex items-center gap-3">
                <button onClick={triggerBackup} disabled={backupRunning} className="btn btn-secondary btn-sm">
                  {backupRunning ? 'Backing up...' : 'Backup now'}
                </button>
                <span className="text-xs text-gray-500">
                  {backupSaving ? 'Saving…' : backupSaved ? 'Saved' : ''}
                </span>
              </div>

              <div>
                <h3 className="text-sm font-medium text-gray-300 mb-2">Stored backups</h3>
                {backupsError && (
                  <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-red-400 text-xs mb-2">
                    {backupsError}
                  </div>
                )}
                {backups.length === 0 ? (
                  <p className="text-sm text-gray-500">No backups yet.</p>
                ) : (
                  <ul className="divide-y divide-gray-800 border border-gray-800 rounded-lg overflow-hidden">
                    {backups.map((b) => (
                      <li key={b.path} className="flex items-center justify-between px-4 py-2 bg-gray-800/20">
                        <div>
                          <div className="text-sm text-gray-200 font-mono">{b.path}</div>
                          <div className="text-xs text-gray-500">{formatDateTime(b.createdAt, timezone)}</div>
                        </div>
                        <button
                          onClick={() => restoreBackup(b.path)}
                          disabled={restoringPath === b.path}
                          className="btn btn-secondary btn-sm"
                        >
                          {restoringPath === b.path ? 'Restoring...' : 'Restore'}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {activeTab === 'users' && (
        <div className="card">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold text-gray-100">Users</h2>
            <button onClick={() => navigate('/settings/users/new')} className="btn btn-primary btn-sm">
              + Add User
            </button>
          </div>

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
