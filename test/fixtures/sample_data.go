package fixtures

import (
	"encoding/binary"
	"time"
	
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

// SampleRedoLogHeader creates a sample redo log header for testing
func SampleRedoLogHeader() *types.RedoLogHeader {
	return &types.RedoLogHeader{
		LogGroupID:     1,
		StartLSN:       1000,
		FileNo:         1,
		Created:        time.Date(2024, 8, 24, 12, 0, 0, 0, time.UTC),
		LastCheckpoint: 5000,
		Format:         1,
	}
}

// SampleInsertRecord creates a sample INSERT log record
func SampleInsertRecord() *types.LogRecord {
	return &types.LogRecord{
		Type:          types.LogTypeInsert,
		Length:        79, // 57 (header) + 18 (data) + 4 (checksum)
		LSN:           1001,
		Timestamp:     time.Date(2024, 8, 24, 12, 0, 1, 0, time.UTC),
		TransactionID: 12345,
		TableID:       100,
		IndexID:       1,
		Data:          []byte("sample insert data"),
		Checksum:      calculateChecksum([]byte("sample insert data")),
		SpaceID:       0,
		PageNo:        1,
		Offset:        128,
	}
}

// SampleUpdateRecord creates a sample UPDATE log record
func SampleUpdateRecord() *types.LogRecord {
	return &types.LogRecord{
		Type:          types.LogTypeUpdate,
		Length:        93, // 57 (header) + 32 (data) + 4 (checksum)
		LSN:           1002,
		Timestamp:     time.Date(2024, 8, 24, 12, 0, 2, 0, time.UTC),
		TransactionID: 12345,
		TableID:       100,
		IndexID:       1,
		Data:          []byte("sample update data before|after"),
		Checksum:      calculateChecksum([]byte("sample update data before|after")),
		SpaceID:       0,
		PageNo:        1,
		Offset:        192,
	}
}

// SampleCommitRecord creates a sample COMMIT log record
func SampleCommitRecord() *types.LogRecord {
	return &types.LogRecord{
		Type:          types.LogTypeCommit,
		Length:        67, // 57 (header) + 6 (data) + 4 (checksum)
		LSN:           1003,
		Timestamp:     time.Date(2024, 8, 24, 12, 0, 3, 0, time.UTC),
		TransactionID: 12345,
		TableID:       0,
		IndexID:       0,
		Data:          []byte("commit"),
		Checksum:      calculateChecksum([]byte("commit")),
		SpaceID:       0,
		PageNo:        0,
		Offset:        0,
	}
}

// SampleTransaction creates a complete sample transaction
func SampleTransaction() []*types.LogRecord {
	return []*types.LogRecord{
		SampleInsertRecord(),
		SampleUpdateRecord(),
		SampleCommitRecord(),
	}
}

// SampleCorruptedRecord creates a record with invalid checksum for testing corruption detection
func SampleCorruptedRecord() *types.LogRecord {
	record := SampleInsertRecord()
	record.Checksum = 0xDEADBEEF // Invalid checksum
	return record
}

// BinaryRedoLogHeader creates binary representation of a redo log header
func BinaryRedoLogHeader() []byte {
	header := SampleRedoLogHeader()
	buf := make([]byte, 64) // Standard header size
	
	binary.LittleEndian.PutUint64(buf[0:8], header.LogGroupID)
	binary.LittleEndian.PutUint64(buf[8:16], header.StartLSN)
	binary.LittleEndian.PutUint32(buf[16:20], header.FileNo)
	binary.LittleEndian.PutUint64(buf[20:28], uint64(header.Created.Unix()))
	binary.LittleEndian.PutUint64(buf[28:36], header.LastCheckpoint)
	binary.LittleEndian.PutUint32(buf[36:40], header.Format)
	
	return buf
}

// BinaryLogRecord creates binary representation of a log record
func BinaryLogRecord(record *types.LogRecord) []byte {
	// Calculate the minimum size needed for the record
	headerSize := 1 + 4 + 8 + 8 + 8 + 4 + 4 + 4 + 4 + 2 // 57 bytes for header
	dataSize := len(record.Data)
	checksumSize := 4
	minSize := headerSize + dataSize + checksumSize
	
	// Use the maximum of record.Length or calculated minimum size
	bufSize := int(record.Length)
	if minSize > bufSize {
		bufSize = minSize
	}
	
	buf := make([]byte, bufSize)
	offset := 0
	
	// Record header
	buf[offset] = uint8(record.Type)
	offset++
	
	// Write the actual buffer size as the length, not the original record.Length
	binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(bufSize))
	offset += 4
	
	binary.LittleEndian.PutUint64(buf[offset:offset+8], record.LSN)
	offset += 8
	
	binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(record.Timestamp.Unix()))
	offset += 8
	
	binary.LittleEndian.PutUint64(buf[offset:offset+8], record.TransactionID)
	offset += 8
	
	binary.LittleEndian.PutUint32(buf[offset:offset+4], record.TableID)
	offset += 4
	
	binary.LittleEndian.PutUint32(buf[offset:offset+4], record.IndexID)
	offset += 4
	
	binary.LittleEndian.PutUint32(buf[offset:offset+4], record.SpaceID)
	offset += 4
	
	binary.LittleEndian.PutUint32(buf[offset:offset+4], record.PageNo)
	offset += 4
	
	binary.LittleEndian.PutUint16(buf[offset:offset+2], record.Offset)
	offset += 2
	
	// Data
	copy(buf[offset:offset+len(record.Data)], record.Data)
	offset += len(record.Data)
	
	// Pad to align checksum
	for offset < len(buf)-4 {
		buf[offset] = 0
		offset++
	}
	
	// Checksum at the end
	binary.LittleEndian.PutUint32(buf[len(buf)-4:], record.Checksum)
	
	return buf
}

// calculateChecksum calculates a simple checksum for testing
func calculateChecksum(data []byte) uint32 {
	var sum uint32
	for _, b := range data {
		sum += uint32(b)
	}
	return sum
}

// InvalidBinaryData creates intentionally malformed binary data for error testing
func InvalidBinaryData() []byte {
	return []byte{0xFF, 0xFF, 0xFF, 0xFF} // Invalid data that should cause parsing errors
}

// EmptyBinaryData creates empty binary data for edge case testing
func EmptyBinaryData() []byte {
	return []byte{}
}

// TruncatedBinaryRecord creates a truncated binary record for error testing
func TruncatedBinaryRecord() []byte {
	record := SampleInsertRecord()
	binary := BinaryLogRecord(record)
	// Return only half the record to simulate truncation
	return binary[:len(binary)/2]
}