import { useState, useRef, ChangeEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

interface ArtifactFormData {
  registryId: string
  artifactType: string
  namespace: string
  artifactName: string
  version: string
  tags: string[]
  description: string
}

export function ArtifactUpload() {
  const { currentUser, isAdmin } = useAuth()
  const navigate = useNavigate()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [isDragging, setIsDragging] = useState(false)
  const [isUploading, setIsUploading] = useState(false)
  const [progress, setProgress] = useState(0)
  const [formData, setFormData] = useState<ArtifactFormData>({
    registryId: 'dockerhub',
    artifactType: 'docker',
    namespace: '',
    artifactName: '',
    version: '',
    tags: [],
    description: '',
  })
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  const artifactTypes = [
    { value: 'docker', label: 'Docker/OCI' },
    { value: 'npm', label: 'npm' },
    { value: 'maven', label: 'Maven' },
    { value: 'pypi', label: 'PyPI' },
    { value: 'nuget', label: 'NuGet' },
    { value: 'helm', label: 'Helm' },
    { value: 'generic', label: 'Generic' },
  ]

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = () => {
    setIsDragging(false)
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    const files = e.dataTransfer.files
    if (files.length > 0) {
      setSelectedFile(files[0])
      setFormData((prev) => ({
        ...prev,
        artifactName: prev.artifactName || extractArtifactName(files[0].name),
        version: prev.version || extractVersion(files[0].name),
      }))
    }
  }

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (files && files.length > 0) {
      setSelectedFile(files[0])
      setFormData((prev) => ({
        ...prev,
        artifactName: prev.artifactName || extractArtifactName(files[0].name),
        version: prev.version || extractVersion(files[0].name),
      }))
    }
  }

  const extractArtifactName = (filename: string): string => {
    const name = filename.split('.')[0]
    if (name.includes('-')) {
      return name.split('-').slice(0, -1).join('-')
    }
    return name
  }

  const extractVersion = (filename: string): string => {
    const name = filename.split('.')[0]
    if (name.includes('-')) {
      return name.split('-').pop() || '1.0.0'
    }
    return '1.0.0'
  }

  const handleInputChange = (e: ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
    const { name, value } = e.target
    setFormData((prev) => ({ ...prev, [name]: value }))
  }

  const handleTagChange = (e: ChangeEvent<HTMLInputElement>) => {
    const tags = e.target.value.split(',').map(t => t.trim()).filter(t => t)
    setFormData((prev) => ({ ...prev, tags }))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setSuccess(false)
    setIsUploading(true)
    setProgress(0)

    if (!selectedFile) {
      setError('Please select a file to upload')
      setIsUploading(false)
      return
    }

    if (!formData.artifactName || !formData.version) {
      setError('Please provide artifact name and version')
      setIsUploading(false)
      return
    }

    try {
      const formDataObj = new FormData()
      formDataObj.append('file', selectedFile)
      formDataObj.append('registry_id', formData.registryId)
      formDataObj.append('artifact_type', formData.artifactType)
      formDataObj.append('namespace', formData.namespace)
      formDataObj.append('artifact_name', formData.artifactName)
      formDataObj.append('version', formData.version)
      formDataObj.append('description', formData.description)
      formDataObj.append('tags', JSON.stringify(formData.tags))

      // Simulate upload progress
      for (let i = 0; i <= 100; i += 5) {
        await new Promise(resolve => setTimeout(resolve, 50))
        setProgress(i)
      }

      // In production, this would call the actual API
      // await fetch('/api/v1/artifacts', {
      //   method: 'POST',
      //   body: formDataObj,
      // })

      setSuccess(true)
      setTimeout(() => {
        navigate(`/registries/${formData.registryId}/${formData.artifactType}/${formData.namespace}/${formData.artifactName}/${formData.version}`)
      }, 2000)
    } catch (err) {
      setError('Failed to upload artifact. Please try again.')
      setIsUploading(false)
    }
  }

  if (!currentUser) {
    return (
      <div className="max-w-4xl mx-auto">
        <div className="card text-center py-12">
          <h2 className="text-2xl font-bold text-white mb-4">Please Log In</h2>
          <p className="text-gray-400 mb-6">You need to be logged in to upload artifacts.</p>
          <button
            onClick={() => navigate('/login')}
            className="btn btn-primary"
          >
            Log In
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="max-w-4xl mx-auto">
      <div className="mb-8">
        <h1 className="text-3xl font-bold text-white mb-2">Upload Artifact</h1>
        <p className="text-gray-400">Upload a new artifact to the registry</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* Form Section */}
        <div className="lg:col-span-2 space-y-6">
          <div className="card p-6">
            <form onSubmit={handleSubmit} className="space-y-6">
              {/* File Upload */}
              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Artifact File</label>
                <div
                  onDragOver={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop}
                  className={`border-2 border-dashed rounded-xl p-8 text-center transition-all ${
                    isDragging
                      ? 'border-blue-500 bg-blue-500/10'
                      : 'border-gray-700 hover:border-gray-600'
                  }`}
                >
                  <input
                    ref={fileInputRef}
                    type="file"
                    onChange={handleFileChange}
                    className="hidden"
                  />
                  {selectedFile ? (
                    <div className="space-y-2">
                      <div className="w-16 h-16 bg-green-500/20 text-green-400 rounded-full flex items-center justify-center mx-auto">
                        <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                        </svg>
                      </div>
                      <p className="text-white font-medium">{selectedFile.name}</p>
                      <p className="text-sm text-gray-400">{(selectedFile.size / 1024).toFixed(2)} KB</p>
                      <button
                        type="button"
                        onClick={() => {
                          setSelectedFile(null)
                          setFormData((prev) => ({ ...prev, artifactName: '', version: '' }))
                        }}
                        className="text-sm text-red-400 hover:text-red-300"
                      >
                        Remove file
                      </button>
                    </div>
                  ) : (
                    <div
                      onClick={() => fileInputRef.current?.click()}
                      className="cursor-pointer space-y-2"
                    >
                      <div className="w-16 h-16 bg-gray-700 text-gray-400 rounded-full flex items-center justify-center mx-auto">
                        <svg className="w-8 h-8" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
                        </svg>
                      </div>
                      <p className="text-white font-medium">Click to select or drag & drop</p>
                      <p className="text-sm text-gray-400">Supported formats: .tar.gz, .whl, .nupkg, .jar, .npm, .tgz, .helm</p>
                    </div>
                  )}
                </div>
              </div>

              {/* Progress Bar */}
              {isUploading && (
                <div>
                  <div className="flex justify-between text-sm mb-1">
                    <span className="text-gray-300">Uploading...</span>
                    <span className="text-blue-400">{progress}%</span>
                  </div>
                  <div className="w-full bg-gray-700 rounded-full h-2">
                    <div
                      className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                      style={{ width: `${progress}%` }}
                    ></div>
                  </div>
                </div>
              )}

              {/* Error Message */}
              {error && (
                <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg">
                  <p className="text-red-400 text-sm">{error}</p>
                </div>
              )}

              {/* Success Message */}
              {success && (
                <div className="p-3 bg-green-500/10 border border-green-500/20 rounded-lg">
                  <p className="text-green-400 text-sm">Artifact uploaded successfully! Redirecting...</p>
                </div>
              )}

              {/* Form Fields */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Registry</label>
                  <select
                    name="registryId"
                    value={formData.registryId}
                    onChange={handleInputChange}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  >
                    <option value="dockerhub">Docker Hub</option>
                    <option value="npm">NPM</option>
                    <option value="maven-central">Maven Central</option>
                    <option value="pypi">PyPI</option>
                  </select>
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Artifact Type</label>
                  <select
                    name="artifactType"
                    value={formData.artifactType}
                    onChange={handleInputChange}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  >
                    {artifactTypes.map((type) => (
                      <option key={type.value} value={type.value}>
                        {type.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Namespace</label>
                  <input
                    type="text"
                    name="namespace"
                    value={formData.namespace}
                    onChange={handleInputChange}
                    placeholder="e.g., library, official"
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-300 mb-2">Version</label>
                  <input
                    type="text"
                    name="version"
                    value={formData.version}
                    onChange={handleInputChange}
                    placeholder="e.g., 1.0.0, latest"
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Tags (comma-separated)</label>
                <input
                  type="text"
                  onChange={handleTagChange}
                  placeholder="e.g., latest, stable, v1.0.0"
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                />
                {formData.tags.length > 0 && (
                  <div className="flex flex-wrap gap-2 mt-2">
                    {formData.tags.map((tag, index) => (
                      <span key={index} className="px-2 py-1 bg-blue-500/20 text-blue-300 rounded text-sm">
                        {tag}
                      </span>
                    ))}
                  </div>
                )}
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-300 mb-2">Description</label>
                <textarea
                  name="description"
                  value={formData.description}
                  onChange={handleInputChange}
                  rows={3}
                  placeholder="Describe this artifact..."
                  className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2 text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                ></textarea>
              </div>

              <div className="flex gap-3">
                <button
                  type="submit"
                  disabled={isUploading || !selectedFile}
                  className="flex-1 bg-blue-600 hover:bg-blue-700 text-white font-medium py-3 px-6 rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isUploading ? 'Uploading...' : 'Upload Artifact'}
                </button>
                <button
                  type="button"
                  onClick={() => navigate(-1)}
                  className="px-6 py-3 border border-gray-700 text-gray-300 hover:bg-gray-800 rounded-lg transition-colors"
                >
                  Cancel
                </button>
              </div>
            </form>
          </div>
        </div>

        {/* Info Panel */}
        <div className="space-y-6">
          <div className="card p-6">
            <h3 className="text-lg font-semibold text-white mb-4">Supported Formats</h3>
            <ul className="space-y-2 text-sm text-gray-400">
              <li className="flex items-center gap-2">
                <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                Docker: .tar.gz, .tar
              </li>
              <li className="flex items-center gap-2">
                <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                npm: .tgz, .tar.gz
              </li>
              <li className="flex items-center gap-2">
                <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                Maven: .jar, .war, .pom
              </li>
              <li className="flex items-center gap-2">
                <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                PyPI: .whl, .tar.gz, .zip
              </li>
              <li className="flex items-center gap-2">
                <span className="w-2 h-2 bg-orange-500 rounded-full"></span>
                NuGet: .nupkg
              </li>
              <li className="flex items-center gap-2">
                <span className="w-2 h-2 bg-pink-500 rounded-full"></span>
                Helm: .tgz, .helm
              </li>
            </ul>
          </div>

          <div className="card p-6">
            <h3 className="text-lg font-semibold text-white mb-4">Quick Tips</h3>
            <ul className="space-y-2 text-sm text-gray-400">
              <li>• Artifact names should use lowercase letters and hyphens</li>
              <li>• Use semantic versioning (e.g., 1.0.0, 2.1.3)</li>
              <li>• Add relevant tags for easier discovery</li>
              <li>• Include a description for better documentation</li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  )
}
