package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/db"
	"clawreef/internal/northbound"
	"clawreef/internal/repository"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}
	if !cfg.Northbound.Enabled {
		log.Fatal("northbound gateway is disabled")
	}
	for name, value := range map[string]string{
		"NORTHBOUND_GATEWAY_TLS_CERT_FILE": cfg.Northbound.GatewayTLSCertFile,
		"NORTHBOUND_GATEWAY_TLS_KEY_FILE":  cfg.Northbound.GatewayTLSKeyFile,
		"NORTHBOUND_JWE_PRIVATE_KEY_FILE":  cfg.Northbound.JWEPrivateKeyFile,
	} {
		if strings.TrimSpace(value) == "" {
			log.Fatalf("%s is required", name)
		}
	}
	database, err := db.Connect(cfg.Database)
	if err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	repo := repository.NewNorthboundRepository(database)
	runtimeSettings := northbound.NewDatabaseRuntimeSettings(repo, cfg.Northbound)
	users := repository.NewUserRepository(database)
	auditRepo := repository.NewAuditEventRepositoryExistingTable(database)
	decryptor, err := northbound.LoadJWEDecryptor(cfg.Northbound.JWEPrivateKeyFile, cfg.Northbound.JWEKeyID)
	if err != nil {
		log.Fatalf("initialize JWE: %v", err)
	}
	authService, err := northbound.NewAuthService(repo, users, cfg.Northbound, decryptor, runtimeSettings)
	if err != nil {
		log.Fatalf("initialize northbound authentication: %v", err)
	}
	coreClient, err := northbound.NewCoreClient(cfg.Northbound)
	if err != nil {
		log.Fatalf("initialize northbound Core client: %v", err)
	}
	coreClient.UseRuntimeSettings(runtimeSettings)
	serverCertificate, err := tls.LoadX509KeyPair(cfg.Northbound.GatewayTLSCertFile, cfg.Northbound.GatewayTLSKeyFile)
	if err != nil {
		log.Fatalf("initialize northbound gateway TLS: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(cfg.Northbound.GatewayTrustedProxies); err != nil {
		log.Fatalf("configure northbound trusted proxies: %v", err)
	}
	router.Use(gin.Recovery(), northbound.RequestContext(), northbound.AuditRequests(auditRepo), northbound.RejectSuspiciousRequest(), northbound.BodyLimit(64<<10))
	northbound.RegisterGatewayRoutes(router, northbound.NewAuthHandler(authService), coreClient, runtimeSettings)
	router.NoRoute(northbound.NotFound)
	router.NoMethod(northbound.NotFound)

	server := &http.Server{
		Addr:              cfg.Northbound.GatewayAddress,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{serverCertificate}},
	}
	go func() {
		log.Printf("northbound gateway listening on %s", server.Addr)
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			log.Fatalf("northbound gateway failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
