import { screen, waitFor } from '@testing-library/react'
import { Breadcrumbs } from '../../components/Breadcrumbs'
import { render } from '../test-utils'

describe('Breadcrumbs', () => {
  const registryId = 'registry-1'
  const artifactType = 'docker'
  const namespace = 'library'
  const artifactName = 'nginx'
  const version = '1.21.0'

  beforeEach(() => {
    // Mock window.fetch for registries API
    global.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve({
            registries: [
              {
                id: registryId,
                name: 'Docker Proxy',
              },
            ],
          }),
      } as Response)
    )
  })

  afterEach(() => {
    vi.resetAllMocks()
  })

  test('returns null when not on registry route', () => {
    render(
      <Breadcrumbs />,
      {
        route: '/',
      }
    )

    expect(screen.queryByText('Registries')).not.toBeInTheDocument()
  })

  test('renders breadcrumbs for registry detail page', async () => {
    const route = `/registries/${registryId}`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Registries')).toBeInTheDocument()
    })

    expect(screen.getByText('Docker Proxy')).toBeInTheDocument()
  })

  test('renders breadcrumbs for artifact type page', async () => {
    const route = `/registries/${registryId}/${artifactType}`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Docker')).toBeInTheDocument()
    })

    expect(screen.getByText('Docker Proxy')).toBeInTheDocument()
    expect(screen.getByText('Registries')).toBeInTheDocument()
  })

  test('renders breadcrumbs for namespace page', async () => {
    // Use a non-docker artifact type so the "library" namespace isn't
    // collapsed out of the trail (that behavior is covered separately below).
    const route = `/registries/${registryId}/npm/${namespace}`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('library')).toBeInTheDocument()
    })

    expect(screen.getByText('Docker Proxy')).toBeInTheDocument()
  })

  test('hides library namespace for Docker artifacts', async () => {
    const route = `/registries/${registryId}/${artifactType}/library`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.queryByText('library')).not.toBeInTheDocument()
    })

    expect(screen.getByText('Docker Proxy')).toBeInTheDocument()
  })

  test('renders breadcrumbs for artifact page', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('nginx')).toBeInTheDocument()
    })

    expect(screen.getByText('Docker Proxy')).toBeInTheDocument()
  })

  test('decodes URL-encoded artifact name', async () => {
    const encodedName = encodeURIComponent('@myorg/package')
    const route = `/registries/${registryId}/${artifactType}/@myorg/${encodedName}`
    render(
      <Breadcrumbs />,
      {
        // The default exact-match route pattern chokes on the percent-encoded
        // segment here (a react-router matching quirk), so use a wildcard.
        route,
        routePath: '*',
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('@myorg/package')).toBeInTheDocument()
    })
  })

  test('decodes URL-encoded namespace', async () => {
    const encodedNs = encodeURIComponent('my/namespace')
    const route = `/registries/${registryId}/${artifactType}/${encodedNs}/artifact`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('my/namespace')).toBeInTheDocument()
    })
  })

  test('renders breadcrumbs for version page', async () => {
    const route = `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}/${version}`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('1.21.0')).toBeInTheDocument()
    })

    expect(screen.getByText('nginx')).toBeInTheDocument()
  })

  test('uses registry ID when name is not available', async () => {
    // Mock fetch returning no registries
    global.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ registries: [] }),
      } as Response)
    )

    const route = `/registries/${registryId}/${artifactType}`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('registry-1')).toBeInTheDocument()
    })
  })

  test('handles missing registry ID gracefully', async () => {
    // With no registryId segment, this URL is structurally indistinguishable
    // from a valid /registries/:registryId/:artifactType route, so it's
    // matched as registryId="docker", artifactType="namespace" rather than
    // being rejected outright.
    const route = `/registries/${artifactType}/namespace`
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    await waitFor(() => {
      expect(screen.getByText('Namespace')).toBeInTheDocument()
    })
  })

  test('does not render when matched path is null', () => {
    const route = '/unknown/route'
    render(
      <Breadcrumbs />,
      {
        route,
        history: [route],
      }
    )

    expect(screen.queryByText('Registries')).not.toBeInTheDocument()
  })
})
