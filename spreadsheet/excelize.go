package spreadsheet

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Format represents a supported spreadsheet format.
type Format string

const (
	FormatXLSX Format = "xlsx"
	FormatCSV  Format = "csv"
)

// Row is a generic alias for a single row of data.
type Row []any

// Sheet holds the data for a single worksheet tab (XLSX only).
type Sheet struct {
	// Name is the tab name. Falls back to Options.DefaultSheetName when empty.
	Name    string
	Headers []string
	Rows    []Row
}

// Options configures the Writer behaviour.
type Options struct {
	// DefaultSheetName is used when Sheet.Name is empty.
	// Default: "Sheet1"
	DefaultSheetName string

	// HeaderStyle overrides the default XLSX header cell style.
	// Default: bold font + yellow background fill.
	HeaderStyle *excelize.Style
}

func (o *Options) applyDefaults() {
	if o.DefaultSheetName == "" {
		o.DefaultSheetName = "Sheet1"
	}
	if o.HeaderStyle == nil {
		o.HeaderStyle = &excelize.Style{
			Font: &excelize.Font{Bold: true, Size: 11},
			Fill: excelize.Fill{
				Type:    "pattern",
				Pattern: 1,
				Color:   []string{"#FFFF00"},
			},
		}
	}
}

// Writer generates spreadsheet files in CSV or XLSX format.
type Writer struct {
	opts Options
}

// New creates a new Writer with the given options.
func New(opts Options) *Writer {
	opts.applyDefaults()
	return &Writer{opts: opts}
}

// Write generates a spreadsheet and returns its raw bytes.
//
// For CSV only the first Sheet is used.
func (w *Writer) Write(ctx context.Context, format Format, sheets ...Sheet) ([]byte, error) {
	if len(sheets) == 0 {
		return nil, fmt.Errorf("spreadsheet: at least one sheet is required")
	}
	if !format.valid() {
		return nil, fmt.Errorf("spreadsheet: unsupported format %q", format)
	}
	for i, s := range sheets {
		if len(s.Headers) == 0 {
			return nil, fmt.Errorf("spreadsheet: sheet[%d] has no headers", i)
		}
	}

	type result struct {
		data []byte
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		var (
			data []byte
			err  error
		)
		switch format {
		case FormatXLSX:
			data, err = w.writeXLSX(ctx, sheets)
		case FormatCSV:
			data, err = w.writeCSV(ctx, sheets[0])
		}
		ch <- result{data, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.data, r.err
	}
}

// writeXLSX encodes one or more sheets into XLSX bytes.
func (w *Writer) writeXLSX(ctx context.Context, sheets []Sheet) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	defaultTab := f.GetSheetName(0)
	firstSheet := true

	for _, s := range sheets {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		name := resolveSheetName(s.Name, w.opts.DefaultSheetName)

		if firstSheet {
			if defaultTab != name {
				f.SetSheetName(defaultTab, name)
			}
			firstSheet = false
		} else {
			if _, err := f.NewSheet(name); err != nil {
				return nil, fmt.Errorf("spreadsheet: create sheet %q: %w", name, err)
			}
		}

		if err := w.writeXLSXHeaders(ctx, f, name, s.Headers); err != nil {
			return nil, err
		}
		if err := w.writeXLSXRows(ctx, f, name, s.Rows); err != nil {
			return nil, err
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("spreadsheet: encode xlsx: %w", err)
	}

	return buf.Bytes(), nil
}

// writeXLSXHeaders writes the header row and applies the configured style.
func (w *Writer) writeXLSXHeaders(ctx context.Context, f *excelize.File, sheet string, headers []string) error {
	for col, h := range headers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cell, err := excelize.CoordinatesToCellName(col+1, 1)
		if err != nil {
			return fmt.Errorf("spreadsheet: header cell name: %w", err)
		}
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return fmt.Errorf("spreadsheet: set header value: %w", err)
		}
	}

	styleID, err := f.NewStyle(w.opts.HeaderStyle)
	if err != nil {
		return fmt.Errorf("spreadsheet: create header style: %w", err)
	}

	start, _ := excelize.CoordinatesToCellName(1, 1)
	end, _ := excelize.CoordinatesToCellName(len(headers), 1)
	if err := f.SetCellStyle(sheet, start, end, styleID); err != nil {
		return fmt.Errorf("spreadsheet: apply header style: %w", err)
	}

	return nil
}

// writeXLSXRows writes data rows starting at row 2.
func (w *Writer) writeXLSXRows(ctx context.Context, f *excelize.File, sheet string, rows []Row) error {
	for ri, row := range rows {
		for ci, val := range row {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			cell, err := excelize.CoordinatesToCellName(ci+1, ri+2)
			if err != nil {
				return fmt.Errorf("spreadsheet: row cell name: %w", err)
			}
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return fmt.Errorf("spreadsheet: set row value: %w", err)
			}
		}
	}
	return nil
}

// writeCSV encodes a single sheet into CSV bytes.
func (w *Writer) writeCSV(ctx context.Context, s Sheet) ([]byte, error) {
	var buf bytes.Buffer

	cw := csv.NewWriter(&buf)

	if err := cw.Write(s.Headers); err != nil {
		return nil, fmt.Errorf("spreadsheet: write csv headers: %w", err)
	}

	for _, row := range s.Rows {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		record := make([]string, len(row))
		for i, v := range row {
			record[i] = fmt.Sprint(v)
		}
		if err := cw.Write(record); err != nil {
			return nil, fmt.Errorf("spreadsheet: write csv row: %w", err)
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return nil, fmt.Errorf("spreadsheet: flush csv: %w", err)
	}

	return buf.Bytes(), nil
}

// valid reports whether f is a supported format.
func (f Format) valid() bool {
	return f == FormatXLSX || f == FormatCSV
}

// resolveSheetName returns name if non-empty, otherwise fallback.
func resolveSheetName(name, fallback string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return fallback
}