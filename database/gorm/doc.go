// Package gorm provides a GORM database connection factory with connection
// pooling, structured logging, and retry support.
//
// Supported dialectors: postgres, mysql, mariadb, sqlite, sqlserver, mssql.
//
// # Basic usage
//
//	db, err := gorm.New(&gorm.GormConfig{
//	    DSN:       "host=localhost user=app password=secret dbname=mydb sslmode=disable",
//	    Dialector: "postgres",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer func() {
//	    sqlDB, _ := db.DB()
//	    _ = sqlDB.Close()
//	}()
//
// # Starting from defaults
//
// DefaultGormConfig returns a sensible starting point with 50 open/idle
// connections, 1 h lifetime, prepared statement caching, and info-level logging.
//
//	cfg := gorm.DefaultGormConfig()
//	cfg.DSN       = os.Getenv("DATABASE_URL")
//	cfg.Dialector = "postgres"
//
//	db, err := gorm.New(cfg)
//
// # Connection pool inspection
//
//	stats, err := gorm.GetConnectionStats(db)
//	fmt.Println(stats.OpenConnections)
package gorm
