import { MOCK_USERS, useAuth } from '../context/AuthContext'

const MOCK_ARTIFACTS = [
  { registryId: 'dockerhub', artifactType: 'docker', name: 'library/nginx', version: 'latest', uploadedBy: 'alice' },
  { registryId: 'dockerhub', artifactType: 'docker', name: 'library/nginx', version: '1.24.0', uploadedBy: 'bob' },
  { registryId: 'maven-central', artifactType: 'maven', name: 'com.google.guava/guava', version: '3.12.0', uploadedBy: 'alice' },
  { registryId: 'npm', artifactType: 'npm', name: 'react', version: '18.19.0', uploadedBy: 'alice' },
  { registryId: 'npm', artifactType: 'npm', name: 'lodash', version: '18.18.0', uploadedBy: 'bob' },
]

export function Admin() {
  const { isAdmin } = useAuth()

  if (!isAdmin) {
    return (
      <div className="card max-w-md mx-auto text-center py-12">
        <h1 className="text-xl font-semibold text-white mb-2">Access denied</h1>
        <p className="text-gray-400">You need admin privileges to view this page.</p>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-3xl font-bold text-white mb-2">Admin</h1>
        <p className="text-gray-400">Manage users and artifacts</p>
      </div>

      <div className="card">
        <h2 className="text-xl font-semibold text-gray-100 mb-4">Users</h2>
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead className="bg-gray-800/50 text-gray-400 text-sm">
              <tr>
                <th className="px-4 py-3 rounded-l-lg font-medium">Username</th>
                <th className="px-4 py-3 font-medium">Email</th>
                <th className="px-4 py-3 rounded-r-lg font-medium">Role</th>
              </tr>
            </thead>
            <tbody className="text-sm">
              {MOCK_USERS.map((user) => (
                <tr key={user.id} className="border-b border-gray-800">
                  <td className="px-4 py-3 font-medium text-gray-100">{user.username}</td>
                  <td className="px-4 py-3 text-gray-400">{user.email}</td>
                  <td className="px-4 py-3">
                    <span className={`badge ${user.role === 'admin' ? 'badge-warning' : 'badge-info'}`}>
                      {user.role}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="card">
        <h2 className="text-xl font-semibold text-gray-100 mb-4">Artifacts</h2>
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead className="bg-gray-800/50 text-gray-400 text-sm">
              <tr>
                <th className="px-4 py-3 rounded-l-lg font-medium">Artifact</th>
                <th className="px-4 py-3 font-medium">Registry</th>
                <th className="px-4 py-3 font-medium">Version</th>
                <th className="px-4 py-3 rounded-r-lg font-medium">Uploaded By</th>
              </tr>
            </thead>
            <tbody className="text-sm">
              {MOCK_ARTIFACTS.map((artifact, idx) => (
                <tr key={idx} className="border-b border-gray-800">
                  <td className="px-4 py-3 font-mono text-blue-400">{artifact.name}</td>
                  <td className="px-4 py-3 text-gray-400">{artifact.registryId}</td>
                  <td className="px-4 py-3 text-gray-300">{artifact.version}</td>
                  <td className="px-4 py-3 text-gray-300">{artifact.uploadedBy}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}
