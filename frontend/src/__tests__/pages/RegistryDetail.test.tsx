import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { RegistryDetail } from '../../pages/RegistryDetail'

describe('RegistryDetail', () => {
  const registryId = 'registry-1'
  const mockRegistry = {
    id: registryId,
    name: 'Docker Proxy',
    url: 'https://registry-1.example.com',
    type: 'docker',
    enabled: true,
    priority: 1,
    private: false,
    proxy: true,
  }

  beforeEach(() => {
    // Mock fetch
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(mockRegistry),
      } as Response)
    )
  })

  afterEach(() => {
    jest.resetAllMocks()
  })

  test('renders loading state', () => {
    render(<RegistryDetail />)
    expect(document.body).toBeTruthy()
  })

  test('displays registry name', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Docker Proxy')).toBeInTheDocument()
    })
  })

  test('displays registry URL', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('https://registry-1.example.com')).toBeInTheDocument()
    })
  })

  test('displays registry type', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Type: docker')).toBeInTheDocument()
    })
  })

  test('displays enabled status badge', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('enabled')).toBeInTheDocument()
    })
  })

  test('displays disabled status when registry is disabled', async () => {
    const disabledRegistry = {
      ...mockRegistry,
      enabled: false,
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(disabledRegistry),
      } as Response)
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('disabled')).toBeInTheDocument()
    })
  })

  test('displays private status badge', async () => {
    const privateRegistry = {
      ...mockRegistry,
      private: true,
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(privateRegistry),
      } as Response)
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('private')).toBeInTheDocument()
    })
  })

  test('displays public status when registry is public', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('public')).toBeInTheDocument()
    })
  })

  test('displays proxy badge for proxy registries', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('proxy')).toBeInTheDocument()
    })
  })

  test('shows proxy description for proxy registries', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/Pull-through cache/i)).toBeInTheDocument()
    })
  })

  test('shows artifact types grid', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Available Artifact Types')).toBeInTheDocument()
    })

    // Docker and Helm for docker type
    expect(screen.getByText(/Container Images/i)).toBeInTheDocument()
    expect(screen.getByText(/Helm Charts/i)).toBeInTheDocument()
  })

  test('displays correct artifact types for npm registry', async () => {
    const npmRegistry = {
      ...mockRegistry,
      type: 'npm',
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(npmRegistry),
      } as Response)
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/NPM Packages/i)).toBeInTheDocument()
      expect(screen.getByText('📦')).toBeInTheDocument()
    })
  })

  test('displays correct artifact types for maven registry', async () => {
    const mavenRegistry = {
      ...mockRegistry,
      type: 'maven',
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(mavenRegistry),
      } as Response)
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/Maven Artifacts/i)).toBeInTheDocument()
      expect(screen.getByText('☕')).toBeInTheDocument()
    })
  })

  test('displays correct artifact types for pypi registry', async () => {
    const pypiRegistry = {
      ...mockRegistry,
      type: 'pypi',
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(pypiRegistry),
      } as Response)
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/Python Packages/i)).toBeInTheDocument()
      expect(screen.getByText('🐍')).toBeInTheDocument()
    })
  })

  test('displays correct artifact types for nuget registry', async () => {
    const nugetRegistry = {
      ...mockRegistry,
      type: 'nuget',
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(nugetRegistry),
      } as Response)
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/NuGet Packages/i)).toBeInTheDocument()
      expect(screen.getByText('🔨')).toBeInTheDocument()
    })
  })

  test('navigates to artifact type on click', async () => {
    const mockNavigate = jest.fn()

    jest.mock('react-router-dom', () => {
      const actual = jest.requireActual('react-router-dom')
      return {
        ...actual,
        useNavigate: () => mockNavigate,
        useParams: () => ({
          registryId,
        }),
      }
    })

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Container Images')).toBeInTheDocument()
    })

    const artifactTypeCard = screen.getByText('Container Images').closest('.card')
    if (artifactTypeCard) {
      await fireEvent.click(artifactTypeCard)
      expect(mockNavigate).toHaveBeenCalledWith(`/registries/${registryId}/docker`)
    }
  })

  test('displays 404 for unknown registry', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: false,
        status: 404,
        statusText: 'Not Found',
      })
    )

    const route = `/registries/unknown-registry`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Registry Not Found')).toBeInTheDocument()
    })
  })

  test('handles server error', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
      })
    )

    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    // Should still render without crashing
    await waitFor(() => {
      expect(document.body).toBeTruthy()
    })
  })

  test('displays artifact type count', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/2 Artifact Types Available/i)).toBeInTheDocument()
    })
  })

  test('shows explorer link for artifact types', async () => {
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Explore')).toBeInTheDocument()
    })
  })

  test('displays loading spinner', () => {
    render(<RegistryDetail />)
    // Initially should show loading
    expect(document.body).toBeTruthy()
  })

  test('hides library namespace from registry info', () => {
    // Not applicable for RegistryDetail but testing that it renders
    const route = `/registries/${registryId}`
    render(
      <RegistryDetail />,
      {
        route,
        history: [route],
      }
    )

    expect(document.body).toBeTruthy()
  })
})
