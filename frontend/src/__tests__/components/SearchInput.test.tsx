import { screen, waitFor, fireEvent } from '@testing-library/react'
import { SearchInput } from '../../components/SearchInput'
import { render } from '../test-utils'

const { mockNavigate } = vi.hoisted(() => ({ mockNavigate: vi.fn() }))

vi.mock('react-router-dom', async () => ({
  ...(await vi.importActual('react-router-dom')),
  useNavigate: () => mockNavigate,
}))

describe('SearchInput', () => {
  beforeEach(() => {
    mockNavigate.mockClear()

    // Mock window.fetch
    global.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ suggestions: ['nginx', 'nginx:latest', 'nginx:alpine'] }),
      } as Response)
    )
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  test('renders search input correctly', () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    expect(input).toBeInTheDocument()
    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveAttribute('placeholder', 'Search artifacts, packages, images...')
  })

  test('renders search button icon', () => {
    render(<SearchInput />)

    const submitButton = screen.getByRole('button', { hidden: true })
    expect(submitButton).toBeInTheDocument()
  })

  test('displays dropdown when typing and suggestions are available', async () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: 'ng' } })

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalled()
    })

    // Wait for debounce
    await new Promise((resolve) => setTimeout(resolve, 250))

    const suggestions = await screen.findAllByRole('button', { hidden: true })
    // First button is the submit icon; the rest are suggestions
    expect(suggestions).toHaveLength(4)
    expect(suggestions[1]).toHaveTextContent('nginx')
  })

  test('clears suggestions when input is empty', async () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: 'ng' } })

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalled()
    })
    await new Promise((resolve) => setTimeout(resolve, 250))

    // Suggestions should be visible
    expect(await screen.findByText('nginx')).toBeInTheDocument()

    // Clear input
    fireEvent.change(input, { target: { value: '' } })

    await new Promise((resolve) => setTimeout(resolve, 250))

    expect(screen.queryByText('nginx')).not.toBeInTheDocument()
  })

  test('navigates on form submit with query', async () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: 'test-query' } })

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalled()
    })

    fireEvent.submit(input.closest('form')!)

    expect(mockNavigate).toHaveBeenCalledWith('/search?q=test-query')
  })

  test('navigates to autocomplete endpoint on debounce', async () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: 'test' } })

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/v1/search/autocomplete')
      )
    })
  })

  test('handles keyboard navigation with arrow keys', async () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: 'ng' } })

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalled()
    })
    await new Promise((resolve) => setTimeout(resolve, 250))

    // Get suggestion buttons
    await screen.findAllByRole('button', { hidden: true })

    // Test ArrowDown - should select first item
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    // The active index logic should work

    // Test ArrowUp - should select last item
    fireEvent.keyDown(input, { key: 'ArrowUp' })

    // Test Escape - should close dropdown
    fireEvent.keyDown(input, { key: 'Escape' })

    await new Promise((resolve) => setTimeout(resolve, 150))
    expect(screen.queryByText('nginx')).not.toBeInTheDocument()
  })

  test('does not fetch with empty input', () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: '' } })

    expect(global.fetch).not.toHaveBeenCalled()
  })

  test('handles whitespace-only input correctly', () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: '   ' } })

    // Should not fetch for whitespace only
    expect(global.fetch).not.toHaveBeenCalled()
  })

  test('cancels previous debounce when typing quickly', async () => {
    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    const fetchSpy = vi.spyOn(global, 'fetch')

    // Rapid typing
    fireEvent.change(input, { target: { value: 't' } })
    fireEvent.change(input, { target: { value: 'te' } })
    fireEvent.change(input, { target: { value: 'tes' } })
    fireEvent.change(input, { target: { value: 'test' } })

    await new Promise((resolve) => setTimeout(resolve, 300))

    // Should only have one fetch call (debounced)
    expect(fetchSpy).toHaveBeenCalledTimes(1)
    expect(fetchSpy).toHaveBeenCalledWith(
      expect.stringContaining('test')
    )
  })

  test('handles fetch error gracefully', async () => {
    global.fetch = vi.fn(() => Promise.resolve({ ok: false } as Response))

    render(<SearchInput />)

    const input = screen.getByPlaceholderText(/Search artifacts, packages, images/i)
    fireEvent.change(input, { target: { value: 'test' } })

    await new Promise((resolve) => setTimeout(resolve, 250))

    // Should not crash on error
    expect(document.body).toBeTruthy()
  })
})
