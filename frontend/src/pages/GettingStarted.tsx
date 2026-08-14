import { ReactNode } from 'react'

function CodeBlock({ children }: { children: string }) {
  return (
    <pre className="bg-gray-800 rounded-lg p-4 font-mono text-xs text-gray-300 overflow-x-auto">
      <code>{children}</code>
    </pre>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="card space-y-4">
      <h2 className="text-xl font-semibold text-gray-100 border-b border-gray-800 pb-2">{title}</h2>
      {children}
    </div>
  )
}

export function GettingStarted() {
  return (
    <div className="space-y-8 max-w-4xl mx-auto">
      <div>
        <h1 className="text-3xl font-bold text-white mb-2">Beginner's Guide</h1>
        <p className="text-gray-400">
          Point your IDE and build tools at cargobay so package installs and pulls go through the proxy and cache.
        </p>
      </div>

      <Section title="npm">
        <p className="text-sm text-gray-400">Configure npm to install packages through cargobay:</p>
        <CodeBlock>{`# Configure npm to use cargobay
npm config set registry http://cargobay:4500/npm/

# Or in .npmrc
registry=http://cargobay:4500/npm/`}</CodeBlock>
        <p className="text-sm text-gray-500">
          In VS Code, this is picked up automatically once <code className="text-gray-400">.npmrc</code> is set — no extra IDE config needed.
        </p>
      </Section>

      <Section title="Gradle">
        <p className="text-sm text-gray-400">Add cargobay as a repository in your <code className="text-gray-400">build.gradle</code>:</p>
        <CodeBlock>{`repositories {
    maven {
        url 'http://cargobay:4500/maven/'
    }
}`}</CodeBlock>
        <p className="text-sm text-gray-500">
          In IntelliJ IDEA: open the Gradle tool window and click "Reload All Gradle Projects" after adding the repository so the IDE re-resolves dependencies through cargobay.
        </p>
      </Section>

      <Section title="Maven">
        <p className="text-sm text-gray-400">Mirror all repositories through cargobay in <code className="text-gray-400">settings.xml</code> (usually <code className="text-gray-400">~/.m2/settings.xml</code>):</p>
        <CodeBlock>{`<settings>
  <mirrors>
    <mirror>
      <id>cargobay</id>
      <url>http://cargobay:4500/maven/</url>
      <mirrorOf>*</mirrorOf>
    </mirror>
  </mirrors>
</settings>`}</CodeBlock>
        <p className="text-sm text-gray-500">
          In IntelliJ IDEA: Settings → Build Tools → Maven → confirm "User settings file" points at the <code className="text-gray-400">settings.xml</code> you edited, then re-import the Maven project.
        </p>
      </Section>

      <Section title="Docker">
        <p className="text-sm text-gray-400">Pull images through cargobay, or set it as a registry mirror:</p>
        <CodeBlock>{`# Pull through cargobay
docker pull cargobay:4500/library/nginx:latest

# Or configure as a registry mirror in /etc/docker/daemon.json
{
  "registry-mirrors": ["http://cargobay:4500/"]
}`}</CodeBlock>
        <p className="text-sm text-gray-500">Restart Docker Desktop (or the daemon) after editing <code className="text-gray-400">daemon.json</code> for the mirror to take effect.</p>
      </Section>

      <Section title="Next steps">
        <ul className="list-disc list-inside text-sm text-gray-400 space-y-1">
          <li>Browse available registries from the <a href="/registries" className="text-blue-400 hover:text-blue-300">Registries</a> page.</li>
          <li>Use the search bar to find a specific package or image.</li>
          <li>Log in from the navbar to upload and manage your own artifacts.</li>
        </ul>
      </Section>
    </div>
  )
}
