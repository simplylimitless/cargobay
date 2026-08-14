import { useNavigate, useLocation } from 'react-router-dom'
import { MOCK_USERS, useAuth } from '../context/AuthContext'

export function Login() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()

  const handleLogin = (userId: string) => {
    login(userId)
    const from = (location.state as { from?: string })?.from || '/'
    navigate(from)
  }

  return (
    <div className="max-w-md mx-auto space-y-8">
      <div>
        <h1 className="text-3xl font-bold text-white mb-2">Log In</h1>
        <p className="text-gray-400">Choose a mock account to continue</p>
      </div>

      <div className="card space-y-3">
        {MOCK_USERS.map((user) => (
          <button
            key={user.id}
            onClick={() => handleLogin(user.id)}
            className="w-full flex items-center justify-between p-4 bg-gray-800/50 hover:bg-gray-800 rounded-lg transition-colors text-left"
          >
            <div>
              <div className="font-medium text-gray-100">{user.username}</div>
              <div className="text-sm text-gray-500">{user.email}</div>
            </div>
            <span
              className={`badge ${user.role === 'admin' ? 'badge-warning' : 'badge-info'}`}
            >
              {user.role}
            </span>
          </button>
        ))}
      </div>
    </div>
  )
}
