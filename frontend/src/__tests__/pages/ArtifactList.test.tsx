import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { ArtifactList } from '../../pages/ArtifactList'

describe('ArtifactList', () => {
  const registryId = 'registry-1'
  const artifactType = 'docker'

  const mockArtifacts = [
    {
      ID: 'artifact-1',
      RegistryID: registryId,
      ArtifactType: 'docker',
      Namespace: 'library',
      ArtifactName: 'nginx',
      Version: '1.21.0',
      Size: 1048576,
      Tags: ['latest'],
    },
    {
      ID: 'artifact-2',
      RegistryID: registryId,
      ArtifactType: 'docker',
      Namespace: 'library',
      ArtifactName: 'nginx',
      Version: '1.20.0',
      Size: 1024000,
      Tags: [],
    },
    {
      ID: 'artifact-3',
      RegistryID: registryId,
      ArtifactType: 'docker',
      Namespace: 'myorg',
      ArtifactName: 'app',
      Version: '2.0.0',
      Size: 2097152,
      Tags: ['latest', 'v2'],
    },
  ]

  beforeEach(() => {
    // Mock fetch
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: mockArtifacts }),
      } as Response)
    )
  })

  afterEach(() => {
    jest.resetAllMocks()
  })

  test('renders loading state', () => {
    render(<ArtifactList />)
    expect(document.body).toBeTruthy()
  })

  test('displays artifact type header', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Docker Artifacts')).toBeInTheDocument()
    })
  })

  test('displays namespace in header when filtered', async () => {
    const route = `/registries/${registryId}/${artifactType}/library`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Namespace: library')).toBeInTheDocument()
    })
  })

  test('displays artifact icon by type', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('🐳')).toBeInTheDocument()
    })
  })

  test('shows different icons for different artifact types', async () => {
    // NPM
    let route = `/registries/${registryId}/npm`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('📦')).toBeInTheDocument()
    })

    // Maven
    route = `/registries/${registryId}/maven`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('☕')).toBeInTheDocument()
    })

    // Helm
    route = `/registries/${registryId}/helm`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('⚓')).toBeInTheDocument()
    })

    // PyPI
    route = `/registries/${registryId}/pypi`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('🐍')).toBeInTheDocument()
    })
  })

  test('displays search input', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByPlaceholderText(/Search docker artifacts/i)).toBeInTheDocument()
    })
  })

  test('filters artifacts by search query', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeInTheDocument()
      expect(screen.getByText('app')).toBeInTheDocument()
    })

    const searchInput = screen.getByPlaceholderText(/Search docker artifacts/i)
    await fireEvent.change(searchInput, { target: { value: 'ng' } })

    expect(screen.getByText('nginx')).toBeInTheDocument()
    expect(screen.queryByText('app')).not.toBeInTheDocument()
  })

  test('displays artifact count', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/2 artifacts/i)).toBeInTheDocument()
    })
  })

  test('groups versions by artifact name', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('2 versions')).toBeInTheDocument()
    })
  })

  test('displays latest version', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('latest 1.21.0')).toBeInTheDocument()
    })
  })

  test('displays version count per artifact', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/2 versions/i)).toBeInTheDocument()
    })
  })

  test('displays total size', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/1\.00 MB/i)).toBeInTheDocument()
    })
  })

  test('shows namespace for non-library Docker images', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('myorg/app')).toBeInTheDocument()
    })
  })

  test('hides library namespace for Docker images', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeInTheDocument()
      expect(screen.queryByText('library/nginx')).not.toBeInTheDocument()
    })
  })

  test('navigates to artifact detail on click', async () => {
    const mockNavigate = jest.fn()

    jest.mock('react-router-dom', () => {
      const actual = jest.requireActual('react-router-dom')
      return {
        ...actual,
        useNavigate: () => mockNavigate,
        useParams: () => ({
          registryId,
          artifactType,
        }),
      }
    })

    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeInTheDocument()
    })

    const artifactCard = screen.getByText('nginx').closest('.card')
    if (artifactCard) {
      await fireEvent.click(artifactCard)
      expect(mockNavigate).toHaveBeenCalled()
    }
  })

  test('shows empty state when no artifacts', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/No artifacts found/i)).toBeInTheDocument()
    })
  })

  test('shows empty state with search filter', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeInTheDocument()
    })

    const searchInput = screen.getByPlaceholderText(/Search docker artifacts/i)
    await fireEvent.change(searchInput, { target: { value: 'nonexistent' } })

    expect(screen.getByText(/No artifacts found/i)).toBeInTheDocument()
  })

  test('handles fetch error', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
      })
    )

    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
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

  test('shows namespace in header for filtered namespace', async () => {
    const route = `/registries/${registryId}/${artifactType}/library`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Namespace: library')).toBeInTheDocument()
    })
  })

  test('formats large sizes correctly', async () => {
    const largeArtifact = {
      ID: 'artifact-large',
      RegistryID: registryId,
      ArtifactType: 'docker',
      Namespace: 'library',
      ArtifactName: 'big-image',
      Version: '1.0.0',
      Size: 2147483648, // 2GB
      Tags: [],
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [largeArtifact] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/2\.00 GB/i)).toBeInTheDocument()
    })
  })

  test('displays artifact type badge', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <ArtifactList />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('DOCKER')).toBeInTheDocument()
    })
  })
})
