package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/yamaru/innodb-redolog-tool/internal/reader"
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

var (
	filename = flag.String("file", "", "InnoDB redo log file to analyze")
	verbose  = flag.Bool("v", false, "Verbose output")
	testMode = flag.Bool("test", false, "Test hex parsing without TUI")
)

type RedoLogApp struct {
	app           *tview.Application
	recordList    *tview.List
	detailsText   *tview.TextView
	footer        *tview.TextView
	records       []*types.LogRecord
	filteredRecords []*types.LogRecord
	recordIndices []int // Maps filtered index to original index
	header        *types.RedoLogHeader
	showTableID0  bool // Toggle for showing Table ID 0 records
	operationFilter string // "all", "insert", "update", "delete"
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

	// Check if verbose mode is enabled for debug output
	if *verbose {
		// Search for MLOG_TABLE_DYNAMIC_META records (type 62) to show Table IDs
		fmt.Printf("\nSearching for MLOG_TABLE_DYNAMIC_META records with Table IDs:\n")
		count := 0
		for i, record := range records {
			if uint8(record.Type) == 62 { // MLOG_TABLE_DYNAMIC_META
				fmt.Printf("Record %d: %s TableID=%d Data=%s\n", i+1, record.Type.String(), record.TableID, string(record.Data))
				count++
				if count >= 10 { // Limit output
					break
				}
			}
		}
		fmt.Printf("Found %d MLOG_TABLE_DYNAMIC_META records\n\n", count)
		
		// Show filtering statistics
		fmt.Printf("Filtering analysis:\n")
		totalRecords := len(records)
		tableID0Records := 0
		for _, record := range records {
			if record.TableID == 0 && record.SpaceID == 0 {
				tableID0Records++
			}
		}
		nonZeroIDRecords := totalRecords - tableID0Records
		fmt.Printf("Total records: %d\n", totalRecords)
		fmt.Printf("Table ID 0 records (would be hidden): %d\n", tableID0Records)
		fmt.Printf("Non-zero ID records (would be shown): %d\n", nonZeroIDRecords)
		fmt.Printf("Filter effectiveness: %.1f%% reduction\n\n", float64(tableID0Records)/float64(totalRecords)*100)
		
		// Show MLOG_REC_INSERT_8027 record analysis
		fmt.Printf("MLOG_REC_INSERT_8027 record analysis:\n")
		insertCount := 0
		for i, record := range records {
			if uint8(record.Type) == 9 { // MLOG_REC_INSERT_8027
				fmt.Printf("Record %d: %s\n", i+1, record.Type.String())
				fmt.Printf("  LSN: %d\n", record.LSN)
				fmt.Printf("  Length: %d\n", record.Length)
				fmt.Printf("  Space ID: %d\n", record.SpaceID)
				fmt.Printf("  Page No: %d\n", record.PageNo)
				fmt.Printf("  Data: %s\n", string(record.Data))
				fmt.Printf("  Group: %d\n", record.MultiRecordGroup)
				fmt.Printf("\n")
				insertCount++
				if insertCount >= 3 { // Limit to 3 records for readability
					break
				}
			}
		}
		if insertCount == 0 {
			fmt.Printf("No MLOG_REC_INSERT_8027 records found\n")
		}
		fmt.Printf("Found %d MLOG_REC_INSERT_8027 records\n\n", insertCount)
		
		// Test hex parsing with user's example
		fmt.Printf("Hex parsing test (user example):\n")
		testHex := "000000000503c20000"
		hexBytes, _ := hex.DecodeString(testHex)
		if len(hexBytes) > 0 {
			// Test our parsing functions
			fmt.Printf("Input: hex=%s\n", testHex)
			
			// Test as different field types
			if len(hexBytes) >= 4 {
				// Test integer parsing
				fmt.Printf("As 4-byte integer: %d\n", binary.BigEndian.Uint32(hexBytes[:4]))
			}
			
			// Test field parsing with simple heuristics
			fieldResult := testParseFields(hexBytes)
			fmt.Printf("Field parsing result: %s\n", fieldResult)
		}
		fmt.Printf("\n")
		
		// Show footer simulation
		fmt.Printf("Footer display simulation:\n")
		// Simulate what would appear in the footer
		showTableID0 := false // Default: filter ON
		filteredRecords := make([]*types.LogRecord, 0)
		for _, record := range records {
			if !showTableID0 && record.TableID == 0 && record.SpaceID == 0 {
				continue // Skip Table ID 0 records when filter is enabled
			}
			filteredRecords = append(filteredRecords, record)
		}
		
		var filterStatus, filterColor string
		if showTableID0 {
			filterStatus = "OFF"
			filterColor = "[green]"
		} else {
			filterStatus = "ON" 
			filterColor = "[red]"
		}
		fmt.Printf("Footer: Press 's' to toggle Table ID 0 filter | Filter: %s%s | Records: %d/%d\n\n",
			filterColor, filterStatus, len(filteredRecords), len(records))
		
		// Analyze multi-record groups
		fmt.Printf("Multi-record group analysis:\n")
		groupCount := 0
		for i, record := range records {
			if record.MultiRecordGroup > 0 {
				if record.IsGroupStart {
					groupCount++
					fmt.Printf("Group %d starts at Record %d (%s)\n", record.MultiRecordGroup, i+1, record.Type.String())
				}
				if record.IsGroupEnd {
					fmt.Printf("Group %d ends at Record %d (%s)\n", record.MultiRecordGroup, i+1, record.Type.String())
				}
			}
		}
		fmt.Printf("Found %d multi-record groups\n\n", groupCount)
		
		// Show MLOG_MULTI_REC_END records and their context
		fmt.Printf("MLOG_MULTI_REC_END records (type 31) for group detection:\n")
		multiRecEndCount := 0
		for i, record := range records {
			if uint8(record.Type) == 31 { // MLOG_MULTI_REC_END
				fmt.Printf("Record %d: MLOG_MULTI_REC_END Group=%d IsGroupEnd=%v\n", i+1, record.MultiRecordGroup, record.IsGroupEnd)
				multiRecEndCount++
				if multiRecEndCount >= 5 { // Limit output
					break
				}
			}
		}
		fmt.Printf("Found %d MLOG_MULTI_REC_END records total\n\n", multiRecEndCount)
		
		// Show visual group representation (like in TUI)
		fmt.Printf("Visual group representation (first 20 records):\n")
		groupColors := []string{"[white]", "[cyan]", "[yellow]", "[green]", "[magenta]", "[blue]"}
		for i := 0; i < 20 && i < len(records); i++ {
			record := records[i]
			recordNum := fmt.Sprintf("%d", i+1)
			recordType := record.Type.String()
			
			// Add visual grouping indicators like in TUI
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
				idInfo = fmt.Sprintf("(S:%d)", record.SpaceID)
			} else if record.TableID != 0 {
				idInfo = fmt.Sprintf("(T:%d)", record.TableID)
			} else {
				idInfo = "(0)"
			}
			
			fmt.Printf("%s%s%-6s %s%s Group=%d\n", colorPrefix, groupIndicator, recordNum, recordType, idInfo, record.MultiRecordGroup)
		}
		return
	}

	// Test mode: parse records and show cross-block reads with strings
	if *testMode {
		fmt.Printf("Test mode: Searching for records with cross-block reads and VARCHAR strings\n\n")
		
		foundVarcharRecords := 0
		foundCrossBlockRecords := 0
		
		for i, record := range records {
			// Look for MLOG_REC_INSERT_8027 records with actual data
			if record.Type == 9 { // MLOG_REC_INSERT_8027
				dataStr := string(record.Data)
				
				// Check if this record has cross-block read success
				if strings.Contains(dataStr, "cross_block_read=success") {
					foundCrossBlockRecords++
					fmt.Printf("Record %d: MLOG_REC_INSERT_8027 with cross-block read success\n", i+1)
					fmt.Printf("  TableID: %d\n", record.TableID)
					
					// Check if it contains string data
					if strings.Contains(dataStr, "found_strings=") {
						foundVarcharRecords++
						fmt.Printf("  *** Contains VARCHAR strings! ***\n")
					}
					
					if strings.Contains(dataStr, "data_hex=") {
						fmt.Printf("  Data: %s\n", record.Data)
					}
					fmt.Printf("\n")
				}
			}
		}
		
		fmt.Printf("Summary:\n")
		fmt.Printf("- Records with cross-block read success: %d\n", foundCrossBlockRecords)
		fmt.Printf("- Records with VARCHAR strings: %d\n", foundVarcharRecords)
		
		if foundVarcharRecords > 0 {
			fmt.Printf("\n✅ Success! Found VARCHAR strings in sakila_redolog.log records!\n")
		} else if foundCrossBlockRecords > 0 {
			fmt.Printf("\n⚠️  Found cross-block reads but no VARCHAR strings yet. Need to improve string extraction.\n")
		} else {
			fmt.Printf("\n❌ No cross-block reads found. The boundary issue may still exist.\n")
		}
		
		return
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
		showTableID0: false, // Default: hide Table ID 0 records
		operationFilter: "all", // Default: show all operation types
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
	app.detailsText.SetWrap(true)
	app.detailsText.SetWordWrap(true)

	// Create footer (bottom pane)
	app.footer = tview.NewTextView()
	app.footer.SetBorder(true)
	app.footer.SetTitle(" Controls & Navigation ")
	app.footer.SetDynamicColors(true)
	app.footer.SetTextAlign(tview.AlignCenter)
	app.updateFooter()

	// Initialize filtered records
	app.updateFilteredRecords()
	
	// Populate record list with multi-record group visualization
	app.rebuildRecordList()

	// Set up selection change handler (automatic update on arrow key selection)
	app.recordList.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if index < len(app.filteredRecords) {
			app.showRecordDetails(index)
		}
	})

	// Set up click handler (automatic update on mouse click selection)
	app.recordList.SetSelectedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		if index < len(app.filteredRecords) {
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
			if y >= 1 && y < height-1 && len(app.filteredRecords) > 0 { // Account for borders
				itemIndex := y - 1 // Subtract 1 for top border
				if itemIndex >= 0 && itemIndex < len(app.filteredRecords) {
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
			if current < len(app.filteredRecords)-1 {
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
			if current < len(app.filteredRecords)-1 {
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
		if event.Rune() == 's' || event.Rune() == 'S' {
			app.toggleTableID0Filter()
			return nil
		}
		if event.Rune() == 'i' || event.Rune() == 'I' {
			app.toggleOperationFilter("insert")
			return nil
		}
		if event.Rune() == 'u' || event.Rune() == 'U' {
			app.toggleOperationFilter("update")
			return nil
		}
		if event.Rune() == 'd' || event.Rune() == 'D' {
			app.toggleOperationFilter("delete")
			return nil
		}
		return event
	})

	app.detailsText.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			// Allow default TextView scrolling behavior for up arrow
			return event
		case tcell.KeyDown:
			// Allow default TextView scrolling behavior for down arrow
			return event
		case tcell.KeyPgUp:
			// Allow default TextView scrolling behavior for Page Up
			return event
		case tcell.KeyPgDn:
			// Allow default TextView scrolling behavior for Page Down
			return event
		case tcell.KeyHome:
			// Allow default TextView scrolling behavior for Home
			return event
		case tcell.KeyEnd:
			// Allow default TextView scrolling behavior for End
			return event
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
		if event.Rune() == 's' || event.Rune() == 'S' {
			app.toggleTableID0Filter()
			return nil
		}
		if event.Rune() == 'i' || event.Rune() == 'I' {
			app.toggleOperationFilter("insert")
			return nil
		}
		if event.Rune() == 'u' || event.Rune() == 'U' {
			app.toggleOperationFilter("update")
			return nil
		}
		if event.Rune() == 'd' || event.Rune() == 'D' {
			app.toggleOperationFilter("delete")
			return nil
		}
		// For other keys (including j/k for vim-style scrolling), pass through
		return event
	})

	// Show header info initially
	app.showHeaderInfo()

	// Show first record if available
	if len(app.filteredRecords) > 0 {
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
Filtered Records: %d
Table ID 0 Filter: %s

[blue]Navigation:[white]
↑/↓: Navigate records (auto-update details)
Tab: Switch panes  
Enter: Focus details pane
s: Toggle Table ID 0 filter
Esc/q: Exit

[yellow]Footer Status:[white] %s
`,
		app.header.LogGroupID,
		app.header.StartLSN,
		app.header.FileNo,
		app.header.Created.Format("2006-01-02 15:04:05"),
		app.header.LastCheckpoint,
		app.header.Format,
		len(app.records),
		len(app.filteredRecords),
		func() string {
			if app.showTableID0 {
				return "[green]OFF (showing all)"
			}
			return "[red]ON (hiding Table ID 0)"
		}(),
		func() string {
			var filterStatus, filterColor string
			if app.showTableID0 {
				filterStatus = "OFF"
				filterColor = "[green]"
			} else {
				filterStatus = "ON"
				filterColor = "[red]"
			}
			return fmt.Sprintf(`Press 's' to toggle filter | Filter: %s%s[white] | Records: [cyan]%d[white]/[blue]%d`,
				filterColor, filterStatus, len(app.filteredRecords), len(app.records))
		}())

	app.detailsText.SetText(headerInfo)
}

func (app *RedoLogApp) showRecordDetails(index int) {
	if index >= len(app.filteredRecords) {
		return
	}

	record := app.filteredRecords[index]
	originalIndex := app.recordIndices[index]
	
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
		originalIndex+1,
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
	// Create main layout with footer
	topFlex := tview.NewFlex()
	topFlex.AddItem(app.recordList, 0, 1, true)   // Left pane (1/3)
	topFlex.AddItem(app.detailsText, 0, 2, false) // Right pane (2/3)

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	mainFlex.AddItem(topFlex, 0, 1, true)     // Top section (main content)
	mainFlex.AddItem(app.footer, 3, 0, false) // Bottom section (footer, fixed 3 lines)

	// Enable mouse support
	app.app.EnableMouse(true)
	
	app.app.SetRoot(mainFlex, true)
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

// testParseFields is a simple test function for parsing hex data
func testParseFields(data []byte) string {
	return reader.ParseRecordDataAsFields(data)
}

// getOperationType determines if a record is INSERT, UPDATE, DELETE, or OTHER
func getOperationType(recordType uint8) string {
	switch recordType {
	// INSERT operations
	case 9, 38: // MLOG_REC_INSERT_8027, MLOG_COMP_REC_INSERT_8027
		return "insert"
	
	// UPDATE operations  
	case 13, 41: // MLOG_REC_UPDATE_IN_PLACE_8027, MLOG_COMP_REC_UPDATE_IN_PLACE_8027
		return "update"
	
	// DELETE operations
	case 10, 11, 14, 15, 16, 39, 40, 42, 43, 44:
		// MLOG_REC_CLUST_DELETE_MARK_8027, MLOG_REC_SEC_DELETE_MARK, MLOG_REC_DELETE_8027,
		// MLOG_LIST_END_DELETE_8027, MLOG_LIST_START_DELETE_8027, 
		// MLOG_COMP_REC_CLUST_DELETE_MARK_8027, MLOG_COMP_REC_SEC_DELETE_MARK,
		// MLOG_COMP_REC_DELETE_8027, MLOG_COMP_LIST_END_DELETE_8027, MLOG_COMP_LIST_START_DELETE_8027
		return "delete"
	
	default:
		return "other"
	}
}

// updateFilteredRecords applies the current filter settings
func (app *RedoLogApp) updateFilteredRecords() {
	app.filteredRecords = make([]*types.LogRecord, 0)
	app.recordIndices = make([]int, 0)
	
	for i, record := range app.records {
		// Apply Table ID 0 filter
		if !app.showTableID0 && record.TableID == 0 && record.SpaceID == 0 {
			continue // Skip Table ID 0 records when filter is enabled
		}
		
		// Apply operation type filter
		if app.operationFilter != "all" && app.operationFilter != "" {
			recordType := uint8(record.Type)
			opType := getOperationType(recordType)
			if opType != app.operationFilter {
				continue // Skip records that don't match the operation filter
			}
		}
		
		app.filteredRecords = append(app.filteredRecords, record)
		app.recordIndices = append(app.recordIndices, i)
	}
}

// rebuildRecordList rebuilds the record list with current filtered records
func (app *RedoLogApp) rebuildRecordList() {
	app.recordList.Clear()
	
	groupColors := []string{"[white]", "[cyan]", "[yellow]", "[green]", "[magenta]", "[blue]"}
	for i, record := range app.filteredRecords {
		originalIndex := app.recordIndices[i]
		recordNum := fmt.Sprintf("%d", originalIndex+1)
		recordType := record.Type.String()
		
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
}

// updateFooter updates the footer display with current filter status
func (app *RedoLogApp) updateFooter() {
	var filterStatus, filterColor string
	if app.showTableID0 {
		filterStatus = "OFF"
		filterColor = "[green]"
	} else {
		filterStatus = "ON"
		filterColor = "[red]"
	}

	// Operation filter display
	var opFilterText string
	switch app.operationFilter {
	case "insert":
		opFilterText = "[green]INSERT"
	case "update":
		opFilterText = "[blue]UPDATE"
	case "delete":
		opFilterText = "[red]DELETE"
	default:
		opFilterText = "[white]ALL"
	}

	footerText := fmt.Sprintf(`[yellow]Keys: [bold]'s'[reset][yellow]=Table ID 0, [bold]'i'[reset][yellow]=INSERT, [bold]'u'[reset][yellow]=UPDATE, [bold]'d'[reset][yellow]=DELETE, [bold]Tab[reset][yellow]=Switch Panes [white]| Filters: Table ID 0=%s%s[white] Op=%s[white] | Records: [cyan]%d[white]/[blue]%d`,
		filterColor, filterStatus, opFilterText, len(app.filteredRecords), len(app.records))

	app.footer.SetText(footerText)
}

// toggleTableID0Filter toggles the Table ID 0 filter and refreshes the display
func (app *RedoLogApp) toggleTableID0Filter() {
	app.showTableID0 = !app.showTableID0
	
	// Update filtered records
	app.updateFilteredRecords()
	
	// Rebuild the record list
	app.rebuildRecordList()
	
	// Update header info to show new filter status
	app.showHeaderInfo()
	
	// Update footer
	app.updateFooter()
	
	// Reset selection to first record if available
	if len(app.filteredRecords) > 0 {
		app.recordList.SetCurrentItem(0)
		app.showRecordDetails(0)
	}
}

// toggleOperationFilter toggles between specific operation filter and "all"
func (app *RedoLogApp) toggleOperationFilter(operation string) {
	if app.operationFilter == operation {
		// If already filtering by this operation, switch to show all
		app.operationFilter = "all"
	} else {
		// Switch to filter by this operation
		app.operationFilter = operation
	}
	
	// Update filtered records
	app.updateFilteredRecords()
	
	// Rebuild the record list
	app.rebuildRecordList()
	
	// Update header info to show new filter status
	app.showHeaderInfo()
	
	// Update footer
	app.updateFooter()
	
	// Reset selection to first record if available
	if len(app.filteredRecords) > 0 {
		app.recordList.SetCurrentItem(0)
		app.showRecordDetails(0)
	}
}