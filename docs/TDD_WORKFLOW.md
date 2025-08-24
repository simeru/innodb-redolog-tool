# Test-Driven Development Workflow

This document outlines the Test-Driven Development (TDD) workflow for the InnoDB Redo Log Analysis Tool.

## TDD Cycle Overview

The project follows the classic Red-Green-Refactor cycle:

1. **RED** - Write a failing test that specifies the desired behavior
2. **GREEN** - Write the minimal code to make the test pass
3. **REFACTOR** - Improve the code while keeping tests passing

## Project Structure

```
innodb-redolog-tool/
├── cmd/innodb-parser/           # Main application entry point
├── internal/                    # Private application code
│   ├── types/                   # Core data types and structures
│   ├── reader/                  # File reading and binary parsing
│   ├── parser/                  # Record parsing and validation
│   └── analyzer/                # High-level analysis and reporting
├── pkg/                         # Public API packages (if any)
├── test/                        # Test utilities and fixtures
│   ├── fixtures/                # Test data and sample files
│   └── integration/             # Integration tests
└── docs/                        # Documentation
```

## Testing Strategy

### Unit Tests
- Located alongside source code (`*_test.go` files)
- Test individual functions and methods in isolation
- Use mocks for dependencies
- Fast execution, no external dependencies

### Integration Tests
- Located in `test/integration/`
- Test component interactions
- Use real file system but controlled test data
- Tagged with `//go:build integration`

### Test Fixtures
- Located in `test/fixtures/`
- Provide sample redo log data and binary structures
- Support various scenarios (valid, corrupted, empty, large)

## TDD Workflow Commands

Use the provided Makefile targets for TDD workflow:

### Starting TDD Cycle
```bash
# RED phase - Run tests to see failures
make red

# GREEN phase - Make tests pass
make green

# REFACTOR phase - Improve code quality
make refactor
```

### Development Commands
```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run tests with race detection
make test-race

# Run only short tests (for quick feedback)
make test-short

# Generate mocks
make mock

# Format and lint code
make fmt vet lint
```

### Watch Mode
```bash
# Auto-run tests on file changes (requires inotifywait)
make tdd-watch
```

## Implementation Order

Follow this sequence when implementing components:

### Phase 1: Foundation (COMPLETED)
1. ✅ Core types (`internal/types/`)
2. ✅ Interface definitions
3. ✅ Test fixtures and sample data

### Phase 2: Binary Reading
1. Implement `BinaryReader` interface
2. Implement `RedoLogReader` interface
3. Add file handling and error cases

### Phase 3: Record Parsing
1. Implement `RedoLogParser` interface
2. Add binary format parsing logic
3. Implement checksum validation

### Phase 4: Analysis
1. Implement `RecordAnalyzer` interface
2. Implement `RedoLogAnalyzer` interface
3. Implement `TransactionAnalyzer` interface

### Phase 5: CLI Integration
1. Wire components together in main application
2. Add output formatting (JSON, CSV, text)
3. Add command-line argument handling

## TDD Best Practices

### Writing Tests
1. **Test First**: Always write tests before implementation
2. **One Test at a Time**: Focus on one failing test
3. **Descriptive Names**: Use clear, descriptive test names
4. **Arrange-Act-Assert**: Structure tests clearly
5. **Edge Cases**: Test boundary conditions and error cases

### Example Test Structure
```go
func TestRedoLogReader_ReadHeader_ValidFile(t *testing.T) {
    // Arrange
    tempDir := t.TempDir()
    filename, err := fixtures.CreateSampleLogFile(tempDir)
    require.NoError(t, err)
    
    reader := NewRedoLogReader()
    err = reader.Open(filename)
    require.NoError(t, err)
    defer reader.Close()
    
    // Act
    header, err := reader.ReadHeader()
    
    // Assert
    assert.NoError(t, err)
    assert.NotNil(t, header)
    assert.Equal(t, uint64(1), header.LogGroupID)
}
```

### Making Tests Pass
1. **Minimal Code**: Write only enough code to pass the test
2. **Avoid Over-Engineering**: Don't implement features not yet tested
3. **Handle Error Cases**: Make error path tests pass too

### Refactoring Guidelines
1. **Keep Tests Green**: All tests must pass during refactoring
2. **Improve Design**: Extract functions, improve naming, reduce duplication
3. **Performance**: Optimize only when tests validate the behavior
4. **Documentation**: Update comments and documentation

## Mock Strategy

Use `gomock` for generating mocks from interfaces:

```bash
# Generate all mocks
make mock

# Generate specific mock
mockgen -source=internal/reader/interfaces.go -destination=internal/reader/mocks/reader_mock.go
```

### Mock Usage Example
```go
func TestSomeFunction(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()
    
    mockReader := mocks.NewMockRedoLogReader(ctrl)
    mockReader.EXPECT().
        ReadHeader().
        Return(&types.RedoLogHeader{LogGroupID: 1}, nil)
    
    // Test with mock
    result := someFunction(mockReader)
    assert.NotNil(t, result)
}
```

## Continuous Integration

The CI pipeline runs automatically on:
- Push to `main` or `develop` branches
- Pull requests to `main`

### CI Stages
1. **Test**: Run unit tests with multiple Go versions
2. **Lint**: Code quality checks with golangci-lint
3. **Build**: Compile binaries for multiple platforms
4. **Integration**: Run integration tests
5. **Security**: Vulnerability scanning

### Pre-commit Checklist
Before committing code, ensure:
- [ ] All tests pass (`make test`)
- [ ] No linting errors (`make lint`)
- [ ] Code is formatted (`make fmt`)
- [ ] Coverage is maintained or improved
- [ ] New tests are added for new functionality

## Test Data Management

### Fixture Files
Test fixtures are managed in `test/fixtures/`:
- `sample_data.go`: In-memory test data structures
- `files.go`: File creation utilities for tests

### Binary Test Data
- Sample redo log files are generated programmatically
- No real MySQL data is stored in the repository
- Corruption scenarios are artificially created

### Performance Testing
Large file performance tests use generated data:
```go
filename, err := fixtures.CreateLargeLogFile(tempDir, 1000) // 1000 transactions
```

## Debugging Failed Tests

### Common Issues
1. **File Path Problems**: Use `t.TempDir()` for temporary files
2. **Mock Expectations**: Verify all expected calls are made
3. **Race Conditions**: Use `make test-race` to detect
4. **Resource Leaks**: Always close files and readers

### Debug Tools
```bash
# Run specific test with verbose output
go test -v -run TestSpecificFunction ./internal/reader

# Run with race detection
go test -race ./...

# Generate coverage profile
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Contributing Guidelines

1. **Follow TDD**: All new code must be test-driven
2. **Maintain Coverage**: Keep test coverage above 80%
3. **Document Interfaces**: All public interfaces need documentation
4. **Error Handling**: Test both success and failure paths
5. **Performance**: Include performance tests for critical paths

## Getting Started

1. **Clone and Setup**:
   ```bash
   git clone <repository>
   cd innodb-redolog-tool
   make deps
   ```

2. **Run Initial Tests** (should mostly be skipped):
   ```bash
   make test
   ```

3. **Start Development**:
   - Pick a component to implement
   - Remove `t.Skip()` from relevant tests
   - Run `make red` to see failing tests
   - Implement minimal code to make tests pass
   - Run `make green` to verify
   - Refactor and run `make refactor`

4. **Verify Your Work**:
   ```bash
   make test-coverage
   make lint
   ```

This workflow ensures high-quality, well-tested code that meets the project requirements.