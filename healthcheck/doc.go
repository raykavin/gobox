// Package healthcheck probes registered database connections and collects Go
// runtime diagnostics, returning a structured snapshot suitable for a
// health-check endpoint.
//
// # Creating the service
//
//	svc := healthcheck.New([]healthcheck.DBEntry{
//	    {Name: "primary", Driver: "postgres", DB: primaryDB},
//	    {Name: "replica", Driver: "postgres", DB: replicaDB},
//	})
//
// # Full report
//
// Report probes all registered connections concurrently and returns runtime
// metrics alongside per-connection pool statistics.
//
//	report := svc.Report(ctx)
//
// # Single connection probe
//
//	dbReport, err := svc.CheckDB(ctx, "primary")
//
// # Dynamic registration
//
// Connections can be added or removed at any time without restarting the service.
//
//	svc.AddDB("cache", "redis", cacheDB)
//	svc.RemoveDB("replica")
//
// # HTTP handler example
//
//	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
//	    report := svc.Report(r.Context())
//	    w.Header().Set("Content-Type", "application/json")
//	    json.NewEncoder(w).Encode(report)
//	})
package healthcheck
