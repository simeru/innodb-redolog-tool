package reader

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
	
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

// redoLogReader implements RedoLogReader interface
type redoLogReader struct {
	file *os.File
	eof  bool
}

// NewRedoLogReader creates a new RedoLogReader instance
func NewRedoLogReader() RedoLogReader {
	return &redoLogReader{}
}

// Open opens a redo log file
func (r *redoLogReader) Open(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	r.file = file
	return nil
}

// ReadHeader reads the header from the redo log file
func (r *redoLogReader) ReadHeader() (*types.RedoLogHeader, error) {
	if r.file == nil {
		return nil, fmt.Errorf("file not opened")
	}
	
	// Seek to beginning of file
	_, err := r.file.Seek(0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to file beginning: %w", err)
	}
	
	// Read header (64 bytes)
	headerBytes := make([]byte, 64)
	n, err := r.file.Read(headerBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}
	if n < 64 {
		return nil, fmt.Errorf("incomplete header read: got %d bytes, expected 64", n)
	}
	
	// Parse header fields
	header := &types.RedoLogHeader{
		LogGroupID:     binary.LittleEndian.Uint64(headerBytes[0:8]),
		StartLSN:       binary.LittleEndian.Uint64(headerBytes[8:16]),
		FileNo:         binary.LittleEndian.Uint32(headerBytes[16:20]),
		Created:        time.Unix(int64(binary.LittleEndian.Uint64(headerBytes[20:28])), 0),
		LastCheckpoint: binary.LittleEndian.Uint64(headerBytes[28:36]),
		Format:         binary.LittleEndian.Uint32(headerBytes[36:40]),
	}
	
	return header, nil
}

// ReadRecord reads the next record from the redo log file
func (r *redoLogReader) ReadRecord() (*types.LogRecord, error) {
	if r.file == nil {
		return nil, fmt.Errorf("file not opened")
	}
	
	// Read record type (1 byte)
	typeBytes := make([]byte, 1)
	n, err := r.file.Read(typeBytes)
	if err != nil {
		if err == io.EOF {
			r.eof = true
		}
		return nil, fmt.Errorf("failed to read record type: %w", err)
	}
	if n < 1 {
		r.eof = true
		return nil, fmt.Errorf("incomplete record type read")
	}
	
	// Read record length (4 bytes)
	lengthBytes := make([]byte, 4)
	n, err = r.file.Read(lengthBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read record length: %w", err)
	}
	if n < 4 {
		return nil, fmt.Errorf("incomplete record length read")
	}
	
	recordType := types.LogType(typeBytes[0])
	recordLength := binary.LittleEndian.Uint32(lengthBytes)
	
	// Read remaining record data (total length - type(1) - length(4))
	remainingSize := int(recordLength) - 5
	if remainingSize <= 0 {
		return nil, fmt.Errorf("invalid record length: %d", recordLength)
	}
	
	remainingBytes := make([]byte, remainingSize)
	n, err = r.file.Read(remainingBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read record data: %w", err)
	}
	if n < remainingSize {
		return nil, fmt.Errorf("incomplete record data read: got %d bytes, expected %d", n, remainingSize)
	}
	
	// Parse record fields
	offset := 0
	record := &types.LogRecord{
		Type:          recordType,
		Length:        recordLength,
		LSN:           binary.LittleEndian.Uint64(remainingBytes[offset : offset+8]),
		Timestamp:     time.Unix(int64(binary.LittleEndian.Uint64(remainingBytes[offset+8:offset+16])), 0),
		TransactionID: binary.LittleEndian.Uint64(remainingBytes[offset+16 : offset+24]),
		TableID:       binary.LittleEndian.Uint32(remainingBytes[offset+24 : offset+28]),
		IndexID:       binary.LittleEndian.Uint32(remainingBytes[offset+28 : offset+32]),
		SpaceID:       binary.LittleEndian.Uint32(remainingBytes[offset+32 : offset+36]),
		PageNo:        binary.LittleEndian.Uint32(remainingBytes[offset+36 : offset+40]),
		Offset:        binary.LittleEndian.Uint16(remainingBytes[offset+40 : offset+42]),
	}
	
	// Read data field (after fixed header, before checksum)
	dataStart := 42 // Fixed header size within remainingBytes
	checksumStart := len(remainingBytes) - 4 // Checksum is last 4 bytes
	
	if dataStart <= checksumStart {
		record.Data = remainingBytes[dataStart:checksumStart]
	}
	
	// Read checksum from the end of remainingBytes
	if checksumStart >= 0 && checksumStart+4 <= len(remainingBytes) {
		record.Checksum = binary.LittleEndian.Uint32(remainingBytes[checksumStart : checksumStart+4])
	}
	
	return record, nil
}

// Seek sets the file position for the next read operation
func (r *redoLogReader) Seek(offset int64) error {
	// TODO: Implement actual seek operation
	_, err := r.file.Seek(offset, 0)
	return err
}

// IsEOF returns true if we've reached the end of the file
func (r *redoLogReader) IsEOF() bool {
	return r.eof
}

// Close closes the redo log file
func (r *redoLogReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}