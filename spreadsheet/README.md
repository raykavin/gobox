# spreadsheet

The `spreadsheet` package provides a lightweight writer for CSV and XLSX exports. It is intended for reporting and data-export workflows where applications need mixed-type row input, optional multi-sheet XLSX generation, configurable header styling, and context-aware file generation without dealing directly with spreadsheet encoding details.

## Import

```go
import "github.com/raykavin/gokit/spreadsheet"
```

## What it provides

- `Writer` for generating spreadsheet files as raw bytes
- support for `xlsx` and `csv` output through `FormatXLSX` and `FormatCSV`
- `Sheet` definitions with tab name, headers, and rows
- `Row` as a convenient `[]any` alias for mixed value types
- configurable XLSX header styling through `Options.HeaderStyle`
- configurable fallback sheet names through `Options.DefaultSheetName`
- context-aware generation with cancellation checks during encoding

## Main types

- `Format`: supported output format (`FormatXLSX` or `FormatCSV`)
- `Row`: a single data row represented as `[]any`
- `Sheet`: worksheet definition with `Name`, `Headers`, and `Rows`
- `Options`: writer configuration including default sheet name and optional `*excelize.Style`
- `Writer`: generates CSV or XLSX output and returns the encoded bytes

## XLSX example

Use `FormatXLSX` when you want one or more worksheet tabs in the same workbook.

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/raykavin/gokit/spreadsheet"
)

func main() {
	writer := spreadsheet.New(spreadsheet.Options{
		DefaultSheetName: "Report",
	})

	data, err := writer.Write(context.Background(), spreadsheet.FormatXLSX,
		spreadsheet.Sheet{
			Name:    "Users",
			Headers: []string{"ID", "Name", "Active"},
			Rows: []spreadsheet.Row{
				{1, "Alice", true},
				{2, "Bob", false},
			},
		},
		spreadsheet.Sheet{
			Name:    "Orders",
			Headers: []string{"OrderID", "Total"},
			Rows: []spreadsheet.Row{
				{1001, 199.90},
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("report.xlsx", data, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

## CSV example

Use `FormatCSV` when you want a single flat export. Only the first `Sheet` is written in CSV mode.

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/raykavin/gokit/spreadsheet"
)

func main() {
	writer := spreadsheet.New(spreadsheet.Options{})

	data, err := writer.Write(context.Background(), spreadsheet.FormatCSV,
		spreadsheet.Sheet{
			Headers: []string{"Name", "Age", "City"},
			Rows: []spreadsheet.Row{
				{"Alice", 30, "New York"},
				{"Bob", 25, "London"},
			},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile("users.csv", data, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

## Notes

- `Write()` requires at least one `Sheet`, and every sheet must define headers
- `FormatCSV` ignores any additional sheets after the first one
- empty or whitespace-only sheet names fall back to `Options.DefaultSheetName`
- the default XLSX header style uses bold text with a yellow background
- `Options.HeaderStyle` lets callers replace the default XLSX header styling
- XLSX cell values are written through `excelize`, while CSV values are converted with `fmt.Sprint()`
- `Write()` returns raw bytes so callers can save, stream, or attach the generated file as needed
