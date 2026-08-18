import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

interface RegistryOption {
  id: string
  name: string
  url: string
  type: string
  icon: string
  defaultChecked: boolean
}

const REGISTRY_OPTIONS: RegistryOption[] = [
  { id: 'dockerhub', name: 'Docker Hub', url: 'https://registry-1.docker.io', type: 'docker', icon: '🐳', defaultChecked: true },
  { id: 'ghcr', name: 'GitHub Container Registry', url: 'https://ghcr.io', type: 'docker', icon: '🐳', defaultChecked: false },
  { id: 'npm', name: 'NPM Registry', url: 'https://registry.npmjs.org', type: 'npm', icon: '📦', defaultChecked: true },
  { id: 'maven-central', name: 'Maven Central', url: 'https://repo.maven.apache.org/maven2', type: 'maven', icon: '☕', defaultChecked: true },
  { id: 'pypi', name: 'PyPI', url: 'https://pypi.org', type: 'pypi', icon: '🐍', defaultChecked: false },
  { id: 'nuget', name: 'NuGet Gallery', url: 'https://api.nuget.org/v3/index.json', type: 'nuget', icon: '🔨', defaultChecked: false },
]

export function Setup() {
  const navigate = useNavigate()
  const { setSession } = useAuth()

  const [step, setStep] = useState<1 | 2>(1)
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [selected, setSelected] = useState<Set<string>>(
    new Set(REGISTRY_OPTIONS.filter((r) => r.defaultChecked).map((r) => r.id))
  )
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const toggleRegistry = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const goToRegistries = (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    if (!username || !email || !password) {
      setError('All fields are required')
      return
    }
    if (password.length < 8) {
      setError('Password must be at least 8 characters')
      return
    }
    if (password !== confirmPassword) {
      setError('Passwords do not match')
      return
    }
    setStep(2)
  }

  const finishSetup = async () => {
    setError(null)
    setSubmitting(true)
    try {
      const registries = REGISTRY_OPTIONS.filter((r) => selected.has(r.id)).map((r, i) => ({
        id: r.id,
        name: r.name,
        url: r.url,
        type: r.type,
        enabled: true,
        priority: i,
      }))

      const res = await fetch('/api/v1/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, email, password, registries }),
      })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        throw new Error(data.error || `Request failed: ${res.status}`)
      }

      setSession(data.accessToken, data.user)
      navigate('/')
    } catch (err: any) {
      setError(err.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="max-w-2xl mx-auto space-y-8 py-8">
      <div className="text-center">
        <h1 className="text-3xl font-bold text-white mb-2">Welcome to cargobay</h1>
        <p className="text-gray-400">Let's get your registry set up in two quick steps.</p>
      </div>

      <div className="flex items-center justify-center gap-2 text-sm">
        <span className={`px-3 py-1 rounded-full ${step === 1 ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400'}`}>
          1. Admin Account
        </span>
        <span className="text-gray-600">→</span>
        <span className={`px-3 py-1 rounded-full ${step === 2 ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400'}`}>
          2. Registries
        </span>
      </div>

      {error && (
        <div className="px-4 py-3 bg-red-500/10 border border-red-500/30 rounded-lg text-red-400 text-sm">
          {error}
        </div>
      )}

      {step === 1 && (
        <form onSubmit={goToRegistries} className="card space-y-4">
          <h2 className="text-xl font-semibold text-gray-100">Create your admin account</h2>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Username</label>
            <input type="text" value={username} onChange={(e) => setUsername(e.target.value)} className="input w-full" autoFocus />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Email</label>
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} className="input w-full" />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Password</label>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} className="input w-full" />
          </div>
          <div>
            <label className="block text-sm text-gray-400 mb-1">Confirm Password</label>
            <input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} className="input w-full" />
          </div>
          <button type="submit" className="btn btn-primary w-full">Continue</button>
        </form>
      )}

      {step === 2 && (
        <div className="card space-y-4">
          <h2 className="text-xl font-semibold text-gray-100">Choose registries to add</h2>
          <p className="text-sm text-gray-500">
            You can add, edit, or remove registries later from Settings.
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            {REGISTRY_OPTIONS.map((reg) => (
              <label
                key={reg.id}
                className={`flex items-center gap-3 p-3 rounded-lg border cursor-pointer transition-colors min-w-0 ${
                  selected.has(reg.id)
                    ? 'border-blue-500/50 bg-blue-500/10'
                    : 'border-gray-700 bg-gray-800/50 hover:bg-gray-800'
                }`}
              >
                <input
                  type="checkbox"
                  checked={selected.has(reg.id)}
                  onChange={() => toggleRegistry(reg.id)}
                  className="w-4 h-4 shrink-0"
                />
                <span className="text-xl shrink-0">{reg.icon}</span>
                <span className="min-w-0">
                  <div className="text-sm font-medium text-gray-100">{reg.name}</div>
                  <div className="text-xs text-gray-500 font-mono break-all">{reg.url}</div>
                </span>
              </label>
            ))}
          </div>

          <div className="flex gap-3 pt-2">
            <button type="button" onClick={() => setStep(1)} className="btn btn-secondary">
              Back
            </button>
            <button type="button" onClick={finishSetup} disabled={submitting} className="btn btn-primary flex-1">
              {submitting ? 'Setting up...' : 'Finish Setup'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
