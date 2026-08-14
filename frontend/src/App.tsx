import { Routes, Route } from 'react-router-dom'
import { Navbar } from './components/Navbar'
import { RegistryList } from './pages/RegistryList'
import { RegistryDetail } from './pages/RegistryDetail'
import { ArtifactList } from './pages/ArtifactList'
import { ArtifactDetail } from './pages/ArtifactDetail'
import { ArtifactVersion } from './pages/ArtifactVersion'
import { SearchResults } from './pages/SearchResults'
import { Login } from './pages/Login'
import { Admin } from './pages/Admin'
import { GettingStarted } from './pages/GettingStarted'

function App() {
  return (
    <div className="min-h-screen bg-gray-950">
      <Navbar />
      <main className="container mx-auto px-4 py-8">
        <Routes>
          <Route path="/" element={<RegistryList />} />
          <Route path="/registries" element={<RegistryList />} />
          <Route path="/registries/:registryId" element={<RegistryDetail />} />
          <Route path="/registries/:registryId/:artifactType" element={<ArtifactList />} />
          <Route path="/registries/:registryId/:artifactType/:namespace" element={<ArtifactList />} />
          <Route path="/registries/:registryId/:artifactType/:namespace/:artifactName" element={<ArtifactDetail />} />
          <Route path="/registries/:registryId/:artifactType/:namespace/:artifactName/:version" element={<ArtifactVersion />} />
          <Route path="/search" element={<SearchResults />} />
          <Route path="/login" element={<Login />} />
          <Route path="/admin" element={<Admin />} />
          <Route path="/getting-started" element={<GettingStarted />} />
        </Routes>
      </main>
    </div>
  )
}

export default App
