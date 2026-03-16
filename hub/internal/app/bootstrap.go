package app

import (
	"context"
	"encoding/json"
	"log"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/discovery"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/mcp"
	"github.com/RapidAI/CodeClaw/hub/internal/memory"
	"github.com/RapidAI/CodeClaw/hub/internal/nlrouter"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func Bootstrap(cfg *config.Config) (*App, error) {
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               cfg.Database.DSN,
		WAL:               cfg.Database.WAL,
		BusyTimeoutMS:     cfg.Database.BusyTimeoutMS,
		MaxReadOpenConns:  cfg.Database.MaxReadOpenConns,
		MaxReadIdleConns:  cfg.Database.MaxReadIdleConns,
		MaxWriteOpenConns: cfg.Database.MaxWriteOpenConns,
		MaxWriteIdleConns: cfg.Database.MaxWriteIdleConns,
		BatchFlushMS:      cfg.Database.BatchFlushMS,
		BatchMaxSize:      cfg.Database.BatchMaxSize,
		BatchQueueSize:    cfg.Database.BatchQueueSize,
	})
	if err != nil {
		return nil, err
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		return nil, err
	}

	st := sqlite.NewStore(provider)
	adminService := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	mailer := mail.New(*cfg, st.System)
	invitationService := invitation.NewService(st.InvitationCodes, st.System)

	identityService := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.EmailInvites, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, invitationService, cfg.Identity.EnrollmentMode, cfg.Identity.AllowSelfEnroll, mailer, cfg.Server.PublicBaseURL)
	centerService := center.NewService(cfg, st.System)
	deviceRuntime := device.NewRuntime()
	deviceService := device.NewService(st.Machines, deviceRuntime)
	sessionCache := session.NewCache()
	sessionService := session.NewService(sessionCache, st.Sessions)
	gateway := ws.NewGateway(identityService, deviceService, sessionService)
	sessionService.RegisterListener(gateway.HandleSessionEvent)

	// Feishu notifier: push session events to users via Feishu cards.
	// Config can come from YAML (cfg.Feishu) or from the admin UI (stored in DB
	// under the "feishu_config" key). DB settings take precedence when present.
	feishuAppID, feishuAppSecret := cfg.Feishu.AppID, cfg.Feishu.AppSecret
	if raw, err := st.System.Get(context.Background(), "feishu_config"); err == nil && raw != "" {
		var dbCfg struct {
			Enabled   bool   `json:"enabled"`
			AppID     string `json:"app_id"`
			AppSecret string `json:"app_secret"`
		}
		if json.Unmarshal([]byte(raw), &dbCfg) == nil && dbCfg.Enabled && dbCfg.AppID != "" && dbCfg.AppSecret != "" {
			feishuAppID = dbCfg.AppID
			feishuAppSecret = dbCfg.AppSecret
		}
	}
	feishuNotifier := feishu.New(feishuAppID, feishuAppSecret, st.Users, st.System, mailer)
	feishuNotifier.SetServices(&feishu.DeviceServiceAdapter{Svc: deviceService}, sessionService)

	// -----------------------------------------------------------------------
	// New NL / IM modules — initialised in dependency order
	// -----------------------------------------------------------------------

	// 1. Memory_Store
	memoryStore := memory.NewStore(st.System)

	// 2. Tool_Discovery_Protocol
	discoveryProtocol := discovery.NewProtocol()

	// 3. MCP_Registry (depends on SystemSettings + Discovery)
	mcpRegistry, err := mcp.NewRegistry(st.System, discoveryProtocol)
	if err != nil {
		log.Printf("[bootstrap] MCP registry init failed (non-fatal): %v", err)
		mcpRegistry = nil
	}

	// 4. Skill_Executor (depends on SystemSettings + Discovery + ActionHandler)
	//    ActionHandler is nil at this point; we wire it later via SetActionHandler.
	skillExecutor, err := skill.NewExecutor(st.System, discoveryProtocol, nil)
	if err != nil {
		log.Printf("[bootstrap] Skill executor init failed (non-fatal): %v", err)
		skillExecutor = nil
	}

	// 5. Context_Window_Manager
	contextWindowMgr := nlrouter.NewContextWindowManager()

	// 6. Skill_Crystallizer (depends on Memory, Executor, ContextWindow, SystemSettings)
	var skillCrystallizer *skill.Crystallizer
	if skillExecutor != nil {
		skillCrystallizer = skill.NewCrystallizer(memoryStore, skillExecutor, contextWindowMgr, st.System)
	}

	// 7. NL_Router (depends on RuleEngine, MemoryStore, ContextWindowManager)
	ruleEngine := nlrouter.NewRuleEngine()
	nlRouter := nlrouter.NewRouter(ruleEngine, memoryStore, contextWindowMgr)

	// 8. BridgeExecutor — maps intents to concrete operations.
	//    Many dependencies are nil for now; the BridgeExecutor handles nil
	//    dependencies gracefully by returning "service not configured" errors.
	bridgeExecutor := im.NewBridgeExecutor(
		nil, // SessionManager — no direct adapter yet
		nil, // DeviceManager — no direct adapter yet
		nil, // ScreenshotService
		nil, // MCPRegistry
		nil, // SkillExecutor
		nil, // SkillCrystallizer
		nil, // MemoryStoreOps
		nil, // ContextManager
		nil, // ToolCatalogChecker
	)

	// 9. IM_Adapter — create with a temporary nil identity resolver; we wire
	//    the real one (PluginIdentityResolver) after plugin registration.
	imAdapter := im.NewAdapter(nlRouter, bridgeExecutor, nil)

	// Wire the PluginIdentityResolver now that the adapter exists.
	pluginIdentity := im.NewPluginIdentityResolver(imAdapter)
	imAdapter.SetIdentityResolver(pluginIdentity)

	// 10. Feishu_Plugin
	feishuPlugin := feishu.NewPlugin(feishuNotifier)

	// 11. Register Feishu_Plugin with IM_Adapter
	if err := imAdapter.RegisterPlugin(feishuPlugin); err != nil {
		log.Printf("[bootstrap] failed to register feishu plugin: %v", err)
	}

	// 12. Wire the plugin back to the notifier so handleBotMessage routes
	//     through the IM Adapter pipeline.
	feishuNotifier.SetPlugin(feishuPlugin)
	feishuPlugin.SetAdapter(imAdapter)

	// Register session event listener — routes through IM Adapter when available,
	// falls back to legacy notifier path.
	sessionService.RegisterListener(feishuNotifier.HandleEvent)

	router := httpapi.NewRouter(
		adminService,
		identityService,
		centerService,
		mailer,
		gateway,
		deviceService,
		sessionService,
		invitationService,
		st.System,
		feishuNotifier,
		skillExecutor,
		skillCrystallizer,
		mcpRegistry,
		cfg.PWA.StaticDir,
		cfg.PWA.RoutePrefix,
	)

	return &App{
		Config:          cfg,
		Provider:        provider,
		AdminService:    adminService,
		IdentityService: identityService,
		CenterService:   centerService,
		DeviceService:   deviceService,
		SessionService:  sessionService,
		Mailer:          mailer,
		WSGateway:       gateway,
		HTTPHandler:     router,

		// New NL / IM modules
		MemoryStore:       memoryStore,
		DiscoveryProtocol: discoveryProtocol,
		MCPRegistry:       mcpRegistry,
		SkillExecutor:     skillExecutor,
		SkillCrystallizer: skillCrystallizer,
		NLRouter:          nlRouter,
		ContextWindowMgr:  contextWindowMgr,
		IMAdapter:         imAdapter,
		FeishuPlugin:      feishuPlugin,
	}, nil
}
