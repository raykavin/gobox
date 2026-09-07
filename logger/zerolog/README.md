# logger/zerolog

> **Legacy path.** This package is an earlier copy of [`logger`](../README.md), kept so existing imports keep compiling. New code should import `github.com/raykavin/gobox/logger`.

Both packages declare `package logger` and expose the same `Config`, `Zerolog`, and `Logger` types with identical behavior. The only difference is that this copy does **not** provide the runtime level controls:

| Symbol | `logger` | `logger/zerolog` |
|---|---|---|
| `New`, `Config`, `Zerolog`, `Logger` | yes | yes |
| `ErrInvalidLogLevel` | yes | yes |
| `SetLevel(level string) error` | yes | no |
| `Level() string` | yes | no |

## Import

```go
import "github.com/raykavin/gobox/logger/zerolog"
```

Note that the import path ends in `zerolog` but the package name is `logger`, so an explicit alias is usually clearer:

```go
import logger "github.com/raykavin/gobox/logger/zerolog"
```

## Migrating

The two packages are source-compatible in the direction that matters: change the import path and nothing else has to move.

```diff
-import logger "github.com/raykavin/gobox/logger/zerolog"
+import "github.com/raykavin/gobox/logger"
```

See the [`logger` README](../README.md) for the full API, configuration reference, and examples.
