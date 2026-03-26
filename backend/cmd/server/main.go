package main

import (
	"log"

	"clawreef/internal/aigateway"
	"clawreef/internal/config"
	"clawreef/internal/db"
	"clawreef/internal/handlers"
	"clawreef/internal/middleware"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	database, err := db.Initialize(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize Kubernetes client
	log.Printf("K8s StorageClass config: %s", cfg.GetStorageClass())
	if err := k8s.Initialize(cfg); err != nil {
		log.Printf("Warning: Failed to initialize Kubernetes client: %v", err)
		log.Println("Instance management features will not work without K8s connectivity")
	} else {
		client := k8s.GetClient()
		log.Printf("Kubernetes client initialized successfully (mode: %s, storageClass: %s)",
			client.GetConnectionMode(), client.StorageClass)
	}

	// Initialize repositories
	userRepo := repository.NewUserRepository(database)
	quotaRepo := repository.NewQuotaRepository(database)
	instanceRepo := repository.NewInstanceRepository(database)
	systemImageSettingRepo := repository.NewSystemImageSettingRepository(database)
	llmModelRepo := repository.NewLLMModelRepository(database)
	modelInvocationRepo := repository.NewModelInvocationRepository(database)
	auditEventRepo := repository.NewAuditEventRepository(database)
	costRecordRepo := repository.NewCostRecordRepository(database)
	chatSessionRepo := repository.NewChatSessionRepository(database)
	chatMessageRepo := repository.NewChatMessageRepository(database)
	riskRuleRepo := repository.NewRiskRuleRepository(database)
	riskHitRepo := repository.NewRiskHitRepository(database)

	// Initialize services
	authService := services.NewAuthService(userRepo, cfg.JWT)
	quotaService := services.NewQuotaService(quotaRepo)
	userService := services.NewUserService(userRepo, quotaRepo)
	systemImageSettingService := services.NewSystemImageSettingService(systemImageSettingRepo)
	llmModelService := services.NewLLMModelService(llmModelRepo)
	modelInvocationService := services.NewModelInvocationService(modelInvocationRepo)
	auditEventService := services.NewAuditEventService(auditEventRepo)
	costRecordService := services.NewCostRecordService(costRecordRepo)
	chatSessionService := services.NewChatSessionService(chatSessionRepo)
	chatMessageService := services.NewChatMessageService(chatMessageRepo)
	riskDetectionService := services.NewRiskDetectionService(riskRuleRepo)
	riskHitService := services.NewRiskHitService(riskHitRepo)
	riskRuleService := services.NewRiskRuleService(riskRuleRepo)
	aiObservabilityService := services.NewAIObservabilityService(modelInvocationRepo, auditEventRepo, costRecordRepo, riskHitRepo, chatMessageRepo, llmModelRepo, instanceRepo, userRepo)
	clusterResourceService := services.NewClusterResourceService(instanceRepo)
	services.SetRuntimeImageSettingsProvider(systemImageSettingService)
	instanceService := services.NewInstanceService(instanceRepo, quotaRepo, llmModelRepo)
	aiGatewayService := aigateway.NewService(llmModelRepo, modelInvocationService, auditEventService, costRecordService, riskDetectionService, riskHitService, chatSessionService, chatMessageService)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService, quotaService)
	instanceHandler := handlers.NewInstanceHandler(instanceService)
	systemSettingsHandler := handlers.NewSystemSettingsHandler(systemImageSettingService)
	llmModelHandler := handlers.NewLLMModelHandler(llmModelService)
	aiGatewayHandler := handlers.NewAIGatewayHandler(aiGatewayService)
	aiObservabilityHandler := handlers.NewAIObservabilityHandler(aiObservabilityService)
	riskRuleHandler := handlers.NewRiskRuleHandler(riskRuleService)
	clusterResourceHandler := handlers.NewClusterResourceHandler(clusterResourceService)
	egressProxyHandler := handlers.NewEgressProxyHandler()

	// Initialize WebSocket hub and handler
	wsHub := services.GetHub()
	wsHandler := handlers.NewWebSocketHandler(wsHub)

	// Start sync service to keep instance status in sync with K8s
	syncService := services.NewSyncService(instanceRepo)
	syncService.Start()
	defer syncService.Stop()

	// Setup router
	r := gin.Default()

	// Middleware
	r.Use(middleware.CORS())
	r.Use(middleware.ErrorHandler())
	r.NoRoute(egressProxyHandler.Handle)
	r.NoMethod(egressProxyHandler.Handle)

	// Routes
	api := r.Group("/api/v1")
	{
		// Auth routes
		auth := api.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.RefreshToken)
			auth.POST("/logout", authHandler.Logout)
			auth.GET("/me", middleware.Auth(), middleware.SetUserInfo(userRepo), authHandler.GetCurrentUser)
			auth.POST("/change-password", middleware.Auth(), authHandler.ChangePassword)
		}

		// User routes (authenticated)
		users := api.Group("/users")
		users.Use(middleware.Auth())
		users.Use(middleware.SetUserInfo(userRepo))
		{
			// Admin only routes
			adminOnly := users.Group("")
			adminOnly.Use(middleware.NewAdminAuth(userRepo))
			{
				adminOnly.GET("", userHandler.ListUsers)
				adminOnly.POST("", userHandler.CreateUser)
				adminOnly.POST("/import", userHandler.ImportUsers)
				adminOnly.DELETE("/:id", userHandler.DeleteUser)
				adminOnly.PUT("/:id/role", userHandler.UpdateRole)
				adminOnly.PUT("/:id/quota", userHandler.UpdateUserQuota)
			}

			// User or admin routes
			users.GET("/:id", userHandler.GetUser)
			users.PUT("/:id", userHandler.UpdateUser)
			users.GET("/:id/quota", userHandler.GetUserQuota)
		}

		// Instance routes (authenticated)
		instances := api.Group("/instances")
		instances.Use(middleware.Auth())
		instances.Use(middleware.SetUserInfo(userRepo))
		{
			instances.GET("", instanceHandler.ListInstances)
			instances.POST("", instanceHandler.CreateInstance)
			instances.GET("/:id", instanceHandler.GetInstance)
			instances.PUT("/:id", instanceHandler.UpdateInstance)
			instances.DELETE("/:id", instanceHandler.DeleteInstance)
			instances.POST("/:id/start", instanceHandler.StartInstance)
			instances.POST("/:id/stop", instanceHandler.StopInstance)
			instances.POST("/:id/restart", instanceHandler.RestartInstance)
			instances.GET("/:id/status", instanceHandler.GetInstanceStatus)
			instances.POST("/:id/access", instanceHandler.GenerateAccessToken)
			instances.GET("/:id/access", instanceHandler.AccessInstance)
			instances.POST("/:id/sync", instanceHandler.ForceSync)
			instances.GET("/:id/openclaw/export", instanceHandler.ExportOpenClaw)
			instances.POST("/:id/openclaw/import", instanceHandler.ImportOpenClaw)
		}

		systemSettings := api.Group("/system-settings")
		systemSettings.Use(middleware.Auth())
		systemSettings.Use(middleware.SetUserInfo(userRepo))
		{
			systemSettings.GET("/images", systemSettingsHandler.ListSystemImageSettings)
		}

		adminSystemSettings := api.Group("/system-settings")
		adminSystemSettings.Use(middleware.Auth())
		adminSystemSettings.Use(middleware.SetUserInfo(userRepo))
		adminSystemSettings.Use(middleware.NewAdminAuth(userRepo))
		{
			adminSystemSettings.PUT("/images", systemSettingsHandler.UpsertSystemImageSetting)
			adminSystemSettings.DELETE("/images/:instanceType", systemSettingsHandler.DeleteSystemImageSetting)
			adminSystemSettings.GET("/cluster-resources", clusterResourceHandler.GetOverview)
		}

		adminModels := api.Group("/admin/models")
		adminModels.Use(middleware.Auth())
		adminModels.Use(middleware.SetUserInfo(userRepo))
		adminModels.Use(middleware.NewAdminAuth(userRepo))
		{
			adminModels.GET("", llmModelHandler.ListModels)
			adminModels.POST("/discover", llmModelHandler.DiscoverModels)
			adminModels.PUT("", llmModelHandler.UpsertModel)
			adminModels.DELETE("/:id", llmModelHandler.DeleteModel)
		}

		adminAIAudit := api.Group("/admin/ai-audit")
		adminAIAudit.Use(middleware.Auth())
		adminAIAudit.Use(middleware.SetUserInfo(userRepo))
		adminAIAudit.Use(middleware.NewAdminAuth(userRepo))
		{
			adminAIAudit.GET("", aiObservabilityHandler.ListAuditItems)
			adminAIAudit.GET("/:traceId", aiObservabilityHandler.GetTraceDetail)
		}

		adminCosts := api.Group("/admin/costs")
		adminCosts.Use(middleware.Auth())
		adminCosts.Use(middleware.SetUserInfo(userRepo))
		adminCosts.Use(middleware.NewAdminAuth(userRepo))
		{
			adminCosts.GET("", aiObservabilityHandler.GetCostOverview)
		}

		adminRiskRules := api.Group("/admin/risk-rules")
		adminRiskRules.Use(middleware.Auth())
		adminRiskRules.Use(middleware.SetUserInfo(userRepo))
		adminRiskRules.Use(middleware.NewAdminAuth(userRepo))
		{
		adminRiskRules.GET("", riskRuleHandler.ListRules)
		adminRiskRules.POST("/test", riskRuleHandler.TestRules)
		adminRiskRules.POST("/bulk-status", riskRuleHandler.BulkUpdateStatus)
		adminRiskRules.PUT("", riskRuleHandler.UpsertRule)
		adminRiskRules.DELETE("/:ruleId", riskRuleHandler.DeleteRule)
	}

		gatewayLLM := api.Group("/gateway/llm")
		gatewayLLM.Use(middleware.GatewayAuth(instanceRepo))
		{
			gatewayLLM.GET("/models", aiGatewayHandler.ListModels)
			gatewayLLM.POST("/chat/completions", aiGatewayHandler.ChatCompletions)
		}

		// Instance proxy routes (token-based auth, no session required)
		// These routes proxy requests to the actual instance pods
		api.Any("/instances/:id/proxy", instanceHandler.ProxyInstance)
		api.Any("/instances/:id/proxy/*path", instanceHandler.ProxyInstance)

		// WebSocket routes
		ws := api.Group("/ws")
		ws.Use(middleware.Auth())
		ws.Use(middleware.SetUserInfo(userRepo))
		{
			ws.GET("", wsHandler.HandleWebSocket)
			ws.GET("/stats", wsHandler.GetConnectionCount)
		}
	}

	// Start server
	log.Printf("Server starting on %s", cfg.Server.Address)
	if err := r.Run(cfg.Server.Address); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
