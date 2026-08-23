import { useEffect, useState } from 'react'
import { Link, matchPath, useLocation } from 'react-router-dom'
import { isHiddenDockerLibraryNamespace } from '../lib/artifactTypes'

interface RegistrySummary {
  id: string
  name: string
}

const REGISTRY_ROUTE_PATTERNS = [
  '/registries/:registryId/:artifactType/:namespace/:artifactName/:version',
  '/registries/:registryId/:artifactType/:namespace/:artifactName',
  '/registries/:registryId/:artifactType/:namespace',
  '/registries/:registryId/:artifactType',
  '/registries/:registryId',
]

interface Crumb {
  label: string
  to?: string
}

export function Breadcrumbs() {
  const location = useLocation()
  const [registryNames, setRegistryNames] = useState<Record<string, string>>({})

  const matched = REGISTRY_ROUTE_PATTERNS.map((pattern) => matchPath(pattern, location.pathname)).find(Boolean)

  useEffect(() => {
    if (!matched) return
    fetch('/api/v1/registries')
      .then((res) => (res.ok ? res.json() : { registries: [] }))
      .then((data) => {
        const names: Record<string, string> = {}
        for (const reg of (data.registries || []) as RegistrySummary[]) {
          names[reg.id] = reg.name
        }
        setRegistryNames(names)
      })
      .catch(() => {})
    // Only needs to run once per navigation into the registries subtree.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!matched])

  if (!matched) return null

  const { registryId, artifactType, namespace, artifactName, version } = matched.params as Record<
    string,
    string | undefined
  >

  const crumbs: Crumb[] = [{ label: 'Registries', to: '/' }]

  if (registryId) {
    crumbs.push({
      label: registryNames[registryId] || registryId,
      to: `/registries/${registryId}`,
    })
  }
  if (artifactType) {
    crumbs.push({
      label: artifactType.charAt(0).toUpperCase() + artifactType.slice(1),
      to: `/registries/${registryId}/${artifactType}`,
    })
  }
  if (namespace && !isHiddenDockerLibraryNamespace(artifactType, namespace)) {
    crumbs.push({
      label: decodeURIComponent(namespace),
      to: `/registries/${registryId}/${artifactType}/${namespace}`,
    })
  }
  if (artifactName) {
    crumbs.push({
      label: decodeURIComponent(artifactName),
      to: `/registries/${registryId}/${artifactType}/${namespace}/${artifactName}`,
    })
  }
  if (version) {
    crumbs.push({ label: decodeURIComponent(version) })
  }

  return (
    <nav className="flex items-center flex-wrap gap-1.5 text-sm mb-6 text-gray-500" aria-label="Breadcrumb">
      {crumbs.map((crumb, idx) => {
        const isLast = idx === crumbs.length - 1
        return (
          <span key={idx} className="flex items-center gap-1.5">
            {idx > 0 && <span className="text-gray-700">/</span>}
            {crumb.to && !isLast ? (
              <Link to={crumb.to} className="hover:text-gray-200 transition-colors">
                {crumb.label}
              </Link>
            ) : (
              <span className={isLast ? 'text-gray-200 font-medium' : ''}>{crumb.label}</span>
            )}
          </span>
        )
      })}
    </nav>
  )
}
