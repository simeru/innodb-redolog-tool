package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/yamaru/innodb-redolog-tool/internal/reader"
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

var (
	filename = flag.String("file", "", "InnoDB redo log file to analyze")
	verbose  = flag.Bool("v", false, "Verbose output")
)

type RedoLogApp struct {
	app           *tview.Application
	recordList    *tview.List
	detailsText   *tview.TextView
	records       []*types.LogRecord
	header        *types.RedoLogHeader
}

func main() {
	flag.Parse()

	if *filename == "" {
		fmt.Printf("Usage: %s -file <redo_log_file>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Load redo log data
	records, header, err := loadRedoLogData(*filename)
	if err != nil {
		fmt.Printf("Error loading redo log: %v\n", err)
		os.Exit(1)
	}

	// Create and run TUI app
	app := NewRedoLogApp(records, header)
	if err := app.Run(); err != nil {
		fmt.Printf("Error running application: %v\n", err)
		os.Exit(1)
	}
}

func NewRedoLogApp(records []*types.LogRecord, header *types.RedoLogHeader) *RedoLogApp {
	app := &RedoLogApp{
		records: records,
		header:  header,
	}

	// Create main application
	app.app = tview.NewApplication()

	// Create record list (left pane)
	app.recordList = tview.NewList()
	app.recordList.SetBorder(true)
	app.recordList.SetTitle(" Records ")
	app.recordList.ShowSecondaryText(false)

	// Create details text view (right pane)
	app.detailsText = tview.NewTextView()
	app.detailsText.SetBorder(true)
	app.detailsText.SetTitle(" Record Details ")
	app.detailsText.SetDynamicColors(true)
	app.detailsText.SetScrollable(true)

	// Populate record list
	for i, record := range records {
		recordNum := fmt.Sprintf("Record %d", i+1)
		recordType := record.Type.String()
		listItem := fmt.Sprintf("%-12s %s", recordNum, recordType)
		
		app.recordList.AddItem(listItem, "", 0, nil)
	}

	// Set up selection change handler (automatic update on arrow key selection)
	app.recordList.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if index < len(app.records) {
			app.showRecordDetails(index)
		}
	})

	// Set up key bindings
	app.recordList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Up arrow should go to previous record (smaller index)
			current := app.recordList.GetCurrentItem()
			if current > 0 {
				app.recordList.SetCurrentItem(current - 1)
			}
			return nil
		case tcell.KeyDown:
			// Down arrow should go to next record (larger index)
			current := app.recordList.GetCurrentItem()
			if current < len(app.records)-1 {
				app.recordList.SetCurrentItem(current + 1)
			}
			return nil
		case tcell.KeyTab:
			app.app.SetFocus(app.detailsText)
			return nil
		case tcell.KeyEscape:
			app.app.Stop()
			return nil
		case tcell.KeyEnter:
			// Enter now just switches to details pane since selection auto-updates
			app.app.SetFocus(app.detailsText)
			return nil
		}
		return event
	})

	app.detailsText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Up arrow should go to previous record (smaller index)
			current := app.recordList.GetCurrentItem()
			if current > 0 {
				app.recordList.SetCurrentItem(current - 1)
			}
			return nil
		case tcell.KeyDown:
			// Down arrow should go to next record (larger index)
			current := app.recordList.GetCurrentItem()
			if current < len(app.records)-1 {
				app.recordList.SetCurrentItem(current + 1)
			}
			return nil
		case tcell.KeyTab:
			app.app.SetFocus(app.recordList)
			return nil
		case tcell.KeyEscape:
			app.app.Stop()
			return nil
		}
		return event
	})

	// Show header info initially
	app.showHeaderInfo()

	// Show first record if available
	if len(records) > 0 {
		app.showRecordDetails(0)
		app.recordList.SetCurrentItem(0)
	}

	return app
}

func (app *RedoLogApp) showHeaderInfo() {
	headerInfo := fmt.Sprintf(`[yellow]InnoDB Redo Log Header[white]

Log Group ID: %d
Start LSN: %d  
File Number: %d
Created: %s
Last Checkpoint: %d
Format: %d

Total Records: %d

[blue]Navigation:[white]
↑/↓: Navigate records (auto-update details)
Tab: Switch panes  
Enter: Focus details pane
Esc: Exit
`,
		app.header.LogGroupID,
		app.header.StartLSN,
		app.header.FileNo,
		app.header.Created.Format("2006-01-02 15:04:05"),
		app.header.LastCheckpoint,
		app.header.Format,
		len(app.records))

	app.detailsText.SetText(headerInfo)
}

func (app *RedoLogApp) showRecordDetails(index int) {
	if index >= len(app.records) {
		return
	}

	record := app.records[index]
	
	details := fmt.Sprintf(`[yellow]Record %d Details[white]

[green]Type:[white]           %s
[green]LSN:[white]            %d
[green]Length:[white]         %d bytes
[green]Transaction ID:[white] %d
[green]Timestamp:[white]      %s
[green]Table ID:[white]       %d
[green]Index ID:[white]       %d
[green]Space ID:[white]       %d
[green]Page Number:[white]    %d
[green]Offset:[white]         %d
[green]Checksum:[white]       0x%08X

[green]Data:[white]
`,
		index+1,
		record.Type.String(),
		record.LSN,
		record.Length,
		record.TransactionID,
		record.Timestamp.Format("2006-01-02 15:04:05"),
		record.TableID,
		record.IndexID,
		record.SpaceID,
		record.PageNo,
		record.Offset,
		record.Checksum)

	if len(record.Data) > 0 {
		details += fmt.Sprintf("%s (%d bytes)", string(record.Data), len(record.Data))
	} else {
		details += "(empty)"
	}

	app.detailsText.SetText(details)
	app.recordList.SetCurrentItem(index)
}

func (app *RedoLogApp) Run() error {
	// Create main layout
	flex := tview.NewFlex()
	flex.AddItem(app.recordList, 0, 1, true)   // Left pane (1/3)
	flex.AddItem(app.detailsText, 0, 2, false) // Right pane (2/3)

	app.app.SetRoot(flex, true)
	app.app.SetFocus(app.recordList)

	return app.app.Run()
}

func loadRedoLogData(filename string) ([]*types.LogRecord, *types.RedoLogHeader, error) {
	// Create appropriate reader
	readerInstance, err := createReader(filename, *verbose)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create reader: %w", err)
	}
	defer readerInstance.Close()

	// Open the file
	if err := readerInstance.Open(filename); err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}

	// Read header
	header, err := readerInstance.ReadHeader()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read header: %w", err)
	}

	if *verbose {
		fmt.Printf("Loading redo log file: %s\n", filename)
		fmt.Printf("Detected format: %d\n", header.Format)
	}

	// Read all records
	var records []*types.LogRecord
	recordCount := 0
	maxRecords := 10000 // Limit for performance

	for recordCount < maxRecords {
		record, err := readerInstance.ReadRecord()
		if err != nil {
			if readerInstance.IsEOF() {
				break
			}
			return nil, nil, fmt.Errorf("failed to read record %d: %w", recordCount+1, err)
		}

		records = append(records, record)
		recordCount++
	}

	if *verbose {
		fmt.Printf("Loaded %d records\n", len(records))
	}

	return records, header, nil
}

func createReader(filename string, verbose bool) (reader.RedoLogReader, error) {
	if info, err := os.Stat(filename); err == nil {
		// MySQL redo logs are typically large (3MB+), test fixtures are small
		if info.Size() > 1000000 { // > 1MB suggests MySQL format
			if verbose {
				fmt.Printf("Detected MySQL format (size: %d bytes)\n", info.Size())
			}
			return reader.NewMySQLRedoLogReader(), nil
		}
	}

	if verbose {
		fmt.Printf("Using test format reader\n")
	}
	return reader.NewRedoLogReader(), nil
}