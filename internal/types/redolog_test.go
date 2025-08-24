package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogType_String(t *testing.T) {
	tests := []struct {
		logType  LogType
		expected string
	}{
		{LogType(9), "MLOG_REC_INSERT_8027"},       // MySQL MLOG_REC_INSERT_8027
		{LogType(13), "MLOG_REC_UPDATE_IN_PLACE_8027"}, // MySQL MLOG_REC_UPDATE_IN_PLACE_8027
		{LogType(14), "MLOG_REC_DELETE_8027"},      // MySQL MLOG_REC_DELETE_8027
		{LogType(1), "MLOG_1BYTE"},        // MySQL MLOG_1BYTE
		{LogType(2), "MLOG_2BYTES"},       // MySQL MLOG_2BYTES
		{LogType(4), "MLOG_4BYTES"},       // MySQL MLOG_4BYTES
		{LogType(8), "MLOG_8BYTES"},       // MySQL MLOG_8BYTES
		{LogType(67), "MLOG_REC_INSERT"},  // Current format INSERT
		{LogType(69), "MLOG_REC_DELETE"},  // Current format DELETE
		{LogType(0), "INVALID_MLOG_0 (should not exist)"}, // Invalid type 0
		{LogType(99), "INVALID_MLOG_99 (exceeds MLOG_BIGGEST_TYPE=76)"}, // Invalid type > 76
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.logType.String())
		})
	}
}

func TestLogType_IsTransactional(t *testing.T) {
	tests := []struct {
		logType       LogType
		transactional bool
	}{
		// Old 8027 format transactional operations
		{LogType(9), true},   // MLOG_REC_INSERT_8027
		{LogType(10), true},  // MLOG_REC_CLUST_DELETE_MARK_8027
		{LogType(13), true},  // MLOG_REC_UPDATE_IN_PLACE_8027  
		{LogType(14), true},  // MLOG_REC_DELETE_8027
		// Compact format transactional operations
		{LogType(38), true},  // MLOG_COMP_REC_INSERT_8027
		{LogType(39), true},  // MLOG_COMP_REC_CLUST_DELETE_MARK_8027
		{LogType(41), true},  // MLOG_COMP_REC_UPDATE_IN_PLACE_8027
		{LogType(42), true},  // MLOG_COMP_REC_DELETE_8027
		// Current format transactional operations
		{LogType(67), true},  // MLOG_REC_INSERT
		{LogType(68), true},  // MLOG_REC_CLUST_DELETE_MARK
		{LogType(69), true},  // MLOG_REC_DELETE
		{LogType(70), true},  // MLOG_REC_UPDATE_IN_PLACE
		// Non-transactional operations
		{LogType(1), false},  // MLOG_1BYTE
		{LogType(2), false},  // MLOG_2BYTES
		{LogType(4), false},  // MLOG_4BYTES
		{LogType(8), false},  // MLOG_8BYTES
		{LogType(19), false}, // MLOG_PAGE_CREATE
		{LogType(30), false}, // MLOG_WRITE_STRING
	}

	for _, tt := range tests {
		t.Run(tt.logType.String(), func(t *testing.T) {
			assert.Equal(t, tt.transactional, tt.logType.IsTransactional())
		})
	}
}

func TestLogRecord_Creation(t *testing.T) {
	timestamp := time.Now()
	record := &LogRecord{
		Type:          LogType(9), // MySQL INSERT
		Length:        100,
		LSN:           12345,
		Timestamp:     timestamp,
		TransactionID: 67890,
		TableID:       1,
		IndexID:       0,
		Data:          []byte("test data"),
		Checksum:      0xABCDEF,
		SpaceID:       0,
		PageNo:        1,
		Offset:        128,
	}

	assert.Equal(t, LogType(9), record.Type)
	assert.Equal(t, uint32(100), record.Length)
	assert.Equal(t, uint64(12345), record.LSN)
	assert.Equal(t, timestamp, record.Timestamp)
	assert.Equal(t, uint64(67890), record.TransactionID)
	assert.Equal(t, uint32(1), record.TableID)
	assert.Equal(t, uint32(0), record.IndexID)
	assert.Equal(t, []byte("test data"), record.Data)
	assert.Equal(t, uint32(0xABCDEF), record.Checksum)
	assert.Equal(t, uint32(0), record.SpaceID)
	assert.Equal(t, uint32(1), record.PageNo)
	assert.Equal(t, uint16(128), record.Offset)
}

func TestRedoLogHeader_Creation(t *testing.T) {
	created := time.Date(2024, 8, 24, 12, 0, 0, 0, time.UTC)
	header := &RedoLogHeader{
		LogGroupID:     1,
		StartLSN:       1000,
		FileNo:         1,
		Created:        created,
		LastCheckpoint: 5000,
		Format:         1,
	}

	assert.Equal(t, uint64(1), header.LogGroupID)
	assert.Equal(t, uint64(1000), header.StartLSN)
	assert.Equal(t, uint32(1), header.FileNo)
	assert.Equal(t, created, header.Created)
	assert.Equal(t, uint64(5000), header.LastCheckpoint)
	assert.Equal(t, uint32(1), header.Format)
}

func TestRedoLogStats_Creation(t *testing.T) {
	startTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	
	stats := &RedoLogStats{
		TotalRecords:     1000,
		RecordsByType:    make(map[LogType]uint64),
		SizeInBytes:      1024 * 1024,
		TransactionCount: 100,
		TimeRange: struct {
			Start time.Time
			End   time.Time
		}{
			Start: startTime,
			End:   endTime,
		},
	}

	stats.RecordsByType[LogType(9)] = 300   // INSERT
	stats.RecordsByType[LogType(13)] = 200  // UPDATE
	stats.RecordsByType[LogType(1)] = 100   // 1BYTE

	require.NotNil(t, stats)
	assert.Equal(t, uint64(1000), stats.TotalRecords)
	assert.Equal(t, uint64(1024*1024), stats.SizeInBytes)
	assert.Equal(t, uint64(100), stats.TransactionCount)
	assert.Equal(t, uint64(300), stats.RecordsByType[LogType(9)])   // INSERT
	assert.Equal(t, uint64(200), stats.RecordsByType[LogType(13)])  // UPDATE
	assert.Equal(t, uint64(100), stats.RecordsByType[LogType(1)])   // 1BYTE
	assert.Equal(t, startTime, stats.TimeRange.Start)
	assert.Equal(t, endTime, stats.TimeRange.End)
}