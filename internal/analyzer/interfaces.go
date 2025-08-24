package analyzer

import (
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

//go:generate mockgen -source=interfaces.go -destination=mocks/analyzer_mock.go

// RedoLogAnalyzer defines the interface for analyzing redo logs
type RedoLogAnalyzer interface {
	// AnalyzeFile performs complete analysis of a redo log file
	AnalyzeFile(filename string) (*AnalysisResult, error)
	
	// AnalyzeRecords analyzes a collection of log records
	AnalyzeRecords(records []*types.LogRecord) (*AnalysisResult, error)
	
	// GenerateStats generates statistics from log records
	GenerateStats(records []*types.LogRecord) (*types.RedoLogStats, error)
	
	// DetectCorruption detects potential corruption in log records
	DetectCorruption(records []*types.LogRecord) (*CorruptionReport, error)
}

// TransactionAnalyzer defines the interface for transaction analysis
type TransactionAnalyzer interface {
	// ReconstructTransactions reconstructs transactions from log records
	ReconstructTransactions(records []*types.LogRecord) ([]*Transaction, error)
	
	// FindIncompleteTransactions finds transactions without commit records
	FindIncompleteTransactions(records []*types.LogRecord) ([]*Transaction, error)
	
	// AnalyzeTransaction provides detailed analysis of a single transaction
	AnalyzeTransaction(txn *Transaction) (*TransactionAnalysis, error)
}

// AnalysisResult contains the complete analysis of a redo log
type AnalysisResult struct {
	Header        *types.RedoLogHeader
	Stats         *types.RedoLogStats
	Transactions  []*Transaction
	Corruption    *CorruptionReport
	Warnings      []string
	Summary       string
}

// Transaction represents a reconstructed database transaction
type Transaction struct {
	ID           uint64
	StartLSN     uint64
	EndLSN       uint64
	Records      []*types.LogRecord
	Status       TransactionStatus
	TableAffected []uint32
	Duration     int64 // in microseconds
}

// TransactionStatus represents the status of a transaction
type TransactionStatus int

const (
	TransactionPending TransactionStatus = iota
	TransactionCommitted
	TransactionRolledBack
	TransactionIncomplete
)

// TransactionAnalysis contains detailed transaction analysis
type TransactionAnalysis struct {
	Type          string
	RowsAffected  uint64
	TablesChanged []uint32
	Complexity    ComplexityLevel
	Issues        []string
}

// ComplexityLevel represents transaction complexity
type ComplexityLevel int

const (
	ComplexitySimple ComplexityLevel = iota
	ComplexityModerate
	ComplexityHigh
)

// CorruptionReport contains information about detected corruption
type CorruptionReport struct {
	HasCorruption    bool
	CorruptedRecords []CorruptionIssue
	Severity         CorruptionSeverity
	Recoverable      bool
}

// CorruptionIssue represents a specific corruption issue
type CorruptionIssue struct {
	LSN         uint64
	RecordIndex int
	IssueType   string
	Description string
	Severity    CorruptionSeverity
}

// CorruptionSeverity represents the severity of corruption
type CorruptionSeverity int

const (
	SeverityLow CorruptionSeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)