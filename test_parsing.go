package main

import (
	"fmt"
	"os"
	
	"github.com/yamaru/innodb-redolog-tool/internal/reader"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run test_parsing.go <redo_log_file>")
		os.Exit(1)
	}
	
	filename := os.Args[1]
	
	// Create MySQL reader
	readerInstance := reader.NewMySQLRedoLogReader()
	defer readerInstance.Close()
	
	// Open file
	if err := readerInstance.Open(filename); err != nil {
		fmt.Printf("Failed to open file: %v\n", err)
		os.Exit(1)
	}
	
	// Read header
	header, err := readerInstance.ReadHeader()
	if err != nil {
		fmt.Printf("Failed to read header: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Header - Format: %d, StartLSN: %d\n", header.Format, header.StartLSN)
	
	// Read first 10 records
	fmt.Println("\nFirst 10 records:")
	for i := 0; i < 10; i++ {
		record, err := readerInstance.ReadRecord()
		if err != nil {
			if readerInstance.IsEOF() {
				fmt.Println("Reached EOF")
				break
			}
			fmt.Printf("Error reading record %d: %v\n", i+1, err)
			break
		}
		
		fmt.Printf("Record %d: Type=%s, LSN=%d, Length=%d, Data=%s\n", 
			i+1, record.Type.String(), record.LSN, record.Length, string(record.Data))
	}
}