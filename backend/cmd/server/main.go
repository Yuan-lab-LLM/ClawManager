package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"clawreef/internal/aigateway"
	"clawreef/internal/config"
	"clawreef/internal/db"
	"clawreef/internal/handlers"
	"clawreef/internal/middleware"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"clawreef/internal/services/leader"

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
	runtimePodRepo := repository.NewRuntimePodRepository(database)
	bindingRepo := repository.NewInstanceRuntimeBindingRepository(database)
	rolloutRepo := repository.NewRuntimeRolloutRepository(database)
	workspaceFileAuditRepo := repository.NewWorkspaceFileAuditRepository(database)
	teamRepo := repository.NewTeamRepository(database)
	skillRepo := repository.NewSkillRepository(database)
	securityScanRepo := repository.NewSecurityScanRepository(database)
	instanceExternalAccessRepo := repository.NewInstanceExternalAccessRepository(database)

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
	openClawConfigService := services.NewOpenClawConfigService(openClawConfigRepo, skillRepo)
	objectStorageService, err := services.NewObjectStorageService(cfg.ObjectStorage)
	if err != nil {
		log.Fatalf("Failed to initialize object storage: %v", err)
	}
	skillScannerClient := services.NewSkillScannerClient(cfg.SkillScanner)
	aiObservabilityService := services.NewAIObservabilityService(modelInvocationRepo, auditEventRepo, costRecordRepo, riskHitRepo, chatMessageRepo, llmModelRepo, instanceRepo, userRepo)
	clusterResourceService := services.NewClusterResourceService(instanceRepo)
	services.SetRuntimeImageSettingsProvider(systemImageSettingService)
	services.SetOpenClawTransferRuntimeRepositories(instanceRepo, bindingRepo, runtimePodRepo)
	runtimeAgentClient := services.NewRuntimeAgentClient(cfg.Runtime.AgentControlToken)
	instanceService := services.NewInstanceService(
		instanceRepo,
		quotaRepo,
		llmModelRepo,
		openClawConfigService,
		services.WithPrivilegedInstancePods(cfg.Kubernetes.Runtime.Pod.Privileged),
		services.WithV2RuntimeLifecycle(runtimePodRepo, bindingRepo, runtimeAgentClient, cfg.Runtime.WorkspaceRoot),
	)
	instanceAgentService := services.NewInstanceAgentService(instanceRepo, instanceAgentRepo, instanceDesiredStateRepo, instanceRuntimeStatusRepo, instanceCommandRepo)
	instanceRuntimeStatusService := services.NewInstanceRuntimeStatusService(instanceRuntimeStatusRepo, instanceAgentRepo, instanceDesiredStateRepo)
	instanceCommandService := services.NewInstanceCommandService(instanceCommandRepo, instanceRuntimeStatusRepo, instanceDesiredStateRepo, skillRepo)
	instanceConfigRevisionService := services.NewInstanceConfigRevisionService(instanceConfigRevisionRepo)
	teamService := services.NewTeamService(
		teamRepo,
		instanceService,
		services.WithTeamRuntimeWorkspaceRoot(cfg.Runtime.WorkspaceRoot),
		services.WithTeamOpenClawConfigService(openClawConfigService),
	)
	var platformRedis services.PlatformRedisClient
	if redisURL := strings.TrimSpace(cfg.Runtime.RedisURL); redisURL != "" {
		var redisErr error
		platformRedis, redisErr = services.NewPlatformRedisClient(redisURL)
		if redisErr != nil {
			log.Printf("platform redis disabled: %v", redisErr)
		}
	} else {
		log.Printf("platform redis disabled: redis url is empty")
	}
	runtimeEvents := services.NewRuntimeEventService(platformRedis)
	workspaceFileService := services.NewWorkspaceFileService(workspaceFileAuditRepo)
	runtimeWorkspaceFileService := services.NewRuntimeWorkspaceFileService(workspaceFileAuditRepo)
	skillService := services.NewSkillService(skillRepo, instanceRepo, instanceCommandService, objectStorageService, skillScannerClient)
	securityScanService := services.NewSecurityScanService(securityScanRepo, skillRepo, objectStorageService, skillScannerClient)
	externalAccessService := services.NewInstanceExternalAccessService(instanceExternalAccessRepo)
	aiGatewayService := aigateway.NewService(llmModelRepo, modelInvocationService, auditEventService, costRecordService, riskDetectionService, riskHitService, chatSessionService, chatMessageService)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userService, quotaService)
	instanceHandler := handlers.NewInstanceHandler(
		instanceService,
		instanceAgentService,
		instanceRuntimeStatusService,
		instanceCommandService,
		instanceConfigRevisionService,
		openClawConfigService,
		skillService,
		externalAccessService,
		services.WithInstanceProxyRuntimeRepositories(instanceRepo, runtimePodRepo, bindingRepo),
	)
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
	teamHandler := handlers.NewTeamHandler(teamService)
	workspaceFileHandler := handlers.NewWorkspaceFileHandler(instanceService, workspaceFileService, runtimeWorkspaceFileService)
	workspaceFileHandler.SetSkillRepository(skillRepo)
	runtimeAgentHandler := handlers.NewRuntimeAgentHandler(cfg.Runtime, runtimePodRepo, bindingRepo, instanceRepo, runtimeEvents)

	// Initialize WebSocket hub and handler
	wsHub := services.GetHub()
	wsHandler := handlers.NewWebSocketHandler(wsHub)
	var runtimeAdminEventBridgeCancel context.CancelFunc
	if platformRedis != nil {
		var bridgeCtx context.Context
		bridgeCtx, runtimeAdminEventBridgeCancel = context.WithCancel(context.Background())
		services.StartRuntimeAdminEventBridge(bridgeCtx, runtimeEvents, wsHub)
	}

	// Control-plane singleton background loops. The HTTP API and the in-pod
	// nginx desktop data plane run on every replica, but these loops must run
	// on exactly one replica. With leader election enabled they only run on the
	// elected leader and migrate on failover; with it disabled (single-replica
	// deployments) they run directly.
	syncService := services.NewSyncService(instanceRepo, instanceRuntimeStatusService)
	var runtimeSchedulerCancel context.CancelFunc
	var runtimeSchedulerMu sync.Mutex
	var runtimeScheduler *services.RuntimeScheduler
	if cfg.Runtime.SchedulerEnabled {
		k8sClient := k8s.GetClient()
		if k8sClient == nil || k8sClient.Clientset == nil {
			log.Printf("runtime scheduler disabled: k8s client is unavailable")
		} else {
			runtimeLeader := services.NewRuntimeLeaderService(k8sClient.Clientset, cfg.Runtime.Namespace, cfg.Runtime.BackendReplicaID)
			runtimeDeployments := k8s.NewRuntimeDeploymentService(k8sClient.Clientset)
			runtimeSchedulerOptions := []services.RuntimeSchedulerOption{
				services.WithRuntimeSchedulerWorkspaceRoot(cfg.Runtime.WorkspaceRoot),
				services.WithRuntimeSchedulerNamespace(cfg.Runtime.Namespace),
				services.WithRuntimeSchedulerGatewayPortRange(cfg.Runtime.GatewayPortStart, cfg.Runtime.GatewayPortEnd),
				services.WithRuntimeSchedulerHeartbeatTimeout(cfg.Runtime.HeartbeatTimeout),
				services.WithRuntimeSchedulerMaxGatewaysPerPod(cfg.Runtime.MaxGatewaysPerPod),
			}
			if gatewayEnvProvider, ok := instanceService.(interface {
				BuildGatewayEnv(*models.Instance) (map[string]string, error)
			}); ok {
				runtimeSchedulerOptions = append(runtimeSchedulerOptions, services.WithRuntimeSchedulerGatewayEnvBuilder(gatewayEnvProvider.BuildGatewayEnv))
			}
			runtimeScheduler = services.NewRuntimeScheduler(
				instanceRepo,
				runtimePodRepo,
				bindingRepo,
				rolloutRepo,
				runtimeAgentClient,
				runtimeEvents,
				runtimeLeader,
				runtimeDeployments,
				cfg.Runtime.SchedulerTick,
				runtimeSchedulerOptions...,
			)
			log.Printf("runtime scheduler initialized")
		}
	} else {
		log.Printf("runtime scheduler disabled by configuration")
	}
	runtimePoolHandler := handlers.NewRuntimePoolHandler(runtimePodRepo, bindingRepo, rolloutRepo, runtimeScheduler, runtimeEvents)

	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	defer leaderCancel()

	startBackground := func(ctx context.Context) {
		log.Printf("Starting leader-only background loops (identity=%s)", cfg.LeaderElection.Identity)
		syncService.Start()
		teamService.StartBackground(ctx)
		if runtimeScheduler != nil {
			runtimeSchedulerMu.Lock()
			if runtimeSchedulerCancel == nil {
				var schedulerCtx context.Context
				schedulerCtx, runtimeSchedulerCancel = context.WithCancel(ctx)
				runtimeScheduler.Start(schedulerCtx)
				log.Printf("runtime scheduler started")
			}
			runtimeSchedulerMu.Unlock()
		}
	}
	stopBackground := func() {
		log.Printf("Stopping leader-only background loops (identity=%s)", cfg.LeaderElection.Identity)
		runtimeSchedulerMu.Lock()
		if runtimeSchedulerCancel != nil {
			runtimeSchedulerCancel()
			runtimeSchedulerCancel = nil
		}
		runtimeSchedulerMu.Unlock()
		teamService.StopBackground()
		syncService.Stop()
	}

	if cfg.LeaderElection.Enabled && k8s.GetClient() != nil && k8s.GetClient().Clientset != nil {
		go leader.Run(leaderCtx, k8s.GetClient().Clientset, leader.Config{
			Namespace:     cfg.LeaderElection.Namespace,
			LeaseName:     cfg.LeaderElection.LeaseName,
			Identity:      cfg.LeaderElection.Identity,
			LeaseDuration: time.Duration(cfg.LeaderElection.LeaseDuration) * time.Second,
			RenewDeadline: time.Duration(cfg.LeaderElection.RenewDeadline) * time.Second,
			RetryPeriod:   time.Duration(cfg.LeaderElection.RetryPeriod) * time.Second,
		}, leader.Callbacks{
			OnStartedLeading: startBackground,
			OnStoppedLeading: stopBackground,
		})
	} else {
		log.Println("Leader election disabled or K8s unavailable; running control-plane background loops directly")
		startBackground(leaderCtx)
	}

	// Setup router
	r := gin.Default()

	// Middleware
	r.Use(middleware.CORS())
	r.Use(middleware.ErrorHandler())
	r.NoRoute(egressProxyHandler.Handle)
	r.NoMethod(egressProxyHandler.Handle)

	// Routes
	r.Any("/s/:code", instanceHandler.OpenShortExternalAccess)
	r.Any("/s/:code/*path", instanceHandler.OpenShortExternalAccess)

	api := r.Group("/api/v1")
	{
		runtimeAgent := api.Group("/runtime-agent")
		{
			runtimeAgent.POST("/register", runtimeAgentHandler.Register)
			runtimeAgent.POST("/heartbeat", runtimeAgentHandler.Heartbeat)
			runtimeAgent.POST("/metrics/report", runtimeAgentHandler.ReportMetrics)
			runtimeAgent.POST("/gateways/report", runtimeAgentHandler.ReportGateways)
			runtimeAgent.POST("/skills/report", runtimeAgentHandler.ReportSkills)
		}

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
			instances.POST("/batch/lite", instanceHandler.BatchCreateLiteInstances)
			instances.POST("/batch/delete", instanceHandler.BatchDeleteLiteInstances)
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
			instances.GET("/:id/shell", instanceHandler.StreamShell)
			instances.POST("/:id/sync", instanceHandler.ForceSync)
			instances.GET("/:id/openclaw/export", instanceHandler.ExportOpenClaw)
			instances.POST("/:id/openclaw/import", instanceHandler.ImportOpenClaw)
			instances.GET("/:id/hermes/export", instanceHandler.ExportHermes)
			instances.POST("/:id/hermes/import", instanceHandler.ImportHermes)
			instances.GET("/:id/external-access", instanceHandler.GetExternalAccess)
			instances.POST("/:id/external-access/share-link", instanceHandler.EnableShareLink)
			instances.POST("/:id/external-access/password", instanceHandler.CreateExternalAccessPassword)
			instances.DELETE("/:id/external-access", instanceHandler.DisableExternalAccess)
			instances.GET("/:id/workspace/files", workspaceFileHandler.List)
			instances.GET("/:id/workspace/preview", workspaceFileHandler.Preview)
			instances.GET("/:id/workspace/download", workspaceFileHandler.Download)
			instances.POST("/:id/workspace/upload", workspaceFileHandler.Upload)
			instances.POST("/:id/workspace/folders", workspaceFileHandler.Mkdir)
			instances.PATCH("/:id/workspace/entries", workspaceFileHandler.Rename)
			instances.DELETE("/:id/workspace/entries", workspaceFileHandler.Delete)
			instances.GET("/:id/skills", skillHandler.ListInstanceSkills)
			instances.GET("/:id/skills/available", skillHandler.ListAvailableInstanceSkills)
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

		adminRuntime := api.Group("/admin")
		adminRuntime.Use(middleware.Auth())
		adminRuntime.Use(middleware.SetUserInfo(userRepo))
		adminRuntime.Use(middleware.NewAdminAuth(userRepo))
		{
			adminRuntime.GET("/runtime-pods", runtimePoolHandler.ListPods)
			adminRuntime.GET("/runtime-pods/:id/gateways", runtimePoolHandler.GetPodGateways)
			adminRuntime.POST("/runtime-pods/:id/drain", runtimePoolHandler.DrainPod)
			adminRuntime.POST("/runtime-rollouts", runtimePoolHandler.StartRollout)
		}

		teams := api.Group("/teams")
		teams.Use(middleware.Auth())
		teams.Use(middleware.SetUserInfo(userRepo))
		{
			teams.GET("", teamHandler.ListTeams)
			teams.POST("", teamHandler.CreateTeam)
			teams.GET("/:id", teamHandler.GetTeam)
			teams.DELETE("/:id", teamHandler.DeleteTeam)
			teams.GET("/:id/tasks", teamHandler.ListTasks)
			teams.POST("/:id/tasks", teamHandler.DispatchTask)
			teams.GET("/:id/events", teamHandler.ListEvents)
			teams.GET("/:id/workspace/files", teamHandler.ListWorkspaceFiles)
			teams.GET("/:id/workspace/preview", teamHandler.PreviewWorkspaceFile)
			teams.GET("/:id/workspace/download", teamHandler.DownloadWorkspaceFile)
			teams.POST("/:id/workspace/folders", teamHandler.CreateWorkspaceFolder)
			teams.POST("/:id/workspace/rename", teamHandler.RenameWorkspaceEntry)
			teams.POST("/:id/workspace/upload", teamHandler.UploadWorkspaceFiles)
			teams.DELETE("/:id/workspace/files", teamHandler.DeleteWorkspaceEntry)
			teams.DELETE("/:id/members/:memberID", teamHandler.DeleteMember)
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
		gatewayLLM.Use(middleware.GatewayAuth(instanceRepo, bindingRepo))
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

	// Start server with graceful shutdown
	srv := &http.Server{
		Addr:    cfg.Server.Address,
		Handler: r,
	}

	go func() {
		log.Printf("Server starting on %s", cfg.Server.Address)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Received signal %v, shutting down gracefully...", sig)

	// Give active requests up to 10 seconds to finish
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server forced to shutdown: %v", err)
	}

	// Stop background services. Cancelling leaderCtx releases the lease (and,
	// if we were leader, triggers stopBackground); the explicit stopBackground
	// call is idempotent and covers the leader-election-disabled path.
	leaderCancel()
	stopBackground()
	if runtimeAdminEventBridgeCancel != nil {
		runtimeAdminEventBridgeCancel()
	}
	wsHub.Stop()
	instanceHandler.Shutdown()

	log.Println("Server exited cleanly")
}
