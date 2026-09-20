# Contributing Guide

Thank you for your interest in contributing to cargobay! This guide will help you get started.

## Getting Started

### Prerequisites

- Go 1.21+
- Node.js 20+
- Docker and Docker Compose
- git

### Development Setup

1. **Fork and clone the repository**

```bash
git clone https://github.com/your-org/cargobay.git
cd cargobay
```

2. **Start development environment**

```bash
# Start database and redis
docker compose -f docker-compose.dev.yml up -d

# Install backend dependencies
cd backend
go mod tidy

# Install frontend dependencies
cd ../frontend
npm install
```

3. **Run development servers**

```bash
# Backend (in backend directory)
go run cmd/server/main.go

# Frontend (in frontend directory)
npm run dev
```

The UI will be available at `http://localhost:3000`

## Project Structure

```
cargobay/
├── backend/               # Go backend
│   ├── cmd/              # Command-line tools
│   │   ├── server/       # Main server
│   │   └── cli/          # CLI tool
│   ├── pkg/              # Core packages
│   │   ├── api/          # API handlers
│   │   ├── proxy/        # Registry proxies
│   │   ├── database/     # Database layer
│   │   ├── storage/      # Storage adapters
│   │   ├── cache/        # Cache layer
│   │   ├── search/       # Search service
│   │   ├── vulnerability/# Scanner
│   │   └── rbac/         # Access control
│   └── migrations/       # Database migrations
├── frontend/             # React frontend
│   ├── src/
│   │   ├── components/   # React components
│   │   ├── pages/        # Page components
│   │   ├── context/      # React context
│   │   └── utils/        # Utility functions
├── charts/               # Helm charts
├── docs/                 # Documentation
└── tests/                # Test suite
```

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/issue-number
```

### 2. Make Changes

- Follow existing code style
- Add tests for new functionality
- Update documentation as needed

### 3. Run Tests

```bash
# Backend tests
cd backend
go test ./...

# Frontend tests
cd ../frontend
npm test

# Linting
cd ../backend
go vet ./...
cd ../frontend
npm run lint
```

### 4. Commit Your Changes

```bash
git add .
git commit -m "feat: add new feature"

# Use conventional commits:
# feat: new feature
# fix: bug fix
# docs: documentation
# style: formatting
# refactor: code refactoring
# test: tests
# chore: maintenance
```

### 5. Push and Open PR

```bash
git push origin feature/your-feature-name
```

Open a Pull Request on GitHub.

## Code Style

### Go

- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Use `go fmt` and `go vet`
- Import packages in groups (standard library, external, internal)
- Use meaningful variable names
- Comment exported functions

```go
// Example
import (
    "fmt"
    "net/http"
    
    "cargobay/pkg/database"
    "cargobay/pkg/models"
)
```

### TypeScript/React

- Follow [React best practices](https://react.dev/learn)
- Use TypeScript for type safety
- Use functional components with hooks
- Follow existing component structure

```typescript
// Example component
import { useState, useEffect } from 'react';

interface Props {
  name: string;
  onSave?: (value: string) => void;
}

export function MyComponent({ name, onSave }: Props) {
  const [value, setValue] = useState(name);
  
  useEffect(() => {
    setValue(name);
  }, [name]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setValue(e.target.value);
  };

  return (
    <div>
      <input
        type="text"
        value={value}
        onChange={handleChange}
        onBlur={() => onSave?.(value)}
      />
    </div>
  );
}
```

## Testing

### Backend Testing

```go
// Example test
func TestArtifactUpload(t *testing.T) {
    // Setup
    db := testDB(t)
    handler := NewArtifactHandler(db)
    
    // Test
    req := createTestRequest()
    rr := httptest.NewRecorder()
    
    handler.Upload(rr, req)
    
    // Assert
    assert.Equal(t, http.StatusCreated, rr.Code)
}
```

#### Testing code that shells out or hits Postgres directly

Some backend components (e.g. `pkg/vulnerability/DBUpdater`, which shells
out to the `trivy` CLI and writes results via `*database.Database`) depend
on things a unit test shouldn't require: a live Postgres connection, or the
real external binary. The established pattern, illustrated by
`pkg/vulnerability/updater_test.go`:

- Depend on a **narrow interface** (e.g. `settingsStore`, covering just the
  two `*database.Database` methods actually used) instead of the concrete
  type, so a lightweight in-memory fake can be substituted in tests.
- Make the external binary's path/name a **struct field with a sane
  default** (e.g. `trivyBin string`, defaulting to `"trivy"`), so tests can
  point it at a small stub shell script and exercise both success and
  failure exit codes without the real tool installed.
- Extract any pure decision logic (e.g. the "is this update due yet"
  check) into a **standalone function** taking plain values (no clock or DB
  access of its own), so it can be tested directly with table-driven time
  scenarios.

```go
// updater_test.go — fake replacing the *database.Database dependency
type fakeSettingsStore struct{ /* ... */ }
func (f *fakeSettingsStore) GetVulnDBSettings() (*database.VulnDBSettings, error)      { /* ... */ }
func (f *fakeSettingsStore) RecordVulnDBUpdateResult(t time.Time, ok bool, msg string) error { /* ... */ }

updater := &DBUpdater{db: store, cacheDir: t.TempDir(), trivyBin: writeStubScript(t, 1, "boom")}
err := updater.RunUpdate(context.Background())
// asserts the failure path was recorded, without a real trivy binary or DB
```

Run just that package's tests while iterating: `go test ./pkg/vulnerability/... -v`.

### Frontend Testing

```typescript
// Example test
describe('ArtifactUpload', () => {
  it('should handle file selection', () => {
    render(<ArtifactUpload />);
    const input = screen.getByTestId('file-input');
    
    fireEvent.change(input, { target: { files: [file] } });
    
    expect(screen.getByText('test.txt')).toBeInTheDocument();
  });
});
```

## Pull Request Guidelines

1. **Title**: Use conventional commit format
   - `feat: add new feature`
   - `fix: resolve bug in proxy`
   - `docs: update configuration guide`

2. **Testing**: Tests are disabled on push — run them locally before submitting.

   ```bash
   # All tests locally
   make test

   # Backend tests
   make test-backend

   # Frontend tests
   make test-frontend

   # Via GitHub Actions (requires Docker)
   act workflow_dispatch -j test
   ```

2. **Description**: Include:
   - What changes were made
   - Why they were made
   - Any relevant issue numbers
   - Testing notes

3. **Review**: 
   - Address all review comments
   - Keep PRs focused on a single feature/fix
   - Rebase on main before merging

## Documentation

All new features should include:

1. **Code Comments**: Document public APIs
2. **User Docs**: Update relevant docs in `docs/`
3. **Examples**: Add usage examples where helpful

## Questions?

- Open an issue for bug reports
- Start a discussion for questions
- Check existing documentation first

Thank you for contributing to cargobay!
