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

func TestEscapeLike(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"plain", "plain"},
		{"50%", "50!%"},
		{"a_b", "a!_b"},
		{"cool!", "cool!!"},
		{`c:\tmp`, `c:\tmp`},
		{"100%_!x", "100!%!_!!x"},
		{42, "42"},
	}

	for _, c := range cases {
		if got := EscapeLike(c.in); got != c.want {
			t.Fatalf("EscapeLike(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFilterBuilderGroupOr(t *testing.T) {
	filters := NewFilterBuilder().
		Where("status", Eq, "active").
		WhereGroupOr(func(g *FilterBuilder) {
			g.Where("description", ILike, "term")
			g.WhereIf(false, "amount", Gte, 10)
			g.Where("amount", Gte, 50)
		}).
		Build()

	if len(filters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(filters))
	}

	group := filters[1].Group
	if group == nil {
		t.Fatalf("expected second filter to carry a group: %+v", filters[1])
	}
	if group.Op != Or {
		t.Fatalf("expected group op %q, got %q", Or, group.Op)
	}
	if len(group.Filters) != 2 {
		t.Fatalf("expected 2 grouped filters, got %d", len(group.Filters))
	}
	if group.Filters[0].Field != "description" || group.Filters[0].Op != ILike {
		t.Fatalf("unexpected grouped filter: %+v", group.Filters[0])
	}
}

func TestFilterBuilderGroupOrSkipsEmptyAndFalseCond(t *testing.T) {
	filters := NewFilterBuilder().
		Where("status", Eq, "active").
		WhereGroupOr(func(g *FilterBuilder) {
			g.WhereIf(false, "description", ILike, "term")
		}).
		WhereGroupOrIf(false, func(g *FilterBuilder) {
			g.Where("amount", Gte, 10)
		}).
		WhereGroupOr(nil).
		Build()

	if len(filters) != 1 {
		t.Fatalf("expected only the simple filter, got %d: %+v", len(filters), filters)
	}
}

func TestApplyFiltersGroupOrGeneratesParenthesizedSQL(t *testing.T) {
	db := newTestDB(t)

	filters := NewFilterBuilder().
		Where("status", Eq, "active").
		WhereGroupOr(func(g *FilterBuilder) {
			g.Where("description", Like, "100%_x")
			g.Where("amount", Gte, 50)
			g.Where("id", In, []int{1, 2})
		}).
		Build()

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyFilters(tx.Model(&testTransaction{}), filters).Find(&[]testTransaction{})
	})

	if !strings.Contains(sql, "(description LIKE") {
		t.Fatalf("expected parenthesized group, got sql: %s", sql)
	}
	if strings.Count(sql, " OR ") != 2 {
		t.Fatalf("expected 2 OR separators, got sql: %s", sql)
	}
	if !strings.Contains(sql, "%100!%!_x%") {
		t.Fatalf("expected escaped LIKE value inside the group, got sql: %s", sql)
	}
	if strings.Count(sql, LikeEscapeClause) != 1 {
		t.Fatalf("expected the grouped LIKE to declare its escape character, got sql: %s", sql)
	}
	if !strings.Contains(sql, "id IN (1,2))") {
		t.Fatalf("expected group to close after the last condition, got sql: %s", sql)
	}
	if !strings.Contains(sql, "status =") {
		t.Fatalf("expected simple filter to be kept, got sql: %s", sql)
	}
}

func TestApplyFiltersNestedGroups(t *testing.T) {
	db := newTestDB(t)

	filters := []Filter{
		OrGroup(
			AndGroup(
				Filter{Field: "status", Op: Eq, Value: "active"},
				Filter{Field: "amount", Op: Gte, Value: 50},
			),
			Filter{Field: "description", Op: Eq, Value: "F"},
		),
	}

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyFilters(tx.Model(&testTransaction{}), filters).Find(&[]testTransaction{})
	})

	if !strings.Contains(sql, "((status = ") || !strings.Contains(sql, " AND amount >= 50) OR description = ") {
		t.Fatalf("unexpected nested group sql: %s", sql)
	}
}

func TestApplyFiltersEmptyGroupEmitsNoClause(t *testing.T) {
	db := newTestDB(t)

	filters := []Filter{
		{Field: "status", Op: Eq, Value: "active"},
		OrGroup(),
	}

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyFilters(tx.Model(&testTransaction{}), filters).Find(&[]testTransaction{})
	})

	if strings.Contains(sql, "()") {
		t.Fatalf("expected empty group to be skipped, got sql: %s", sql)
	}
}

func TestScopeWithGroupOrFiltersRows(t *testing.T) {
	db := newTestDB(t)
	seedTransactions(t, db)

	filters := NewFilterBuilder().
		Where("status", Eq, "active").
		WhereGroupOr(func(g *FilterBuilder) {
			g.Where("description", Like, "A")
			g.Where("amount", Gte, 50)
		}).
		Build()

	query := NewQuery(
		Page{Number: 1, PerPage: 10},
		filters,
		[]Sort{{Field: "amount", Direction: Asc}},
	)

	var total int64
	var rows []testTransaction
	if err := db.Model(&testTransaction{}).
		Scopes(Scope(query, &total)).
		Find(&rows).Error; err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(rows) != 2 || rows[0].Description != "A" || rows[1].Description != "E" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestLikeFiltersDeclareTheEscapeClause(t *testing.T) {
	db := newTestDB(t)

	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return applyFilters(tx.Model(&testTransaction{}), []Filter{
			{Field: "description", Op: Like, Value: "a"},
		}).Find(&[]testTransaction{})
	})

	if !strings.Contains(sql, "description LIKE ? "+LikeEscapeClause) &&
		!strings.Contains(sql, `description LIKE "%a%" `+LikeEscapeClause) {
		t.Fatalf("expected the LIKE filter to declare its escape character, got sql: %s", sql)
	}
}

// TestLikeFilterMatchesWildcardsLiterally is the regression test for the
// escaping being inert: without the ESCAPE clause the escape character is not
// recognised by dialects whose default differs, and a term made of wildcards
// silently matches every row.
func TestLikeFilterMatchesWildcardsLiterally(t *testing.T) {
	db := newTestDB(t)
	rows := []testTransaction{
		{Description: "100% approved", Amount: 1, Status: "active"},
		{Description: "plain one", Amount: 2, Status: "active"},
		{Description: "a_b", Amount: 3, Status: "active"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("failed to seed transactions: %v", err)
	}

	cases := []struct {
		term string
		want int64
	}{
		{"%", 1},
		{"_", 1},
		{"100%", 1},
		{"plain", 1},
		{"!", 0},
	}

	for _, c := range cases {
		var got int64
		err := db.Model(&testTransaction{}).
			Scopes(FilterScope([]Filter{{Field: "description", Op: Like, Value: c.term}})).
			Count(&got).Error
		if err != nil {
			t.Fatalf("count for %q failed: %v", c.term, err)
		}
		if got != c.want {
			t.Fatalf("term %q matched %d rows, want %d", c.term, got, c.want)
		}
	}
}
