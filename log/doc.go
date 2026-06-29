// Package log provides a structured logger built on top of zerolog with
// colored console output, JSON mode, API request logging, and field helpers.
//
// # Creating a logger
//
//	zl, err := log.New(&log.Config{
//	    Level:          "info",
//	    DateTimeLayout: time.RFC3339,
//	    Colored:        true,
//	    JSONFormat:     false,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Basic logging
//
//	zl.Info("server started")
//	zl.Warnf("retrying in %s", delay)
//	zl.WithField("user", userID).Error("authentication failed")
//
// # Wrapping in Logger
//
// Logger embeds Zerolog and adds convenience methods compatible with common
// logging interfaces.
//
//	logger := &log.Logger{Zerolog: zl}
//	logger.Infof("listening on :%d", port)
//
// # API request logging
//
//	zl.API(r.Method, r.URL.Path, r.RemoteAddr, status, duration)
package log
