import { Link, useNavigate } from 'react-router-dom'
import { SearchInput } from './SearchInput'
import { useAuth } from '../context/AuthContext'

export function Navbar() {
  const navigate = useNavigate()
  const { currentUser, isAdmin, logout } = useAuth()

  const handleLogout = () => {
    logout()
    navigate('/')
  }

  return (
    <nav className="bg-gray-900/95 backdrop-blur-sm border-b border-gray-800 sticky top-0 z-50 shadow-lg">
      <div className="container mx-auto px-4 py-3">
        <div className="flex items-center justify-between gap-4">
          <Link to="/" className="flex items-center gap-3 group">
            <div className="w-10 h-10 rounded-xl overflow-hidden shadow-lg shadow-blue-600/20 group-hover:shadow-purple-600/20 transition-all">
              <img src="/apple-touch-icon.png" alt="cargobay" className="w-full h-full object-cover" />
            </div>
            <div className="hidden md:block">
              <span className="text-xl font-bold text-white tracking-tight">cargobay</span>
              <div className="text-xs text-gray-400 font-medium">Universal Artifact Registry</div>
            </div>
          </Link>

          <div className="flex-1 max-w-xl">
            <SearchInput />
          </div>

          <div className="flex items-center gap-3">
            <Link
              to="/getting-started"
              className="hidden md:block px-3 py-1.5 text-sm font-medium text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-all"
            >
              Guide
            </Link>
            {currentUser && (
              <>
                <Link
                  to="/upload"
                  className="hidden md:block px-3 py-1.5 text-sm font-medium text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-all"
                >
                  Upload
                </Link>
                <Link
                  to="/browse"
                  className="hidden md:block px-3 py-1.5 text-sm font-medium text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-all"
                >
                  Browse
                </Link>
                <Link
                  to="/stats"
                  className="hidden md:block px-3 py-1.5 text-sm font-medium text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-all"
                >
                  Stats
                </Link>
              </>
            )}
            {isAdmin && (
              <Link
                to="/vulnerabilities"
                className="hidden md:block px-3 py-1.5 text-sm font-medium text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-all"
              >
                Vulnerabilities
              </Link>
            )}
            {currentUser ? (
              <div className="flex items-center gap-2">
                {isAdmin && (
                  <Link
                    to="/settings"
                    className="hidden md:block px-3 py-1.5 text-sm font-medium text-gray-300 hover:text-white hover:bg-gray-800 rounded-lg transition-all"
                  >
                    Settings
                  </Link>
                )}
                <Link
                  to="/profile"
                  className="hidden md:flex items-center gap-1.5 px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded-lg text-sm transition-colors"
                >
                  <span className="text-gray-200 font-medium">{currentUser.username}</span>
                  {currentUser.roles.length > 0 && (
                    <span className={`badge ${
                      currentUser.roles.includes('admin') ? 'badge-warning' :
                      currentUser.roles.includes('developer') ? 'badge-blue' :
                      currentUser.roles.includes('publisher') ? 'badge-green' :
                      currentUser.roles.includes('auditor') ? 'badge-purple' : 'badge-info'
                    }`}>
                      {currentUser.roles[0]}
                    </span>
                  )}
                </Link>
                <button onClick={handleLogout} className="btn btn-secondary btn-sm">
                  Log out
                </button>
              </div>
            ) : (
              <Link to="/login" className="btn btn-primary btn-sm">
                Log in
              </Link>
            )}
          </div>
        </div>
      </div>
    </nav>
  )
}
