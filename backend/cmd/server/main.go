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
	openClawConfigRepo := repository.NewOpenClawConfigRepository(database)
	instanceAgentRepo := repository.NewInstanceAgentRepository(database)
	instanceRuntimeStatusRepo := repository.NewInstanceRuntimeStatusRepository(database)
	instanceDesiredStateRepo := repository.NewInstanceDesiredStateRepository(database)
	instanceCommandRepo := repository.NewInstanceCommandRepository(database)
	instanceConfigRevisionRepo := repository.NewInstanceConfigRevisionRepository(database)
	skillRepo := repository.NewSkillRepository(database)
	securityScanRepo := repository.NewSecurityScanRepository(database)

	if repaired, repairErr := services.RepairSeededAdminPassword(userRepo); repairErr != nil {
		log.Printf("Warning: failed to repair seeded admin password: %v", repairErr)
	} else if repaired {
		log.Printf("Repaired seeded admin password hash for default admin account")
	}

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
	openClawConfigService := services.NewOpenClawConfigService(openClawConfigRepo)
	objectStorageService, err := services.NewObjectStorageService(cfg.ObjectStorage)
	if err != nil {
		log.Fatalf("Failed to initialize object storage: %v", err)
	}
	skillScannerClient := services.NewSkillScannerClient(cfg.SkillScanner)
	aiObservabilityService := services.NewAIObservabilityService(modelInvocationRepo, auditEventRepo, costRecordRepo, riskHitRepo, chatMessageRepo, llmModelRepo, instanceRepo, userRepo)
	clusterResourceService := services.NewClusterResourceService(instanceRepo)
	services.SetRuntimeImageSettingsProvider(systemImageSettingService)
	instanceService := services.NewInstanceService(instanceRepo, quotaRepo, llmModelRepo, openClawConfigService)
	instanceAgentService := services.NewInstanceAgentService(instanceRepo, instanceAgentRepo, instanceDesiredStateRepo, instanceRuntimeStatusRepo, instanceCommandRepo)
	instanceRuntimeStatusService := services.NewInstanceRuntimeStatusService(instanceRuntimeStatusRepo, instanceAgentRepo, instanceDesiredStateRepo)
	instanceCommandService := services.NewInstanceCommandService(instanceCommandRepo, instanceRuntimeStatusRepo, instanceDesiredStateRepo)
	instanceConfigRevisionService := services.NewInstanceConfigRevisionService(instanceConfigRevisionRepo)
	skillService := services.NewSkillService(skillRepo, instanceRepo, instanceCommandService, objectStorageService, skillScannerClient)
	securityScanService := services.NewSecurityScanService(securityScanRepo, skillRepo, objectStorageService, skillScannerClient)
	aiGatewayService := aigateway.NewService(llmModelRepo, modelInvocationService, auditEventService, costRecordService, riskDetectionService, riskHitService, chatSessionService, chatMessageService)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService, quotaService)
	instanceHandler := handlers.NewInstanceHandler(instanceService, instanceAgentService, instanceRuntimeStatusService, instanceCommandService, instanceConfigRevisionService, openClawConfigService, skillService)
	systemSettingsHandler := handlers.NewSystemSettingsHandler(systemImageSettingService)
	llmModelHandler := handlers.NewLLMModelHandler(llmModelService)
	aiGatewayHandler := handlers.NewAIGatewayHandler(aiGatewayService)
	aiObservabilityHandler := handlers.NewAIObservabilityHandler(aiObservabilityService)
	riskRuleHandler := handlers.NewRiskRuleHandler(riskRuleService)
	clusterResourceHandler := handlers.NewClusterResourceHandler(clusterResourceService)
	egressProxyHandler := handlers.NewEgressProxyHandler()
	openClawConfigHandler := handlers.NewOpenClawConfigHandler(openClawConfigService)
	skillHandler := handlers.NewSkillHandler(skillService, instanceService)
	securityHandler := handlers.NewSecurityHandler(securityScanService)
	agentHandler := handlers.NewAgentHandler(instanceAgentService, instanceCommandService, instanceRuntimeStatusService, instanceConfigRevisionService, skillService)

	// Initialize WebSocket hub and handler
	wsHub := services.GetHub()
	wsHandler := handlers.NewWebSocketHandler(wsHub)

	// Start sync service to keep instance status in sync with K8s
	syncService := services.NewSyncService(instanceRepo, instanceRuntimeStatusService)
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
			instances.GET("/:id/runtime", instanceHandler.GetRuntimeDetails)
			instances.POST("/:id/runtime/:command", instanceHandler.CreateRuntimeCommand)
			instances.GET("/:id/config/revisions", instanceHandler.ListConfigRevisions)
			instances.POST("/:id/config/revisions/publish", instanceHandler.PublishConfigRevision)
			instances.POST("/:id/access", instanceHandler.GenerateAccessToken)
			instances.GET("/:id/access", instanceHandler.AccessInstance)
			instances.POST("/:id/sync", instanceHandler.ForceSync)
			instances.GET("/:id/openclaw/export", instanceHandler.ExportOpenClaw)
			instances.POST("/:id/openclaw/import", instanceHandler.ImportOpenClaw)
			instances.GET("/:id/skills", skillHandler.ListInstanceSkills)
			instances.POST("/:id/skills", skillHandler.AttachSkillToInstance)
			instances.DELETE("/:id/skills/:skillId", skillHandler.RemoveSkillFromInstance)
		}

		// Admin console: cross-user instance listing. Gated by admin
		// middleware — non-admin callers get 403. The workspace
		// /instances endpoint above stays caller-scoped regardless of
		// role; admin status only unlocks this dedicated surface.
		adminInstances := api.Group("/admin/instances")
		adminInstances.Use(middleware.Auth())
		adminInstances.Use(middleware.SetUserInfo(userRepo))
		adminInstances.Use(middleware.NewAdminAuth(userRepo))
		{
			adminInstances.GET("", instanceHandler.ListAllInstances)
		}

		openClawConfigs := api.Group("/openclaw-configs")
		openClawConfigs.Use(middleware.Auth())
		openClawConfigs.Use(middleware.SetUserInfo(userRepo))
		{
			openClawConfigs.GET("/resources", openClawConfigHandler.ListResources)
			openClawConfigs.POST("/resources", openClawConfigHandler.CreateResource)
			openClawConfigs.POST("/resources/validate", openClawConfigHandler.ValidateResource)
			openClawConfigs.GET("/resources/:id", openClawConfigHandler.GetResource)
			openClawConfigs.PUT("/resources/:id", openClawConfigHandler.UpdateResource)
			openClawConfigs.DELETE("/resources/:id", openClawConfigHandler.DeleteResource)
			openClawConfigs.POST("/resources/:id/clone", openClawConfigHandler.CloneResource)

			openClawConfigs.GET("/bundles", openClawConfigHandler.ListBundles)
			openClawConfigs.POST("/bundles", openClawConfigHandler.CreateBundle)
			openClawConfigs.GET("/bundles/:id", openClawConfigHandler.GetBundle)
			openClawConfigs.PUT("/bundles/:id", openClawConfigHandler.UpdateBundle)
			openClawConfigs.DELETE("/bundles/:id", openClawConfigHandler.DeleteBundle)
			openClawConfigs.POST("/bundles/:id/clone", openClawConfigHandler.CloneBundle)

			openClawConfigs.POST("/compile-preview", openClawConfigHandler.CompilePreview)
			openClawConfigs.GET("/injections", openClawConfigHandler.ListSnapshots)
			openClawConfigs.GET("/injections/:id", openClawConfigHandler.GetSnapshot)
		}

		skills := api.Group("/skills")
		skills.Use(middleware.Auth())
		skills.Use(middleware.SetUserInfo(userRepo))
		{
			skills.GET("", skillHandler.ListSkills)
			skills.POST("/import", skillHandler.ImportSkills)
			skills.GET("/:id", skillHandler.GetSkill)
			skills.PUT("/:id", skillHandler.UpdateSkill)
			skills.DELETE("/:id", skillHandler.DeleteSkill)
			skills.GET("/:id/download", skillHandler.DownloadSkill)
			skills.GET("/:id/versions", skillHandler.ListVersions)
			skills.GET("/:id/scan-results", skillHandler.ListScanResults)
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

		adminSkills := api.Group("/admin/skills")
		adminSkills.Use(middleware.Auth())
		adminSkills.Use(middleware.SetUserInfo(userRepo))
		adminSkills.Use(middleware.NewAdminAuth(userRepo))
		{
			adminSkills.GET("", skillHandler.ListAllSkills)
		}

		adminSecurity := api.Group("/admin/security")
		adminSecurity.Use(middleware.Auth())
		adminSecurity.Use(middleware.SetUserInfo(userRepo))
		adminSecurity.Use(middleware.NewAdminAuth(userRepo))
		{
			adminSecurity.GET("/config", securityHandler.GetConfig)
			adminSecurity.PUT("/config", securityHandler.SaveConfig)
			adminSecurity.POST("/scan-jobs", securityHandler.StartScan)
			adminSecurity.POST("/skills/:id/rescan", securityHandler.RescanSkill)
			adminSecurity.GET("/scan-jobs", securityHandler.ListJobs)
			adminSecurity.GET("/scan-jobs/:id", securityHandler.GetJob)
		}

		gatewayLLM := api.Group("/gateway/llm")
		gatewayLLM.Use(middleware.GatewayAuth(instanceRepo))
		{
			gatewayLLM.GET("/models", aiGatewayHandler.ListModels)
			gatewayLLM.POST("/chat/completions", aiGatewayHandler.ChatCompletions)
		}

		agent := api.Group("/agent")
		{
			agent.POST("/register", agentHandler.Register)
			agent.POST("/heartbeat", agentHandler.Heartbeat)
			agent.GET("/commands/next", agentHandler.NextCommand)
			agent.POST("/commands/:id/start", agentHandler.StartCommand)
			agent.POST("/commands/:id/finish", agentHandler.FinishCommand)
			agent.POST("/state/report", agentHandler.ReportState)
			agent.POST("/skills/inventory", agentHandler.ReportSkillInventory)
			agent.POST("/skills/upload", agentHandler.UploadSkillPackage)
			agent.GET("/skills/versions/:skillVersion/download", skillHandler.DownloadSkillVersionForAgent)
			agent.GET("/config/revisions/:id", agentHandler.GetConfigRevision)
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
