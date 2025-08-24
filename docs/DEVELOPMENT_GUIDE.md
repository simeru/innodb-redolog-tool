# Development Guide

This guide provides comprehensive information for developers working on the InnoDB Redo Log Analysis Tool.

## Project Overview

The InnoDB Redo Log Analysis Tool is designed to parse, analyze, and report on MySQL InnoDB redo log files. It follows Test-Driven Development (TDD) principles and is built with Go.

### Key Features
- Parse InnoDB redo log binary format
- Analyze transaction patterns and consistency
- Detect corruption and integrity issues
- Generate reports in multiple formats (JSON, CSV, text)
- High-performance processing of large log files

## Architecture

### Component Overview
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   CLI Layer     │    │  Analysis Layer │    │  Parser Layer   │
│                 │    │                 │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │ innodb-     │ │    │ │ RedoLog     │ │    │ │ RedoLog     │ │
│ │ parser      │ │────▶│ Analyzer    │ │────▶│ Parser      │ │
│ │             │ │    │ │             │ │    │ │             │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│                 │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│                 │    │ │ Transaction │ │    │ │ Record      │ │
│                 │    │ │ Analyzer    │ │    │ │ Analyzer    │ │
│                 │    │ │             │ │    │ │             │ │
│                 │    │ └─────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                              ┌─────────────────┐     │
                              │  Reader Layer   │     │
                              │                 │     │
                              │ ┌─────────────┐ │     │
                              │ │ RedoLog     │ │◀────┘
                              │ │ Reader      │ │
                              │ │             │ │
                              │ └─────────────┘ │
                              │ ┌─────────────┐ │
                              │ │ Binary      │ │
                              │ │ Reader      │ │
                              │ │             │ │
                              │ └─────────────┘ │
                              └─────────────────┘
```

### Layer Responsibilities

#### Reader Layer (`internal/reader/`)
- **BinaryReader**: Low-level binary data reading with endianness handling
- **RedoLogReader**: High-level redo log file operations and navigation

#### Parser Layer (`internal/parser/`)
- **RedoLogParser**: Convert binary data to structured log records
- **RecordAnalyzer**: Analyze individual records for consistency and issues

#### Analysis Layer (`internal/analyzer/`)
- **RedoLogAnalyzer**: Comprehensive file analysis and reporting
- **TransactionAnalyzer**: Transaction reconstruction and analysis

#### CLI Layer (`cmd/innodb-parser/`)
- Command-line interface and argument parsing
- Output formatting and user interaction

## Interface Design

All components are designed around interfaces to enable:
- Easy testing with mocks
- Flexible implementation swapping
- Clear separation of concerns

### Key Interfaces

```go
// Core data flow interfaces
type RedoLogReader interface {
    Open(filename string) error
    ReadHeader() (*types.RedoLogHeader, error)
    ReadRecord() (*types.LogRecord, error)
    Seek(lsn uint64) error
    Close() error
}

type RedoLogParser interface {
    ParseRecord(data []byte) (*types.LogRecord, error)
    ParseHeader(data []byte) (*types.RedoLogHeader, error)
    ValidateChecksum(record *types.LogRecord) error
}

type RedoLogAnalyzer interface {
    AnalyzeFile(filename string) (*AnalysisResult, error)
    AnalyzeRecords(records []*types.LogRecord) (*AnalysisResult, error)
    DetectCorruption(records []*types.LogRecord) (*CorruptionReport, error)
}
```

## Data Types

### Core Types (`internal/types/`)

#### LogRecord
Represents a single redo log entry:
```go
type LogRecord struct {
    Type          LogType   // INSERT, UPDATE, DELETE, COMMIT, etc.
    Length        uint32    // Record length in bytes
    LSN           uint64    // Log Sequence Number
    Timestamp     time.Time // When the record was created
    TransactionID uint64    // Transaction identifier
    TableID       uint32    // Affected table
    IndexID       uint32    // Affected index
    Data          []byte    // Record payload
    Checksum      uint32    // Integrity checksum
    SpaceID       uint32    // Tablespace identifier
    PageNo        uint32    // Page number
    Offset        uint16    // Offset within page
}
```

#### RedoLogHeader
File-level metadata:
```go
type RedoLogHeader struct {
    LogGroupID     uint64    // Log group identifier
    StartLSN       uint64    // First LSN in file
    FileNo         uint32    // File sequence number
    Created        time.Time // File creation time
    LastCheckpoint uint64    // Last checkpoint LSN
    Format         uint32    // Format version
}
```

## Binary Format Handling

### InnoDB Redo Log Format
The tool handles the InnoDB redo log binary format:

```
File Structure:
┌──────────────────┐
│    File Header   │ (64 bytes)
├──────────────────┤
│   Log Record 1   │ (variable length)
├──────────────────┤
│   Log Record 2   │ (variable length)
├──────────────────┤
│       ...        │
├──────────────────┤
│   Log Record N   │ (variable length)
└──────────────────┘

Record Structure:
┌─────────────┬─────────────┬─────────────┬─────────────┐
│ Record Type │ Length      │ LSN         │ Timestamp   │
│ (1 byte)    │ (4 bytes)   │ (8 bytes)   │ (8 bytes)   │
├─────────────┼─────────────┼─────────────┼─────────────┤
│Transaction  │ Table ID    │ Index ID    │ Space ID    │
│ID (8 bytes) │ (4 bytes)   │ (4 bytes)   │ (4 bytes)   │
├─────────────┼─────────────┼─────────────┼─────────────┤
│ Page No     │ Offset      │ Data        │ Checksum    │
│ (4 bytes)   │ (2 bytes)   │ (variable)  │ (4 bytes)   │
└─────────────┴─────────────┴─────────────┴─────────────┘
```

### Endianness
All multi-byte integers are stored in little-endian format, consistent with InnoDB conventions.

## Error Handling Strategy

### Error Categories
1. **I/O Errors**: File access, permission issues
2. **Format Errors**: Invalid binary data, unknown record types
3. **Corruption Errors**: Checksum mismatches, truncated data
4. **Logic Errors**: Inconsistent transaction states

### Error Types
```go
// Custom error types for different categories
type ParseError struct {
    Position int64
    Message  string
    Cause    error
}

type CorruptionError struct {
    LSN     uint64
    Type    string
    Details string
}

type ValidationError struct {
    Record  *LogRecord
    Field   string
    Message string
}
```

### Error Handling Pattern
```go
func (p *parser) ParseRecord(data []byte) (*types.LogRecord, error) {
    if len(data) < MinRecordSize {
        return nil, &ParseError{
            Message: "record too short",
            Cause:   fmt.Errorf("got %d bytes, need at least %d", len(data), MinRecordSize),
        }
    }
    
    // Parse record...
    record := &types.LogRecord{}
    
    if err := p.ValidateChecksum(record); err != nil {
        return nil, &CorruptionError{
            LSN:     record.LSN,
            Type:    "checksum_mismatch",
            Details: err.Error(),
        }
    }
    
    return record, nil
}
```

## Performance Considerations

### Memory Management
- Stream processing to handle large files
- Buffer pooling for frequent allocations
- Lazy loading of record data

### Concurrency
- Reader components are NOT thread-safe by design
- Analysis can be parallelized at the file level
- Use worker pools for batch processing

### Optimization Targets
- Parse 100MB+ files in under 30 seconds
- Memory usage should not exceed 2x file size
- Support files up to 10GB

## Testing Strategy

### Test Categories

#### Unit Tests
```go
// Example unit test structure
func TestRedoLogParser_ParseRecord_ValidInsert(t *testing.T) {
    // Arrange
    parser := NewRedoLogParser()
    recordData := fixtures.BinaryLogRecord(fixtures.SampleInsertRecord())
    
    // Act
    record, err := parser.ParseRecord(recordData)
    
    // Assert
    require.NoError(t, err)
    assert.Equal(t, types.LogTypeInsert, record.Type)
    assert.Equal(t, uint64(12345), record.TransactionID)
}
```

#### Integration Tests
```go
//go:build integration

func TestEndToEndAnalysis(t *testing.T) {
    // Create sample file
    filename, err := fixtures.CreateLargeLogFile(t.TempDir(), 1000)
    require.NoError(t, err)
    
    // Run full analysis
    analyzer := NewRedoLogAnalyzer(
        NewRedoLogReader(),
        NewRedoLogParser(),
    )
    
    result, err := analyzer.AnalyzeFile(filename)
    require.NoError(t, err)
    
    // Verify results
    assert.Equal(t, 1000, len(result.Transactions))
}
```

#### Benchmark Tests
```go
func BenchmarkParseRecord(b *testing.B) {
    parser := NewRedoLogParser()
    recordData := fixtures.BinaryLogRecord(fixtures.SampleInsertRecord())
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := parser.ParseRecord(recordData)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

### Test Data Management

#### Fixtures
- `test/fixtures/sample_data.go`: In-memory test structures
- `test/fixtures/files.go`: File generation utilities
- Binary data is generated programmatically
- No real MySQL data in the repository

#### Mock Strategy
```go
func TestAnalyzer_WithMockedReader(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()
    
    mockReader := mocks.NewMockRedoLogReader(ctrl)
    mockReader.EXPECT().
        ReadHeader().
        Return(fixtures.SampleRedoLogHeader(), nil)
    
    // Test with mocked dependencies
    analyzer := NewRedoLogAnalyzer(mockReader, NewRedoLogParser())
    // ... test logic
}
```

## Build and Deployment

### Build Targets
```bash
# Development build
make build

# Cross-platform builds
make build-linux
make build-windows
make build-darwin

# Release build with optimizations
go build -ldflags="-s -w" -o bin/innodb-parser ./cmd/innodb-parser
```

### Release Process
1. Update version in code
2. Create and push git tag: `git tag v1.0.0`
3. GitHub Actions automatically builds and releases
4. Binaries are available for multiple platforms

### Installation Options
```bash
# Install from source
go install github.com/yamaru/innodb-redolog-tool/cmd/innodb-parser@latest

# Download release binary
curl -L https://github.com/yamaru/innodb-redolog-tool/releases/latest/download/innodb-parser-linux-amd64.tar.gz | tar xz
```

## Contributing Workflow

### Before Starting
1. Check existing issues and PRs
2. Discuss large changes in an issue first
3. Ensure you understand the TDD workflow

### Development Process
1. **Fork and Clone**:
   ```bash
   git clone https://github.com/your-username/innodb-redolog-tool.git
   cd innodb-redolog-tool
   ```

2. **Create Feature Branch**:
   ```bash
   git checkout -b feature/your-feature-name
   ```

3. **Follow TDD Cycle**:
   - Write failing tests
   - Implement minimal code
   - Refactor and improve

4. **Verify Quality**:
   ```bash
   make test
   make test-coverage
   make lint
   ```

5. **Submit PR**:
   - Clear description of changes
   - Link to related issues
   - Include test coverage information

### Code Review Checklist
- [ ] Tests cover new functionality
- [ ] All existing tests pass
- [ ] Code follows Go conventions
- [ ] Interfaces are properly documented
- [ ] Error handling is comprehensive
- [ ] Performance impact is considered

## Troubleshooting

### Common Development Issues

#### Test Failures
```bash
# Run specific test with verbose output
go test -v -run TestSpecificFunction ./internal/parser

# Check for race conditions
make test-race
```

#### Binary Format Issues
- Use hex dumps to examine binary data: `hexdump -C sample.log`
- Verify byte order and alignment
- Check MySQL documentation for format changes

#### Performance Problems
```bash
# Profile CPU usage
go test -cpuprofile=cpu.prof -bench=. ./internal/parser
go tool pprof cpu.prof

# Profile memory usage
go test -memprofile=mem.prof -bench=. ./internal/parser
go tool pprof mem.prof
```

### Getting Help
1. Check existing documentation
2. Search closed issues for similar problems
3. Create a detailed issue with:
   - Go version
   - Operating system
   - Sample data (if possible)
   - Full error output

## Security Considerations

### Input Validation
- All binary input is untrusted
- Implement bounds checking
- Validate checksums and magic numbers
- Handle malformed data gracefully

### Resource Limits
- Limit memory usage for large files
- Implement timeouts for long operations
- Prevent infinite loops in corrupt data

### Sensitive Data
- Never log sensitive data
- Sanitize error messages
- Support data anonymization options

This development guide provides the foundation for contributing to the InnoDB Redo Log Analysis Tool. Follow the TDD workflow and architectural patterns to maintain code quality and project consistency.