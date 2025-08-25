package main

import (
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/yamaru/innodb-redolog-tool/internal/reader"
	"github.com/yamaru/innodb-redolog-tool/internal/types"
)

var (
	filename = flag.String("file", "", "InnoDB redo log file to analyze")
	verbose  = flag.Bool("v", false, "Verbose output")
	testMode = flag.Bool("test", false, "Test hex parsing without TUI")
	exportFormat = flag.String("export", "", "Export format: json, csv (skips TUI)")
	exportFile = flag.String("output", "", "Export output file (default: stdout)")
)

type RedoLogApp struct {
	app           *tview.Application
	recordList    *tview.List
	detailsText   *tview.TextView
	footer        *tview.TextView
	searchInput   *tview.InputField
	searchModal   *tview.Modal
	records       []*types.LogRecord
	filteredRecords []*types.LogRecord
	recordIndices []int // Maps filtered index to original index
	header        *types.RedoLogHeader
	showTableID0  bool // Toggle for showing Table ID 0 records
	operationFilter string // "all", "insert", "update", "delete"
	searchTerm    string // Current search term
	searchMatches []int  // Indices of records matching current search
	currentSearchIndex int // Current position in search matches
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
		fmt.Printf("Test mode: Searching for sakila-data.sql VARCHAR content in redo log\n\n")
		
		// Expected unique sakila data strings from sakila-data.sql
		sakilaStrings := []string{
			"sakila", "SAKILA", // Database name
			"PENELOPE", "GUINESS", "WAHLBERG", "LOLLOBRIGIDA", 
			"ACADEMY DINOSAUR", "AFRICAN EGG", "AGENT TRUMAN", 
			"AIRPLANE SIERRA", "ALABAMA DEVIL", "ALADDIN CALENDAR",
			"Epic Drama of a Feminist", "Astounding Documentary", 
			"Fanciful Documentary", "Fast-Paced Documentary",
			"Canadian Rockies", "Mad Scientist", "Battle a Teacher",
			"actor", "film", "rental", "customer", "payment", // Table names
		}
		
		fmt.Printf("Looking for these sakila-data.sql VARCHAR strings:\n")
		for i, str := range sakilaStrings {
			fmt.Printf("  %d. '%s'\n", i+1, str)
		}
		fmt.Printf("\n" + strings.Repeat("-", 60) + "\n")
		
		foundSakilaCount := 0
		foundSystemCount := 0
		foundAnyStrings := 0
		
		for i, record := range records {
			// Search both ASCII data and raw binary data
			recordData := string(record.Data)
			rawData := record.Data // Raw binary data
			recordHasSakila := false
			recordHasSystem := false
			
			// Check for sakila strings in both ASCII and binary data
			for _, sakilaStr := range sakilaStrings {
				foundInAscii := strings.Contains(recordData, sakilaStr)
				foundInBinary := strings.Contains(string(rawData), sakilaStr)
				
				if foundInAscii || foundInBinary {
					if !recordHasSakila {
						dataSource := "ASCII"
						if foundInBinary && !foundInAscii {
							dataSource = "BINARY"
						} else if foundInBinary && foundInAscii {
							dataSource = "ASCII+BINARY"
						}
						
						fmt.Printf("🎯 Record %d: %s - FOUND SAKILA DATA! [%s]\n", i+1, record.Type.String(), dataSource)
						fmt.Printf("   LSN: %d, TableID: %d, SpaceID: %d\n", record.LSN, record.TableID, record.SpaceID)
						recordHasSakila = true
						foundSakilaCount++
					}
					
					// Show context around the found string (prefer binary if found there)
					searchData := recordData
					if foundInBinary {
						searchData = string(rawData)
					}
					
					index := strings.Index(searchData, sakilaStr)
					start := index - 30
					end := index + len(sakilaStr) + 30
					if start < 0 { start = 0 }
					if end > len(searchData) { end = len(searchData) }
					
					context := searchData[start:end]
					// Replace non-printable characters for display
					displayContext := strings.Map(func(r rune) rune {
						if r >= 32 && r < 127 {
							return r
						}
						return '.'
					}, context)
					
					fmt.Printf("   Found: '%s' in context: ...%s...\n", sakilaStr, displayContext)
					
					// Show hex dump of the area around the found string
					fmt.Printf("   Hex context: ")
					hexStart := start
					hexEnd := end
					if hexEnd-hexStart > 60 { // Limit hex display
						hexEnd = hexStart + 60
					}
					for j := hexStart; j < hexEnd && j < len(rawData); j++ {
						fmt.Printf("%02x ", rawData[j])
					}
					fmt.Printf("\n")
					
					// Show full record details for sakila records
					fmt.Printf("   Full record details:\n")
					fmt.Printf("     Type: %s (ID: %d)\n", record.Type.String(), uint8(record.Type))
					fmt.Printf("     Length: %d bytes\n", record.Length)
					if record.SpaceID != 0 {
						fmt.Printf("     SpaceID: %d\n", record.SpaceID)
					}
					if record.PageNo != 0 {
						fmt.Printf("     PageNo: %d\n", record.PageNo)
					}
					if record.TableID != 0 {
						fmt.Printf("     TableID: %d\n", record.TableID)
					}
					fmt.Printf("     Group: %d\n", record.MultiRecordGroup)
					
					// Show more data context for sakila records (first 200 bytes)
					dataLen := len(rawData)
					if dataLen > 200 {
						dataLen = 200
					}
					fmt.Printf("   Raw data (first %d bytes): ", dataLen)
					for k := 0; k < dataLen; k++ {
						if k%16 == 0 {
							fmt.Printf("\n     ")
						}
						fmt.Printf("%02x ", rawData[k])
					}
					fmt.Printf("\n")
				}
			}
			
			// Check for system strings (for comparison)
			systemStrings := []string{"statement_analysis", "host_summary", "schema_unused", "sys_config", "setup_actors"}
			for _, sysStr := range systemStrings {
				foundSysAscii := strings.Contains(recordData, sysStr)
				foundSysBinary := strings.Contains(string(rawData), sysStr)
				
				if (foundSysAscii || foundSysBinary) && !recordHasSystem {
					recordHasSystem = true
					foundSystemCount++
					// Also show system string details
					dataSource := "ASCII"
					if foundSysBinary && !foundSysAscii {
						dataSource = "BINARY"
					}
					fmt.Printf("📊 Record %d: %s - Found system string '%s' [%s]\n", i+1, record.Type.String(), sysStr, dataSource)
					break
				}
			}
			
			// Check for any VARCHAR strings found
			if strings.Contains(recordData, "found_strings=") {
				foundAnyStrings++
			}
		}
		
		fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
		fmt.Printf("SEARCH RESULTS:\n")
		fmt.Printf("- Records with sakila data: %d\n", foundSakilaCount)
		fmt.Printf("- Records with system data: %d\n", foundSystemCount)
		fmt.Printf("- Records with any VARCHAR strings: %d\n", foundAnyStrings)
		
		if foundSakilaCount > 0 {
			fmt.Printf("\n✅ SUCCESS! Found actual sakila-data.sql VARCHAR content!\n")
		} else if foundAnyStrings > 0 {
			fmt.Printf("\n⚠️  Found VARCHAR strings, but they are system DB data, not sakila data\n")
			fmt.Printf("   This suggests:\n")
			fmt.Printf("   - The redo log was captured during MySQL system operations\n")
			fmt.Printf("   - Sakila data inserts may be in a different redo log file/timeframe\n")
		} else {
			fmt.Printf("\n❌ No VARCHAR strings found at all\n")
		}
		
		return
	}

	// Check if export mode is requested
	if *exportFormat != "" {
		err := exportRecords(records, header, *exportFormat, *exportFile)
		if err != nil {
			fmt.Printf("Export error: %v\n", err)
			os.Exit(1)
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
		x, y := event.Position()
		
		// Check if mouse is over the details text view (right pane)
		detailsX, detailsY, detailsWidth, detailsHeight := app.detailsText.GetRect()
		if x >= detailsX && x < detailsX+detailsWidth && y >= detailsY && y < detailsY+detailsHeight {
			// Mouse is over right pane - handle scrolling there
			if action == tview.MouseScrollUp {
				// Send up arrow key to details pane for scrolling
				upEvent := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
				for i := 0; i < 3; i++ { // Scroll by 3 lines
					app.detailsText.InputHandler()(upEvent, nil)
				}
				return tview.MouseConsumed, nil
			} else if action == tview.MouseScrollDown {
				// Send down arrow key to details pane for scrolling
				downEvent := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
				for i := 0; i < 3; i++ { // Scroll by 3 lines
					app.detailsText.InputHandler()(downEvent, nil)
				}
				return tview.MouseConsumed, nil
			}
			// For other actions (like clicks), pass through to details pane
			return action, event
		}
		
		// Mouse is over left pane - handle record navigation
		if action == tview.MouseLeftClick {
			// Handle mouse clicks manually to ensure they work
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
		if event.Rune() == '/' {
			app.showSearchModal()
			return nil
		}
		if event.Rune() == 'n' {
			app.nextSearchResult()
			return nil
		}
		if event.Rune() == 'N' {
			app.prevSearchResult()
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

	// Initialize search components
	app.initializeSearch()
	
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
/: Open search modal
n: Next search result
N: Previous search result
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
		details += app.formatRecordData(string(record.Data))
	} else {
		details += "(empty)"
	}

	app.detailsText.SetText(details)
	// Remove SetCurrentItem call to prevent infinite loop with SetChangedFunc
}

// formatRecordData formats the record data in a structured, readable way
func (app *RedoLogApp) formatRecordData(data string) string {
	if data == "" {
		return "(empty)"
	}
	
	result := fmt.Sprintf("\n[cyan]═══ RECORD DATA ANALYSIS (%d bytes) ═══[white]\n", len(data))
	
	// Parse different sections of the data
	sections := app.parseDataSections(data)
	
	// Display each section with proper formatting
	for _, section := range sections {
		result += section
	}
	
	// Add raw data at the end for reference (only if structured sections exist)
	if len(sections) > 0 {
		result += "\n[cyan]═══ RAW DATA (for reference) ═══[white]\n"
		// Truncate very long raw data to avoid overwhelming display
		if len(data) > 500 {
			result += fmt.Sprintf("[gray]%s...[white]\n[gray](truncated - %d total chars)[white]\n", data[:500], len(data))
		} else {
			result += fmt.Sprintf("[gray]%s[white]\n", data)
		}
	}
	
	return result
}

// parseDataSections breaks down the record data into logical sections
func (app *RedoLogApp) parseDataSections(data string) []string {
	var sections []string
	
	// Section 1: Basic Information
	if strings.Contains(data, "space_id=") || strings.Contains(data, "page_no=") {
		basicInfo := app.extractBasicInfo(data)
		if basicInfo != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ BASIC INFORMATION[white]\n%s\n", basicInfo))
		}
	}
	
	// Section 2: Index Information
	if strings.Contains(data, "index_info=") {
		indexInfo := app.extractIndexInfo(data)
		if indexInfo != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ INDEX INFORMATION[white]\n%s\n", indexInfo))
		}
	}
	
	// Section 3: Record Data (most detailed section)
	if strings.Contains(data, "record_data=") {
		recordData := app.extractRecordData(data)
		if recordData != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ RECORD DATA DETAILS[white]\n%s\n", recordData))
		}
	}
	
	// Section 4: Hex Data
	if strings.Contains(data, "data_hex=") {
		hexData := app.extractHexData(data)
		if hexData != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ HEX DATA[white]\n%s\n", hexData))
		}
	}
	
	// Section 5: Found Strings
	if strings.Contains(data, "found_strings=") {
		foundStrings := app.extractFoundStrings(data)
		if foundStrings != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ EXTRACTED STRINGS[white]\n%s\n", foundStrings))
		}
	}
	
	// Section 6: Parsed Data
	if strings.Contains(data, "parsed=") {
		parsedData := app.extractParsedData(data)
		if parsedData != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ PARSED ANALYSIS[white]\n%s\n", parsedData))
		}
	}
	
	// Section 7: Fields Information
	if strings.Contains(data, "fields=") {
		fieldsData := app.extractFieldsData(data)
		if fieldsData != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ FIELD ANALYSIS[white]\n%s\n", fieldsData))
		}
	}
	
	// If no structured sections found, try to parse as simple key-value pairs
	if len(sections) == 0 {
		generalInfo := app.extractGeneralInfo(data)
		if generalInfo != "" {
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ RECORD INFORMATION[white]\n%s\n", generalInfo))
		} else {
			// Only show raw data if nothing else can be parsed
			sections = append(sections, fmt.Sprintf("\n[yellow]▶ UNSTRUCTURED DATA[white]\n[gray]%s[white]\n", data))
		}
	}
	
	return sections
}

// extractBasicInfo extracts space_id, page_no, and similar basic information
func (app *RedoLogApp) extractBasicInfo(data string) string {
	var info []string
	
	// Extract space_id
	if match := regexp.MustCompile(`space_id=(\d+)`).FindStringSubmatch(data); match != nil {
		info = append(info, fmt.Sprintf("[green]Space ID:[white] %s", match[1]))
	}
	
	// Extract page_no
	if match := regexp.MustCompile(`page_no=(\d+)`).FindStringSubmatch(data); match != nil {
		info = append(info, fmt.Sprintf("[green]Page Number:[white] %s", match[1]))
	}
	
	// Extract offset
	if match := regexp.MustCompile(`offset=(\d+)`).FindStringSubmatch(data); match != nil {
		info = append(info, fmt.Sprintf("[green]Offset:[white] %s", match[1]))
	}
	
	// Extract badlen
	if match := regexp.MustCompile(`badlen=(\d+)`).FindStringSubmatch(data); match != nil {
		info = append(info, fmt.Sprintf("[green]Bad Length:[white] %s bytes", match[1]))
	}
	
	// Extract hex values in basic section  
	if match := regexp.MustCompile(`hex=([a-fA-F0-9]+)`).FindStringSubmatch(data); match != nil && !strings.Contains(data, "data_hex=") {
		info = append(info, fmt.Sprintf("[green]Hex Value:[white] %s", match[1]))
	}
	
	return strings.Join(info, "\n")
}

// extractGeneralInfo parses general key-value pairs that don't fit other categories
func (app *RedoLogApp) extractGeneralInfo(data string) string {
	var info []string
	
	// Common patterns to parse
	patterns := map[string]string{
		`offset=(\d+)`:           "[green]Offset:[white] %s",
		`badlen=(\d+)`:           "[green]Bad Length:[white] %s bytes", 
		`length=(\d+)`:           "[green]Length:[white] %s bytes",
		`size=(\d+)`:             "[green]Size:[white] %s bytes",
		`count=(\d+)`:            "[green]Count:[white] %s",
		`type=(\w+)`:             "[green]Type:[white] %s",
		`status=(\w+)`:           "[green]Status:[white] %s",
		`flags?=([a-fA-F0-9x]+)`: "[green]Flags:[white] %s",
		`id=(\d+)`:               "[green]ID:[white] %s",
		`version=(\d+)`:          "[green]Version:[white] %s",
	}
	
	for pattern, format := range patterns {
		re := regexp.MustCompile(pattern)
		if match := re.FindStringSubmatch(data); match != nil {
			info = append(info, fmt.Sprintf(format, match[1]))
		}
	}
	
	// Parse hex values (but not data_hex which is handled elsewhere)
	if !strings.Contains(data, "data_hex=") {
		hexRe := regexp.MustCompile(`hex=([a-fA-F0-9]+)`)
		if match := hexRe.FindStringSubmatch(data); match != nil {
			hexValue := match[1]
			// Format hex nicely if it's long
			if len(hexValue) > 16 {
				info = append(info, fmt.Sprintf("[green]Hex Data:[white] %s... (%d bytes)", hexValue[:16], len(hexValue)/2))
			} else {
				info = append(info, fmt.Sprintf("[green]Hex Data:[white] %s", hexValue))
			}
		}
	}
	
	// Parse simple string values
	stringRe := regexp.MustCompile(`(\w+)='([^']+)'`)
	matches := stringRe.FindAllStringSubmatch(data, -1)
	for _, match := range matches {
		key := match[1]
		value := match[2]
		// Skip if already handled by other sections
		if !strings.Contains(key, "found_strings") && !strings.Contains(key, "varchar") {
			info = append(info, fmt.Sprintf("[green]%s:[white] [yellow]'%s'[white]", strings.Title(key), value))
		}
	}
	
	return strings.Join(info, "\n")
}

// extractIndexInfo extracts index-related information
func (app *RedoLogApp) extractIndexInfo(data string) string {
	// Find index_info= section
	re := regexp.MustCompile(`index_info=\(([^)]*)\)`)
	match := re.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	
	indexData := match[1]
	var info []string
	
	// Parse n_fields
	if match := regexp.MustCompile(`n_fields=(\d+)`).FindStringSubmatch(indexData); match != nil {
		info = append(info, fmt.Sprintf("[green]Number of Fields:[white] %s", match[1]))
	}
	
	// Parse n_uniq
	if match := regexp.MustCompile(`n_uniq=(\d+)`).FindStringSubmatch(indexData); match != nil {
		info = append(info, fmt.Sprintf("[green]Unique Fields:[white] %s", match[1]))
	}
	
	// Parse instant_cols if present
	if strings.Contains(indexData, "instant_cols=true") {
		info = append(info, fmt.Sprintf("[green]Instant Columns:[white] [yellow]YES[white]"))
	}
	
	// Parse n_instant_cols if present
	if match := regexp.MustCompile(`n_instant_cols=(\d+)`).FindStringSubmatch(indexData); match != nil {
		info = append(info, fmt.Sprintf("[green]Instant Column Count:[white] %s", match[1]))
	}
	
	// Parse fields array
	fieldsRe := regexp.MustCompile(`fields=\[([^\]]*)\]`)
	if fieldsMatch := fieldsRe.FindStringSubmatch(indexData); fieldsMatch != nil {
		info = append(info, "[green]Fields:[white]")
		fieldsList := app.parseFieldsList(fieldsMatch[1])
		info = append(info, fieldsList)
	}
	
	return strings.Join(info, "\n")
}

// parseFieldsList parses the fields array and formats it nicely
func (app *RedoLogApp) parseFieldsList(fieldsStr string) string {
	if fieldsStr == "" {
		return "  (none)"
	}
	
	// Split by field entries (each field starts with field_)
	fieldRe := regexp.MustCompile(`field_(\d+)\(([^)]*)\)`)
	matches := fieldRe.FindAllStringSubmatch(fieldsStr, -1)
	
	if len(matches) == 0 {
		return fmt.Sprintf("  [gray]%s[white]", fieldsStr)
	}
	
	var fields []string
	for _, match := range matches {
		fieldNum := match[1]
		fieldInfo := match[2]
		
		// Parse len and type info
		var attributes []string
		if lenMatch := regexp.MustCompile(`len=(\d+)`).FindStringSubmatch(fieldInfo); lenMatch != nil {
			attributes = append(attributes, fmt.Sprintf("len=%s", lenMatch[1]))
		}
		if strings.Contains(fieldInfo, "NOT_NULL") {
			attributes = append(attributes, "[red]NOT_NULL[white]")
		} else if strings.Contains(fieldInfo, "NULLABLE") {
			attributes = append(attributes, "[green]NULLABLE[white]")
		}
		
		attrStr := strings.Join(attributes, ", ")
		fields = append(fields, fmt.Sprintf("  [cyan]Field %s:[white] %s", fieldNum, attrStr))
		
		// Limit display to avoid overwhelming output
		if len(fields) >= 10 {
			remaining := len(matches) - 10
			if remaining > 0 {
				fields = append(fields, fmt.Sprintf("  [gray]... and %d more fields[white]", remaining))
			}
			break
		}
	}
	
	return strings.Join(fields, "\n")
}

// extractRecordData extracts and formats the record_data section
func (app *RedoLogApp) extractRecordData(data string) string {
	// Find record_data= section
	re := regexp.MustCompile(`record_data=\(([^)]+(?:\([^)]*\)[^)]*)*)\)`)
	match := re.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	
	recordData := match[1]
	var info []string
	
	// Parse various fields in record data
	patterns := map[string]string{
		"cursor_offset":         "[green]Cursor Offset:[white] %s",
		"end_seg_len":          "[green]End Segment Length:[white] %s",
		"info_bits":            "[green]Info Bits:[white] %s",
		"origin_offset":        "[green]Origin Offset:[white] %s",
		"mismatch_index":       "[green]Mismatch Index:[white] %s",
		"debug_actualDataLen":  "[green]Actual Data Length:[white] %s bytes",
		"debug_dataOffset":     "[green]Data Offset:[white] %s",
		"debug_blockDataLen":   "[green]Block Data Length:[white] %s",
		"cross_block_read":     "[green]Cross Block Read:[white] %s",
	}
	
	for field, format := range patterns {
		re := regexp.MustCompile(fmt.Sprintf(`%s=([^,)]+)`, field))
		if match := re.FindStringSubmatch(recordData); match != nil {
			value := match[1]
			if field == "cross_block_read" && value == "success" {
				value = "[green]SUCCESS[white]"
			}
			info = append(info, fmt.Sprintf(format, value))
		}
	}
	
	return strings.Join(info, "\n")
}

// extractHexData extracts and formats hex data
func (app *RedoLogApp) extractHexData(data string) string {
	re := regexp.MustCompile(`data_hex=([a-fA-F0-9]+)`)
	match := re.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	
	hexStr := match[1]
	
	// Format hex data in nice columns (16 bytes per line)
	var formatted []string
	formatted = append(formatted, fmt.Sprintf("[green]Length:[white] %d bytes", len(hexStr)/2))
	formatted = append(formatted, "[green]Data:[white]")
	
	for i := 0; i < len(hexStr); i += 32 { // 32 hex chars = 16 bytes
		end := i + 32
		if end > len(hexStr) {
			end = len(hexStr)
		}
		
		line := hexStr[i:end]
		// Add spaces every 2 characters for readability
		var spaced []string
		for j := 0; j < len(line); j += 2 {
			spaced = append(spaced, line[j:j+2])
		}
		
		addr := fmt.Sprintf("%04x:", i/2)
		formatted = append(formatted, fmt.Sprintf("  [gray]%s[white] %s", addr, strings.Join(spaced, " ")))
	}
	
	return strings.Join(formatted, "\n")
}

// extractFoundStrings extracts and formats found strings
func (app *RedoLogApp) extractFoundStrings(data string) string {
	re := regexp.MustCompile(`found_strings='([^']*)'`)
	match := re.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	
	strings_data := match[1]
	
	// Split by | separator and clean up
	strings_list := strings.Split(strings_data, "|")
	var formatted []string
	
	for i, s := range strings_list {
		s = strings.TrimSpace(s)
		if s != "" {
			formatted = append(formatted, fmt.Sprintf("[green]String %d:[white] [yellow]'%s'[white]", i+1, s))
		}
	}
	
	if len(formatted) == 0 {
		return "[gray](no readable strings found)[white]"
	}
	
	return strings.Join(formatted, "\n")
}

// extractParsedData extracts parsed analysis information
func (app *RedoLogApp) extractParsedData(data string) string {
	re := regexp.MustCompile(`parsed=\(([^)]+)\)`)
	match := re.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	
	parsedContent := match[1]
	
	// Parse innodb_record structure
	if strings.Contains(parsedContent, "innodb_record=") {
		return app.formatInnoDBRecord(parsedContent)
	}
	
	// For other parsed content, format nicely
	return app.formatGeneralParsedData(parsedContent)
}

// formatInnoDBRecord formats InnoDB record analysis with proper structure
func (app *RedoLogApp) formatInnoDBRecord(content string) string {
	var result []string
	result = append(result, "[green]InnoDB Record Structure:[white]")
	
	// Extract innodb_record content
	recordRe := regexp.MustCompile(`innodb_record=\[([^\]]+)\]`)
	match := recordRe.FindStringSubmatch(content)
	if match == nil {
		return fmt.Sprintf("[gray]%s[white]", content)
	}
	
	recordData := match[1]
	
	// Parse header_skip
	if headerMatch := regexp.MustCompile(`header_skip=(\d+)`).FindStringSubmatch(recordData); headerMatch != nil {
		result = append(result, fmt.Sprintf("  [green]Header Skip:[white] %s bytes", headerMatch[1]))
	}
	
	// Parse field lengths if present
	if lengthsMatch := regexp.MustCompile(`field_lengths=\[([^\]]+)\]`).FindStringSubmatch(recordData); lengthsMatch != nil {
		lengths := strings.Split(lengthsMatch[1], " ")
		result = append(result, "[green]Field Lengths:[white]")
		for i, length := range lengths {
			result = append(result, fmt.Sprintf("    [cyan]Field %d:[white] %s bytes", i, strings.TrimSpace(length)))
		}
	}
	
	// Parse individual fields (field1_type=value, field2_type=value, etc.)
	fieldRe := regexp.MustCompile(`field(\d+)_([^=]+)=([^\s]+)`)
	fieldMatches := fieldRe.FindAllStringSubmatch(recordData, -1)
	
	if len(fieldMatches) > 0 {
		result = append(result, "[green]Field Values:[white]")
		for _, fieldMatch := range fieldMatches {
			fieldNum := fieldMatch[1]
			fieldType := fieldMatch[2]
			fieldValue := fieldMatch[3]
			
			// Format based on field type
			switch fieldType {
			case "tinyint":
				result = append(result, fmt.Sprintf("    [cyan]Field %s (TINYINT):[white] %s", fieldNum, fieldValue))
			case "int":
				result = append(result, fmt.Sprintf("    [cyan]Field %s (INT):[white] %s", fieldNum, fieldValue))
			case "varchar":
				result = append(result, fmt.Sprintf("    [cyan]Field %s (VARCHAR):[white] [yellow]%s[white]", fieldNum, fieldValue))
			case "bigint":
				result = append(result, fmt.Sprintf("    [cyan]Field %s (BIGINT):[white] %s", fieldNum, fieldValue))
			default:
				result = append(result, fmt.Sprintf("    [cyan]Field %s (%s):[white] %s", fieldNum, strings.ToUpper(fieldType), fieldValue))
			}
		}
	}
	
	// Parse remaining_hex if present
	if hexMatch := regexp.MustCompile(`remaining_hex=([a-fA-F0-9]+)`).FindStringSubmatch(recordData); hexMatch != nil {
		hexValue := hexMatch[1]
		result = append(result, fmt.Sprintf("[green]Remaining Hex:[white] %s (%d bytes)", hexValue, len(hexValue)/2))
	}
	
	return strings.Join(result, "\n")
}

// formatGeneralParsedData formats other types of parsed content
func (app *RedoLogApp) formatGeneralParsedData(content string) string {
	var result []string
	
	// Split by common separators and format each part
	parts := regexp.MustCompile(`[,\s]+`).Split(content, -1)
	
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		
		// Check if it's a key=value pair
		if strings.Contains(part, "=") {
			keyValue := strings.SplitN(part, "=", 2)
			if len(keyValue) == 2 {
				key := strings.TrimSpace(keyValue[0])
				value := strings.TrimSpace(keyValue[1])
				result = append(result, fmt.Sprintf("[green]%s:[white] %s", strings.Title(key), value))
			} else {
				result = append(result, fmt.Sprintf("[gray]%s[white]", part))
			}
		} else {
			result = append(result, fmt.Sprintf("[gray]%s[white]", part))
		}
	}
	
	if len(result) == 0 {
		return fmt.Sprintf("[gray]%s[white]", content)
	}
	
	return strings.Join(result, "\n")
}

// extractFieldsData extracts and formats field analysis
func (app *RedoLogApp) extractFieldsData(data string) string {
	re := regexp.MustCompile(`fields=\(([^)]+)\)`)
	match := re.FindStringSubmatch(data)
	if match == nil {
		return ""
	}
	
	fieldsData := match[1]
	var info []string
	
	// Parse field entries
	fieldRe := regexp.MustCompile(`field_(\d+)=([^,)]+)`)
	matches := fieldRe.FindAllStringSubmatch(fieldsData, -1)
	
	for _, match := range matches {
		fieldNum := match[1]
		fieldValue := match[2]
		
		// Format different field types
		if strings.Contains(fieldValue, "varchar=") {
			varcharRe := regexp.MustCompile(`varchar='([^']*)'`)
			if varcharMatch := varcharRe.FindStringSubmatch(fieldValue); varcharMatch != nil {
				info = append(info, fmt.Sprintf("[green]Field %s (VARCHAR):[white] [yellow]'%s'[white]", fieldNum, varcharMatch[1]))
			}
		} else if strings.Contains(fieldValue, "int_") {
			info = append(info, fmt.Sprintf("[green]Field %s (INTEGER):[white] %s", fieldNum, fieldValue))
		} else if strings.Contains(fieldValue, "hex=") {
			hexRe := regexp.MustCompile(`hex=([a-fA-F0-9]+)`)
			if hexMatch := hexRe.FindStringSubmatch(fieldValue); hexMatch != nil {
				info = append(info, fmt.Sprintf("[green]Field %s (HEX):[white] %s", fieldNum, hexMatch[1]))
			}
		} else {
			info = append(info, fmt.Sprintf("[green]Field %s:[white] %s", fieldNum, fieldValue))
		}
	}
	
	if len(info) == 0 {
		return fmt.Sprintf("[gray]%s[white]", fieldsData)
	}
	
	return strings.Join(info, "\n")
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
			// Check if this is a normal end-of-log condition
			if strings.Contains(err.Error(), "end of valid log data") {
				if *verbose {
					fmt.Printf("Reached end of log data at record %d\n", recordCount+1)
				}
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
// Search functionality methods
func (app *RedoLogApp) initializeSearch() {
	// Create search input field
	app.searchInput = tview.NewInputField()
	app.searchInput.SetLabel("Search: ")
	app.searchInput.SetFieldWidth(50)
	app.searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			searchTerm := app.searchInput.GetText()
			app.performSearch(searchTerm)
		} else if key == tcell.KeyEscape {
			app.hideSearchModal()
		}
	})
	
	// Create search modal
	app.searchModal = tview.NewModal()
	app.searchModal.SetText("Search in record data (VARCHAR strings, hex data, LSN, etc.)")
	app.searchModal.AddButtons([]string{"Search", "Cancel"})
	app.searchModal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		if buttonLabel == "Search" {
			searchTerm := app.searchInput.GetText()
			app.performSearch(searchTerm)
		}
		app.hideSearchModal()
	})
}

func (app *RedoLogApp) showSearchModal() {
	// Clear previous search term
	app.searchInput.SetText("")
	
	// Create a flex container for the input
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(app.searchInput, 1, 0, true)
	flex.AddItem(app.searchModal, 0, 1, false)
	
	// Show the modal with input field
	app.app.SetRoot(flex, true)
	app.app.SetFocus(app.searchInput)
}

func (app *RedoLogApp) hideSearchModal() {
	// Return to main layout
	mainLayout := tview.NewFlex()
	
	// Add left pane (record list)
	mainLayout.AddItem(app.recordList, 0, 1, true)
	
	// Add right pane (details)
	rightPane := tview.NewFlex().SetDirection(tview.FlexRow)
	rightPane.AddItem(app.detailsText, 0, 1, false)
	rightPane.AddItem(app.footer, 3, 0, false)
	
	mainLayout.AddItem(rightPane, 0, 2, false)
	
	app.app.SetRoot(mainLayout, true)
	app.app.SetFocus(app.recordList)
}

func (app *RedoLogApp) performSearch(searchTerm string) {
	if searchTerm == "" {
		return
	}
	
	app.searchTerm = searchTerm
	app.searchMatches = []int{}
	app.currentSearchIndex = 0
	
	// Search through all records (not just filtered ones)
	for i, record := range app.records {
		// Search in multiple fields
		recordData := string(record.Data)
		lsnStr := fmt.Sprintf("%d", record.LSN)
		typeStr := record.Type.String()
		
		// Case-insensitive search
		searchLower := strings.ToLower(searchTerm)
		
		if strings.Contains(strings.ToLower(recordData), searchLower) ||
		   strings.Contains(strings.ToLower(lsnStr), searchLower) ||
		   strings.Contains(strings.ToLower(typeStr), searchLower) ||
		   strings.Contains(strings.ToLower(fmt.Sprintf("%d", record.TableID)), searchLower) ||
		   strings.Contains(strings.ToLower(fmt.Sprintf("%d", record.SpaceID)), searchLower) {
			app.searchMatches = append(app.searchMatches, i)
		}
	}
	
	// Update footer with search results
	app.updateSearchStatus()
	
	// Navigate to first match if any
	if len(app.searchMatches) > 0 {
		app.goToSearchResult(0)
	}
}

func (app *RedoLogApp) nextSearchResult() {
	if len(app.searchMatches) == 0 {
		return
	}
	
	app.currentSearchIndex = (app.currentSearchIndex + 1) % len(app.searchMatches)
	app.goToSearchResult(app.currentSearchIndex)
}

func (app *RedoLogApp) prevSearchResult() {
	if len(app.searchMatches) == 0 {
		return
	}
	
	app.currentSearchIndex = (app.currentSearchIndex - 1 + len(app.searchMatches)) % len(app.searchMatches)
	app.goToSearchResult(app.currentSearchIndex)
}

func (app *RedoLogApp) goToSearchResult(matchIndex int) {
	if matchIndex >= len(app.searchMatches) {
		return
	}
	
	recordIndex := app.searchMatches[matchIndex]
	
	// Find this record in the filtered records
	for filteredIndex, originalIndex := range app.recordIndices {
		if originalIndex == recordIndex {
			// Found the record in filtered list
			app.recordList.SetCurrentItem(filteredIndex)
			app.showRecordDetails(filteredIndex)
			app.updateSearchStatus()
			return
		}
	}
	
	// Record not in current filtered view - temporarily show all to navigate to it
	app.showTableID0 = true
	app.operationFilter = "all"
	app.updateFilteredRecords()
	app.rebuildRecordList()
	
	// Try again to find in new filtered list
	for filteredIndex, originalIndex := range app.recordIndices {
		if originalIndex == recordIndex {
			app.recordList.SetCurrentItem(filteredIndex)
			app.showRecordDetails(filteredIndex)
			app.updateSearchStatus()
			return
		}
	}
}

func (app *RedoLogApp) updateSearchStatus() {
	if app.searchTerm == "" || len(app.searchMatches) == 0 {
		return
	}
	
	// Add search status to footer
	currentMatch := app.currentSearchIndex + 1
	totalMatches := len(app.searchMatches)
	
	searchStatus := fmt.Sprintf(" | Search: '%s' (%d/%d matches)", app.searchTerm, currentMatch, totalMatches)
	
	// Get current footer text and append search status
	currentFooter := app.footer.GetText(false)
	if !strings.Contains(currentFooter, "Search:") {
		app.footer.SetText(currentFooter + searchStatus)
	}
}

// Export functionality
func exportRecords(records []*types.LogRecord, header *types.RedoLogHeader, format, outputFile string) error {
	var output io.Writer = os.Stdout
	
	if outputFile != "" {
		file, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %v", err)
		}
		defer file.Close()
		output = file
	}
	
	switch strings.ToLower(format) {
	case "json":
		return exportJSON(output, records, header)
	case "csv":
		return exportCSV(output, records, header)
	default:
		return fmt.Errorf("unsupported export format: %s (supported: json, csv)", format)
	}
}

func exportJSON(w io.Writer, records []*types.LogRecord, header *types.RedoLogHeader) error {
	data := struct {
		Header  *types.RedoLogHeader `json:"header"`
		Records []*types.LogRecord   `json:"records"`
		Stats   map[string]interface{} `json:"stats"`
	}{
		Header:  header,
		Records: records,
		Stats: map[string]interface{}{
			"total_records": len(records),
			"export_timestamp": time.Now().Format(time.RFC3339),
			"format_version": header.Format,
		},
	}
	
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func exportCSV(w io.Writer, records []*types.LogRecord, header *types.RedoLogHeader) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()
	
	// Write header
	headers := []string{
		"Record_Number", "LSN", "Type", "Type_ID", "Length", 
		"Space_ID", "Page_No", "Table_ID", "Group", "Data_Preview", "Data_Length",
	}
	if err := writer.Write(headers); err != nil {
		return err
	}
	
	// Write records
	for i, record := range records {
		// Limit data preview to first 100 characters
		dataPreview := string(record.Data)
		if len(dataPreview) > 100 {
			dataPreview = dataPreview[:100] + "..."
		}
		// Replace newlines and control characters for CSV
		dataPreview = strings.ReplaceAll(dataPreview, "\n", "\\n")
		dataPreview = strings.ReplaceAll(dataPreview, "\r", "\\r")
		dataPreview = strings.ReplaceAll(dataPreview, "\"", "\"\"")
		
		row := []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("%d", record.LSN),
			record.Type.String(),
			fmt.Sprintf("%d", uint8(record.Type)),
			fmt.Sprintf("%d", record.Length),
			fmt.Sprintf("%d", record.SpaceID),
			fmt.Sprintf("%d", record.PageNo),
			fmt.Sprintf("%d", record.TableID),
			fmt.Sprintf("%d", record.MultiRecordGroup),
			dataPreview,
			fmt.Sprintf("%d", len(record.Data)),
		}
		
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	
	return nil
}
