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

// String returns the string representation of LogType using MySQL 8.0 mlog_id_t definitions
func (lt LogType) String() string {
	recordType := uint8(lt)
	switch recordType {
	// Basic byte operations
	case 1:
		return "MLOG_1BYTE"
	case 2:
		return "MLOG_2BYTES"
	case 4:
		return "MLOG_4BYTES"
	case 8:
		return "MLOG_8BYTES"
	
	// Record operations (8027 series - older format)
	case 9:
		return "MLOG_REC_INSERT_8027"
	case 10:
		return "MLOG_REC_CLUST_DELETE_MARK_8027"
	case 11:
		return "MLOG_REC_SEC_DELETE_MARK"
	case 13:
		return "MLOG_REC_UPDATE_IN_PLACE_8027"
	case 14:
		return "MLOG_REC_DELETE_8027"
	case 15:
		return "MLOG_LIST_END_DELETE_8027"
	case 16:
		return "MLOG_LIST_START_DELETE_8027"
	case 17:
		return "MLOG_LIST_END_COPY_CREATED_8027"
	case 18:
		return "MLOG_PAGE_REORGANIZE_8027"
	
	// Page and undo operations
	case 19:
		return "MLOG_PAGE_CREATE"
	case 20:
		return "MLOG_UNDO_INSERT"
	case 21:
		return "MLOG_UNDO_ERASE_END"
	case 22:
		return "MLOG_UNDO_INIT"
	case 24:
		return "MLOG_UNDO_HDR_REUSE"
	case 25:
		return "MLOG_UNDO_HDR_CREATE"
	case 26:
		return "MLOG_REC_MIN_MARK"
	case 27:
		return "MLOG_IBUF_BITMAP_INIT"
	case 28:
		return "MLOG_LSN"
	case 29:
		return "MLOG_INIT_FILE_PAGE"
	case 30:
		return "MLOG_WRITE_STRING"
	case 31:
		return "MLOG_MULTI_REC_END"
	case 32:
		return "MLOG_DUMMY_RECORD"
	
	// File operations
	case 33:
		return "MLOG_FILE_CREATE"
	case 34:
		return "MLOG_FILE_RENAME"
	case 35:
		return "MLOG_FILE_DELETE"
	
	// Compact record operations
	case 36:
		return "MLOG_COMP_REC_MIN_MARK"
	case 37:
		return "MLOG_COMP_PAGE_CREATE"
	case 38:
		return "MLOG_COMP_REC_INSERT_8027"
	case 39:
		return "MLOG_COMP_REC_CLUST_DELETE_MARK_8027"
	case 40:
		return "MLOG_COMP_REC_SEC_DELETE_MARK"
	case 41:
		return "MLOG_COMP_REC_UPDATE_IN_PLACE_8027"
	case 42:
		return "MLOG_COMP_REC_DELETE_8027"
	case 43:
		return "MLOG_COMP_LIST_END_DELETE_8027"
	case 44:
		return "MLOG_COMP_LIST_START_DELETE_8027"
	case 45:
		return "MLOG_COMP_LIST_END_COPY_CREATED_8027"
	case 46:
		return "MLOG_COMP_PAGE_REORGANIZE_8027"
	
	// Compressed page operations
	case 48:
		return "MLOG_ZIP_WRITE_NODE_PTR"
	case 49:
		return "MLOG_ZIP_WRITE_BLOB_PTR"
	case 50:
		return "MLOG_ZIP_WRITE_HEADER"
	case 51:
		return "MLOG_ZIP_PAGE_COMPRESS"
	case 52:
		return "MLOG_ZIP_PAGE_COMPRESS_NO_DATA_8027"
	case 53:
		return "MLOG_ZIP_PAGE_REORGANIZE_8027"
	
	// R-Tree operations
	case 57:
		return "MLOG_PAGE_CREATE_RTREE"
	case 58:
		return "MLOG_COMP_PAGE_CREATE_RTREE"
	
	// Newer operations
	case 59:
		return "MLOG_INIT_FILE_PAGE2"
	case 61:
		return "MLOG_INDEX_LOAD"
	case 62:
		return "MLOG_TABLE_DYNAMIC_META"
	
	// SDI operations
	case 63:
		return "MLOG_PAGE_CREATE_SDI"
	case 64:
		return "MLOG_COMP_PAGE_CREATE_SDI"
	
	// File extend and test
	case 65:
		return "MLOG_FILE_EXTEND"
	case 66:
		return "MLOG_TEST"
	
	// Current format record operations (newer)
	case 67:
		return "MLOG_REC_INSERT"
	case 68:
		return "MLOG_REC_CLUST_DELETE_MARK"
	case 69:
		return "MLOG_REC_DELETE"
	case 70:
		return "MLOG_REC_UPDATE_IN_PLACE"
	case 71:
		return "MLOG_LIST_END_COPY_CREATED"
	case 72:
		return "MLOG_PAGE_REORGANIZE"
	case 73:
		return "MLOG_ZIP_PAGE_REORGANIZE"
	case 74:
		return "MLOG_ZIP_PAGE_COMPRESS_NO_DATA"
	case 75:
		return "MLOG_LIST_END_DELETE"
	case 76:
		return "MLOG_LIST_START_DELETE"
	
	// Handle invalid values
	case 0:
		return "INVALID_MLOG_0 (should not exist)"
	default:
		if recordType > 76 {
			return fmt.Sprintf("INVALID_MLOG_%d (exceeds MLOG_BIGGEST_TYPE=76)", recordType)
		}
		return fmt.Sprintf("UNKNOWN_MLOG_%d", recordType)
	}
}

// IsTransactional returns true if the log type is transactional
func (lt LogType) IsTransactional() bool {
	recordType := uint8(lt)
	// MySQL transactional record types (both old 8027 format and current format)
	switch recordType {
	case 9, 10, 13, 14: // MLOG_REC_INSERT_8027, MLOG_REC_CLUST_DELETE_MARK_8027, MLOG_REC_UPDATE_IN_PLACE_8027, MLOG_REC_DELETE_8027
		return true
	case 38, 39, 41, 42: // MLOG_COMP_REC_INSERT_8027, MLOG_COMP_REC_CLUST_DELETE_MARK_8027, MLOG_COMP_REC_UPDATE_IN_PLACE_8027, MLOG_COMP_REC_DELETE_8027
		return true
	case 67, 68, 69, 70: // MLOG_REC_INSERT, MLOG_REC_CLUST_DELETE_MARK, MLOG_REC_DELETE, MLOG_REC_UPDATE_IN_PLACE
		return true
	default:
		return false
	}
}