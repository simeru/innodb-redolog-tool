package types

import (
	"fmt"
	"time"
)

// LogType represents the type of redo log entry
type LogType uint8

const (
	// Core log types based on InnoDB specification
	LogTypeInsert LogType = iota + 1
	LogTypeUpdate
	LogTypeDelete
	LogTypeCommit
	LogTypeRollback
	LogTypeCheckpoint
	LogTypeUnknown
)

// LogRecord represents a single redo log record
type LogRecord struct {
	// Header information
	Type      LogType
	Length    uint32
	LSN       uint64    // Log Sequence Number
	Timestamp time.Time
	
	// Transaction information
	TransactionID uint64
	TableID       uint32
	IndexID       uint32
	
	// Record data
	Data     []byte
	Checksum uint32
	
	// Metadata
	SpaceID uint32
	PageNo  uint32
	Offset  uint16
}

// RedoLogHeader represents the redo log file header
type RedoLogHeader struct {
	LogGroupID    uint64
	StartLSN      uint64
	FileNo        uint32
	Created       time.Time
	LastCheckpoint uint64
	Format        uint32
}

// RedoLogStats provides statistics about the redo log
type RedoLogStats struct {
	TotalRecords     uint64
	RecordsByType    map[LogType]uint64
	SizeInBytes      uint64
	TransactionCount uint64
	TimeRange        struct {
		Start time.Time
		End   time.Time
	}
}

// String returns the string representation of LogType
func (lt LogType) String() string {
	// For MySQL format, use actual mlog_id_t values
	recordType := uint8(lt)
	switch recordType {
	case 9:
		return "INSERT"
	case 13:
		return "UPDATE"
	case 14:
		return "DELETE"
	case 15:
		return "LIST_END_DELETE"
	case 16:
		return "LIST_START_DELETE"
	case 17:
		return "LIST_END_COPY_CREATED"
	case 18:
		return "PAGE_REORGANIZE"
	case 19:
		return "PAGE_CREATE"
	case 20:
		return "UNDO_INSERT"
	case 21:
		return "UNDO_ERASE_END"
	case 22:
		return "UNDO_INIT"
	case 24:
		return "UNDO_HDR_REUSE"
	case 1:
		return "1BYTE"
	case 2:
		return "2BYTES"
	case 4:
		return "4BYTES"
	case 8:
		return "8BYTES"
	default:
		return fmt.Sprintf("MLOG_%d", recordType)
	}
}

// IsTransactional returns true if the log type is transactional
func (lt LogType) IsTransactional() bool {
	recordType := uint8(lt)
	// MySQL transactional record types
	return recordType == 9 || recordType == 13 || recordType == 14 // INSERT, UPDATE, DELETE
}