package http

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/http2"
)

var (
	ErrInvalidListenAddress = errors.New("invalid listen address")
	ErrServerNotInitialized = errors.New("server not initialized")
	ErrInvalidSSLConfig     = errors.New("invalid SSL configuration")
	ErrHostResolutionFailed = errors.New("failed to resolve host address")
)

type (
	// Engine wraps Gin engine and implements HttpServer interface
	Engine struct {
		config    *GinConfig
		router    *gin.Engine
		server    *http.Server
		tlsConfig *tls.Config
		addr      string
	}

	// GinConfig holds the configuration for creating a new Gin engine
	GinConfig struct {
		// Basic server config
		Host      string
		Port      uint16
		DebugMode bool

		// Timeouts
		ReadTimeout  time.Duration
		WriteTimeout time.Duration
		IdleTimeout  time.Duration

		// SSL/TLS config
		UseSSL        bool
		SSLCert       string
		SSLKey        string
		MinTLSVersion uint16

		// HTTP/2 config
		EnableHTTP2 bool

		// Route handling
		NoRouteTo   string
		NoRouteJSON bool

		// Middleware config
		UseRecovery    bool
		TrustedProxies []string

		// Request limits
		MaxPayloadSize int64
	}

	// RouteSetup is a function type for setting up routes
	RouteSetup func(*gin.Engine)

	// MiddlewareSetup is a function type for setting up middleware
	MiddlewareSetup func(*gin.Engine)
)

// DefaultGinConfig returns a GinConfig with sensible defaults
func DefaultGinConfig() *GinConfig {
	return &GinConfig{
		Host:           "",
		Port:           8080,
		DebugMode:      false,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MinTLSVersion:  tls.VersionTLS12,
		UseSSL:         false,
		EnableHTTP2:    true,
		UseRecovery:    true,
		NoRouteJSON:    true,
		MaxPayloadSize: 10 * 1024 * 1024, // 10MB default
	}
}

// NewGin creates a new Gin engine with the provided configuration
func NewGin(config *GinConfig) (*Engine, error) {
	if config == nil {
		config = DefaultGinConfig()
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	// Set Gin mode
	setGinMode(config.DebugMode)

	// Create router
	router := createRouter(config)

	// Build address
	addr := buildAddress(config.Host, config.Port)

	// Create engine
	engine := &Engine{
		router: router,
		addr:   addr,
		config: config,
	}

	// Configure TLS if needed
	if config.UseSSL {
		engine.tlsConfig = createTLSConfig(config)
	}

	// Create HTTP server
	engine.server = engine.createHTTPServer()

	// Setup default middleware
	engine.setupDefaultMiddleware()

	// Setup 404 handler
	engine.setupNoRouteHandler()

	return engine, nil
}

// SetupRoutes applies route setup function to the router
func (e *Engine) SetupRoutes(setup RouteSetup) {
	if setup != nil && e.router != nil {
		setup(e.router)
	}
}

// SetupMiddleware applies middleware setup function to the router
func (e *Engine) SetupMiddleware(setup MiddlewareSetup) {
	if setup != nil && e.router != nil {
		setup(e.router)
	}
}

// Use adds middleware to the router
func (e *Engine) Use(middleware ...gin.HandlerFunc) {
	if e.router != nil {
		e.router.Use(middleware...)
	}
}

// Listen starts the HTTP server
func (e *Engine) Listen() error {
	if e.server == nil {
		return ErrServerNotInitialized
	}

	if e.config.UseSSL {
		return e.server.ListenAndServeTLS(
			e.config.SSLCert,
			e.config.SSLKey,
		)
	}

	return e.server.ListenAndServe()
}

func (e Engine) GetPort() uint16 {
	return e.config.Port
}

// ListenAndServeTLS starts the server with TLS
func (e *Engine) ListenAndServeTLS(certFile, keyFile string) error {
	if e.server == nil {
		return ErrServerNotInitialized
	}

	return e.server.ListenAndServeTLS(certFile, keyFile)
}

// Shutdown gracefully shuts down the server
func (e *Engine) Shutdown(ctx context.Context) error {
	if e.server == nil {
		return ErrServerNotInitialized
	}

	return e.server.Shutdown(ctx)
}

// Router provides access to the underlying Gin router
func (e *Engine) Router() *gin.Engine {
	return e.router
}

// Server provides access to the underlying HTTP server
func (e *Engine) Server() *http.Server {
	return e.server
}

// Addr returns the server address
func (e *Engine) Addr() string {
	return e.addr
}

// IsSSLEnabled returns whether SSL is enabled
func (e Engine) IsSSLEnabled() bool {
	return e.config != nil && e.config.UseSSL
}

// createHTTPServer creates the underlying HTTP server
func (e *Engine) createHTTPServer() *http.Server {
	server := &http.Server{
		Addr:         e.addr,
		Handler:      e.router,
		ReadTimeout:  e.config.ReadTimeout,
		WriteTimeout: e.config.WriteTimeout,
		IdleTimeout:  e.config.IdleTimeout,
		TLSConfig:    e.tlsConfig,
	}

	// Configure HTTP/2 if enabled and using SSL
	if e.config.EnableHTTP2 && e.config.UseSSL {
		_ = http2.ConfigureServer(server, &http2.Server{})
	}

	return server
}

// setupDefaultMiddleware sets up default middleware based on config
func (e *Engine) setupDefaultMiddleware() {
	if e.config.UseRecovery {
		e.router.Use(gin.Recovery())
	}

	if e.config.MaxPayloadSize > 0 {
		e.Use(createPayloadLimitMiddleware(e.config.MaxPayloadSize))
	}
}

// setupNoRouteHandler sets up the 404 handler based on config
func (e *Engine) setupNoRouteHandler() {
	e.router.NoRoute(func(c *gin.Context) {
		if e.config.NoRouteTo != "" && !e.config.NoRouteJSON {
			c.Redirect(http.StatusTemporaryRedirect, e.config.NoRouteTo)
			return
		}

		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Não Encontrado",
			"message": "O recurso solicitado não foi encontrado",
			"path":    c.Request.URL.Path,
		})
	})
}

// setGinMode configures the Gin mode
func setGinMode(debugMode bool) {
	if debugMode {
		gin.SetMode(gin.DebugMode)
		return
	}

	gin.SetMode(gin.ReleaseMode)
}

// createRouter creates and configures a new Gin router
func createRouter(config *GinConfig) *gin.Engine {
	router := gin.New()

	if len(config.TrustedProxies) > 0 {
		_ = router.SetTrustedProxies(config.TrustedProxies)
	}

	return router
}

// buildAddress constructs the server address
func buildAddress(host string, port uint16) string {
	if host == "" {
		return fmt.Sprint(":", port)
	}

	return fmt.Sprint(host, ":", port)
}

// createTLSConfig creates TLS configuration
func createTLSConfig(config *GinConfig) *tls.Config {
	minVersion := config.MinTLSVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS13
	}

	return &tls.Config{
		MinVersion: minVersion,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}
}

// createPayloadLimitMiddleware creates middleware to limit request payload size
func createPayloadLimitMiddleware(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(
			c.Writer,
			c.Request.Body,
			maxSize,
		)
		c.Next()
	}
}

// validateConfig validates the engine configuration
func validateConfig(config *GinConfig) error {
	if config.Port == 0 {
		return ErrInvalidListenAddress
	}

	if config.Host != "" && config.Host != ":" {
		addr := buildAddress(config.Host, config.Port)

		if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
			return fmt.Errorf("%w: address=%s: %v", ErrHostResolutionFailed, addr, err)
		}
	}

	if config.UseSSL {
		if config.SSLCert == "" || config.SSLKey == "" {
			return fmt.Errorf("%w: ssl_cert_empty=%t ssl_key_empty=%t",
				ErrInvalidSSLConfig,
				config.SSLCert == "",
				config.SSLKey == "")
		}
	}

	return nil
}
