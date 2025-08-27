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
	"strconv"
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
	referenceView *tview.List  // Left pane: type list
	referenceModal *tview.Flex // Reference main layout
	typeDetailView *tview.TextView // Right pane: type details
	referenceDetailPane *tview.Flex // Right pane container
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

// TypeInfo holds information about each redo log type
type TypeInfo struct {
	ID          uint8
	Name        string
	Category    string
	Description string
	Format      string // Detailed format information
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
		showTableID0 := true // Default: show all records
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
		showTableID0: true, // Default: show all records including Table ID 0
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
		if event.Rune() == 'r' || event.Rune() == 'R' {
			app.showReferenceModal()
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
		if event.Rune() == 'r' || event.Rune() == 'R' {
			app.showReferenceModal()
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
r: Show Type Reference
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

// buildBlockFormatDisplay builds the 512-byte block format display for a record
func (app *RedoLogApp) buildBlockFormatDisplay(record *types.LogRecord, originalIndex int) string {
	// Get type-specific format info
	typeID := uint8(record.Type)
	typeName := record.Type.String()
	
	// Group information
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
		groupInfo = fmt.Sprintf("\n[green]Multi-Record Group:[white] %d%s", record.MultiRecordGroup, groupStatus)
	}
	
	// Calculate sizes for compressed fields (estimate)
	spaceIDSize := app.getCompressedSize(uint32(record.SpaceID))
	pageNoSize := app.getCompressedSize(uint32(record.PageNo))
	lengthSize := app.getCompressedSize(uint32(record.Length))
	
	// Calculate record size in block
	recordSize := 1 + lengthSize + spaceIDSize + pageNoSize + int(record.Length) // Type + compressed fields + data
	blockUtilization := float64(recordSize) * 100 / 496 // 496 bytes data area
	
	// Build the block format display
	blockDisplay := fmt.Sprintf(`[cyan]═══ Record %d: %s ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number
  Offset 4-5:   Data Length = %d
  Offset 6-7:   First Record Group Offset
  Offset 8-11:  Epoch Number

[cyan]Record Data Area (496 bytes available):[white]
[yellow]%s Record Structure:[white]
  Byte 0:       Type = 0x%02X (%s)
  Byte 1-%d:     Length = %d (compressed %d bytes)
  Byte %d-%d:    Space ID = %d (compressed %d bytes)
  Byte %d-%d:    Page Number = %d (compressed %d bytes)
`,
		originalIndex+1,
		typeName,
		record.Length,
		typeName,
		typeID,
		typeName,
		lengthSize, record.Length, lengthSize,
		1+lengthSize, 1+lengthSize+spaceIDSize-1, record.SpaceID, spaceIDSize,
		1+lengthSize+spaceIDSize, 1+lengthSize+spaceIDSize+pageNoSize-1, record.PageNo, pageNoSize)
	
	// Add type-specific fields
	currentByte := 1 + lengthSize + spaceIDSize + pageNoSize
	
	// Add record-specific fields based on type
	switch typeID {
	case 9, 38, 67: // Insert operations
		blockDisplay += fmt.Sprintf(`  Byte %d-%d:   Cursor Position = %d (2 bytes)
  Byte %d-%d:   Info & Status Bits (1 byte)
  Byte %d-N:    Record Data (variable)
                ├─ Transaction ID: %d
                ├─ Table ID: %d
                ├─ Index ID: %d
                └─ Column Data
`,
			currentByte, currentByte+1, record.Offset,
			currentByte+2, currentByte+2,
			currentByte+3,
			record.TransactionID,
			record.TableID,
			record.IndexID)
			
	case 13, 41, 70: // Update operations
		blockDisplay += fmt.Sprintf(`  Byte %d-%d:   Cursor Position = %d (2 bytes)
  Byte %d:      Update Flags (1 byte)
  Byte %d-N:    Update Vectors
                ├─ Transaction ID: %d
                ├─ Table ID: %d
                └─ Modified Fields
`,
			currentByte, currentByte+1, record.Offset,
			currentByte+2,
			currentByte+3,
			record.TransactionID,
			record.TableID)
			
	case 31: // MLOG_MULTI_REC_END
		blockDisplay += `  (No additional fields - marker record)
`
		
	default:
		// Generic fields for other types
		if record.Offset > 0 {
			blockDisplay += fmt.Sprintf(`  Byte %d-%d:   Page Offset = %d (2 bytes)
`, currentByte, currentByte+1, record.Offset)
			currentByte += 2
		}
		if record.TransactionID > 0 {
			blockDisplay += fmt.Sprintf(`  Byte %d-%d:   Transaction ID = %d (6 bytes)
`, currentByte, currentByte+5, record.TransactionID)
			currentByte += 6
		}
		if len(record.Data) > 0 {
			blockDisplay += fmt.Sprintf(`  Byte %d-N:    Data Payload (%d bytes)
`, currentByte, len(record.Data))
		}
	}
	
	// Add metadata and statistics
	blockDisplay += fmt.Sprintf(`
[yellow]Record Metadata:[white]
  LSN:            %d
  Timestamp:      %s
  Checksum:       0x%08X%s
  
[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum = 0x%08X

[green]Size Analysis:[white]
  Record Size:        %d bytes
  Block Utilization:  %.1f%% of data area
  Remaining Space:    %d bytes available
`,
		record.LSN,
		record.Timestamp.Format("2006-01-02 15:04:05"),
		record.Checksum,
		groupInfo,
		record.Checksum,
		recordSize,
		blockUtilization,
		496-recordSize)
	
	// Add type hint
	if record.Type.IsTransactional() {
		blockDisplay += `
[yellow]Type Category:[white] Transactional Operation
`
	}
	
	blockDisplay += `
[cyan]═══ Record Data Details ═══[white]
`
	
	return blockDisplay
}

// getCompressedSize estimates the size of a compressed integer
func (app *RedoLogApp) getCompressedSize(value uint32) int {
	if value < 128 {
		return 1
	} else if value < 16384 {
		return 2
	} else if value < 2097152 {
		return 3
	} else if value < 268435456 {
		return 4
	}
	return 5
}

func (app *RedoLogApp) showRecordDetails(index int) {
	if index >= len(app.filteredRecords) {
		return
	}

	record := app.filteredRecords[index]
	originalIndex := app.recordIndices[index]
	
	// Build 512-byte block format display
	details := app.buildBlockFormatDisplay(record, originalIndex)
	

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

// getTypeInfoMap returns a map of all redo log types with their detailed information
func getTypeInfoMap() map[uint8]*TypeInfo {
	return map[uint8]*TypeInfo{
		// Basic byte operations
		1: {
			ID: 1, Name: "MLOG_1BYTE", Category: "Basic Byte Operations",
			Description: "Write 1 byte to a page",
			Format: `[cyan]═══ MLOG_1BYTE Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_1BYTE Record Format:[white]
  Byte 0:       Type = 0x01 (1 byte)
  Byte 1:       Length (compressed 1-5 bytes)
  Byte 2-6:     Space ID (compressed 1-5 bytes)
  Byte 7-11:    Page Number (compressed 1-5 bytes)
  Byte 12-13:   Page Offset (2 bytes)
  Byte 14:      Value to write (1 byte)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Record Size: 7-18 bytes[white]
[green]Block Utilization: ~1.4-3.6% of data area[white]
[gray]Multiple MLOG_1BYTE records can fit in single block[white]
[gray]Remaining space: 478-489 bytes available for other records[white]`,
		},
		2: {
			ID: 2, Name: "MLOG_2BYTES", Category: "Basic Byte Operations",
			Description: "Write 2 bytes to a page",
			Format: `[cyan]═══ MLOG_2BYTES Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes) 
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_2BYTES Record Format:[white]
  Byte 0:       Type = 0x02 (1 byte)
  Byte 1:       Length (compressed 1-5 bytes)
  Byte 2-6:     Space ID (compressed 1-5 bytes)  
  Byte 7-11:    Page Number (compressed 1-5 bytes)
  Byte 12-13:   Page Offset (2 bytes)
  Byte 14-15:   Value to write (2 bytes)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Record Size: 8-19 bytes[white]
[green]Block Utilization: ~1.6-3.8% of data area[white]
[gray]Multiple MLOG_2BYTES records can fit in single block[white]
[gray]Remaining space: 477-488 bytes available for other records[white]`,
		},
		4: {
			ID: 4, Name: "MLOG_4BYTES", Category: "Basic Byte Operations",
			Description: "Write 4 bytes to a page",
			Format: `[cyan]═══ MLOG_4BYTES Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_4BYTES Record Format:[white]
  Byte 0:       Type = 0x04 (1 byte)
  Byte 1:       Length (compressed 1-5 bytes)
  Byte 2-6:     Space ID (compressed 1-5 bytes)
  Byte 7-11:    Page Number (compressed 1-5 bytes)
  Byte 12-13:   Page Offset (2 bytes)
  Byte 14-17:   Value to write (4 bytes)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Record Size: 10-21 bytes[white]
[green]Block Utilization: ~2.0-4.2% of data area[white]
[gray]Used for 32-bit values like page numbers, checksums[white]
[gray]Remaining space: 475-486 bytes available for other records[white]`,
		},
		8: {
			ID: 8, Name: "MLOG_8BYTES", Category: "Basic Byte Operations",
			Description: "Write 8 bytes to a page",
			Format: `[cyan]═══ MLOG_8BYTES Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_8BYTES Record Format:[white]
  Byte 0:       Type = 0x08 (1 byte)
  Byte 1:       Length (compressed 1-5 bytes)
  Byte 2-6:     Space ID (compressed 1-5 bytes)
  Byte 7-11:    Page Number (compressed 1-5 bytes)
  Byte 12-13:   Page Offset (2 bytes)
  Byte 14-21:   Value to write (8 bytes)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Record Size: 14-25 bytes[white]
[green]Block Utilization: ~2.8-5.0% of data area[white]
[gray]Used for 64-bit values like LSN, transaction IDs[white]
[gray]Remaining space: 471-482 bytes available for other records[white]`,
		},

		// Record operations
		9: {
			ID: 9, Name: "MLOG_REC_INSERT_8027", Category: "Record Operations (Old Format)",
			Description: "Insert a record (old format)",
			Format: `[cyan]═══ MLOG_REC_INSERT_8027 Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_REC_INSERT_8027 Record Format:[white]
  Byte 0:       Type = 0x09 (1 byte)
  Byte 1-5:     Space ID (compressed 1-5 bytes)
  Byte 6-10:    Page Number (compressed 1-5 bytes)
  Byte 11-12:   Cursor Position (2 bytes)
  Byte 13-14:   Record End Offset (2 bytes)
  Byte 15:      Info & Status Bits (1 byte)
  Byte 16-20:   Origin Offset (compressed 1-5 bytes)
  Byte 21-25:   Mismatch Index (compressed 1-5 bytes)
  Byte 26-N:    Record Data (variable length)
                ├─ Record Header (5-8 bytes typical)
                ├─ Column Data (variable per column)
                └─ Field Offsets (variable)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Typical Record Size: 30-200 bytes[white]
[green]Block Utilization: ~6-40% of data area[white]
[yellow]Info Bits Detail:[white]
• Bit 0: Delete mark flag
• Bit 1: Min record flag  
• Bits 2-7: Reserved

[gray]Legacy format from MySQL 8.0.27 and earlier[white]
[gray]Can contain complete row data for INSERT operations[white]`,
		},
		67: {
			ID: 67, Name: "MLOG_REC_INSERT", Category: "Record Operations (Current)",
			Description: "Insert record (current format)",
			Format: `[cyan]═══ MLOG_REC_INSERT Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_REC_INSERT Record Format:[white]
  Byte 0:       Type = 0x43 (1 byte)
  Byte 1-5:     Space ID (compressed 1-5 bytes)
  Byte 6-10:    Page Number (compressed 1-5 bytes)
  
[yellow]Index Info Section (if present):[white]
  Byte 11:      Flags (1 byte) - bit 0: has index info
  Byte 12-16:   N_uniq (compressed 1-5 bytes, if flag set)
  Byte 17-21:   N_fields (compressed 1-5 bytes, if flag set)
  Byte 22-N:    Field Type Array (variable, if flag set)

[yellow]Record Section:[white]
  Byte X:       Cursor Position (2 bytes)
  Byte X+2:     Record Size (compressed 1-5 bytes)
  Byte X+7:     Extra Size (1 byte)
  Byte X+8:     Info & Status Bits (1 byte)
  Byte X+9-N:   Record Data
                ├─ Null Bitmap ((n_fields+7)/8 bytes)
                ├─ Variable Field Lengths (per VARCHAR/BLOB)
                └─ Field Data (actual column values)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Typical Record Size: 25-300 bytes[white]
[green]Block Utilization: ~5-60% of data area[white]
[yellow]Optimizations:[white]
• Instant ADD COLUMN support
• Online DDL compatibility  
• Compressed metadata encoding

[gray]Current format since MySQL 8.0.28[white]
[gray]Can span multiple blocks for very large records[white]`,
		},
		70: {
			ID: 70, Name: "MLOG_REC_UPDATE_IN_PLACE", Category: "Record Operations (Current)",
			Description: "Update record in place",
			Format: `[cyan]═══ MLOG_REC_UPDATE_IN_PLACE Format ═══[white]

[yellow]Header:[white]
• Type:     1 byte  (0x46)
• Space ID: Compressed (1-5 bytes)
• Page No:  Compressed (1-5 bytes)

[yellow]Update Info:[white]
• Flags:          1 byte
• Cursor Offset:  2 bytes
• Update Count:   Compressed

[yellow]Update Vectors:[white]
For each field update:
• Field Number:   Compressed
• Field Length:   Compressed  
• Field Data:     Variable

[yellow]Special Flags:[white]
• 0x01: Update affects indexed columns
• 0x02: Update changes row format
• 0x04: Update is partial (BLOB)

[green]Optimized for:[white]
• Minimal logging of changes
• Efficient replay during recovery
• Support for partial BLOB updates

[gray]Only logs changed fields, not entire record[white]`,
		},
		69: {
			ID: 69, Name: "MLOG_REC_DELETE", Category: "Record Operations (Current)",
			Description: "Delete record",
			Format: `[cyan]═══ MLOG_REC_DELETE Format ═══[white]

[yellow]Header:[white]
• Type:     1 byte  (0x45)
• Space ID: Compressed (1-5 bytes)
• Page No:  Compressed (1-5 bytes)

[yellow]Body:[white]
• Cursor Offset:   2 bytes
• Record Size:     Compressed

[yellow]Delete Info:[white]
• Will be Purged:  1 bit flag
• Has Undo Info:   1 bit flag
• Transaction ID:  6 bytes (if has undo)
• Undo Number:     Compressed (if has undo)

[green]Process:[white]
1. Record is marked deleted (not removed)
2. Purge thread removes later
3. Space reclaimed during page reorganize

[gray]Two-phase delete for MVCC consistency[white]`,
		},

		// Page operations
		19: {
			ID: 19, Name: "MLOG_PAGE_CREATE", Category: "Page Operations",
			Description: "Create an index page",
			Format: `[cyan]═══ MLOG_PAGE_CREATE Format ═══[white]

[yellow]Header:[white]
• Type:     1 byte  (0x13)
• Space ID: Compressed (1-5 bytes)
• Page No:  Compressed (1-5 bytes)

[yellow]Page Info:[white]
• Page Type:    1 byte
• Index ID:     8 bytes
• Page Level:   2 bytes

[yellow]Page Types:[white]
• 0x45BD: B-tree node
• 0x45BF: B-tree leaf  
• 0x0002: Undo log page
• 0x0003: Inode page
• 0x0004: Insert buffer free list
• 0x0005: Insert buffer bitmap
• 0x0006: System page
• 0x0007: Transaction system
• 0x0008: BLOB page

[green]Initializes:[white]
• Page header (38 bytes)
• Page trailer (8 bytes)
• Infimum and supremum records

[gray]Creates new page in buffer pool[white]`,
		},
		72: {
			ID: 72, Name: "MLOG_PAGE_REORGANIZE", Category: "Page Operations",
			Description: "Reorganize page",
			Format: `[cyan]═══ MLOG_PAGE_REORGANIZE Format ═══[white]

[yellow]Header:[white]
• Type:     1 byte  (0x48)
• Space ID: Compressed (1-5 bytes)
• Page No:  Compressed (1-5 bytes)

[yellow]Reorganize Info:[white]
• Has Index Info: 1 bit
• Is Compressed:  1 bit
• Index Info:     Variable (if flag set)

[green]Purpose:[white]
• Defragments page
• Reclaims deleted record space
• Optimizes record storage
• Rebuilds page directory

[yellow]Process:[white]
1. Copy all valid records to temp
2. Clear page body
3. Re-insert records optimally
4. Update page header stats

[gray]Triggered when page becomes fragmented[white]`,
		},

		// Transaction operations
		31: {
			ID: 31, Name: "MLOG_MULTI_REC_END", Category: "Transaction Control",
			Description: "Multi-record group end marker",
			Format: `[cyan]═══ MLOG_MULTI_REC_END Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_MULTI_REC_END Record Format:[white]
  Byte 0:       Type = 0x1F (1 byte)
  Byte 1:       Length = 0x00 (1 byte) - no body data

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Record Size: 2 bytes (minimal)[white]
[green]Block Utilization: ~0.4% of data area[white]
[yellow]Purpose:[white]
• Marks end of atomic operation group
• All records since group start must succeed
• Used for multi-statement transactions
• Critical for crash recovery consistency

[yellow]Multi-Record Group in Block:[white]
  Record 1: [Operation start] (e.g., MLOG_REC_INSERT)
  Record 2: [Related operation] (e.g., MLOG_REC_UPDATE)  
  Record 3: [Another operation]
  ...
  Record N: MLOG_MULTI_REC_END (this record)
  
[gray]Remaining space: 494 bytes available for other records[white]
[gray]Often packed with other operations in same block[white]`,
		},

		// File operations
		33: {
			ID: 33, Name: "MLOG_FILE_CREATE", Category: "File Operations",
			Description: "File create",
			Format: `[cyan]═══ MLOG_FILE_CREATE Format ═══[white]

[yellow]Header:[white]
• Type:     1 byte  (0x21)
• Length:   Variable

[yellow]Body:[white]
• Space ID:       4 bytes
• Tablespace Flags: 4 bytes
• File Name Len:  2 bytes
• File Name:      Variable string

[yellow]Tablespace Flags Include:[white]
• Page size (3 bits)
• Compressed flag (1 bit)
• Encryption flag (1 bit)
• Shared tablespace (1 bit)

[green]Creates:[white]
• New .ibd file
• Initial file pages
• Metadata structures

[gray]Logged before physical file creation[white]`,
		},

		// Special operations
		30: {
			ID: 30, Name: "MLOG_WRITE_STRING", Category: "Page Operations",
			Description: "Write a string to a page",
			Format: `[cyan]═══ MLOG_WRITE_STRING Format ═══[white]

[yellow]Header:[white]
• Type:     1 byte  (0x1E)
• Space ID: Compressed (1-5 bytes)
• Page No:  Compressed (1-5 bytes)

[yellow]Body:[white]
• Page Offset: 2 bytes
• String Len:  2 bytes
• String Data: Variable bytes

[green]Usage:[white]
• Write arbitrary data to page
• Update page headers
• Modify system records
• Used when other types don't fit

[gray]Generic write operation[white]`,
		},
		62: {
			ID: 62, Name: "MLOG_TABLE_DYNAMIC_META", Category: "Metadata Operations",
			Description: "Dynamic table metadata",
			Format: `[cyan]═══ MLOG_TABLE_DYNAMIC_META Format within 512-byte Block ═══[white]

[yellow]512-Byte Block Structure:[white]
[cyan]Block Header (12 bytes):[white]
  Offset 0-3:   Block Number (4 bytes)
  Offset 4-5:   Data Length (2 bytes)
  Offset 6-7:   First Record Group Offset (2 bytes)
  Offset 8-11:  Epoch Number (4 bytes)

[cyan]Record Data Area (496 bytes available):[white]
[yellow]MLOG_TABLE_DYNAMIC_META Record Format:[white]
  Byte 0:       Type = 0x3E (1 byte)
  Byte 1-5:     Length (compressed 1-5 bytes)
  Byte 6-13:    Table ID (8 bytes)
  Byte 14-21:   Version (8 bytes)
  Byte 22:      Metadata Type Flags (1 byte)
  Byte 23-27:   Metadata Length (compressed 1-5 bytes)
  Byte 28-N:    Metadata Content (variable)

[yellow]Metadata Type Flags:[white]
  Bit 0 (0x01): Corrupted flag
  Bit 1 (0x02): Auto-increment value present
  Bit 2 (0x04): Statistics version present
  Bit 3 (0x08): Instant column info present
  Bits 4-7:     Reserved

[yellow]Instant Column Metadata Structure:[white]
  - Original Column Count (compressed 1-5 bytes)
  - Added Column Count (compressed 1-5 bytes)  
  - Default Value Length (compressed 1-5 bytes)
  - Default Values (variable per added column)
  - Column Position Map (variable)

[cyan]Block Trailer (4 bytes):[white]
  Offset 508-511: Checksum (4 bytes)

[green]Typical Record Size: 30-150 bytes[white]
[green]Block Utilization: ~6-30% of data area[white]
[yellow]Purpose:[white]
• Enables online DDL operations
• Instant ADD COLUMN without table rebuild
• Statistics and auto-increment tracking

[gray]Critical for instant schema changes in MySQL 8.0+[white]`,
		},
	}
}

// initializeReference initializes the reference modal with left-right pane layout
func (app *RedoLogApp) initializeReference() {
	// Create left pane - reference list (clickable items)
	app.referenceView = tview.NewList()
	app.referenceView.SetBorder(true)
	app.referenceView.SetTitle(" Redo Log Types ")
	app.referenceView.ShowSecondaryText(true)
	
	// Create right pane - type detail view
	app.typeDetailView = tview.NewTextView()
	app.typeDetailView.SetDynamicColors(true)
	app.typeDetailView.SetScrollable(true)
	app.typeDetailView.SetWrap(true)
	app.typeDetailView.SetWordWrap(true)
	app.typeDetailView.SetBorder(true)
	app.typeDetailView.SetTitle(" Type Details ")
	
	// Set initial content for right pane
	app.typeDetailView.SetText(`[cyan]═══ InnoDB Redo Log Type Reference ═══[white]

[yellow]Welcome to the Type Reference![white]

Select a type from the left panel to view its detailed format information.

[green]Navigation:[white]
• [cyan]↑/↓[white] - Navigate type list
• [cyan]Enter[white] - Show type details
• [cyan]Tab[white] - Switch between panes
• [cyan]ESC/q[white] - Close reference
• [cyan]r[white] - Toggle reference

[yellow]About 512-byte Blocks:[white]
InnoDB redo log records are stored in 512-byte blocks with:
• [cyan]Block Header[white] (12 bytes) - Block metadata
• [cyan]Data Area[white] (496 bytes) - Redo log records  
• [cyan]Block Trailer[white] (4 bytes) - Checksum

Each type shows detailed byte-level formatting within this structure.`)
	
	// Get type information
	typeInfoMap := getTypeInfoMap()
	
	// Add categories and their types
	categories := []struct {
		name  string
		types []uint8
	}{
		{"Basic Byte Operations", []uint8{1, 2, 4, 8}},
		{"Record Operations (Old Format)", []uint8{9, 10, 11, 13, 14, 15, 16, 17, 18}},
		{"Page and Undo Operations", []uint8{19, 20, 21, 22, 24, 25, 26, 27, 28, 29, 30}},
		{"Transaction Control", []uint8{31, 32}},
		{"File Operations", []uint8{33, 34, 35}},
		{"Compressed Record Operations", []uint8{36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46}},
		{"ZIP Page Operations", []uint8{48, 49, 50, 51, 52, 53}},
		{"R-Tree Operations", []uint8{57, 58}},
		{"Special Operations", []uint8{59, 61}},
		{"Metadata Operations", []uint8{62}},
		{"SDI Operations", []uint8{63, 64}},
		{"Extended Operations", []uint8{65, 66}},
		{"Record Operations (Current)", []uint8{67, 68, 69, 70, 71, 72, 73, 74, 75, 76}},
	}
	
	// Add category headers and types
	for _, category := range categories {
		// Add category header
		app.referenceView.AddItem(fmt.Sprintf("[yellow]▶ %s[white]", category.name), "", 0, nil)
		
		// Add types in category
		for _, typeID := range category.types {
			if info, exists := typeInfoMap[typeID]; exists {
				mainText := fmt.Sprintf("  [green]%s (%d)[white]", info.Name, info.ID)
				secondaryText := fmt.Sprintf("[gray]%s[white]", info.Description)
				
				// Capture typeID in closure
				func(capturedTypeID uint8) {
					app.referenceView.AddItem(mainText, secondaryText, 0, func() {
						app.updateTypeDetailPane(capturedTypeID)
					})
				}(typeID)
			} else {
				// Handle missing types with basic info from types package
				logType := types.LogType(typeID)
				mainText := fmt.Sprintf("  [gray]%s (%d)[white]", logType.String(), typeID)
				secondaryText := "[gray]No detailed format available[white]"
				
				func(capturedTypeID uint8) {
					app.referenceView.AddItem(mainText, secondaryText, 0, func() {
						app.updateTypeDetailPane(capturedTypeID)
					})
				}(typeID)
			}
		}
	}
	
	// Create main reference layout (left-right panes)
	mainReferenceLayout := tview.NewFlex()
	mainReferenceLayout.AddItem(app.referenceView, 0, 1, true)        // Left pane (1/3)
	mainReferenceLayout.AddItem(app.typeDetailView, 0, 2, false)      // Right pane (2/3)
	
	// Create reference modal with instructions and main layout
	app.referenceModal = tview.NewFlex().SetDirection(tview.FlexRow)
	
	// Add navigation instructions
	instructions := tview.NewTextView()
	instructions.SetDynamicColors(true)
	instructions.SetText("[yellow]Navigation: ↑/↓=Navigate • Enter=Select • Tab=Switch Panes • ESC/q=Close • r=Toggle[white]")
	instructions.SetTextAlign(tview.AlignCenter)
	
	app.referenceModal.AddItem(instructions, 1, 0, false)
	app.referenceModal.AddItem(mainReferenceLayout, 0, 1, true)
	
	// Set up selection change handler to auto-update right pane
	app.referenceView.SetChangedFunc(func(index int, mainText string, secondaryText string, shortcut rune) {
		// Extract type ID from mainText and update details
		app.handleReferenceSelection(mainText)
	})
	
	// Set up key handlers for the reference view
	app.referenceView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			app.hideReferenceModal()
			return nil
		case tcell.KeyTab:
			app.app.SetFocus(app.typeDetailView)
			return nil
		}
		
		if event.Rune() == 'q' || event.Rune() == 'Q' || event.Rune() == 'r' || event.Rune() == 'R' {
			app.hideReferenceModal()
			return nil
		}
		return event
	})
	
	// Set up key handlers for the type detail view
	app.typeDetailView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			app.hideReferenceModal()
			return nil
		case tcell.KeyTab:
			app.app.SetFocus(app.referenceView)
			return nil
		}
		
		if event.Rune() == 'q' || event.Rune() == 'Q' || event.Rune() == 'r' || event.Rune() == 'R' {
			app.hideReferenceModal()
			return nil
		}
		return event
	})
}

// handleReferenceSelection handles selection change in the reference list
func (app *RedoLogApp) handleReferenceSelection(mainText string) {
	// Extract type ID from mainText using regex
	re := regexp.MustCompile(`\((\d+)\)`)
	matches := re.FindStringSubmatch(mainText)
	
	if len(matches) > 1 {
		// Parse type ID
		if typeID, err := strconv.ParseUint(matches[1], 10, 8); err == nil {
			app.updateTypeDetailPane(uint8(typeID))
		}
	}
}

// updateTypeDetailPane updates the right pane with type details
func (app *RedoLogApp) updateTypeDetailPane(typeID uint8) {
	typeInfoMap := getTypeInfoMap()
	if info, exists := typeInfoMap[typeID]; exists {
		app.typeDetailView.SetText(info.Format)
		app.typeDetailView.SetTitle(fmt.Sprintf(" %s - Format Details ", info.Name))
	} else {
		app.showBasicTypeInfoInPane(typeID)
	}
}

// showBasicTypeInfoInPane shows basic info in the right pane for types without detailed format info
func (app *RedoLogApp) showBasicTypeInfoInPane(typeID uint8) {
	logType := types.LogType(typeID)
	basicInfo := fmt.Sprintf(`[cyan]═══ %s ═══[white]

[yellow]Type ID:[white] %d (0x%02X)
[yellow]Name:[white] %s
[yellow]Category:[white] %s

[green]Basic Information:[white]
This is a MySQL 8.0 InnoDB redo log record type.

[gray]Detailed format information is not available for this type.
This may be because:[white]
• [gray]It's a rarely used or internal type[white]
• [gray]It has a simple format similar to other basic operations[white]
• [gray]Format documentation is not yet implemented[white]

[yellow]General Structure:[white]
All redo log records follow this basic pattern:
• [cyan]Type[white] (1 byte) - The MLOG type identifier
• [cyan]Length[white] (1-5 bytes) - Compressed record length  
• [cyan]Space ID[white] (1-5 bytes) - Tablespace identifier
• [cyan]Page No[white] (1-5 bytes) - Page number within tablespace
• [cyan]Body[white] (Variable) - Type-specific data

[gray]For accurate format details, refer to MySQL source code or documentation.[white]
`, logType.String(), typeID, typeID, logType.String(), 
		func() string {
			if logType.IsTransactional() {
				return "Transactional Operation"
			}
			return "Non-transactional Operation"
		}())
	
	app.typeDetailView.SetText(basicInfo)
	app.typeDetailView.SetTitle(fmt.Sprintf(" %s - Basic Info ", logType.String()))
}

// showReferenceModal displays the reference modal
func (app *RedoLogApp) showReferenceModal() {
	// Initialize reference if not already done
	if app.referenceView == nil {
		app.initializeReference()
	}
	
	// Show the reference modal
	app.app.SetRoot(app.referenceModal, true)
	app.app.SetFocus(app.referenceView)
}


// hideReferenceModal hides the reference modal and returns to main view
func (app *RedoLogApp) hideReferenceModal() {
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

	footerText := fmt.Sprintf(`[yellow]Keys: [bold]'i'[reset][yellow]=INSERT, [bold]'u'[reset][yellow]=UPDATE, [bold]'d'[reset][yellow]=DELETE, [bold]'r'[reset][yellow]=REFERENCE, [bold]Tab[reset][yellow]=Switch Panes [white]| Filters: Table ID 0=%s%s[white] Op=%s[white] | Records: [cyan]%d[white]/[blue]%d`,
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
