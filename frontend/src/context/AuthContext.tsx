import { createContext, useContext, useState, useEffect, ReactNode } from 'react'

export type Role = 'admin' | 'developer' | 'viewer' | 'publisher' | 'auditor'

export interface User {
  id: string
  username: string
  email: string
  roles: Role[]
  permissions: string[]
}

const STORAGE_KEY = 'cargobay_access_token'
const STORAGE_USER_KEY = 'cargobay_user'

interface AuthContextValue {
  currentUser: User | null
  token: string | null
  isAdmin: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => void
  canManage: (uploadedBy: string | null | undefined) => boolean
  updateProfile: (username: string, email: string) => void
  setSession: (accessToken: string, user: { userId: string; username: string; email: string; roles?: string[]; permissions?: string[] }) => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

function loadStoredUser(): User | null {
  const raw = localStorage.getItem(STORAGE_USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

function loadToken(): string | null {
  return localStorage.getItem(STORAGE_KEY)
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [currentUser, setCurrentUser] = useState<User | null>(loadStoredUser)
  const [token, setToken] = useState<string | null>(loadToken())

  useEffect(() => {
    // Check if we have a token but no user - this can happen on refresh
    if (token && !currentUser) {
      const user = loadStoredUser()
      if (user) {
        setCurrentUser(user)
      }
    }
  }, [token])

  const login = async (username: string, password: string) => {
    try {
      const response = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })

      const data = await response.json()

      if (!response.ok) {
        throw new Error(data.error || 'Login failed')
      }

      if (data.accessToken) {
        setToken(data.accessToken)
        if (data.user) {
          const user: User = {
            id: data.user.userId,
            username: data.user.username,
            email: data.user.email,
            roles: data.user.roles || [],
            permissions: data.user.permissions || [],
          }
          setCurrentUser(user)
          localStorage.setItem(STORAGE_KEY, data.accessToken)
          localStorage.setItem(STORAGE_USER_KEY, JSON.stringify(user))
        }
      }
    } catch (error) {
      console.error('Login error:', error)
      throw error
    }
  }

  const logout = () => {
    // Call the logout endpoint to invalidate the token
    const currentToken = localStorage.getItem(STORAGE_KEY)
    if (currentToken) {
      fetch('/api/v1/auth/logout', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${currentToken}`,
        },
      }).catch(() => {}) // Ignore errors on logout
    }

    setToken(null)
    setCurrentUser(null)
    localStorage.removeItem(STORAGE_KEY)
    localStorage.removeItem(STORAGE_USER_KEY)
  }

  const setSession: AuthContextValue['setSession'] = (accessToken, user) => {
    const newUser: User = {
      id: user.userId,
      username: user.username,
      email: user.email,
      roles: (user.roles || []) as Role[],
      permissions: user.permissions || [],
    }
    setToken(accessToken)
    setCurrentUser(newUser)
    localStorage.setItem(STORAGE_KEY, accessToken)
    localStorage.setItem(STORAGE_USER_KEY, JSON.stringify(newUser))
  }

  const updateProfile = (username: string, email: string) => {
    setCurrentUser((prev) => {
      if (!prev) return prev
      const updated = { ...prev, username, email }
      localStorage.setItem(STORAGE_USER_KEY, JSON.stringify(updated))
      return updated
    })
  }

  const isAdmin = currentUser?.roles.includes('admin') || false

  const canManage = (uploadedBy: string | null | undefined) => {
    if (!currentUser) return false
    return isAdmin || (!!uploadedBy && currentUser.username === uploadedBy)
  }

  return (
    <AuthContext.Provider value={{ currentUser, token, isAdmin, login, logout, canManage, updateProfile, setSession }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}
