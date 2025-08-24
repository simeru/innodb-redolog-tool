package reader

import "github.com/yamaru/innodb-redolog-tool/internal/types"

// RedoLogReader defines the interface for reading InnoDB redo log files
type RedoLogReader interface {
	// Open opens a redo log file
	Open(filename string) error
	
	// ReadHeader reads the header from the redo log file
	ReadHeader() (*types.RedoLogHeader, error)
	
	// ReadRecord reads the next record from the redo log file
	ReadRecord() (*types.LogRecord, error)
	
	// Seek sets the file position for the next read operation
	Seek(offset int64) error
	
	// IsEOF returns true if we've reached the end of the file
	IsEOF() bool
	
	// Close closes the redo log file
	Close() error
}

// BinaryReader defines the interface for low-level binary reading operations
type BinaryReader interface {
	// ReadBytes reads n bytes from the current position
	ReadBytes(n int) ([]byte, error)
	
	// ReadUint32 reads a 32-bit unsigned integer
	ReadUint32() (uint32, error)
	
	// ReadUint64 reads a 64-bit unsigned integer
	ReadUint64() (uint64, error)
	
	// Skip skips n bytes from the current position
	Skip(n int64) error
	
	// Position returns the current position in the file
	Position() int64
}