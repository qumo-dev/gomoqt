# Contributing to gomoqt

Thank you for your interest in contributing to gomoqt! We welcome contributions from the community to help improve this Go implementation of the Media over QUIC Transport (MOQ) protocol.

## Code of Conduct

This project follows a code of conduct to ensure a welcoming environment for all contributors. By participating, you agree to:
- Be respectful and inclusive
- Focus on constructive feedback
- Accept responsibility for mistakes
- Show empathy towards other contributors

## Getting Started

### Prerequisites

Before you begin, ensure you have the following installed:
- Go 1.25.0 or later
- [Mage](https://magefile.org/) build tool (install with `go install github.com/magefile/mage@latest`)
- Git

### Development Setup

1. Fork and clone the repository:
   ```bash
   git clone https://github.com/your-username/gomoqt.git
   cd gomoqt
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Install Mage build tool:
   ```bash
   go install github.com/magefile/mage@latest
   ```

4. Verify your setup:
   ```bash
   mage test
   ```

Note: Development setup commands (dev-setup, certificate generation, etc.) are still available via the Justfile if needed. The core build commands have been migrated to Mage.

## Development Workflow

### 1. Choose an Issue

- Check existing [issues](https://github.com/qumo-dev/gomoqt/issues) for good first issues
- Look for issues labeled `good first issue` or `help wanted`
- Comment on the issue to indicate you're working on it

### 2. Create a Branch

Create a feature branch from `main`:
```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/issue-number-description
```

### 3. Make Changes

- Write clear, focused commits
- Follow Go coding standards
- Add tests for new functionality
- Update documentation as needed

### 4. Testing

Run tests before submitting:
```bash
# Run all tests
mage test

# Run specific package tests
go test ./moqt/...

# Run with race detection
go test -race ./...

# Run benchmarks
go test -bench=. ./...
```

For benchmarks and performance regression comparison with `benchstat`, see
[BENCHMARKS.md](./BENCHMARKS.md). CI runs `-race` on every Go-touching PR and
compares PR benchmarks against a baseline captured on `main`.

### 5. Code Quality

Ensure your code meets our standards:
```bash
# Format code
mage fmt

# Run linter (requires golangci-lint)
mage lint
```

## Coding Standards

### Go Style Guide

- Follow the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `go fmt` for formatting
- Write clear, concise function and variable names
- Add comments for exported functions and types
- Keep functions small and focused

### Documentation Guidelines

All exported symbols (functions, types, constants, variables) must be documented with godoc comments:

- **Start with the symbol name**: Comments should begin with the name of the item being documented
- **Use complete sentences**: Write clear, grammatically correct sentences
- **Be concise but clear**: Explain what, not how (code shows how)
- **Package documentation**: Add a `doc.go` file or package comment in any `.go` file

Example:
```go
// Package example provides utilities for demonstration purposes.
package example

// Config holds configuration options for the service.
type Config struct {
    Timeout time.Duration
}

// NewConfig creates a new Config with default values.
func NewConfig() *Config {
    return &Config{
        Timeout: 30 * time.Second,
    }
}
```

### Example Functions

For key packages and common use cases, add `Example` functions in `*_test.go` files:

```go
package example_test

import "fmt"

// Example demonstrates basic usage of the package.
func Example() {
    config := example.NewConfig()
    fmt.Printf("Default timeout: %v\n", config.Timeout)
    // Output: Default timeout: 30s
}
```

These examples will appear on pkg.go.dev and help users understand how to use your code.

### Commit Messages

Use clear, descriptive commit messages:
```
type(scope): description

[optional body]

[optional footer]
```

Types:
- `feat`: New features
- `fix`: Bug fixes
- `docs`: Documentation
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Testing
- `chore`: Maintenance

Example:
```
feat(moqt): add support for track prioritization

Implement track priority handling in the session manager
to allow clients to specify delivery preferences.

Closes #123
```

### Pull Request Guidelines

When submitting a pull request:

1. **Title**: Use a clear, descriptive title
2. **Description**: Explain what the PR does and why
3. **Tests**: Ensure all tests pass
4. **Documentation**: Update docs if needed
5. **Breaking Changes**: Clearly mark any breaking changes
6. **Changelog**: Add or update an entry in `CHANGELOG.md` describing your changes. A GitHub workflow enforces this requirement and will block the PR if `CHANGELOG.md` isn't modified.


### Testing Requirements

- Add unit tests for new functionality
- Ensure existing tests still pass
- Test edge cases and error conditions
- Consider integration tests for complex features

### Testing Strategy (Mock Usage)

- Prefer hand-written fake/stub implementations for new tests.
- Use `testify/mock` only for boundary/protocol interaction checks.
- Keep assertions result/state-focused; avoid strict call-order checks unless required by protocol behavior.

## Project Structure

```
gomoqt/
├── moqt/           # Core MOQ protocol implementation
├── moq-web/        # WebTransport support for browsers
├── interop/        # Interoperability testing
├── examples/       # Usage examples
└── docs/          # Documentation
```

## Communication

- **Issues**: For bug reports and feature requests
- **Discussions**: For questions and general discussion
- **Pull Requests**: For code contributions

## License

By contributing to this project, you agree that your contributions will be licensed under the same license as the project (see LICENSE file).

## Recognition

Contributors will be acknowledged in the project documentation. Significant contributions may be recognized in release notes.

Thank you for contributing to gomoqt! 🚀