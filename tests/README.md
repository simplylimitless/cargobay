# Cargobay Testing Suite

This directory contains comprehensive tests for the cargobay project.

## Test Structure

```
tests/
├── testutil/           # Shared test utilities and fixtures
│   ├── testutil.go     # Test helpers and data generators
│   └── mock.go         # Mock implementations
├── frontend/           # Frontend tests
│   ├── components/     # Component unit tests
│   ├── pages/          # Page component tests
│   └── e2e/            # End-to-end tests with Playwright
├── README.md           # This file
└── README.md           # Frontend testing guide
```

## Running Tests

### All Tests
```bash
make test
```

### Unit Tests
```bash
make backend-test
```

### E2E Tests
```bash
make frontend-e2e
```

### Specific Package Tests
```bash
go test ./backend/pkg/proxy/npm/...
go test ./backend/pkg/proxy/maven/...
go test ./backend/pkg/proxy/docker/...
go test ./backend/pkg/proxy/pypi/...
go test ./backend/pkg/proxy/nuget/...
go test ./backend/pkg/proxy/helm/...
go test ./backend/pkg/middleware/...
go test ./backend/pkg/database/...
go test ./backend/pkg/cache/...
go test ./backend/pkg/storage/...
go test ./backend/pkg/rbac/...
go test ./backend/pkg/vulnerability/...
go test ./backend/pkg/auth/...
```

### Frontend Tests
```bash
# Unit tests
cd frontend && npm test

# With coverage
cd frontend && npm run test:coverage

# E2E tests
cd frontend && npx playwright test

# E2E tests with UI
cd frontend && npx playwright test --ui
```

## Test Types

### Go Unit Tests
Go unit tests test individual functions and components in isolation. They use mock implementations to avoid dependencies on external services.

#### Proxy Tests (`backend/pkg/proxy/*/test.go`)
- npm, Maven, Docker, PyPI, NuGet, Helm proxy implementations
- Request handling, caching, upstream fetching
- Edge cases and error conditions

#### Middleware Tests (`backend/pkg/middleware/test.go`)
- Authentication, rate limiting, path normalization
- Role-based access control
- Permission checking

#### Database Tests (`backend/pkg/database/test.go`)
- CRUD operations for all models
- Search, pagination, filtering
- Concurrency handling

#### Cache Tests (`backend/pkg/cache/test.go`)
- Redis caching operations
- TTL handling
- Cache hit/miss behavior

#### Storage Tests (`backend/pkg/storage/test.go`)
- Local, S3, GCS, Azure storage adapters
- Artifact upload/download
- Concurrent access

#### RBAC Tests (`backend/pkg/rbac/test.go`)
- Role creation and management
- Permission checking
- Role-permission mapping

#### Auth Tests (`backend/pkg/auth/`)
- Password hashing and verification
- Login/logout
- Access key generation

#### Vulnerability Tests (`backend/pkg/vulnerability/test.go`)
- Scanner integration (Trivy/Grype)
- Severity calculation
- Scan result processing

### React Component Tests
Unit tests for React components using React Testing Library.

#### Component Tests (`frontend/src/__tests__/components/`)
- **Navbar.test.tsx** - Navigation bar rendering, authentication state
- **SearchInput.test.tsx** - Search functionality, autocomplete
- **Breadcrumbs.test.tsx** - Navigation trail, URL decoding

#### Page Tests (`frontend/src/__tests__/pages/`)
- **ArtifactDetail.test.tsx** - Artifact details, pull commands, signatures
- **ArtifactList.test.tsx** - Artifact grid, filtering, pagination
- **RegistryDetail.test.tsx** - Registry info, artifact types

### React E2E Tests
End-to-end tests using Playwright to verify the complete user workflows.

#### Test Files (`frontend/tests/e2e/`)
- **home.spec.ts** - Home page, navigation, responsive design
- **login.spec.ts** - Login flow, authentication, security
- **artifacts.spec.ts** - Artifact browsing, search, upload, deletion
- **settings.spec.ts** - Settings management, notifications

## Test Utilities

### Go Mock Implementations
The test utilities provide mock implementations for common dependencies:

- `MockDatabase` - Mock database with in-memory storage
- `MockStorageAdapter` - Mock storage for testing artifact operations
- `MockCache` - Mock Redis cache
- `MockRBAC` - Mock RBAC manager
- `MockVulnerabilityScanner` - Mock vulnerability scanner
- `MockHTTPHandler` - HTTP handler for testing proxy routes

### Test Data Generators
Utility functions for creating test data:

- `MockArtifactMetadata()` - Create mock artifact metadata
- `MockRegistryConfig()` - Create mock registry configuration
- `MockUser()` - Create mock user
- `MockAccessKey()` - Create mock access key
- `MockAuditLog()` - Create mock audit log
- `MockSignature()` - Create mock signature
- `MockRBACRole()` - Create mock RBAC role
- `MockVulnerability()` - Create mock vulnerability

### Helper Functions
- `TestTempDir()` - Create temporary test directory
- `WriteTempFile()` - Write content to temp file
- `ReadJSONFile()` - Parse JSON from file
- `EqualJSON()` - Compare JSON strings
- `DeepEqualJSON()` - Deep equality comparison for JSON

## Writing Tests

### Go Test Structure
```go
package mypackage

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestMyFunction(t *testing.T) {
    // Arrange
    input := "test input"
    
    // Act
    result := MyFunction(input)
    
    // Assert
    assert.NotNil(t, result)
    assert.Equal(t, "expected", result)
}
```

### Go Table-Driven Tests
```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"empty input", "", ""},
        {"normal input", "test", "test"},
        {"special chars", "test@123", "test@123"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MyFunction(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

### Go Database Tests
```go
func TestDatabaseSaveArtifact(t *testing.T) {
    db := database.New("postgres://localhost:5432/testdb")
    
    artifact := &database.ArtifactMetadata{
        ID:           "test-artifact",
        ArtifactType: "docker",
        ArtifactName: "test",
        Version:      "1.0.0",
    }
    
    err := db.SaveArtifact(artifact)
    assert.NoError(t, err)
    
    // Cleanup
    db.DeleteArtifact(artifact.ID)
}
```

### Go Middleware Tests
```go
func TestAuthMiddleware(t *testing.T) {
    db := database.New("postgres://localhost:5432/testdb")
    lookup := NewMockRoleAndPermissionLookup()
    
    middleware := NewAuthMiddleware(db, lookup)
    
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        user := GetUser(r)
        assert.NotNil(t, user)
        w.WriteHeader(http.StatusOK)
    })
    
    next := middleware(handler)
    
    // Test with valid credentials
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    req.Header.Set("Authorization", "Bearer valid-token")
    rec := httptest.NewRecorder()
    
    next.ServeHTTP(rec, req)
    assert.Equal(t, http.StatusOK, rec.Code)
}
```

### React Test Structure
```tsx
import { render, screen } from '@testing-library/react'
import { MyComponent } from './MyComponent'

describe('MyComponent', () => {
  test('renders correctly', () => {
    render(<MyComponent />)
    expect(screen.getByText('Hello')).toBeInTheDocument()
  })
})
```

### React Component Test with Mocks
```tsx
import { render, screen, waitFor } from '@testing-library/react'
import { SearchInput } from './SearchInput'

jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useNavigate: () => jest.fn(),
}))

describe('SearchInput', () => {
  test('displays suggestions', async () => {
    global.fetch = jest.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ suggestions: ['test'] }),
      })
    )

    render(<SearchInput />)
    const input = screen.getByPlaceholderText(/Search/i)
    
    fireEvent.change(input, { target: { value: 'test' } })
    
    await waitFor(() => {
      expect(screen.getByText('test')).toBeInTheDocument()
    })
  })
})
```

### Playwright E2E Test
```tsx
import { test, expect } from '@playwright/test'

test.describe('Login Flow', () => {
  test('logs in successfully', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('Username').fill('admin')
    await page.getByLabel('Password').fill('admin')
    await page.getByRole('button', { name: 'Log in' }).click()
    
    await expect(page).toHaveURL('/dashboard')
    await expect(page.getByText('Welcome')).toBeVisible()
  })
})
```

## Test Best Practices

### Go
1. **Use table-driven tests** for multiple test cases
2. **Test edge cases and error conditions**
3. **Clean up resources** after tests
4. **Use descriptive test names**
5. **Keep tests independent** - no shared state
6. **Mock external dependencies** to ensure fast, reliable tests
7. **Test the interface, not implementation**

### React
1. **Test user-facing behavior** - not implementation details
2. **Use meaningful queries** - `getByRole`, `getByText`
3. **Wait for async operations** - `waitFor`, `findBy`
4. **Mock external dependencies** - `fetch`, `useNavigate`
5. **Test loading states** and error states
6. **Test accessibility** - `aria-*` attributes

### E2E
1. **Test real user workflows** - not individual components
2. **Use realistic data** - avoid hard-coded IDs
3. **Test error handling** - network failures, validation
4. **Test across devices** - responsive design
5. **Use proper selectors** - data-testid attributes

## Coverage

### Go Coverage
```bash
go test ./backend/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Frontend Coverage
```bash
cd frontend && npm run test:coverage
```

## CI/CD Integration

Tests are run automatically in CI/CD pipelines:

```bash
# All checks
make ci

# Backend tests only
make ci-backend

# Frontend tests only
make ci-frontend

# E2E tests
make frontend-e2e
```

## Troubleshooting

### Common Issues

**Go tests timeout:**
```bash
go test -timeout 30s ./pkg/...
```

**React tests fail on async:**
```tsx
await waitFor(() => {
  expect(screen.getByText('Loaded')).toBeInTheDocument()
})
```

**Playwright tests fail on CI:**
```bash
npx playwright install-deps
npx playwright install chromium
```
