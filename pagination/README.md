# pagination

Offset-based pagination for Go + GORM with a fluent filter/sort API.

## Features

- `Page` struct that embeds directly into request DTOs and binds from query-string params
- Generic `Result[T]` response envelope with metadata (`total`, `total_pages`, `has_next`, `has_prev`)
- Fluent `FilterBuilder` with conditional helpers (`WhereIf`)
- Fluent `SortBuilder` and a `ParseSort` helper for user-supplied sort strings
- Single `Scope` function that plugs into any `*gorm.DB` chain

## Installation

```bash
go get github.com/raykavin/gobox/pagination
```

## Usage

### 1. Embed `Page` in your request DTO

```go
import "github.com/raykavin/gobox/pagination"

type ListTransactionsRequest struct {
    pagination.Page
    Status    string `form:"status"`
    MinAmount string `form:"min_amount"`
    Sort      string `form:"sort"` // e.g. "created_at desc,amount asc"
}
```

Query-string params `page` and `per_page` bind automatically. Missing or invalid values are normalised to safe defaults.

### 2. Build filters

```go
fb := pagination.NewFilterBuilder().
    WhereIf(req.Status != "", "status", pagination.Eq, req.Status)

if req.MinAmount != "" {
    if v, err := strconv.ParseFloat(req.MinAmount, 64); err == nil {
        fb.Where("amount", pagination.Gte, v)
    }
}

filters := fb.Build()
```

`WhereIf` only appends the condition when the first argument (`cond`) is `true`, making optional filters concise.

### 3. Build sorts

Parse from a user-supplied string:

```go
sorts := pagination.ParseSort(req.Sort) // "name asc,created_at desc"
```

Or build programmatically with fallback defaults:

```go
if len(sorts) == 0 {
    sorts = pagination.NewSortBuilder().
        OrderBy("created_at", pagination.Desc).
        Build()
}
```

### 4. Assemble a `Query` and execute

```go
query := pagination.NewQuery(req.Page, filters, sorts)

var rows []Transaction
var total int64

db.Model(&Transaction{}).
    Scopes(pagination.Scope(query, &total)).
    Find(&rows)
```

`Scope` applies filters, sorts, counts the total rows (without LIMIT/OFFSET) and then applies `LIMIT`/`OFFSET` in one shot.

### 5. Return the response envelope

```go
result := pagination.NewResult(rows, int(total), query.Page)
c.JSON(http.StatusOK, result)
```

JSON output:

```json
{
  "data": [...],
  "total": 42,
  "page": 2,
  "per_page": 20,
  "total_pages": 3,
  "has_next": true,
  "has_prev": true
}
```

## Full handler example (Gin)

```go
package main

import (
    "net/http"
    "strconv"

    "github.com/gin-gonic/gin"
    "github.com/raykavin/gobox/pagination"
    "gorm.io/gorm"
)

type Transaction struct {
    ID          uint    `json:"id"          gorm:"primaryKey"`
    Description string  `json:"description"`
    Amount      float64 `json:"amount"`
    Status      string  `json:"status"`
}

type ListTransactionsRequest struct {
    pagination.Page
    Status    string `form:"status"`
    MinAmount string `form:"min_amount"`
    Sort      string `form:"sort"`
}

type TransactionHandler struct{ db *gorm.DB }

func (h *TransactionHandler) List(c *gin.Context) {
    var req ListTransactionsRequest
    if err := c.ShouldBindQuery(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    fb := pagination.NewFilterBuilder().
        WhereIf(req.Status != "", "status", pagination.Eq, req.Status)

    if req.MinAmount != "" {
        if v, err := strconv.ParseFloat(req.MinAmount, 64); err == nil {
            fb.Where("amount", pagination.Gte, v)
        }
    }

    sorts := pagination.ParseSort(req.Sort)
    if len(sorts) == 0 {
        sorts = pagination.NewSortBuilder().
            OrderBy("created_at", pagination.Desc).
            Build()
    }

    query := pagination.NewQuery(req.Page, fb.Build(), sorts)

    var rows []Transaction
    var total int64
    h.db.Model(&Transaction{}).
        Scopes(pagination.Scope(query, &total)).
        Find(&rows)

    c.JSON(http.StatusOK, pagination.NewResult(rows, int(total), query.Page))
}
```

## Reference

### Constants

| Constant     | Value | Description                            |
|--------------|-------|----------------------------------------|
| `DefPage`    | `1`   | Default page number                    |
| `DefPerPage` | `20`  | Default items per page                 |
| `MaxPerPage` | `100` | Maximum allowed value for `per_page`   |

### Filter operators

| Operator    | SQL equivalent    |
|-------------|-------------------|
| `Eq`        | `=`               |
| `Neq`       | `<>`              |
| `Gt`        | `>`               |
| `Gte`       | `>=`              |
| `Lt`        | `<`               |
| `Lte`       | `<=`              |
| `Like`      | `LIKE '%…%'`      |
| `ILike`     | `ILIKE '%…%'`     |
| `In`        | `IN (?)`          |
| `NotIn`     | `NOT IN (?)`      |
| `IsNull`    | `IS NULL`         |
| `IsNotNull` | `IS NOT NULL`     |

### Sort directions

| Constant | SQL equivalent |
|----------|----------------|
| `Asc`    | `ASC`          |
| `Desc`   | `DESC`         |

### `ParseSort` string format

A comma-separated list of `field [asc|desc]` tokens. Direction is case-insensitive and defaults to `ASC` when omitted.

```
"created_at desc,name asc,amount"
```

## License

Same as the parent module.
