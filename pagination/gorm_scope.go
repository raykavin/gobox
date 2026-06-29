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
		db = applySorts(db, q.Sorts)

		// Count on a cloned session so we
		// don't mutate the main query statement.
		db.Session(&gorm.Session{}).Count(total)

		return db.
			Limit(q.Page.PerPage).
			Offset(q.Page.Offset())
	}
}

func applyFilters(db *gorm.DB, filters []Filter) *gorm.DB {
	for _, f := range filters {
		switch f.Op {
		case IsNull:
			db = db.Where(fmt.Sprintf("%s IS NULL", f.Field))
		case IsNotNull:
			db = db.Where(fmt.Sprintf("%s IS NOT NULL", f.Field))
		case In, NotIn:
			db = db.Where(fmt.Sprintf("%s %s (?)", f.Field, f.Op), f.Value)
		case Like, ILike:
			db = db.Where(fmt.Sprintf("%s %s ?", f.Field, f.Op), fmt.Sprintf("%%%v%%", f.Value))
		default:
			db = db.Where(fmt.Sprintf("%s %s ?", f.Field, f.Op), f.Value)
		}
	}
	return db
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
