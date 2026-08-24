// Shared registry icon lookup, used by the home page's "Browse Registry"
// grid and the registry detail page so both stay in sync.

export interface RegistrySelector {
  id: string
  type: string
}

// Emoji fallback per registry type — the icons used across the app before
// real provider logos were added, kept as the fallback tier so an unknown
// or custom registry (or a failed logo image load) still gets a
// recognizable icon rather than a bare letter.
export const REGISTRY_EMOJI: Record<string, string> = {
  docker: '🐳',
  npm: '📦',
  maven: '☕',
  pypi: '🐍',
  nuget: '🔨',
  helm: '⚓',
}

// Real provider logos for known registries, matched by id first (so e.g. a
// 'ghcr' registry gets the GitHub mark rather than the generic Docker
// whale) and falling back to registry type. Returns null for registries
// that don't match either (custom/self-hosted upstreams) — callers should
// fall back to REGISTRY_EMOJI (and then a letter avatar) in that case.
export function getRegistryLogo(reg: RegistrySelector): string | null {
  switch (reg.id) {
    case 'ghcr':
      return '/registries/github.svg'
    case 'quay':
      return '/registries/redhat.svg'
  }
  switch (reg.type) {
    case 'docker':
      return '/registries/docker.svg'
    case 'npm':
      return '/registries/npm.svg'
    case 'maven':
      return '/registries/maven.svg'
    case 'pypi':
      return '/registries/pypi.svg'
    case 'nuget':
      return '/registries/nuget.svg'
    default:
      return null
  }
}
