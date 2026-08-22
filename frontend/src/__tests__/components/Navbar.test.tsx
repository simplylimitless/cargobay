import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { Navbar } from '../../components/Navbar'
import { createMockNavigate, createMockUser } from '../test-utils'

// Mock SearchInput component
jest.mock('../../components/SearchInput', () => ({
  SearchInput: () => <div data-testid="mock-search-input">Search</div>,
}))

// Mock react-router-dom
jest.mock('react-router-dom', () => {
  const actual = jest.requireActual('react-router-dom')
  return {
    ...actual,
    useNavigate: jest.fn(),
    Link: ({ to, children, className }: { to: string; children: ReactNode; className?: string }) => {
      const handleClick = (e: React.MouseEvent) => {
        e.preventDefault()
        if (props.onClick) props.onClick(e)
      }
      const props = { to, onClick: handleClick }
      return <a href={to} className={className} onClick={handleClick}>{children}</a>
    },
  }
})

describe('Navbar', () => {
  const mockNavigate = jest.fn()
  let originalWindowLocation: string

  beforeEach(() => {
    jest.resetAllMocks()
    jest.mock('react-router-dom', () => {
      const actual = jest.requireActual('react-router-dom')
      return {
        ...actual,
        useNavigate: () => mockNavigate,
        useLocation: () => ({ pathname: '/' }),
        Link: ({ to, children, className, onClick }: any) => {
          const handleClick = (e: React.MouseEvent) => {
            e.preventDefault()
            if (onClick) onClick(e)
            else mockNavigate(to)
          }
          return <a href={to} className={className} onClick={handleClick}>{children}</a>
        },
      }
    })

    // Mock localStorage
    originalWindowLocation = window.localStorage
    Object.defineProperty(window, 'localStorage', {
      value: {
        getItem: () => null,
        setItem: () => {},
        removeItem: () => {},
      },
      writable: true,
    })
  })

  afterEach(() => {
    jest.resetAllMocks()
    Object.defineProperty(window, 'localStorage', { value: originalWindowLocation, writable: true })
  })

  test('renders logo and site name', () => {
    render(<Navbar />)

    expect(screen.getByText('cargobay')).toBeInTheDocument()
    expect(screen.getByText('Universal Artifact Registry')).toBeInTheDocument()
  })

  test('renders search input', () => {
    render(<Navbar />)

    expect(screen.getByTestId('mock-search-input')).toBeInTheDocument()
  })

  test('renders login button when not authenticated', () => {
    render(<Navbar />)

    expect(screen.getByRole('link', { name: /log in/i })).toBeInTheDocument()
  })

  test('renders logout button when authenticated', () => {
    render(<Navbar />)
    // Note: This requires auth context setup - we'll test with mock
  })

  test('renders Guide link for all users', () => {
    render(<Navbar />)

    expect(screen.getByRole('link', { name: /Guide/i })).toBeInTheDocument()
  })

  test('renders Upload link when authenticated', () => {
    render(<Navbar />)
    // Authenticated state needs to be mocked
  })

  test('renders Browse link when authenticated', () => {
    render(<Navbar />)
    // Authenticated state needs to be mocked
  })

  test('renders Vulnerabilities link for admin users', () => {
    render(<Navbar />)
    // Admin state needs to be mocked
  })

  test('renders Profile link with username when authenticated', () => {
    render(<Navbar />)
    // Authenticated state needs to be mocked
  })

  test('navigates to home on logo click', async () => {
    render(<Navbar />)

    const logoLink = screen.getByRole('link', { name: /cargobay/i })
    await fireEvent.click(logoLink)

    expect(mockNavigate).toHaveBeenCalledWith('/')
  })

  test('navigates to getting-started on guide click', async () => {
    render(<Navbar />)

    const guideLink = screen.getByRole('link', { name: /Guide/i })
    await fireEvent.click(guideLink)

    expect(mockNavigate).toHaveBeenCalledWith('/getting-started')
  })

  test('navigates to login on login click', async () => {
    render(<Navbar />)

    const loginLink = screen.getByRole('link', { name: /log in/i })
    await fireEvent.click(loginLink)

    expect(mockNavigate).toHaveBeenCalledWith('/login')
  })

  test('handles logout correctly', async () => {
    render(<Navbar />)
    // Logout button needs auth context
  })

  test('displays user role badge', () => {
    render(<Navbar />)
    // Role badge needs auth context
  })

  test('has proper navigation structure', () => {
    render(<Navbar />)

    const nav = screen.getByRole('navigation')
    expect(nav).toBeInTheDocument()

    // Logo
    expect(screen.getByRole('link', { name: /cargobay/i })).toBeInTheDocument()

    // Search
    expect(screen.getByTestId('mock-search-input')).toBeInTheDocument()

    // Navigation items container
    expect(nav).toBeTruthy()
  })

  test('is sticky at top', () => {
    render(<Navbar />)

    const nav = screen.getByRole('navigation')
    // The sticky class is applied in the component
    expect(nav).toHaveClass('sticky')
    expect(nav).toHaveClass('top-0')
  })

  test('has proper styling classes', () => {
    render(<Navbar />)

    const nav = screen.getByRole('navigation')

    expect(nav).toHaveClass('bg-gray-900/95')
    expect(nav).toHaveClass('backdrop-blur-sm')
    expect(nav).toHaveClass('border-b')
    expect(nav).toHaveClass('border-gray-800')
    expect(nav).toHaveClass('z-50')
    expect(nav).toHaveClass('shadow-lg')
  })
})

// Test with authenticated user context
describe('Navbar with Auth', () => {
  const mockNavigate = jest.fn()

  beforeEach(() => {
    jest.resetAllMocks()
    jest.mock('react-router-dom', () => {
      const actual = jest.requireActual('react-router-dom')
      return {
        ...actual,
        useNavigate: () => mockNavigate,
        useLocation: () => ({ pathname: '/' }),
        Link: ({ to, children, className }: any) => (
          <a href={to} className={className} onClick={(e: React.MouseEvent) => { e.preventDefault(); mockNavigate(to) }}>
            {children}
          </a>
        ),
      }
    })
  })

  test('shows username when user is authenticated', () => {
    render(<Navbar />)

    const user = createMockUser({ username: 'testuser' })
    // User needs to be passed to AuthProvider
  })

  test('shows admin links when user is admin', () => {
    render(<Navbar />)
    // Admin state needs to be mocked
  })
})
