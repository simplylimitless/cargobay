import { screen, fireEvent } from '@testing-library/react'
import { Navbar } from '../../components/Navbar'
import { render, createMockUser } from '../test-utils'

const { mockNavigate } = vi.hoisted(() => ({ mockNavigate: vi.fn() }))

vi.mock('react-router-dom', async () => ({
  ...(await vi.importActual('react-router-dom')),
  useNavigate: () => mockNavigate,
}))

// Mock SearchInput component
vi.mock('../../components/SearchInput', () => ({
  SearchInput: () => <div data-testid="mock-search-input">Search</div>,
}))

describe('Navbar', () => {
  beforeEach(() => {
    mockNavigate.mockClear()
  })

  afterEach(() => {
    vi.clearAllMocks()
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
    render(<Navbar />, { user: createMockUser({ username: 'testuser' }) })

    expect(screen.getByRole('button', { name: /log out/i })).toBeInTheDocument()
  })

  test('renders Guide link for all users', () => {
    render(<Navbar />)

    expect(screen.getByRole('link', { name: /Guide/i })).toBeInTheDocument()
  })

  test('renders Upload link when authenticated', () => {
    render(<Navbar />, { user: createMockUser({ username: 'testuser' }) })

    expect(screen.getByRole('link', { name: /Upload/i })).toBeInTheDocument()
  })

  test('renders Browse link when authenticated', () => {
    render(<Navbar />, { user: createMockUser({ username: 'testuser' }) })

    expect(screen.getByRole('link', { name: /Browse/i })).toBeInTheDocument()
  })

  test('renders Vulnerabilities link for admin users', () => {
    render(<Navbar />, { user: createMockUser({ username: 'admin', roles: ['admin'] }) })

    expect(screen.getByRole('link', { name: /Vulnerabilities/i })).toBeInTheDocument()
  })

  test('renders Profile link with username when authenticated', () => {
    render(<Navbar />, { user: createMockUser({ username: 'testuser' }) })

    expect(screen.getByText('testuser')).toBeInTheDocument()
  })

  test('navigates to home on logo click', async () => {
    render(<Navbar />)

    const logoLink = screen.getByRole('link', { name: /cargobay/i })
    await fireEvent.click(logoLink)

    expect(logoLink).toHaveAttribute('href', '/')
  })

  test('navigates to getting-started on guide click', async () => {
    render(<Navbar />)

    const guideLink = screen.getByRole('link', { name: /Guide/i })
    expect(guideLink).toHaveAttribute('href', '/getting-started')
  })

  test('navigates to login on login click', async () => {
    render(<Navbar />)

    const loginLink = screen.getByRole('link', { name: /log in/i })
    expect(loginLink).toHaveAttribute('href', '/login')
  })

  test('handles logout correctly', async () => {
    render(<Navbar />, { user: createMockUser({ username: 'testuser' }) })

    const logoutButton = screen.getByRole('button', { name: /log out/i })
    await fireEvent.click(logoutButton)

    expect(mockNavigate).toHaveBeenCalledWith('/')
  })

  test('displays user role badge', () => {
    render(<Navbar />, { user: createMockUser({ username: 'admin', roles: ['admin'] }) })

    expect(screen.getByText('admin', { selector: '.badge' })).toBeInTheDocument()
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

describe('Navbar with Auth', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  test('shows username when user is authenticated', () => {
    render(<Navbar />, { user: createMockUser({ username: 'testuser' }) })

    expect(screen.getByText('testuser')).toBeInTheDocument()
  })

  test('shows admin links when user is admin', () => {
    render(<Navbar />, { user: createMockUser({ username: 'admin', roles: ['admin'] }) })

    expect(screen.getByRole('link', { name: /Vulnerabilities/i })).toBeInTheDocument()
  })
})
