package fixtures

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateSampleLogFile creates a sample redo log file for testing
func CreateSampleLogFile(dir string) (string, error) {
	filename := filepath.Join(dir, "sample_redo.log")
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create sample log file: %w", err)
	}
	defer file.Close()

	// Write header
	header := BinaryRedoLogHeader()
	if _, err := file.Write(header); err != nil {
		return "", fmt.Errorf("failed to write header: %w", err)
	}

	// Write sample transaction records
	transaction := SampleTransaction()
	for _, record := range transaction {
		binary := BinaryLogRecord(record)
		if _, err := file.Write(binary); err != nil {
			return "", fmt.Errorf("failed to write record: %w", err)
		}
	}

	return filename, nil
}

// CreateCorruptedLogFile creates a corrupted redo log file for testing
func CreateCorruptedLogFile(dir string) (string, error) {
	filename := filepath.Join(dir, "corrupted_redo.log")
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create corrupted log file: %w", err)
	}
	defer file.Close()

	// Write valid header
	header := BinaryRedoLogHeader()
	if _, err := file.Write(header); err != nil {
		return "", fmt.Errorf("failed to write header: %w", err)
	}

	// Write one valid record
	validRecord := BinaryLogRecord(SampleInsertRecord())
	if _, err := file.Write(validRecord); err != nil {
		return "", fmt.Errorf("failed to write valid record: %w", err)
	}

	// Write corrupted record
	corruptedRecord := BinaryLogRecord(SampleCorruptedRecord())
	if _, err := file.Write(corruptedRecord); err != nil {
		return "", fmt.Errorf("failed to write corrupted record: %w", err)
	}

	return filename, nil
}

// CreateEmptyLogFile creates an empty redo log file for testing
func CreateEmptyLogFile(dir string) (string, error) {
	filename := filepath.Join(dir, "empty_redo.log")
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create empty log file: %w", err)
	}
	file.Close()
	return filename, nil
}

// CreateTruncatedLogFile creates a truncated redo log file for testing
func CreateTruncatedLogFile(dir string) (string, error) {
	filename := filepath.Join(dir, "truncated_redo.log")
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create truncated log file: %w", err)
	}
	defer file.Close()

	// Write only partial header
	header := BinaryRedoLogHeader()
	partialHeader := header[:32] // Only half the header
	if _, err := file.Write(partialHeader); err != nil {
		return "", fmt.Errorf("failed to write partial header: %w", err)
	}

	return filename, nil
}

// CreateLargeLogFile creates a larger log file with multiple transactions for performance testing
func CreateLargeLogFile(dir string, transactionCount int) (string, error) {
	filename := filepath.Join(dir, "large_redo.log")
	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create large log file: %w", err)
	}
	defer file.Close()

	// Write header
	header := BinaryRedoLogHeader()
	if _, err := file.Write(header); err != nil {
		return "", fmt.Errorf("failed to write header: %w", err)
	}

	// Write multiple transactions
	for i := 0; i < transactionCount; i++ {
		transaction := SampleTransaction()
		// Modify transaction ID to make each unique
		for _, record := range transaction {
			record.TransactionID = uint64(12345 + i)
			record.LSN = uint64(1000 + i*10)
		}
		
		for _, record := range transaction {
			binary := BinaryLogRecord(record)
			if _, err := file.Write(binary); err != nil {
				return "", fmt.Errorf("failed to write record in transaction %d: %w", i, err)
			}
		}
	}

	return filename, nil
}

// CleanupTestFiles removes all test files in the specified directory
func CleanupTestFiles(dir string) error {
	pattern := filepath.Join(dir, "*_redo.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			return err
		}
	}
	
	return nil
}