package pagination

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Scope returns a GORM scope that applies filters, sorts and pagination from a Query
//
//	var users []User
//	var total int64
//
//	q := paginator.NewQuery(page, filters, sorts)
//
//	db.Model(&User{}).
//	    Scopes(paginator.Scope(q, &total)).
//	    Find(&users)
//
//	result := paginator.NewResult(users, int(total), q.Page)
func Scope(q Query, total *int64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		db = applyFilters(db, q.Filters)

		// Count before sorting: ORDER BY is irrelevant for COUNT(*)
		// and just adds planner work on the counting query.
		db.Session(&gorm.Session{}).Count(total)

		db = applySorts(db, q.Sorts)

		return db.
			Limit(q.Page.PerPage).
			Offset(q.Page.Offset())
	}
}

// FilterScope returns a GORM scope that applies only the given filters,
// without pagination or sorting. Useful for First/Take style lookups
// that share the FilterBuilder API
//
//	var user User
//
//	filters := paginator.NewFilterBuilder().
//	    Where("email", paginator.Eq, "user@example.com").
//	    Build()
//
//	db.Model(&User{}).
//	    Scopes(paginator.FilterScope(filters)).
//	    First(&user)
func FilterScope(filters []Filter) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return applyFilters(db, filters)
	}
}

func applyFilters(db *gorm.DB, filters []Filter) *gorm.DB {
	for _, f := range filters {
		expr, args := filterExpr(f)
		if expr == "" {
			continue
		}
		db = db.Where(expr, args...)
	}
	return db
}

func filterExpr(f Filter) (string, []any) {
	if f.Group != nil {
		return groupExpr(*f.Group)
	}

	switch f.Op {
	case IsNull:
		return fmt.Sprintf("%s IS NULL", f.Field), nil
	case IsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", f.Field), nil
	case In, NotIn:
		return fmt.Sprintf("%s %s (?)", f.Field, f.Op), []any{f.Value}
	case Like, ILike:
		return fmt.Sprintf("%s %s ? %s", f.Field, f.Op, LikeEscapeClause),
			[]any{fmt.Sprintf("%%%s%%", EscapeLike(derefVal(f.Value)))}
	default:
		return fmt.Sprintf("%s %s ?", f.Field, f.Op), []any{f.Value}
	}
}

func groupExpr(g FilterGroup) (string, []any) {
	var (
		exprs []string
		args  []any
	)

	for _, f := range g.Filters {
		expr, exprArgs := filterExpr(f)
		if expr == "" {
			continue
		}
		exprs = append(exprs, expr)
		args = append(args, exprArgs...)
	}

	if len(exprs) == 0 {
		return "", nil
	}

	op := g.Op
	if op != Or {
		op = And
	}

	return "(" + strings.Join(exprs, " "+string(op)+" ") + ")", args
}

func applySorts(db *gorm.DB, sorts []Sort) *gorm.DB {
	for _, s := range sorts {
		dir := strings.ToUpper(string(s.Direction))
		if dir != "ASC" && dir != "DESC" {
			dir = "ASC"
		}
		db = db.Order(fmt.Sprintf("%s %s", s.Field, dir))
	}
	return db
}
