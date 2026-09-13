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

// LogicOp combines the conditions of a FilterGroup
type LogicOp string

const (
	And LogicOp = "AND"
	Or  LogicOp = "OR"
)

// Filter represents a single WHERE condition. When Group is set the other
// fields are ignored and the filter renders as a parenthesized group
type Filter struct {
	Field string
	Op    Operator
	Value any
	Group *FilterGroup
}

// FilterGroup is a set of conditions combined by a single logical operator
// and wrapped in parentheses
type FilterGroup struct {
	Op      LogicOp
	Filters []Filter
}

// OrGroup returns a Filter that combines the given conditions with OR
//
//	pagination.OrGroup(
//	    pagination.Filter{Field: "name", Op: pagination.ILike, Value: term},
//	    pagination.Filter{Field: "document", Op: pagination.ILike, Value: term},
//	)
func OrGroup(filters ...Filter) Filter { return newGroup(Or, filters) }

// AndGroup returns a Filter that combines the given conditions with AND
func AndGroup(filters ...Filter) Filter { return newGroup(And, filters) }

func newGroup(op LogicOp, filters []Filter) Filter {
	return Filter{Group: &FilterGroup{Op: op, Filters: filters}}
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

// WhereGroup appends a parenthesized group whose conditions are combined
// with op. The group is dropped when build adds no condition
//
//	filters := pagination.NewFilterBuilder().
//	    Where("status", pagination.Eq, "active").
//	    WhereGroup(pagination.Or, func(g *pagination.FilterBuilder) {
//	        g.Where("name", pagination.ILike, term)
//	        g.Where("document", pagination.ILike, term)
//	    }).
//	    Build()
//
//	// status = ? AND (name ILIKE ? OR document ILIKE ?)
func (b *FilterBuilder) WhereGroup(op LogicOp, build func(*FilterBuilder)) *FilterBuilder {
	if build == nil {
		return b
	}

	inner := NewFilterBuilder()
	build(inner)
	if len(inner.filters) == 0 {
		return b
	}

	b.filters = append(b.filters, newGroup(op, inner.filters))

	return b
}

// WhereGroupIf appends the group only when cond is true
func (b *FilterBuilder) WhereGroupIf(cond bool, op LogicOp, build func(*FilterBuilder)) *FilterBuilder {
	if cond {
		return b.WhereGroup(op, build)
	}

	return b
}

// WhereGroupOr appends a parenthesized group whose conditions are combined with OR
func (b *FilterBuilder) WhereGroupOr(build func(*FilterBuilder)) *FilterBuilder {
	return b.WhereGroup(Or, build)
}

// WhereGroupOrIf appends the OR group only when cond is true
func (b *FilterBuilder) WhereGroupOrIf(cond bool, build func(*FilterBuilder)) *FilterBuilder {
	return b.WhereGroupIf(cond, Or, build)
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
