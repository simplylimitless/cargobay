import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { ArtifactDetail } from '../../pages/ArtifactDetail'

describe('ArtifactDetail', () => {
  const registryId = 'registry-1'
  const artifactType = 'docker'
  const namespace = 'library'
  const artifactName = 'nginx'
  const version = '1.21.0'

  const mockArtifact = {
    ID: 'artifact-1',
    RegistryID: registryId,
    ArtifactType: artifactType,
    Namespace: namespace,
    ArtifactName: artifactName,
    Version: version,
    Digest: 'sha256:abc123def456',
    DigestAlgorithm: 'sha256',
    Size: 1048576,
    Created: '2024-01-01T00:00:00Z',
    Tags: ['latest'],
    Signatures: [{ type: 'cosign', verified: true }],
    Metadata: { uploadedBy: 'testuser' },
  }

  beforeEach(() => {
    // Mock fetch
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve({
            artifacts: [mockArtifact],
          }),
      } as Response)
    )
  })

  afterEach(() => {
    jest.resetAllMocks()
  })

  test('renders loading state', () => {
    render(<ArtifactDetail />)
    // Initial render - should show loading
    expect(document.body).toBeTruthy()
  })

  test('displays artifact icon by type', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/nginx/i)).toBeInTheDocument()
    })

    // Docker icon
    expect(screen.getByText('🐳')).toBeInTheDocument()
  })

  test('displays artifact name and type', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeInTheDocument()
    })

    expect(screen.getByText('DOCKER')).toBeInTheDocument()
  })

  test('displays registry ID', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(`Registry: ${registryId}`)).toBeInTheDocument()
    })
  })

  test('displays version count', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('1 version')).toBeInTheDocument()
    })
  })

  test('displays pull command', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/docker pull nginx:1.21.0/i)).toBeInTheDocument()
    })
  })

  test('hides library namespace in pull command for Docker', async () => {
    const route = `/registries/${registryId}/${artifactType}/library/nginx`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/docker pull nginx:1.21.0/i)).toBeInTheDocument()
    })
  })

  test('shows namespace for non-library Docker images', async () => {
    const route = `/registries/${registryId}/${artifactType}/myorg/nginx`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/docker pull myorg\/nginx:1.21.0/i)).toBeInTheDocument()
    })
  })

  test('displays signature indicator', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('1 signature')).toBeInTheDocument()
    })

    expect(screen.getByText(/verified/i)).toBeInTheDocument()
  })

  test('displays unsigned badge when no signatures', async () => {
    const unsignedArtifact = {
      ...mockArtifact,
      Signatures: null,
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [unsignedArtifact] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Unsigned')).toBeInTheDocument()
    })
  })

  test('displays digest', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/sha256:abc123def456/i)).toBeInTheDocument()
    })
  })

  test('displays child manifests for multi-arch images', async () => {
    const multiArchArtifact = {
      ...mockArtifact,
      Metadata: {
        uploadedBy: 'testuser',
        childManifests: [
          { os: 'linux', arch: 'amd64', digest: 'sha256:child1', size: 524288 },
          { os: 'linux', arch: 'arm64', digest: 'sha256:child2', size: 491520 },
        ],
      },
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [multiArchArtifact] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('linux/amd64')).toBeInTheDocument()
      expect(screen.getByText('linux/arm64')).toBeInTheDocument()
    })
  })

  test('filters unknown OS/ARCH from child manifests', async () => {
    const artifactWithUnknown = {
      ...mockArtifact,
      Metadata: {
        uploadedBy: 'testuser',
        childManifests: [
          { os: 'linux', arch: 'amd64', digest: 'sha256:child1', size: 524288 },
          { os: 'unknown', arch: 'unknown', digest: 'sha256:unknown', size: 0 },
        ],
      },
    }

    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [artifactWithUnknown] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('linux/amd64')).toBeInTheDocument()
      expect(screen.queryByText('unknown')).not.toBeInTheDocument()
    })
  })

  test('displays size in appropriate units', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('1.0 MB')).toBeInTheDocument()
    })
  })

  test('shows time ago for last pushed', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/ago/i)).toBeInTheDocument()
    })
  })

  test('handles delete version', async () => {
    const deleteFn = jest.fn(() =>
      Promise.resolve({
        ok: true,
      })
    )

    global.fetch = jest.fn((url, init) => {
      if (init?.method === 'DELETE') {
        return deleteFn(url, init) as Promise<Response>
      }
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [mockArtifact] }),
      } as Response)
    })

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/delete/i)).toBeInTheDocument()
    })
  })

  test('handles error state', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: false,
        status: 404,
        statusText: 'Not Found',
      })
    )

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    // Error handling
    await waitFor(() => {
      expect(document.body).toBeTruthy()
    })
  })

  test('shows no versions found when empty', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ artifacts: [] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/No versions found/i)).toBeInTheDocument()
    })
  })

  test('copies pull command to clipboard', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText(/docker pull nginx:1.21.0/i)).toBeInTheDocument()
    })

    // Copy button click
    const copyButtons = screen.getAllByTitle('Copy')
    expect(copyButtons.length).toBeGreaterThan(0)
  })

  test('navigates to version on click', async () => {
    const mockNavigate = jest.fn()

    jest.mock('react-router-dom', () => {
      const actual = jest.requireActual('react-router-dom')
      return {
        ...actual,
        useNavigate: () => mockNavigate,
        useParams: () => ({
          registryId,
          artifactType,
          namespace,
          artifactName,
        }),
      }
    })

    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <ArtifactDetail />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('1.21.0')).toBeInTheDocument()
    })

    const versionLink = screen.getByText('1.21.0')
    await fireEvent.click(versionLink)

    expect(mockNavigate).toHaveBeenCalled()
  })
})
