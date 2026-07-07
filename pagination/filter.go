package pagination

import (
	"strings"
)

type Operator string

const (
	Eq        Operator = "="
	Neq       Operator = "<>"
	Gt        Operator = ">"
	Gte       Operator = ">="
	Lt        Operator = "<"
	Lte       Operator = "<="
	Like      Operator = "LIKE"
	ILike     Operator = "ILIKE"
	In        Operator = "IN"
	NotIn     Operator = "NOT IN"
	IsNull    Operator = "IS NULL"
	IsNotNull Operator = "IS NOT NULL"
)

// SortDirection for ORDER BY clauses
type SortDirection string

const (
	Asc  SortDirection = "ASC"
	Desc SortDirection = "DESC"
)

// Filter represents a single WHERE condition
type Filter struct {
	Field string
	Op    Operator
	Value any
}

// Sort represents a single ORDER BY clause
type Sort struct {
	Field     string
	Direction SortDirection
}

// Query aggregates Page, Filters and Sorts in a single request object
type Query struct {
	Page    Page
	Filters []Filter
	Sorts   []Sort
}

// NewQuery returns a Query with normalized defaults
func NewQuery(page Page, filters []Filter, sorts []Sort) Query {
	page.Normalize()
	return Query{
		Page:    page,
		Filters: filters,
		Sorts:   sorts,
	}
}

// FilterBuilder provides a fluent API to build []Filter
//
//	filters := paginator.NewFilterBuilder()
//	    Where("status", paginator.Eq, "active")
//	    Where("amount", paginator.Gte, 100)
//	    Build()
type FilterBuilder struct {
	filters []Filter
}

func NewFilterBuilder() *FilterBuilder { return &FilterBuilder{} }

func (b *FilterBuilder) Where(field string, op Operator, value any) *FilterBuilder {
	b.filters = append(b.filters, Filter{
		Field: field,
		Op:    op,
		Value: derefVal(value),
	})
	return b
}

func (b *FilterBuilder) WhereIf(cond bool, field string, op Operator, value any) *FilterBuilder {
	if cond {
		return b.Where(field, op, value)
	}

	return b
}

func (b *FilterBuilder) Build() []Filter { return b.filters }

// SortBuilder provides a fluent API to build []Sort
//
//	sorts := paginator.NewSortBuilder()
//	    OrderBy("created_at", paginator.Desc)
//	    Build()
type SortBuilder struct {
	sorts []Sort
}

func NewSortBuilder() *SortBuilder { return &SortBuilder{} }

func (b *SortBuilder) OrderBy(field string, dir SortDirection) *SortBuilder {
	b.sorts = append(b.sorts, Sort{
		Field:     field,
		Direction: dir,
	})

	return b
}

// ParseSort parses a comma-separated sort string like "name asc,created_at desc"
func ParseSort(raw string) []Sort {
	var sorts []Sort
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens := strings.Fields(part)
		dir := Asc
		if len(tokens) == 2 && strings.EqualFold(tokens[1], "desc") {
			dir = Desc
		}
		sorts = append(sorts, Sort{Field: tokens[0], Direction: dir})
	}
	return sorts
}

func (b *SortBuilder) Build() []Sort { return b.sorts }
