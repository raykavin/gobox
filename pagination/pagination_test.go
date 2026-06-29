package pagination

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testTransaction struct {
	ID          uint
	Description string
	Amount      float64
	Status      string
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&testTransaction{}); err != nil {
		t.Fatalf("failed to migrate test table: %v", err)
	}
	return db
}

func seedTransactions(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []testTransaction{
		{Description: "A", Amount: 10, Status: "active"},
		{Description: "B", Amount: 20, Status: "active"},
		{Description: "C", Amount: 30, Status: "active"},
		{Description: "D", Amount: 40, Status: "active"},
		{Description: "E", Amount: 50, Status: "active"},
		{Description: "F", Amount: 5, Status: "inactive"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("failed to seed transactions: %v", err)
	}
}

func TestPageNormalizeAndOffset(t *testing.T) {
	p := Page{Number: 0, PerPage: 0}
	p.Normalize()

	if p.Number != DefPage {
		t.Fatalf("expected default page number %d, got %d", DefPage, p.Number)
	}
	if p.PerPage != DefPerPage {
		t.Fatalf("expected default per page %d, got %d", DefPerPage, p.PerPage)
	}
	if p.Offset() != 0 {
		t.Fatalf("expected offset 0, got %d", p.Offset())
	}

	p = Page{Number: 3, PerPage: 500}
	p.Normalize()
	if p.PerPage != MaxPerPage {
		t.Fatalf("expected capped per page %d, got %d", MaxPerPage, p.PerPage)
	}
	if p.Offset() != 200 {
		t.Fatalf("expected offset 200, got %d", p.Offset())
	}
}

func TestNewResult(t *testing.T) {
	p := Page{Number: 2, PerPage: 2}
	result := NewResult([]int{3, 4}, 5, p)

	if result.TotalPages != 3 {
		t.Fatalf("expected total pages 3, got %d", result.TotalPages)
	}
	if !result.HasNext {
		t.Fatalf("expected has_next=true")
	}
	if !result.HasPrev {
		t.Fatalf("expected has_prev=true")
	}
}

func TestNewQueryNormalizesPage(t *testing.T) {
	q := NewQuery(Page{Number: 0, PerPage: 1000}, nil, nil)

	if q.Page.Number != DefPage {
		t.Fatalf("expected normalized page number %d, got %d", DefPage, q.Page.Number)
	}
	if q.Page.PerPage != MaxPerPage {
		t.Fatalf("expected normalized per page %d, got %d", MaxPerPage, q.Page.PerPage)
	}
}

func TestFilterBuilder(t *testing.T) {
	filters := NewFilterBuilder().
		Where("status", Eq, "active").
		WhereIf(false, "amount", Gte, 10).
		WhereIf(true, "amount", Gte, 10).
		Build()

	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}
	if filters[0].Field != "status" || filters[0].Op != Eq || filters[0].Value != "active" {
		t.Fatalf("unexpected first filter: %+v", filters[0])
	}
	if filters[1].Field != "amount" || filters[1].Op != Gte || filters[1].Value != 10 {
		t.Fatalf("unexpected second filter: %+v", filters[1])
	}
}

func TestSortBuilderAndParseSort(t *testing.T) {
	sorts := NewSortBuilder().
		OrderBy("created_at", Desc).
		OrderBy("name", Asc).
		Build()

	if len(sorts) != 2 {
		t.Fatalf("expected 2 sorts, got %d", len(sorts))
	}

	parsed := ParseSort("name asc, created_at DESC,updated_at")
	if len(parsed) != 3 {
		t.Fatalf("expected 3 parsed sorts, got %d", len(parsed))
	}

	if parsed[0].Field != "name" || parsed[0].Direction != Asc {
		t.Fatalf("unexpected parsed[0]: %+v", parsed[0])
	}
	if parsed[1].Field != "created_at" || parsed[1].Direction != Desc {
		t.Fatalf("unexpected parsed[1]: %+v", parsed[1])
	}
	if parsed[2].Field != "updated_at" || parsed[2].Direction != Asc {
		t.Fatalf("unexpected parsed[2]: %+v", parsed[2])
	}
}

func TestApplySortsDefaultsInvalidDirectionToAsc(t *testing.T) {
	db := newTestDB(t)

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applySorts(tx.Model(&testTransaction{}), []Sort{{Field: "amount", Direction: SortDirection("up")}}).Find(&[]testTransaction{})
	})

	if !strings.Contains(sql, "ORDER BY amount ASC") {
		t.Fatalf("expected ORDER BY amount ASC, got sql: %s", sql)
	}
}

func TestApplyFiltersGeneratesExpectedClauses(t *testing.T) {
	db := newTestDB(t)

	filters := []Filter{
		{Field: "status", Op: Eq, Value: "active"},
		{Field: "amount", Op: Gte, Value: 10},
		{Field: "description", Op: Like, Value: "A"},
		{Field: "id", Op: In, Value: []int{1, 2, 3}},
		{Field: "deleted_at", Op: IsNull},
	}

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyFilters(tx.Model(&testTransaction{}), filters).Find(&[]testTransaction{})
	})

	checks := []string{
		"status =",
		"amount >=",
		"description LIKE",
		"id IN",
		"deleted_at IS NULL",
	}
	for _, want := range checks {
		if !strings.Contains(sql, want) {
			t.Fatalf("expected sql to contain %q, got: %s", want, sql)
		}
	}
}

func TestScopeAppliesFiltersSortsAndPagination(t *testing.T) {
	db := newTestDB(t)
	seedTransactions(t, db)

	page := Page{Number: 2, PerPage: 2}
	query := NewQuery(
		page,
		[]Filter{{Field: "status", Op: Eq, Value: "active"}},
		[]Sort{{Field: "amount", Direction: Desc}},
	)

	var total int64
	var rows []testTransaction
	err := db.Model(&testTransaction{}).
		Scopes(Scope(query, &total)).
		Find(&rows).Error
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if total != 5 {
		t.Fatalf("expected total 5, got %d", total)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Amount != 30 || rows[1].Amount != 20 {
		t.Fatalf("unexpected rows for page 2 sorted desc by amount: %+v", rows)
	}
}
