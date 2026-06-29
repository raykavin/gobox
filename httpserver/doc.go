// Package httpserver provides a Gin-based HTTP server with TLS, HTTP/2,
// configurable timeouts, trusted proxies, payload size limits, and graceful
// shutdown.
//
// # Basic usage
//
//	engine, err := httpserver.NewGin(httpserver.DefaultGinConfig())
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	engine.SetupRoutes(func(r *gin.Engine) {
//	    r.GET("/ping", func(c *gin.Context) {
//	        c.JSON(http.StatusOK, gin.H{"message": "pong"})
//	    })
//	})
//
//	log.Fatal(engine.Listen())
//
// # Graceful shutdown
//
//	quit := make(chan os.Signal, 1)
//	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
//	go func() { log.Fatal(engine.Listen()) }()
//	<-quit
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	_ = engine.Shutdown(ctx)
//
// # TLS
//
//	cfg := httpserver.DefaultGinConfig()
//	cfg.UseSSL  = true
//	cfg.SSLCert = "/etc/ssl/cert.pem"
//	cfg.SSLKey  = "/etc/ssl/key.pem"
//
//	engine, _ := httpserver.NewGin(cfg)
//	log.Fatal(engine.Listen())
package httpserver
