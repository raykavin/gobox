package pagination

import "math"

const (
	DefPage    = 1
	DefPerPage = 20
	MaxPerPage = 100
)

// Page holds pagination request params.
type Page struct {
	Number  int `json:"page"     gorm:"page"`
	PerPage int `json:"per_page" gorm:"per_page"`
}

// Normalize ensures sane defaults and enforces MaxPerPage
func (p *Page) Normalize() {
	if p.Number <= 0 {
		p.Number = DefPage
	}
	if p.PerPage <= 0 {
		p.PerPage = DefPerPage
	}
	if p.PerPage > MaxPerPage {
		p.PerPage = MaxPerPage
	}
}

// Offset returns the SQL offset for the current page
func (p *Page) Offset() int {
	return (p.Number - 1) * p.PerPage
}

// Result is the generic paginated response
type Result[T any] struct {
	Data       []T  `json:"data"`
	Total      int  `json:"total"`
	Page       int  `json:"page"`
	PerPage    int  `json:"per_page"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// NewResult builds a Result from a slice, total count and page config
func NewResult[T any](data []T, total int, p Page) Result[T] {
	totalPages := int(math.Ceil(float64(total) / float64(p.PerPage)))
	return Result[T]{
		Data:       data,
		Total:      total,
		Page:       p.Number,
		PerPage:    p.PerPage,
		TotalPages: totalPages,
		HasNext:    p.Number < totalPages,
		HasPrev:    p.Number > 1,
	}
}
