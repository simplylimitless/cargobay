import { ReactNode, Context } from 'react'
import { render as testingLibraryRender } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AuthProvider, User, Role } from '../context/AuthContext'

// Custom render function that wraps components with providers
export function render(
  ui: ReactNode,
  {
    route = '/',
    history = [route],
    user = null,
    ...renderOptions
  }: {
    route?: string
    history?: string[]
    user?: User | null
  } & Omit<Parameters<typeof testingLibraryRender>[1], 'wrapper'>
) {
  const Wrapper: React.FC<{ children: ReactNode }> = ({ children }) => (
    <MemoryRouter initialEntries={history}>
      <AuthProvider user={user}>{children}</AuthProvider>
    </MemoryRouter>
  )

  return testingLibraryRender(ui, { wrapper: Wrapper, ...renderOptions })
}

// Mock functions
export const createMockUser = (overrides?: Partial<User>): User => ({
  id: 'test-user-id',
  username: 'testuser',
  email: 'test@example.com',
  roles: ['viewer'],
  permissions: ['artifact:read'],
  ...overrides,
})

export const createMockAuthContext = (overrides?: Partial<ReturnType<typeof useAuth>>) => ({
  currentUser: createMockUser(),
  token: 'test-token',
  isAdmin: false,
  login: jest.fn(),
  logout: jest.fn(),
  canManage: jest.fn().mockReturnValue(true),
  updateProfile: jest.fn(),
  setSession: jest.fn(),
  ...overrides,
})

// Mock localStorage
export const mockLocalStorage = () => {
  const store: Record<string, string> = {}
  const originalLocalStorage = window.localStorage

  Object.defineProperty(window, 'localStorage', {
    value: {
      getItem: (key: string) => store[key] || null,
      setItem: (key: string, value: string) => {
        store[key] = value.toString()
      },
      removeItem: (key: string) => {
        delete store[key]
      },
      clear: () => {
        Object.keys(store).forEach((key) => delete store[key])
      },
    },
    writable: true,
  })

  return () => {
    Object.defineProperty(window, 'localStorage', { value: originalLocalStorage, writable: true })
  }
}

// Mock fetch
export const mockFetch = (responses: Record<string, { ok: boolean; json: any }>) => {
  const originalFetch = global.fetch

  global.fetch = jest.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : (input as Request).url
    const path = url ? new URL(url).pathname + (url.includes('?') ? '?' + new URL(url).searchParams.toString() : '') : ''

    for (const key in responses) {
      if (path.includes(key)) {
        const response = responses[key]
        return Promise.resolve({
          ok: response.ok,
          status: response.ok ? 200 : 400,
          statusText: response.ok ? 'OK' : 'Bad Request',
          json: () => Promise.resolve(response.json),
          text: () => Promise.resolve(JSON.stringify(response.json)),
        })
      }
    }

    return Promise.resolve({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      json: () => Promise.resolve({ error: 'Not found' }),
      text: () => Promise.resolve('Not found'),
    })
  })

  return () => {
    global.fetch = originalFetch
  }
}

// Mock useNavigate
export const createMockNavigate = () => {
  const mockFn = jest.fn()
  return mockFn
}

// Mock matchPath for Breadcrumbs
export const mockMatchPath = (patterns: string[], pathname: string) => {
  const originalMatchPath = jest.requireActual('react-router-dom').matchPath

  return originalMatchPath(patterns[0], pathname)
}

// Wait for state updates
export async function waitForAsync() {
  await new Promise((resolve) => setTimeout(resolve, 0))
}

// Test data
export const testArtifacts = [
  {
    ID: 'artifact-1',
    RegistryID: 'registry-1',
    ArtifactType: 'docker',
    Namespace: 'library',
    ArtifactName: 'nginx',
    Version: '1.21.0',
    Digest: 'sha256:abc123',
    DigestAlgorithm: 'sha256',
    Size: 1048576,
    Created: '2024-01-01T00:00:00Z',
    Tags: ['latest'],
    Signatures: [{ type: 'cosign', verified: true }],
    Metadata: { uploadedBy: 'testuser' },
  },
  {
    ID: 'artifact-2',
    RegistryID: 'registry-1',
    ArtifactType: 'npm',
    Namespace: '@myorg',
    ArtifactName: 'package',
    Version: '1.0.0',
    Digest: 'sha256:def456',
    DigestAlgorithm: 'sha256',
    Size: 2097152,
    Created: '2024-01-02T00:00:00Z',
    Tags: ['latest'],
    Signatures: null,
    Metadata: { uploadedBy: 'testuser' },
  },
]

export const testRegistries = [
  {
    id: 'registry-1',
    name: 'Docker Proxy',
    url: 'https://registry-1.example.com',
    type: 'docker',
    enabled: true,
    priority: 1,
    private: false,
    proxy: true,
  },
]
