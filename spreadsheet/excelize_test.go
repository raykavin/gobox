package spreadsheet

import (
	"bytes"
	"context"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// helpers

func newWriter(t *testing.T, opts Options) *Writer {
	t.Helper()
	return New(opts)
}

func defaultWriter(t *testing.T) *Writer {
	t.Helper()
	return newWriter(t, Options{})
}

// parseXLSX opens raw XLSX bytes and returns all rows for the given sheet name.
func parseXLSX(t *testing.T, data []byte, sheetName string) [][]string {
	t.Helper()

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parseXLSX: open reader: %v", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(sheetName)
	if err != nil {
		t.Fatalf("parseXLSX: get rows for sheet %q: %v", sheetName, err)
	}
	return rows
}

// sheetNames returns the ordered list of sheet names in the workbook.
func sheetNames(t *testing.T, data []byte) []string {
	t.Helper()

	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("sheetNames: open reader: %v", err)
	}
	defer func() { _ = f.Close() }()

	return f.GetSheetList()
}

// parseCSV decodes raw CSV bytes into a slice of rows.
func parseCSV(t *testing.T, data []byte) [][]string {
	t.Helper()

	r := csv.NewReader(bytes.NewReader(data))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	return records
}

// ---- Write validation -------------------------------------------------------

func TestWrite_NoSheets(t *testing.T) {
	_, err := defaultWriter(t).Write(context.Background(), FormatXLSX)
	if err == nil {
		t.Fatal("expected error when no sheets provided, got nil")
	}
}

func TestWrite_UnsupportedFormat(t *testing.T) {
	sheet := Sheet{
		Headers: []string{"A"},
		Rows:    []Row{{"1"}},
	}
	_, err := defaultWriter(t).Write(context.Background(), "ods", sheet)
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
}

func TestWrite_EmptyHeaders(t *testing.T) {
	sheet := Sheet{
		Name:    "NoHeaders",
		Headers: []string{},
		Rows:    []Row{{"value"}},
	}
	for _, format := range []Format{FormatXLSX, FormatCSV} {
		_, err := defaultWriter(t).Write(context.Background(), format, sheet)
		if err == nil {
			t.Errorf("format %q: expected error for empty headers, got nil", format)
		}
	}
}

func TestWrite_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sheet := Sheet{
		Headers: []string{"ID", "Name"},
		Rows:    []Row{{1, "Alice"}},
	}
	for _, format := range []Format{FormatXLSX, FormatCSV} {
		_, err := defaultWriter(t).Write(ctx, format, sheet)
		if err == nil {
			t.Errorf("format %q: expected context error, got nil", format)
		}
	}
}

// XLSX

func TestXLSX_SingleSheet_HeadersAndRows(t *testing.T) {
	sheet := Sheet{
		Name:    "Sales",
		Headers: []string{"ID", "Product", "Amount"},
		Rows: []Row{
			{1, "Widget A", 99.90},
			{2, "Widget B", 49.90},
		},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatXLSX, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows := parseXLSX(t, data, "Sales")

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (1 header + 2 data), got %d", len(rows))
	}

	wantHeaders := []string{"ID", "Product", "Amount"}
	for i, h := range wantHeaders {
		if rows[0][i] != h {
			t.Errorf("header[%d]: want %q, got %q", i, h, rows[0][i])
		}
	}

	wantRows := [][]string{
		{"1", "Widget A", "99.9"},
		{"2", "Widget B", "49.9"},
	}
	for ri, want := range wantRows {
		for ci, v := range want {
			if rows[ri+1][ci] != v {
				t.Errorf("row[%d][%d]: want %q, got %q", ri, ci, v, rows[ri+1][ci])
			}
		}
	}
}

func TestXLSX_MultipleSheets_NamesAndOrder(t *testing.T) {
	sheets := []Sheet{
		{Name: "Alpha", Headers: []string{"X"}, Rows: []Row{{"a"}}},
		{Name: "Beta", Headers: []string{"Y"}, Rows: []Row{{"b"}}},
		{Name: "Gamma", Headers: []string{"Z"}, Rows: []Row{{"c"}}},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatXLSX, sheets...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := sheetNames(t, data)
	want := []string{"Alpha", "Beta", "Gamma"}

	if len(names) != len(want) {
		t.Fatalf("sheet count: want %d, got %d", len(want), len(names))
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("sheet[%d]: want %q, got %q", i, n, names[i])
		}
	}
}

func TestXLSX_MultipleSheets_IndependentData(t *testing.T) {
	sheets := []Sheet{
		{
			Name:    "Users",
			Headers: []string{"ID", "Name"},
			Rows:    []Row{{1, "Alice"}, {2, "Bob"}},
		},
		{
			Name:    "Orders",
			Headers: []string{"OrderID", "Total"},
			Rows:    []Row{{100, 250.0}},
		},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatXLSX, sheets...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usersRows := parseXLSX(t, data, "Users")
	if len(usersRows) != 3 {
		t.Errorf("Users: want 3 rows, got %d", len(usersRows))
	}

	ordersRows := parseXLSX(t, data, "Orders")
	if len(ordersRows) != 2 {
		t.Errorf("Orders: want 2 rows, got %d", len(ordersRows))
	}
}

func TestXLSX_DefaultSheetName_WhenNameIsEmpty(t *testing.T) {
	w := newWriter(t, Options{DefaultSheetName: "MyDefault"})

	sheet := Sheet{
		Headers: []string{"Col"},
		Rows:    []Row{{"val"}},
	}

	data, err := w.Write(context.Background(), FormatXLSX, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := sheetNames(t, data)
	if len(names) != 1 || names[0] != "MyDefault" {
		t.Errorf("expected sheet name %q, got %v", "MyDefault", names)
	}
}

func TestXLSX_NoOrphanDefaultSheet(t *testing.T) {
	sheets := []Sheet{
		{Name: "First", Headers: []string{"A"}, Rows: nil},
		{Name: "Second", Headers: []string{"B"}, Rows: nil},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatXLSX, sheets...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := sheetNames(t, data)
	for _, n := range names {
		// excelize creates "Sheet1" by default; it must not survive in output.
		if strings.EqualFold(n, "Sheet1") && n != "First" && n != "Second" {
			t.Errorf("orphan default sheet found: %q", n)
		}
	}
	if len(names) != 2 {
		t.Errorf("expected exactly 2 sheets, got %d: %v", len(names), names)
	}
}

func TestXLSX_EmptyRows(t *testing.T) {
	sheet := Sheet{
		Name:    "Empty",
		Headers: []string{"Col1", "Col2"},
		Rows:    nil,
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatXLSX, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rows := parseXLSX(t, data, "Empty")
	if len(rows) != 1 {
		t.Errorf("expected only the header row, got %d rows", len(rows))
	}
}

func TestXLSX_CustomHeaderStyle(t *testing.T) {
	w := newWriter(t, Options{
		HeaderStyle: &excelize.Style{
			Font: &excelize.Font{Bold: false, Size: 9},
			Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#FF0000"}},
		},
	})

	sheet := Sheet{
		Name:    "Styled",
		Headers: []string{"A", "B"},
		Rows:    []Row{{1, 2}},
	}

	// Just assert it generates without error; style inspection via excelize
	// requires reading the raw XML, which is out of scope for a unit test.
	if _, err := w.Write(context.Background(), FormatXLSX, sheet); err != nil {
		t.Fatalf("unexpected error with custom style: %v", err)
	}
}

// CSV

func TestCSV_HeadersAndRows(t *testing.T) {
	sheet := Sheet{
		Headers: []string{"Name", "Age", "City"},
		Rows: []Row{
			{"Alice", 30, "New York"},
			{"Bob", 25, "London"},
		},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatCSV, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := parseCSV(t, data)

	if len(records) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(records))
	}
	if strings.Join(records[0], ",") != "Name,Age,City" {
		t.Errorf("header mismatch: %v", records[0])
	}
	if records[1][0] != "Alice" || records[1][1] != "30" {
		t.Errorf("row 1 mismatch: %v", records[1])
	}
}

func TestCSV_OnlyFirstSheetIsUsed(t *testing.T) {
	sheets := []Sheet{
		{Headers: []string{"A"}, Rows: []Row{{"first"}}},
		{Headers: []string{"B"}, Rows: []Row{{"second"}}},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatCSV, sheets...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := parseCSV(t, data)

	// Only the first sheet's header should be present.
	if records[0][0] != "A" {
		t.Errorf("expected first sheet header %q, got %q", "A", records[0][0])
	}
	for _, row := range records[1:] {
		for _, cell := range row {
			if cell == "second" {
				t.Error("second sheet data found in CSV output")
			}
		}
	}
}

func TestCSV_EmptyRows(t *testing.T) {
	sheet := Sheet{
		Headers: []string{"Col1", "Col2"},
		Rows:    nil,
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatCSV, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := parseCSV(t, data)
	if len(records) != 1 {
		t.Errorf("expected only the header row, got %d rows", len(records))
	}
}

func TestCSV_ValuesConvertedToString(t *testing.T) {
	sheet := Sheet{
		Headers: []string{"Int", "Float", "Bool"},
		Rows: []Row{
			{42, 3.14, true},
		},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatCSV, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	records := parseCSV(t, data)
	row := records[1]

	if row[0] != "42" {
		t.Errorf("int: want %q, got %q", "42", row[0])
	}
	if row[1] != "3.14" {
		t.Errorf("float: want %q, got %q", "3.14", row[1])
	}
	if row[2] != "true" {
		t.Errorf("bool: want %q, got %q", "true", row[2])
	}
}

func TestXLSX_SheetNameTrimsWhitespace(t *testing.T) {
	sheet := Sheet{
		Name:    "  Spaced  ",
		Headers: []string{"X"},
	}

	data, err := defaultWriter(t).Write(context.Background(), FormatXLSX, sheet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := sheetNames(t, data)
	if names[0] != "Spaced" {
		t.Errorf("expected trimmed sheet name %q, got %q", "Spaced", names[0])
	}
}
