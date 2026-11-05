package pagination

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Use Statement.SQL and Statement.Vars: ToSQL interpolates values for logging
// and cannot establish that data was sent separately from the SQL template.
func securityStatement(t *testing.T, filters []Filter, sorts []Sort) *gorm.Statement {
	t.Helper()
	db := newTestDB(t).Session(&gorm.Session{DryRun: true}).Model(&testTransaction{})
	tx := applySorts(applyFilters(db, filters), sorts).Find(&[]testTransaction{})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	return tx.Statement
}

func TestSecurityScalarValuesRemainBound(t *testing.T) {
	payloads := []string{
		"O'Reilly", "' OR 1=1 --", "x'; DROP TABLE test_transactions; --",
		`\' OR 1=1 --`, `" OR "1"="1`, "/* comment */ -- ? ;",
		"100%_!", "ação 日本語 😀", "line\nnext", "x\x00y",
	}
	for _, op := range []Operator{Eq, Neq, Gt, Gte, Lt, Lte, Like, ILike} {
		t.Run(string(op), func(t *testing.T) {
			base := securityStatement(t, []Filter{{Field: "description", Op: op, Value: "baseline"}}, nil).SQL.String()
			for i, payload := range payloads {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					stmt := securityStatement(t, []Filter{{Field: "description", Op: op, Value: payload}}, nil)
					want := payload
					if op == Like || op == ILike {
						want = "%" + EscapeLike(payload) + "%"
					}
					if stmt.SQL.String() != base || !reflect.DeepEqual(stmt.Vars, []any{want}) {
						t.Fatalf("input changed SQL or arguments: SQL=%q vars=%#v", stmt.SQL.String(), stmt.Vars)
					}
				})
			}
		})
	}
}

func TestSecurityListValuesRemainBound(t *testing.T) {
	payload := "x') OR 1=1 --"
	for _, op := range []Operator{In, NotIn} {
		t.Run(string(op), func(t *testing.T) {
			stmt := securityStatement(t, []Filter{{Field: "description", Op: op, Value: []string{"A", payload}}}, nil)
			if strings.Contains(stmt.SQL.String(), payload) || !reflect.DeepEqual(stmt.Vars, []any{"A", payload}) {
				t.Fatalf("list values not bound: %s %#v", stmt.SQL.String(), stmt.Vars)
			}
			db := newTestDB(t)
			seedTransactions(t, db)
			var count int64
			if err := db.Model(&testTransaction{}).Scopes(FilterScope([]Filter{{Field: "description", Op: op, Value: []string{"A", payload}}})).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(1)
			if op == NotIn {
				want = 5
			}
			if count != want {
				t.Fatalf("got %d rows, want %d", count, want)
			}
		})
	}
}

func TestSecurityLikeMatchesLiteralInput(t *testing.T) {
	db := newTestDB(t)
	terms := []string{"%", "_", "!", "!!", "!%_", "100%_!", "O'Reilly", "' OR 1=1 --", "x'; DROP TABLE test_transactions; --", `c:\tmp`, "ação 日本語 😀", "[ab]"}
	rows := []testTransaction{{Description: "ordinary", Status: "active"}}
	for _, term := range terms {
		rows = append(rows, testTransaction{Description: "prefix " + term + " suffix", Status: "active"})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, term := range terms {
		t.Run(term, func(t *testing.T) {
			var got []testTransaction
			if err := db.Scopes(FilterScope([]Filter{{Field: "description", Op: Like, Value: term}})).Order("id").Find(&got).Error; err != nil {
				t.Fatal(err)
			}
			var wantIDs, gotIDs []uint
			for _, row := range rows {
				if strings.Contains(row.Description, term) {
					wantIDs = append(wantIDs, row.ID)
				}
			}
			for _, row := range got {
				gotIDs = append(gotIDs, row.ID)
			}
			if !reflect.DeepEqual(gotIDs, wantIDs) {
				t.Fatalf("matched IDs %v, want %v", gotIDs, wantIDs)
			}
		})
	}
	var total int64
	if err := db.Model(&testTransaction{}).Count(&total).Error; err != nil || total != int64(len(rows)) {
		t.Fatalf("table changed: count=%d error=%v", total, err)
	}
}

func TestSecurityScopeNestedValuesCannotBypassOuterFilter(t *testing.T) {
	db := newTestDB(t)
	seedTransactions(t, db)
	q := NewQuery(Page{Number: 1, PerPage: 2}, []Filter{
		{Field: "status", Op: Eq, Value: "active"},
		OrGroup(
			Filter{Field: "description", Op: Like, Value: "' OR 1=1 --"},
			AndGroup(Filter{Field: "description", Op: Eq, Value: "F"}, Filter{Field: "amount", Op: Gt, Value: 0}),
		),
	}, nil)
	var total int64
	var rows []testTransaction
	if err := db.Model(&testTransaction{}).Scopes(Scope(q, &total)).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("outer filter bypassed: total=%d rows=%v", total, rows)
	}
}

func TestSecuritySortDirectionsCannotInjectSQL(t *testing.T) {
	for _, dir := range []SortDirection{"DESC; SELECT 1 --", "ASC NULLS LAST", "up", "", "desc"} {
		stmt := securityStatement(t, nil, []Sort{{Field: "amount", Direction: dir}})
		want := "ORDER BY amount ASC"
		if dir == "desc" {
			want = "ORDER BY amount DESC"
		}
		if !strings.HasSuffix(stmt.SQL.String(), want) {
			t.Fatalf("direction %q produced %s", dir, stmt.SQL.String())
		}
	}
}

// Characterization tests below deliberately PASS when the current unsafe
// behavior is reproduced. They are exploit demonstrations, NOT security guards.
// Once validation is implemented, replace them with rejection assertions.
func TestSecurityDemonstrationFilterFieldCanInjectPredicate(t *testing.T) {
	db := newTestDB(t)
	seedTransactions(t, db)
	var rows []testTransaction
	filters := []Filter{{Field: "description = description OR 1=1 OR description", Op: Eq, Value: "no such description"}}
	if err := db.Scopes(FilterScope(filters)).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected current field-injection behavior to return all 6 rows, got %d", len(rows))
	}
	t.Log("CONFIRMED: untrusted Filter.Field can alter the WHERE predicate")
}

func TestSecurityDemonstrationOperatorCanInjectPredicate(t *testing.T) {
	db := newTestDB(t)
	seedTransactions(t, db)
	var rows []testTransaction
	filters := []Filter{{Field: "description", Op: Operator("= description OR 1=1 OR description ="), Value: "no such description"}}
	if err := db.Scopes(FilterScope(filters)).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected current operator-injection behavior to return all 6 rows, got %d", len(rows))
	}
	t.Log("CONFIRMED: Operator is a string alias, not a runtime allowlist")
}

func TestSecurityDemonstrationSortFieldsAcceptSQLExpressions(t *testing.T) {
	// No spaces or commas: ParseSort still accepts this scalar subquery.
	payload := "(SELECT/**/1)"
	for name, sorts := range map[string][]Sort{
		"ParseSort":    ParseSort(payload),
		"SortBuilder":  NewSortBuilder().OrderBy(payload, Asc).Build(),
		"Sort literal": {{Field: payload, Direction: Asc}},
	} {
		t.Run(name, func(t *testing.T) {
			stmt := securityStatement(t, nil, sorts)
			if !strings.Contains(stmt.SQL.String(), "ORDER BY "+payload+" ASC") {
				t.Fatalf("expected raw ORDER BY expression, got %s", stmt.SQL.String())
			}
			db := newTestDB(t)
			seedTransactions(t, db)
			var rows []testTransaction
			var total int64
			if err := db.Model(&testTransaction{}).Scopes(Scope(NewQuery(Page{Number: 1, PerPage: 10}, nil, sorts), &total)).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if total != 6 || len(rows) != 6 {
				t.Fatalf("unexpected query result: total=%d rows=%d", total, len(rows))
			}
			t.Log("CONFIRMED: arbitrary scalar subquery reached ORDER BY and executed")
		})
	}
}

func TestSecurityDemonstrationExpressionValuesAreTrustedCode(t *testing.T) {
	// Ordinary JSON strings cannot become gorm.Expr; this is a caller trust
	// boundary, not evidence that a normal bound string is injectable.
	stmt := securityStatement(t, []Filter{{Field: "description", Op: Eq, Value: gorm.Expr("description OR 1=1")}}, nil)
	if !strings.Contains(stmt.SQL.String(), "description = description OR 1=1") || len(stmt.Vars) != 0 {
		t.Fatalf("unexpected expression handling: %s %#v", stmt.SQL.String(), stmt.Vars)
	}
}

func FuzzSecurityEscapeLikeSQLite(f *testing.F) {
	for _, s := range []string{"", "%", "_", "!", "!!%_", "O'Reilly", "' OR 1=1 --", `c:\tmp`, "ação 😀"} {
		f.Add(s)
	}
	// One isolated database per fuzz worker, closed when that worker completes.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		f.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = sqlDB.Close() })
	f.Fuzz(func(t *testing.T, term string) {
		// SQLite LIKE stops at NUL; invalid UTF-8 is not a portable text value.
		if len(term) > 4096 || !utf8.ValidString(term) || strings.ContainsRune(term, 0) {
			t.Skip()
		}
		var same, different bool
		pattern := EscapeLike(term)
		if err := sqlDB.QueryRow("SELECT ? LIKE ? "+LikeEscapeClause+", ? LIKE ? "+LikeEscapeClause, term, pattern, term+"x", pattern).Scan(&same, &different); err != nil {
			t.Fatal(err)
		}
		if !same || different {
			t.Fatalf("literal pattern mismatch: term=%q pattern=%q same=%v different=%v", term, pattern, same, different)
		}
	})
}
