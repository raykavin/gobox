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
//	pagination.Like      // LIKE  (value is automatically wrapped with %)
//	pagination.ILike     // ILIKE (value is automatically wrapped with %)
//	pagination.In        // IN (?)
//	pagination.NotIn     // NOT IN (?)
//	pagination.IsNull    // IS NULL
//	pagination.IsNotNull // IS NOT NULL
package pagination
