import { createContext, useContext, useState, ReactNode } from 'react'

export type Role = 'admin' | 'user'

export interface MockUser {
  id: string
  username: string
  email: string
  role: Role
}

export const MOCK_USERS: MockUser[] = [
  { id: 'admin', username: 'admin', email: 'admin@cargobay.dev', role: 'admin' },
  { id: 'alice', username: 'alice', email: 'alice@cargobay.dev', role: 'user' },
  { id: 'bob', username: 'bob', email: 'bob@cargobay.dev', role: 'user' },
]

const STORAGE_KEY = 'cargobay_auth_user'

interface AuthContextValue {
  currentUser: MockUser | null
  isAdmin: boolean
  login: (userId: string) => void
  logout: () => void
  canManage: (uploadedBy: string) => boolean
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

function loadStoredUser(): MockUser | null {
  const raw = localStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  const found = MOCK_USERS.find((u) => u.id === raw)
  return found ?? null
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [currentUser, setCurrentUser] = useState<MockUser | null>(loadStoredUser)

  const login = (userId: string) => {
    const user = MOCK_USERS.find((u) => u.id === userId)
    if (!user) return
    setCurrentUser(user)
    localStorage.setItem(STORAGE_KEY, user.id)
  }

  const logout = () => {
    setCurrentUser(null)
    localStorage.removeItem(STORAGE_KEY)
  }

  const isAdmin = currentUser?.role === 'admin'

  const canManage = (uploadedBy: string) => {
    if (!currentUser) return false
    return isAdmin || currentUser.id === uploadedBy
  }

  return (
    <AuthContext.Provider value={{ currentUser, isAdmin, login, logout, canManage }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider')
  return ctx
}
