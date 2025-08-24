package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/yamaru/innodb-redolog-tool/internal/reader"
)

func main() {
	var (
		filename = flag.String("file", "", "InnoDB redo log file to analyze")
		verbose  = flag.Bool("v", false, "Verbose output")
	)
	flag.Parse()

	if *filename == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -file <redo_log_file>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("Analyzing redo log file: %s\n", *filename)
	}

	// Detect format and create appropriate reader
	logReader, err := createReader(*filename, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating reader: %v\n", err)
		os.Exit(1)
	}
	defer logReader.Close()

	// Open file
	if err := logReader.Open(*filename); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}

	// Read header
	header, err := logReader.ReadHeader()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading header: %v\n", err)
		os.Exit(1)
	}

	// Display header information
	fmt.Println("=== InnoDB Redo Log Header ===")
	fmt.Printf("Log Group ID: %d\n", header.LogGroupID)
	fmt.Printf("Start LSN: %d\n", header.StartLSN)
	fmt.Printf("File Number: %d\n", header.FileNo)
	fmt.Printf("Created: %s\n", header.Created.Format("2006-01-02 15:04:05"))
	fmt.Printf("Last Checkpoint: %d\n", header.LastCheckpoint)
	fmt.Printf("Format: %d\n", header.Format)
	fmt.Println()

	// Read records
	fmt.Println("=== Log Records ===")
	recordCount := 0
	maxRecords := 100 // Limit output for readability

	for !logReader.IsEOF() && recordCount < maxRecords {
		record, err := logReader.ReadRecord()
		if err != nil {
			if *verbose {
				fmt.Printf("Error reading record %d: %v\n", recordCount, err)
			}
			break
		}

		recordCount++
		fmt.Printf("Record %d:\n", recordCount)
		fmt.Printf("  Type: %s\n", record.Type)
		fmt.Printf("  LSN: %d\n", record.LSN)
		fmt.Printf("  Length: %d bytes\n", record.Length)
		fmt.Printf("  Transaction ID: %d\n", record.TransactionID)
		fmt.Printf("  Timestamp: %s\n", record.Timestamp.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Table ID: %d\n", record.TableID)
		fmt.Printf("  Index ID: %d\n", record.IndexID)
		fmt.Printf("  Space ID: %d\n", record.SpaceID)
		fmt.Printf("  Page Number: %d\n", record.PageNo)
		fmt.Printf("  Offset: %d\n", record.Offset)
		
		if len(record.Data) > 0 {
			fmt.Printf("  Data: %s (%d bytes)\n", string(record.Data), len(record.Data))
		} else {
			fmt.Printf("  Data: (empty)\n")
		}
		fmt.Printf("  Checksum: 0x%08X\n", record.Checksum)
		fmt.Println()
	}

	if recordCount >= maxRecords {
		fmt.Printf("... (showing first %d records)\n", maxRecords)
	}

	fmt.Printf("Total records analyzed: %d\n", recordCount)
	
	// Display file statistics
	if info, err := os.Stat(*filename); err == nil {
		fmt.Println("\n=== File Statistics ===")
		fmt.Printf("File size: %d bytes\n", info.Size())
		fmt.Printf("Header size: 64 bytes\n")
		fmt.Printf("Records size: %d bytes\n", info.Size()-64)
		if recordCount > 0 {
			fmt.Printf("Average record size: %.1f bytes\n", float64(info.Size()-64)/float64(recordCount))
		}
	}
}

// createReader detects the redo log format and creates appropriate reader
func createReader(filename string, verbose bool) (reader.RedoLogReader, error) {
	// Check file size to determine format
	if info, err := os.Stat(filename); err == nil {
		// MySQL redo logs are typically large (3MB+), test fixtures are small
		if info.Size() > 1000000 { // > 1MB suggests MySQL format
			if verbose {
				fmt.Printf("Detected MySQL format (size: %d bytes)\n", info.Size())
			}
			return reader.NewMySQLRedoLogReader(), nil
		}
	}
	
	// Default to test format for small files
	if verbose {
		fmt.Printf("Using test format reader\n")
	}
	return reader.NewRedoLogReader(), nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}