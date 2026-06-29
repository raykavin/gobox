// Package spreadsheet provides a lightweight writer for generating CSV and
// XLSX files as raw bytes.
//
// # Basic usage
//
//	writer := spreadsheet.New(spreadsheet.Options{})
//
//	data, err := writer.Write(ctx, spreadsheet.FormatXLSX,
//	    spreadsheet.Sheet{
//	        Name:    "Users",
//	        Headers: []string{"ID", "Name", "Active"},
//	        Rows: []spreadsheet.Row{
//	            {1, "Alice", true},
//	            {2, "Bob", false},
//	        },
//	    },
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	os.WriteFile("users.xlsx", data, 0o644)
//
// # CSV output
//
// Pass FormatCSV to write a single flat sheet. Only the first Sheet is used.
//
//	data, err := writer.Write(ctx, spreadsheet.FormatCSV, sheet)
//
// # Custom header style
//
// The default XLSX header style uses bold text with a yellow background. Pass
// an Options.HeaderStyle to replace it.
//
//	writer := spreadsheet.New(spreadsheet.Options{
//	    DefaultSheetName: "Report",
//	    HeaderStyle: &excelize.Style{
//	        Font: &excelize.Font{Bold: true, Color: "#FFFFFF"},
//	        Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#0070C0"}},
//	    },
//	})
package spreadsheet
