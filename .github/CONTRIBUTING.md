# Contributing to Cargobay

Thank you for your interest in contributing to Cargobay! This document provides guidelines for contributing to the project.

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct. Please read it here: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

## How Can I Contribute?

### Reporting Bugs

Before creating a bug report, please check the following:

1. Use the GitHub issue search to check if the bug has already been reported
2. Check if the bug is already fixed in the latest version
3. Make sure it's a bug and not expected behavior

**How to report a bug:**

1. Open a GitHub issue
2. Use the "Bug Report" template
3. Include as much detail as possible
4. Provide steps to reproduce
5. Include your environment details

### Suggesting Features

1. Check if the feature has already been suggested
2. Use the "Feature Request" template
3. Explain the use case and benefits
4. Provide examples if applicable

### Pull Requests

1. Fork the repository
2. Create a branch from `main` (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests (`make test`)
5. Run linting (`make lint`)
6. Update documentation if needed
7. Open a pull request

## Development Setup

### Prerequisites

- Go 1.21 or higher
- Node.js 20 or higher
- Docker and Docker Compose
- Git

### Getting Started

```bash
# Clone the repository
git clone https://github.com/cargobay/cargobay.git
cd cargobay

# Install dependencies
make backend-test
make frontend-test

# Start development services
make docker-up

# Build and run
make dev
```

## Coding Standards

### Go

- Use `gofmt` for formatting
- Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `golangci-lint` for linting
- Write unit tests for all new code
- Aim for >80% test coverage

### TypeScript/React

- Use Prettier for formatting
- Follow React best practices
- Write unit tests with Jest
- Write E2E tests with Playwright

### Git Conventions

- Use conventional commit messages:
  - `feat: Add new feature`
  - `fix: Fix bug`
  - `docs: Update documentation`
  - `style: Code style changes`
  - `refactor: Code refactoring`
  - `test: Add tests`
  - `chore: Maintenance tasks`

## Testing

### Running Tests

```bash
# All tests
make test

# Backend tests only
make backend-test

# Frontend tests only
make frontend-test

# E2E tests
make frontend-e2e

# With coverage
make backend-test-cover
```

### Writing Tests

**Backend tests:**

```go
func TestMyFunction(t *testing.T) {
    // Arrange
    // Act
    // Assert
}
```

**Frontend tests:**

```tsx
test('renders component', () => {
    render(<MyComponent />);
    expect(screen.getByText('Hello')).toBeInTheDocument();
});
```

## Documentation

- Update documentation for new features
- Update API documentation
- Update configuration examples

## Code Review Process

1. At least one approval required
2. All tests must pass
3. No merge commits allowed
4. Squash and merge preferred

## Questions?

- Join our Discord: [Discord Link]
- Check the docs: [Documentation Link]
- Open an issue for questions

Thank you for contributing!
