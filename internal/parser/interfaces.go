package parser

import "github.com/yamaru/innodb-redolog-tool/internal/types"

//go:generate mockgen -source=interfaces.go -destination=mocks/parser_mock.go

// RedoLogParser defines the interface for parsing redo log records
type RedoLogParser interface {
	// ParseRecord parses raw bytes into a structured LogRecord
	ParseRecord(data []byte) (*types.LogRecord, error)
	
	// ParseHeader parses raw header bytes into RedoLogHeader
	ParseHeader(data []byte) (*types.RedoLogHeader, error)
	
	// ValidateChecksum validates the checksum of a log record
	ValidateChecksum(record *types.LogRecord) error
	
	// GetRecordSize returns the size of a record from its header
	GetRecordSize(headerData []byte) (uint32, error)
}

// RecordAnalyzer defines the interface for analyzing log records
type RecordAnalyzer interface {
	// AnalyzeRecord provides detailed analysis of a log record
	AnalyzeRecord(record *types.LogRecord) (*RecordAnalysis, error)
	
	// DetectRecordType attempts to identify the record type from raw data
	DetectRecordType(data []byte) (types.LogType, error)
	
	// ExtractTransaction extracts transaction information from a record
	ExtractTransaction(record *types.LogRecord) (*TransactionInfo, error)
}

// RecordAnalysis contains detailed analysis of a log record
type RecordAnalysis struct {
	RecordType    types.LogType
	TableName     string
	Operation     string
	AffectedRows  uint32
	DataSize      uint32
	IsConsistent  bool
	Issues        []string
}

// TransactionInfo contains transaction-related information
type TransactionInfo struct {
	ID          uint64
	StartLSN    uint64
	EndLSN      uint64
	RecordCount uint32
	Operations  []types.LogType
	IsComplete  bool
}