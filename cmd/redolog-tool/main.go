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

	// Populate record list with multi-record group visualization
	groupColors := []string{"[white]", "[cyan]", "[yellow]", "[green]", "[magenta]", "[blue]"}
	for i, record := range records {
		recordNum := fmt.Sprintf("%d", i+1)
		recordType := record.Type.String()
		// This variable is now replaced by idInfo below
		
		// Add visual grouping indicators
		var groupIndicator string
		var colorPrefix string
		
		if record.MultiRecordGroup > 0 {
			// Use different colors for different groups
			colorIndex := (record.MultiRecordGroup - 1) % len(groupColors)
			colorPrefix = groupColors[colorIndex]
			
			if record.IsGroupStart {
				groupIndicator = "┌─ "
			} else if record.IsGroupEnd {
				groupIndicator = "└─ "
			} else {
				groupIndicator = "├─ "
			}
		} else {
			colorPrefix = "[white]"
			groupIndicator = "   "
		}
		
		// Show SpaceID if available, otherwise TableID
		var idInfo string
		if record.SpaceID != 0 {
			idInfo = fmt.Sprintf("(S:%d)", record.SpaceID) // Space ID
		} else if record.TableID != 0 {
			idInfo = fmt.Sprintf("(T:%d)", record.TableID) // Table ID
		} else {
			idInfo = "(0)" // Default
		}
		
		listItem := fmt.Sprintf("%s%s%-6s %s%s", colorPrefix, groupIndicator, recordNum, recordType, idInfo)
		
		app.recordList.AddItem(listItem, "", 0, nil)
	}

	// Set up selection change handler (automatic update on arrow key selection)
	app.recordList.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if index < len(app.records) {
			app.showRecordDetails(index)
		}
	})

	// Set up click handler (automatic update on mouse click selection)
	app.recordList.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if index < len(app.records) {
			app.showRecordDetails(index)
		}
	})

	// Set up mouse handler for click and scroll support
	app.recordList.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick {
			// Handle mouse clicks manually to ensure they work
			_, y := event.Position()
			// Convert screen coordinates to list item index
			_, _, _, height := app.recordList.GetRect()
			if y >= 1 && y < height-1 && len(app.records) > 0 { // Account for borders
				itemIndex := y - 1 // Subtract 1 for top border
				if itemIndex >= 0 && itemIndex < len(app.records) {
					app.recordList.SetCurrentItem(itemIndex)
					return action, event
				}
			}
		} else if action == tview.MouseScrollUp {
			// Scroll up - move to previous record
			current := app.recordList.GetCurrentItem()
			if current > 0 {
				app.recordList.SetCurrentItem(current - 1)
			}
			return action, event
		} else if action == tview.MouseScrollDown {
			// Scroll down - move to next record
			current := app.recordList.GetCurrentItem()
			if current < len(app.records)-1 {
				app.recordList.SetCurrentItem(current + 1)
			}
			return action, event
		}
		return action, event
	})

	// Set up key bindings
	app.recordList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Up arrow should go to previous record (smaller index) - natural list navigation
			current := app.recordList.GetCurrentItem()
			if current > 0 {
				app.recordList.SetCurrentItem(current - 1)
			}
			return nil
		case tcell.KeyDown:
			// Down arrow should go to next record (larger index) - natural list navigation
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
		// Check for character keys
		if event.Rune() == 'q' || event.Rune() == 'Q' {
			app.app.Stop()
			return nil
		}
		return event
	})

	app.detailsText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Up arrow should go to previous record (smaller index) - natural list navigation
			current := app.recordList.GetCurrentItem()
			if current > 0 {
				app.recordList.SetCurrentItem(current - 1)
			}
			return nil
		case tcell.KeyDown:
			// Down arrow should go to next record (larger index) - natural list navigation
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
		// Check for character keys
		if event.Rune() == 'q' || event.Rune() == 'Q' {
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
Esc/q: Exit
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
	
	// Add group information to details
	var groupInfo string
	if record.MultiRecordGroup > 0 {
		groupStatus := ""
		if record.IsGroupStart {
			groupStatus = " (Group Start)"
		} else if record.IsGroupEnd {
			groupStatus = " (Group End)"
		} else {
			groupStatus = " (Group Member)"
		}
		groupInfo = fmt.Sprintf("[green]Multi-Record Group:[white] %d%s\n", record.MultiRecordGroup, groupStatus)
	}
	
	details := fmt.Sprintf(`[yellow]Record %d Details[white]

[green]Type:[white]           %s
[green]LSN:[white]            %d
[green]Length:[white]         %d bytes
[green]Transaction ID:[white] %d
[green]Timestamp:[white]      %s
[green]Space ID:[white]       %d
[green]Table ID:[white]       %d
[green]Index ID:[white]       %d
[green]Page Number:[white]    %d
[green]Offset:[white]         %d
[green]Checksum:[white]       0x%08X
%s
[green]Data:[white]
`,
		index+1,
		record.Type.String(),
		record.LSN,
		record.Length,
		record.TransactionID,
		record.Timestamp.Format("2006-01-02 15:04:05"),
		record.SpaceID,
		record.TableID,
		record.IndexID,
		record.PageNo,
		record.Offset,
		record.Checksum,
		groupInfo)

	if len(record.Data) > 0 {
		details += fmt.Sprintf("%s (%d bytes)", string(record.Data), len(record.Data))
	} else {
		details += "(empty)"
	}

	app.detailsText.SetText(details)
	// Remove SetCurrentItem call to prevent infinite loop with SetChangedFunc
}

func (app *RedoLogApp) Run() error {
	// Create main layout
	flex := tview.NewFlex()
	flex.AddItem(app.recordList, 0, 1, true)   // Left pane (1/3)
	flex.AddItem(app.detailsText, 0, 2, false) // Right pane (2/3)

	// Enable mouse support
	app.app.EnableMouse(true)
	
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

	// Post-process records to properly detect multi-record groups
	detectMultiRecordGroups(records)

	return records, header, nil
}

// detectMultiRecordGroups analyzes records to identify multi-record groups
func detectMultiRecordGroups(records []*types.LogRecord) {
	groupID := 0
	groupStart := -1
	
	for i, record := range records {
		// Look for MLOG_MULTI_REC_END (31) to identify group boundaries
		if uint8(record.Type) == 31 { // MLOG_MULTI_REC_END
			if groupStart != -1 {
				// We found a group from groupStart to current position
				groupID++
				
				// Mark all records in this group
				for j := groupStart; j <= i; j++ {
					records[j].MultiRecordGroup = groupID
					if j == groupStart {
						records[j].IsGroupStart = true
					} else if j == i {
						records[j].IsGroupEnd = true
					}
				}
				
				groupStart = -1 // Reset for next group
			} else {
				// Isolated MULTI_REC_END - shouldn't happen normally
				record.MultiRecordGroup = 0
			}
		} else {
			// Regular record
			if groupStart == -1 {
				// This could be the start of a multi-record group or a single record
				// We'll determine this when we see MULTI_REC_END or the next group start
				groupStart = i
			}
		}
	}
	
	// Handle any remaining records that didn't have MULTI_REC_END
	if groupStart != -1 {
		// These are likely single records
		for j := groupStart; j < len(records); j++ {
			records[j].MultiRecordGroup = 0 // Single records
		}
	}
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