// Package pagination provides building blocks for cursor-free, offset-based
// pagination with filtering and sorting, designed to work seamlessly with GORM.
//
// # Overview
//
// The package exposes three concerns:
//
//   - Page / Result request params and response envelope
//   - Filter / Sort builders fluent APIs to compose WHERE and ORDER BY clauses
//   - Scope a GORM scope that wires everything together in a single call
//
// # Quick start
//
//	// 1. Embed Page in your request DTO so it binds from query-string params.
//	type ListReq struct {
//	    pagination.Page
//	    Status string `form:"status"`
//	}
//
//	// 2. Build filters and sorts.
//	filters := pagination.NewFilterBuilder().
//	    WhereIf(req.Status != "", "status", pagination.Eq, req.Status).
//	    Build()
//
//	sorts := pagination.ParseSort(req.Sort) // "created_at desc,amount asc"
//	if len(sorts) == 0 {
//	    sorts = pagination.NewSortBuilder().
//	        OrderBy("created_at", pagination.Desc).
//	        Build()
//	}
//
//	// 3. Assemble a Query (normalises the page automatically).
//	query := pagination.NewQuery(req.Page, filters, sorts)
//
//	// 4. Execute with GORM.
//	var rows []MyModel
//	var total int64
//	db.Model(&MyModel{}).
//	    Scopes(pagination.Scope(query, &total)).
//	    Find(&rows)
//
//	// 5. Build the response envelope.
//	result := pagination.NewResult(rows, int(total), query.Page)
//
// # Defaults and limits
//
// When the caller omits pagination params, [Page.Normalize] applies safe defaults:
//   - page defaults to 1 ([DefPage])
//   - per_page defaults to 20 ([DefPerPage])
//   - per_page is capped at 100 ([MaxPerPage])
//
// # Supported filter operators
//
//	pagination.Eq        // =
//	pagination.Neq       // <>
//	pagination.Gt        // >
//	pagination.Gte       // >=
//	pagination.Lt        // <
//	pagination.Lte       // <=
//	pagination.Like      // LIKE  (value wrapped with % and escaped)
//	pagination.ILike     // ILIKE (value wrapped with % and escaped)
//	pagination.In        // IN (?)
//	pagination.NotIn     // NOT IN (?)
//	pagination.IsNull    // IS NULL
//	pagination.IsNotNull // IS NOT NULL
//
// # Grouping conditions
//
// Filters are AND-chained. [FilterBuilder.WhereGroupOr] declares a group of
// conditions combined with OR and wrapped in parentheses, which is then
// AND-ed with the remaining filters:
//
//	filters := pagination.NewFilterBuilder().
//	    Where("status", pagination.Eq, "active").
//	    WhereGroupOrIf(term != "", func(g *pagination.FilterBuilder) {
//	        g.Where("description", pagination.ILike, term)
//	        g.Where("document", pagination.ILike, term)
//	    }).
//	    Build()
//
//	// WHERE status = ? AND (description ILIKE ? OR document ILIKE ?)
//
// Groups support every operator a simple filter does, escape Like/ILike
// values the same way, and are carried inside the same []Filter, so [Query],
// [Scope] and [FilterScope] need no extra wiring. [FilterBuilder.WhereGroup]
// takes the [LogicOp] explicitly, and [OrGroup] / [AndGroup] build the same
// value for []Filter literals and nested groups.
//
// # LIKE escaping
//
// Like/ILike values are wrapped in %...%, escaped automatically and rendered
// with an ESCAPE clause so the escaping is honored by every dialect.
// [EscapeLike] and [LikeEscapeClause] expose the same pair for raw SQL written
// outside the builder, and must be used together:
//
//	db.Where("name ILIKE ? "+pagination.LikeEscapeClause,
//	    "%"+pagination.EscapeLike(term)+"%",
//	)
//
// The escape character is [LikeEscapeChar], not a backslash: MySQL parses
// backslashes inside string literals, so no single ESCAPE '\' text is valid
// on Postgres, SQLite and MySQL at once.
package pagination
