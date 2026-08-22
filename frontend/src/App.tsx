import { useEffect, useState } from 'react'
import { Routes, Route, useNavigate, useLocation } from 'react-router-dom'
import { Navbar } from './components/Navbar'
import { Breadcrumbs } from './components/Breadcrumbs'
import { RegistryList } from './pages/RegistryList'
import { RegistryDetail } from './pages/RegistryDetail'
import { ArtifactList } from './pages/ArtifactList'
import { ArtifactDetail } from './pages/ArtifactDetail'
import { ArtifactVersion } from './pages/ArtifactVersion'
import { SearchResults } from './pages/SearchResults'
import { Login } from './pages/Login'
import { Settings } from './pages/Settings'
import { UserEdit } from './pages/UserEdit'
import { GettingStarted } from './pages/GettingStarted'
import { ArtifactUpload } from './pages/ArtifactUpload'
import { ArtifactBrowse } from './pages/ArtifactBrowse'
import { VulnerabilityResults } from './pages/VulnerabilityResults'
import { Profile } from './pages/Profile'
import { Setup } from './pages/Setup'
import { Stats } from './pages/Stats'

function App() {
  const navigate = useNavigate()
  const location = useLocation()
  const [checkingSetup, setCheckingSetup] = useState(true)

  useEffect(() => {
    fetch('/api/v1/setup/status')
      .then((res) => (res.ok ? res.json() : { needsSetup: false }))
      .then((data) => {
        if (data.needsSetup && location.pathname !== '/setup') {
          navigate('/setup')
        } else if (!data.needsSetup && location.pathname === '/setup') {
          navigate('/')
        }
      })
      .catch(() => {})
      .finally(() => setCheckingSetup(false))
    // Only run once on initial load - setup status doesn't change mid-session.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (checkingSetup) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-950">
      <Navbar />
      <main className="container mx-auto px-4 py-8">
        <Breadcrumbs />
        <Routes>
          <Route path="/setup" element={<Setup />} />
          <Route path="/" element={<RegistryList />} />
          <Route path="/registries" element={<RegistryList />} />
          <Route path="/registries/:registryId" element={<RegistryDetail />} />
          <Route path="/registries/:registryId/:artifactType" element={<ArtifactList />} />
          <Route path="/registries/:registryId/:artifactType/:namespace" element={<ArtifactList />} />
          <Route path="/registries/:registryId/:artifactType/:namespace/:artifactName" element={<ArtifactDetail />} />
          <Route path="/registries/:registryId/:artifactType/:namespace/:artifactName/:version" element={<ArtifactVersion />} />
          <Route path="/search" element={<SearchResults />} />
          <Route path="/login" element={<Login />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="/settings/users/:userId" element={<UserEdit />} />
          <Route path="/getting-started" element={<GettingStarted />} />
          <Route path="/upload" element={<ArtifactUpload />} />
          <Route path="/browse" element={<ArtifactBrowse />} />
          <Route path="/browse/:registryId" element={<ArtifactBrowse />} />
          <Route path="/browse/:registryId/:artifactType" element={<ArtifactBrowse />} />
          <Route path="/browse/:registryId/:artifactType/:namespace" element={<ArtifactBrowse />} />
          <Route path="/vulnerabilities" element={<VulnerabilityResults />} />
          <Route path="/vulnerabilities/:artifactId" element={<VulnerabilityResults />} />
          <Route path="/stats" element={<Stats />} />
          <Route path="/profile" element={<Profile />} />
        </Routes>
      </main>
    </div>
  )
}

export default App
