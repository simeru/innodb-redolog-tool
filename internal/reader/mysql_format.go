package reader

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

// MySQL 8.0 InnoDB Redo Log Format Constants
const (
	// Block sizes from MySQL source code
	OSFileLogBlockSize = 512 // OS_FILE_LOG_BLOCK_SIZE
	LogBlockHdrSize    = 12  // LOG_BLOCK_HDR_SIZE  
	LogBlockTrlSize    = 4   // LOG_BLOCK_TRL_SIZE
	LogBlockDataSize   = OSFileLogBlockSize - LogBlockHdrSize - LogBlockTrlSize // 496

	// Header offsets
	LogBlockHdrNo          = 0 // Block number (4 bytes)
	LogBlockHdrDataLen     = 4 // Data length (2 bytes)
	LogBlockFirstRecGroup  = 6 // First record group offset (2 bytes)
	LogBlockEpochNo        = 8 // Epoch number (4 bytes)

	// Footer offset (from end of block)
	LogBlockChecksum = 4 // Checksum (4 bytes)

	// File structure
	LogFileHdrSize  = 4 * OSFileLogBlockSize // File header size
	LogCheckpoint1  = OSFileLogBlockSize     // Checkpoint 1 offset
	LogCheckpoint2  = 3 * OSFileLogBlockSize // Checkpoint 2 offset
)

// MySQL Log Record Types (mlog_id_t from mtr0types.h)
const (
	MLogRecInsert          = 9
	MLogRecUpdateInPlace   = 13
	MLogRecDelete          = 14
	MLogListEndDelete      = 15
	MLogListStartDelete    = 16
	MLogListEndCopyCreated = 17
	MLogPageReorganize     = 18
	MLogPageCreate         = 19
	MLogUndoInsert         = 20
	MLogUndoEraseEnd       = 21
	MLogUndoInit           = 22
	MLogUndoHdrReuse       = 24
)

// MySQLLogBlockHeader represents the 12-byte log block header
type MySQLLogBlockHeader struct {
	HdrNo         uint32 // Block number
	DataLen       uint16 // Data length in this block
	FirstRecGroup uint16 // Offset to first record group
	EpochNo       uint32 // Epoch number
	Checksum      uint32 // Block checksum (from trailer)
}

// MySQLLogRecord represents a single redo log record
type MySQLLogRecord struct {
	Type       uint8  // Log record type (mlog_id_t)
	SpaceID    uint32 // Tablespace ID (if applicable)
	PageNo     uint32 // Page number (if applicable)
	DataLength uint16 // Length of record data
	Data       []byte // Record payload
}

// MySQLRedoLogReader implements RedoLogReader for actual MySQL format
type MySQLRedoLogReader struct {
	file         *os.File
	currentBlock MySQLLogBlockHeader
	blockData    []byte
	dataOffset   int
	position     int64
}

// NewMySQLRedoLogReader creates a new MySQL format redo log reader
func NewMySQLRedoLogReader() *MySQLRedoLogReader {
	return &MySQLRedoLogReader{
		blockData: make([]byte, LogBlockDataSize),
	}
}

// Open opens the MySQL redo log file
func (r *MySQLRedoLogReader) Open(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	r.file = file
	return nil
}

// ReadHeader reads the MySQL redo log file header
func (r *MySQLRedoLogReader) ReadHeader() (*types.RedoLogHeader, error) {
	// Skip to after file header (first actual log block)
	_, err := r.file.Seek(LogFileHdrSize, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to seek to log blocks: %w", err)
	}
	r.position = LogFileHdrSize

	// Read first block header to get basic info
	blockHeader, err := r.readBlockHeader()
	if err != nil {
		return nil, fmt.Errorf("failed to read first block header: %w", err)
	}

	// Create a simplified header for compatibility
	header := &types.RedoLogHeader{
		LogGroupID:     uint64(blockHeader.EpochNo)<<32 | uint64(blockHeader.HdrNo),
		StartLSN:       uint64(LogFileHdrSize), // Start after file header
		FileNo:         1,
		Created:        time.Now(), // Would need to parse from file header
		LastCheckpoint: 0,
		Format:         2, // Indicate MySQL format
	}

	// Reset to beginning of log data for record reading
	r.file.Seek(LogFileHdrSize, io.SeekStart)
	r.position = LogFileHdrSize
	
	return header, nil
}

// readBlockHeader reads a 12-byte MySQL log block header
func (r *MySQLRedoLogReader) readBlockHeader() (*MySQLLogBlockHeader, error) {
	headerBytes := make([]byte, LogBlockHdrSize)
	n, err := r.file.Read(headerBytes)
	if err != nil {
		return nil, err
	}
	if n < LogBlockHdrSize {
		return nil, io.EOF
	}
	r.position += LogBlockHdrSize

	header := &MySQLLogBlockHeader{
		HdrNo:         binary.LittleEndian.Uint32(headerBytes[LogBlockHdrNo:LogBlockHdrNo+4]),
		DataLen:       binary.LittleEndian.Uint16(headerBytes[LogBlockHdrDataLen:LogBlockHdrDataLen+2]),
		FirstRecGroup: binary.LittleEndian.Uint16(headerBytes[LogBlockFirstRecGroup:LogBlockFirstRecGroup+2]),
		EpochNo:       binary.LittleEndian.Uint32(headerBytes[LogBlockEpochNo:LogBlockEpochNo+4]),
	}

	return header, nil
}

// ReadRecord reads the next log record
func (r *MySQLRedoLogReader) ReadRecord() (*types.LogRecord, error) {
	// Search for a valid record type across blocks if needed
	for {
		// If we need to read a new block
		if r.dataOffset >= len(r.blockData) || r.dataOffset == 0 {
			err := r.readNextBlock()
			if err != nil {
				return nil, err
			}
		}

		// Search for a valid record type in the current block
		for r.dataOffset < len(r.blockData) {
			// Check if we have enough data for a basic record (at least 2 bytes)
			if r.dataOffset+2 > len(r.blockData) {
				// Need to read next block
				break
			}

			// Read potential record type (first byte)
			recordType := r.blockData[r.dataOffset]
			
			// Validate that this is a valid MySQL mlog_id_t value (1-76, excluding 0)
			if recordType == 0 || recordType > 76 {
				// Skip this byte and continue searching for valid record type
				r.dataOffset++
				continue
			}

			// Found a valid record type, advance offset and continue parsing
			r.dataOffset++
			return r.parseValidRecord(recordType)
		}
		
		// If we reach here, we need to read the next block
		r.dataOffset = len(r.blockData)
	}
}

// parseValidRecord parses a record with a validated record type
func (r *MySQLRedoLogReader) parseValidRecord(recordType uint8) (*types.LogRecord, error) {
	// Parse record structure based on actual MySQL format discovered from source
	var recordLength uint32 = 1
	var spaceID uint32 = 0
	var pageNo uint32 = 0
	var recordData []byte
	
	// Parse based on actual MySQL mlog record structure
	remainingData := len(r.blockData) - r.dataOffset
	
	if remainingData >= 4 {
		switch recordType {
		case 1, 2, 4, 8: // MLOG_1BYTE, 2BYTES, 4BYTES, 8BYTES
			// Format from mlog_parse_nbytes: offset(2) + compressed_value
			if remainingData >= 6 && r.dataOffset+6 <= len(r.blockData) {
				offset := binary.LittleEndian.Uint16(r.blockData[r.dataOffset:r.dataOffset+2])
				r.dataOffset += 2
				
				// Read the value (simplified - real MySQL uses compressed integers)
				valueBytes := binary.LittleEndian.Uint32(r.blockData[r.dataOffset:r.dataOffset+4])
				r.dataOffset += 4
				recordLength += 6
				
				recordData = []byte(fmt.Sprintf("offset=%d value=0x%x", offset, valueBytes))
			}
			
		case 9, 13, 14: // INSERT, UPDATE, DELETE records
			// These often contain space_id and page_no
			if remainingData >= 8 && r.dataOffset+8 <= len(r.blockData) {
				spaceID = binary.LittleEndian.Uint32(r.blockData[r.dataOffset:r.dataOffset+4])
				r.dataOffset += 4
				pageNo = binary.LittleEndian.Uint32(r.blockData[r.dataOffset:r.dataOffset+4])
				r.dataOffset += 4
				recordLength += 8
				
				// Try to parse additional data as potential string/row data
				remainingAfterSpacePage := len(r.blockData) - r.dataOffset
				if remainingAfterSpacePage > 0 {
					extraDataLen := remainingAfterSpacePage
					if extraDataLen > 128 { // Limit to reasonable size
						extraDataLen = 128
					}
					
					// Ensure we don't read beyond blockData bounds
					if r.dataOffset+extraDataLen <= len(r.blockData) {
						extraData := r.blockData[r.dataOffset:r.dataOffset+extraDataLen]
						r.dataOffset += extraDataLen
						recordLength += uint32(extraDataLen)
						
						// Try to extract readable strings from the data
						readableData := extractReadableStrings(extraData)
						if len(readableData) > 0 {
							recordData = []byte(fmt.Sprintf("space=%d page=%d data=%s", spaceID, pageNo, readableData))
						} else {
							recordData = []byte(fmt.Sprintf("space=%d page=%d hex=%x", spaceID, pageNo, extraData))
						}
					} else {
						recordData = []byte(fmt.Sprintf("space=%d page=%d", spaceID, pageNo))
					}
				} else {
					recordData = []byte(fmt.Sprintf("space=%d page=%d", spaceID, pageNo))
				}
			}
			
		default:
			// For other record types, try mlog_parse_string format: offset(2) + len(2) + data
			if remainingData >= 4 && r.dataOffset+4 <= len(r.blockData) {
				offset := binary.LittleEndian.Uint16(r.blockData[r.dataOffset:r.dataOffset+2])
				length := binary.LittleEndian.Uint16(r.blockData[r.dataOffset+2:r.dataOffset+4])
				r.dataOffset += 4
				recordLength += 4
				
				// Try to read string data if length is reasonable
				remainingAfterHeader := len(r.blockData) - r.dataOffset
				if length > 0 && int(length) <= remainingAfterHeader && length <= 256 && r.dataOffset+int(length) <= len(r.blockData) {
					stringData := r.blockData[r.dataOffset:r.dataOffset+int(length)]
					r.dataOffset += int(length)
					recordLength += uint32(length)
					
					readableStr := extractReadableStrings(stringData)
					if len(readableStr) > 0 {
						recordData = []byte(fmt.Sprintf("offset=%d len=%d str='%s'", offset, length, readableStr))
					} else {
						recordData = []byte(fmt.Sprintf("offset=%d len=%d hex=%x", offset, length, stringData))
					}
				} else {
					// Maybe it's a different format, try to read what we can
					maxRead := remainingAfterHeader
					if maxRead > 64 {
						maxRead = 64
					}
					if maxRead > 0 && r.dataOffset+maxRead <= len(r.blockData) {
						someData := r.blockData[r.dataOffset:r.dataOffset+maxRead]
						r.dataOffset += maxRead
						recordLength += uint32(maxRead)
						
						readableStr := extractReadableStrings(someData)
						if len(readableStr) > 0 {
							recordData = []byte(fmt.Sprintf("offset=%d badlen=%d data=%s", offset, length, readableStr))
						} else {
							recordData = []byte(fmt.Sprintf("offset=%d badlen=%d hex=%x", offset, length, someData))
						}
					} else {
						recordData = []byte(fmt.Sprintf("offset=%d len=%d", offset, length))
					}
				}
			} else {
				// Not enough data for structured parsing
				recordData = []byte{recordType}
			}
		}
	} else {
		// Not enough data for structured parsing
		recordData = []byte{recordType}
	}

	record := &types.LogRecord{
		Type:          types.LogType(recordType), // Store raw type for now
		LSN:           uint64(r.position + int64(r.dataOffset)),
		Length:        recordLength,
		TransactionID: 0, // Not directly available in redo log records
		Timestamp:     time.Now(), // Not stored in redo log
		TableID:       0, // Would need complex parsing to extract
		SpaceID:       spaceID,
		PageNo:        pageNo,
		Data:          recordData,
		Checksum:      r.currentBlock.Checksum,
	}

	// Note: r.dataOffset has already been advanced by the parsing logic above
	// Don't skip to end of block - continue parsing from current position
	
	return record, nil
}

// readNextBlock reads the next 512-byte log block
func (r *MySQLRedoLogReader) readNextBlock() error {
	// Read block header
	header, err := r.readBlockHeader()
	if err != nil {
		return err
	}
	r.currentBlock = *header

	// Read block data (496 bytes)
	dataBytes := make([]byte, LogBlockDataSize)
	n, err := r.file.Read(dataBytes)
	if err != nil {
		return err
	}
	if n < LogBlockDataSize {
		return io.EOF
	}
	r.position += LogBlockDataSize
	
	// Only use valid data length
	dataLen := int(header.DataLen) - LogBlockHdrSize
	if dataLen > 0 && dataLen <= len(dataBytes) {
		r.blockData = dataBytes[:dataLen]
	} else if dataLen > len(dataBytes) {
		// DataLen header is larger than available data, use all available data
		r.blockData = dataBytes
	} else {
		// DataLen is 0 or negative, use all available data
		r.blockData = dataBytes
	}
	r.dataOffset = 0

	// Read and verify checksum (4 bytes)
	checksumBytes := make([]byte, LogBlockTrlSize)
	_, err = r.file.Read(checksumBytes)
	if err != nil {
		return err
	}
	r.position += LogBlockTrlSize
	
	// Calculate and store the actual checksum for verification
	r.currentBlock.Checksum = binary.LittleEndian.Uint32(checksumBytes)
	
	// Calculate expected checksum (CRC32 of header + data)
	blockData := make([]byte, OSFileLogBlockSize-LogBlockTrlSize)
	// Reconstruct block for checksum calculation
	binary.LittleEndian.PutUint32(blockData[LogBlockHdrNo:], header.HdrNo)
	binary.LittleEndian.PutUint16(blockData[LogBlockHdrDataLen:], header.DataLen)
	binary.LittleEndian.PutUint16(blockData[LogBlockFirstRecGroup:], header.FirstRecGroup)
	binary.LittleEndian.PutUint32(blockData[LogBlockEpochNo:], header.EpochNo)
	copy(blockData[LogBlockHdrSize:], dataBytes)
	
	// Note: We would need to implement CRC32 calculation here for full verification
	// For now, we just store the checksum value for display

	return nil
}

// extractReadableStrings extracts readable ASCII strings from binary data
func extractReadableStrings(data []byte) string {
	var result []string
	var current []byte
	
	for _, b := range data {
		if b >= 32 && b <= 126 { // Printable ASCII
			current = append(current, b)
		} else {
			if len(current) >= 3 { // Only keep strings of 3+ chars
				result = append(result, string(current))
			}
			current = nil
		}
	}
	
	// Don't forget the last string
	if len(current) >= 3 {
		result = append(result, string(current))
	}
	
	if len(result) == 0 {
		return ""
	}
	
	// Join strings with "|" separator and show actual content
	var formattedResult []string
	for _, str := range result {
		if len(str) > 30 {
			formattedResult = append(formattedResult, str[:30]+"...")
		} else {
			formattedResult = append(formattedResult, str)
		}
	}
	return strings.Join(formattedResult, "|")
}

// getRecordTypeName converts MySQL mlog_id_t to string
func getRecordTypeName(recordType uint8) string {
	switch recordType {
	case MLogRecInsert:
		return "INSERT"
	case MLogRecUpdateInPlace:
		return "UPDATE"
	case MLogRecDelete:
		return "DELETE"
	case MLogListEndDelete:
		return "LIST_END_DELETE"
	case MLogListStartDelete:
		return "LIST_START_DELETE"
	case MLogListEndCopyCreated:
		return "LIST_END_COPY_CREATED"
	case MLogPageReorganize:
		return "PAGE_REORGANIZE"
	case MLogPageCreate:
		return "PAGE_CREATE"
	case MLogUndoInsert:
		return "UNDO_INSERT"
	case MLogUndoEraseEnd:
		return "UNDO_ERASE_END"
	case MLogUndoInit:
		return "UNDO_INIT"
	case MLogUndoHdrReuse:
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

// Remaining methods for interface compatibility
func (r *MySQLRedoLogReader) Seek(offset int64) error {
	_, err := r.file.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}
	r.position = offset
	return nil
}

func (r *MySQLRedoLogReader) IsEOF() bool {
	if r.file == nil {
		return true
	}
	// Try to read one byte ahead to check EOF
	currentPos, _ := r.file.Seek(0, io.SeekCurrent)
	_, err := r.file.Read(make([]byte, 1))
	if err == io.EOF {
		return true
	}
	// Seek back
	r.file.Seek(currentPos, io.SeekStart)
	return false
}

func (r *MySQLRedoLogReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}