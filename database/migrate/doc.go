// Package migrate provides schema migration and seed data execution using
// golang-migrate.
//
// It supports PostgreSQL, MySQL, and SQLite. Migration files are read from the
// local filesystem; seed files are plain .sql files executed in directory order.
//
// # Basic usage
//
//	m, err := migrate.New(migrate.MigrateConfig{
//	    DSN:            "postgres://user:pass@localhost/mydb?sslmode=disable",
//	    Dialector:      "postgres",
//	    MigrationsPath: "./migrations",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer m.Close()
//
//	if err := m.Migrate(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// # Seeding
//
// Populate executes all .sql files found in PopulationPath in alphabetical
// order. It is a no-op when PopulationPath is empty.
//
//	m, _ := migrate.New(migrate.MigrateConfig{
//	    DSN:            dsn,
//	    Dialector:      "postgres",
//	    MigrationsPath: "./migrations",
//	    PopulationPath: "./seeds",
//	})
//
//	_ = m.Migrate(ctx)
//	_ = m.Populate(ctx)
package migrate
