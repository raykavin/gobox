# pagination

Offset-based pagination for Go + GORM with a fluent filter/sort API.

## Features

- `Page` request params with `Normalize` for safe defaults and a `MaxPerPage` ceiling
- Generic `Result[T]` response envelope with metadata (`total`, `total_pages`, `has_next`, `has_prev`)
- Fluent `FilterBuilder` with conditional helpers (`WhereIf`)
- Fluent `SortBuilder` and a `ParseSort` helper for user-supplied sort strings
- Single `Scope` function that plugs into any `*gorm.DB` chain, plus `FilterScope` for non-paginated lookups
- `Presenter` for mapping a `Result[T]` to a `Result[E]` of response DTOs

## Installation

```bash
go get github.com/raykavin/gobox/pagination
```

## Usage

### 1. Accept the params in your request DTO

```go
import "github.com/raykavin/gobox/pagination"

type ListTransactionsRequest struct {
    Page      int    `form:"page"`
    PerPage   int    `form:"per_page"`
    Status    string `form:"status"`
    MinAmount string `form:"min_amount"`
    Sort      string `form:"sort"` // e.g. "created_at desc,amount asc"
}

func (r ListTransactionsRequest) page() pagination.Page {
    return pagination.Page{Number: r.Page, PerPage: r.PerPage}
}
```

`Page` carries only `json` and `gorm` struct tags, not `form` ones, so embedding it does **not** give you query-string binding for free: Gin's `ShouldBindQuery` would look for `?Number=` and `?PerPage=`. Declare your own `form`-tagged fields as above and build a `Page` from them.

Whatever values arrive, `NewQuery` calls `Normalize` on the page, so a zero or negative page becomes `DefPage`, a zero or negative size becomes `DefPerPage`, and anything above `MaxPerPage` is clamped down to it.

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

**`ParseSort` does not validate field names.** It takes whatever identifier the caller supplies, and `Scope` interpolates it directly into `ORDER BY`. Feeding a raw query-string parameter straight in, as the line above does, is a SQL injection vector. Whitelist the sortable columns first:

```go
var sortable = map[string]bool{"created_at": true, "amount": true, "status": true}

sorts := pagination.ParseSort(req.Sort)
safe := sorts[:0]
for _, srt := range sorts {
    if sortable[srt.Field] {
        safe = append(safe, srt)
    }
}
sorts = safe
```

The same applies to `Filter.Field`. Sort *directions* are safe: anything that is not `ASC` or `DESC` is coerced to `ASC`.

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
query := pagination.NewQuery(req.page(), filters, sorts)

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
    Page      int    `form:"page"`
    PerPage   int    `form:"per_page"`
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

    page := pagination.Page{Number: req.Page, PerPage: req.PerPage}
    query := pagination.NewQuery(page, fb.Build(), sorts)

    var rows []Transaction
    var total int64
    h.db.Model(&Transaction{}).
        Scopes(pagination.Scope(query, &total)).
        Find(&rows)

    c.JSON(http.StatusOK, pagination.NewResult(rows, int(total), query.Page))
}
```

## Reusing filters without pagination

`FilterScope` applies only the filters, so `First`/`Take` style lookups can share the same `FilterBuilder` API:

```go
filters := pagination.NewFilterBuilder().
    Where("email", pagination.Eq, "user@example.com").
    Build()

var user User
db.Model(&User{}).
    Scopes(pagination.FilterScope(filters)).
    First(&user)
```

## Mapping rows to response DTOs

`Presenter` converts a `Result[T]` into a `Result[E]` while carrying every metadata field across, so entities never have to leak into the API layer:

```go
result := pagination.NewResult(rows, int(total), query.Page)

c.JSON(http.StatusOK, pagination.Presenter(result, func(txs []Transaction) []TransactionDTO {
    out := make([]TransactionDTO, 0, len(txs))
    for _, t := range txs {
        out = append(out, toDTO(t))
    }
    return out
}))
```

## Field names and SQL safety

Filter and sort **values** are always passed to GORM as bound parameters, and `Like`/`ILike` values additionally have `%`, `_`, and `\` escaped so user input matches literally inside the surrounding `%...%`.

Field **names** are a different matter: they are column identifiers, which cannot be parameterized, so `Scope`, `FilterScope`, and `applySorts` interpolate them into the SQL text as given. Any field name that can originate from a request must be validated against a whitelist before it reaches a `Filter` or a `Sort`.

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
| `Like`      | `LIKE '%…%'`, with wildcards in the value escaped |
| `ILike`     | `ILIKE '%…%'`, with wildcards in the value escaped |
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
