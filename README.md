# gokit

`gokit` is a Go module for shared libraries that can be reused across different projects. The goal is to keep common building blocks in one place so teams can reduce code duplication, standardize recurring infrastructure concerns, and move faster when starting or evolving services.

At the moment, the repository provides utilities for configuration loading, HTTP requests, logging, database access, terminal UX, and authenticated pREST integrations, with an emphasis on low coupling and practical reuse between applications.

## Purpose

This module exists to:

- centralize reusable code across projects
- avoid reimplementing the same utilities in multiple repositories
- provide a common base for infrastructure-related concerns
- improve maintenance, consistency, and long-term reuse across services

## Available packages

- [`config`](./config/README.md): configuration loading, validation, reload handling, and environment variable expansion
- [`database`](./database/README.md): typed `database/sql` helpers and GORM bootstrap utilities
- [`http`](./http/README.md): HTTP request helpers, shared header constants, and compressed response handling
- [`logger`](./logger/README.md): reusable structured logging built on top of `zerolog`
- [`prest`](./prest/README.md): authenticated pREST client with typed JSON decoding and token reuse
- [`terminal`](./terminal/README.md): terminal banners, headers, highlighted output, and concurrent progress rendering

## Installation

To use the module in another project:

```bash
go get github.com/raykavin/gokit
```

Then import only the packages you need in the consuming application.

## Documentation index

The root README is intentionally focused on repository-level information. Package-specific usage, examples, and API notes live next to the corresponding code:

- [`config/README.md`](./config/README.md)
- [`database/README.md`](./database/README.md)
- [`http/README.md`](./http/README.md)
- [`logger/README.md`](./logger/README.md)
- [`prest/README.md`](./prest/README.md)
- [`terminal/README.md`](./terminal/README.md)

## Reuse strategy

This repository can grow over time as new shared libraries emerge from real project needs. A good rule of thumb is to move code here when it:

- appears repeatedly across different services
- represents generic infrastructure or integration logic
- is not tightly coupled to the business rules of a single system

That way, each application can stay focused on domain logic while reusing a common foundation for recurring technical concerns.
