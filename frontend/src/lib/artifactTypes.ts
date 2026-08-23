// Per-ecosystem detail-page presentation: the pull/install command shown at
// the top (kept exactly as task #11 fixed it for the original 6 types) plus
// an optional secondary "usage" snippet for ecosystems whose real client
// needs the registry configured before a one-liner install works (e.g. a
// repo file for apt/yum, a resolver block for sbt). Paths mirror the routes
// each proxy actually registers (see backend/cmd/server/main.go).

export interface PullCommandParams {
  origin: string
  host: string
  registryHost: string | null
  namespace: string
  artifactName: string
  version: string
}

export interface ArtifactTypeConfig {
  icon: string
  pullCommand(p: PullCommandParams): string
  usage?: {
    title: string
    code(p: PullCommandParams): string
  }
}

const dockerRepo = (p: PullCommandParams) => {
  // Docker official images (namespace "library") are referenced with no
  // namespace prefix at all, e.g. `docker pull nginx:latest` — not
  // `library/nginx`.
  const isLibrary = p.namespace === 'library'
  return p.namespace && !isLibrary ? `${p.namespace}/${p.artifactName}` : p.artifactName
}

const ARTIFACT_TYPES: Record<string, ArtifactTypeConfig> = {
  docker: {
    icon: '🐳',
    pullCommand: (p) => {
      // The default registry is reached via Docker's registry-mirrors config
      // (see docs/configuration.md), so it stays bare; a bound (private)
      // registry needs its host in the reference.
      const prefix = p.registryHost ? `${p.registryHost}/` : ''
      return `docker pull ${prefix}${dockerRepo(p)}:${p.version}`
    },
  },
  oci: {
    icon: '📦',
    pullCommand: (p) => {
      const prefix = p.registryHost ? `${p.registryHost}/` : ''
      return `oras pull ${prefix}${dockerRepo(p)}:${p.version}`
    },
  },
  npm: {
    icon: '📦',
    pullCommand: (p) => `npm install ${p.artifactName}@${p.version} --registry ${p.origin}/npm/`,
  },
  pypi: {
    icon: '🐍',
    pullCommand: (p) => `pip install ${p.artifactName}==${p.version} --index-url ${p.origin}/pypi/simple/`,
  },
  helm: {
    icon: '⚓',
    pullCommand: (p) => `helm pull ${p.origin}/helm/charts/${p.artifactName}-${p.version}.tgz`,
  },
  nuget: {
    icon: '🔨',
    pullCommand: (p) =>
      `dotnet add package ${p.artifactName} --version ${p.version} --source ${p.origin}/nuget/v3/index.json`,
  },
  maven: {
    icon: '☕',
    // maven.go's {group} route only matches a single flat path segment, so a
    // real mvn/gradle groupId with dots would 404 — use a direct download.
    pullCommand: (p) =>
      `curl -O ${p.origin}/maven/${p.namespace}/${p.artifactName}/${p.version}/${p.artifactName}-${p.version}.jar`,
    usage: {
      title: 'Add to pom.xml',
      code: (p) =>
        `<dependency>\n  <groupId>${p.namespace}</groupId>\n  <artifactId>${p.artifactName}</artifactId>\n  <version>${p.version}</version>\n</dependency>`,
    },
  },
  gradle: {
    icon: '☕',
    pullCommand: (p) =>
      `curl -O ${p.origin}/gradle/${p.namespace}/${p.artifactName}/${p.version}/${p.artifactName}-${p.version}.jar`,
    usage: {
      title: 'Add to build.gradle',
      code: (p) =>
        `repositories {\n    maven { url "${p.origin}/gradle/" }\n}\ndependencies {\n    implementation '${p.namespace}:${p.artifactName}:${p.version}'\n}`,
    },
  },
  sbt: {
    icon: '☕',
    pullCommand: (p) =>
      `curl -O ${p.origin}/sbt/${p.namespace}/${p.artifactName}/${p.version}/${p.artifactName}-${p.version}.jar`,
    usage: {
      title: 'Add to build.sbt',
      code: (p) =>
        `resolvers += "cargobay" at "${p.origin}/sbt/"\nlibraryDependencies += "${p.namespace}" % "${p.artifactName}" % "${p.version}"`,
    },
  },
  cargo: {
    icon: '⚙️',
    pullCommand: (p) => `cargo add ${p.artifactName}@${p.version} --registry cargobay`,
    usage: {
      title: 'Configure the registry first (~/.cargo/config.toml)',
      code: (p) => `[registries.cargobay]\nindex = "sparse+${p.origin}/cargo/"`,
    },
  },
  go: {
    icon: '🐹',
    pullCommand: (p) => {
      const mod = p.namespace ? `${p.namespace}/${p.artifactName}` : p.artifactName
      return `GOPROXY=${p.origin}/go,direct go get ${mod}@${p.version}`
    },
  },
  alpine: {
    icon: '🏔️',
    pullCommand: (p) => `apk add --repository ${p.origin}/alpine/x86_64 ${p.artifactName}=${p.version}`,
    usage: {
      title: 'Or add the repo permanently',
      code: (p) => `echo "${p.origin}/alpine/x86_64" >> /etc/apk/repositories`,
    },
  },
  debian: {
    icon: '🌀',
    pullCommand: (p) => `apt-get install ${p.artifactName}=${p.version}`,
    usage: {
      title: 'Add the repo first',
      code: (p) =>
        `echo "deb [trusted=yes] ${p.origin}/debian stable main" | sudo tee /etc/apt/sources.list.d/cargobay.list\napt-get update`,
    },
  },
  rpm: {
    icon: '🎩',
    pullCommand: (p) => `yum install ${p.artifactName}-${p.version}`,
    usage: {
      title: 'Add the repo first (/etc/yum.repos.d/cargobay.repo)',
      code: (p) => `[cargobay]\nname=cargobay\nbaseurl=${p.origin}/rpm/x86_64/\nenabled=1\ngpgcheck=0`,
    },
  },
  yum: {
    icon: '🎩',
    pullCommand: (p) => `yum install ${p.artifactName}-${p.version}`,
    usage: {
      title: 'Add the repo first (/etc/yum.repos.d/cargobay.repo)',
      code: (p) => `[cargobay]\nname=cargobay\nbaseurl=${p.origin}/yum/x86_64/\nenabled=1\ngpgcheck=0`,
    },
  },
  conan: {
    icon: '🔧',
    pullCommand: (p) => `conan install ${p.artifactName}/${p.version}@_/_ -r cargobay`,
    usage: {
      title: 'Add the remote first',
      code: (p) => `conan remote add cargobay ${p.origin}/conan`,
    },
  },
  cocoapods: {
    icon: '🍫',
    pullCommand: (p) => `pod '${p.artifactName}', '${p.version}'`,
    usage: {
      title: 'Add the source to your Podfile',
      code: (p) => `source '${p.origin}/cocoapods'\npod '${p.artifactName}', '${p.version}'`,
    },
  },
  swift: {
    icon: '🐦',
    pullCommand: (p) => `swift package-registry set ${p.origin}/swift`,
    usage: {
      title: 'Add to Package.swift',
      code: (p) => `dependencies: [\n    .package(id: "${p.namespace}.${p.artifactName}", from: "${p.version}")\n]`,
    },
  },
  dart: {
    icon: '🎯',
    pullCommand: (p) => `PUB_HOSTED_URL=${p.origin}/dart dart pub add ${p.artifactName}:${p.version}`,
    usage: {
      title: 'Or add to pubspec.yaml',
      code: (p) => `dependencies:\n  ${p.artifactName}: ${p.version}`,
    },
  },
  terraform: {
    icon: '🟪',
    pullCommand: (p) => `${p.host}/${p.namespace}/${p.artifactName}/<system>`,
    usage: {
      title: 'Use in a module block',
      code: (p) =>
        `module "${p.artifactName}" {\n  source  = "${p.host}/${p.namespace}/${p.artifactName}/<system>"\n  version = "${p.version}"\n}`,
    },
  },
  composer: {
    icon: '🐘',
    pullCommand: (p) => `composer require ${p.namespace}/${p.artifactName}:${p.version}`,
    usage: {
      title: 'Add the repository to composer.json',
      code: (p) => `"repositories": [\n  { "type": "composer", "url": "${p.origin}/composer" }\n]`,
    },
  },
  conda: {
    icon: '🐍',
    pullCommand: (p) =>
      `conda install -c ${p.origin}/conda/${p.namespace || 'main'} ${p.artifactName}=${p.version}`,
  },
}

const DEFAULT_TYPE: ArtifactTypeConfig = {
  icon: '📦',
  pullCommand: (p) => `${p.namespace}:${p.artifactName}:${p.version}`,
}

export function getArtifactTypeConfig(artifactType: string | undefined): ArtifactTypeConfig {
  return (artifactType && ARTIFACT_TYPES[artifactType]) || DEFAULT_TYPE
}

export function isContainerType(artifactType: string | undefined): boolean {
  return artifactType === 'docker' || artifactType === 'oci'
}

// Docker's official images live under the "library" namespace, but Docker
// Hub itself never shows it (hub.docker.com/_/nginx, `docker pull nginx`) —
// mirror that by collapsing it out of any namespace/name display.
export function isHiddenDockerLibraryNamespace(artifactType?: string, namespace?: string): boolean {
  return artifactType === 'docker' && namespace === 'library'
}
