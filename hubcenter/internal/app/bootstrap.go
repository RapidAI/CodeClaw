package app

import (
	"context"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/httpapi"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
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
	hubService := hubs.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs, st.System, mailer, cfg.Server.PublicBaseURL)
	entryService := entry.NewService(st.Hubs, st.HubUserLinks, st.BlockedEmails, st.BlockedIPs)

	// Skill store: derive directory from database DSN path.
	skillStoreDir := filepath.Join(filepath.Dir(cfg.Database.DSN), "skills")
	skillStore := skill.NewSkillStore(skillStoreDir)

	// Gossip snapshot cache: static gzip file for zero-CPU client polling.
	gossipCachePath := filepath.Join(filepath.Dir(cfg.Database.DSN), "gossip_cache.json.gz")
	gossipCache := httpapi.NewGossipCache(st.Gossip, gossipCachePath)
	gossipCache.EnsureExists(context.Background())

	// SkillMarket: data models, services, processor.
	dataDir := filepath.Dir(cfg.Database.DSN)
	smStore, err := skillmarket.NewStore(provider.Write, provider.Read)
	if err != nil {
		return nil, err
	}
	userSvc := skillmarket.NewUserService(smStore, mailer)
	creditsSvc := skillmarket.NewCreditsService(smStore)
	pendingDir := filepath.Join(dataDir, "sm_pending")
	sandboxDir := filepath.Join(dataDir, "sm_sandbox")
	processor := skillmarket.NewProcessor(pendingDir, sandboxDir, smStore, skillStore, mailer)

	rsaPrivKey, err := skillmarket.EnsureRSAKeyPair(dataDir)
	if err != nil {
		return nil, err
	}

	smHandlers := httpapi.NewSkillMarketHandlers(httpapi.SkillMarketConfig{
		Store:      smStore,
		UserSvc:    userSvc,
		CreditsSvc: creditsSvc,
		Processor:  processor,
		RSAPrivKey: rsaPrivKey,
		PendingDir: pendingDir,
		DataDir:    dataDir,
	})

	// 启动异步处理器后台 goroutine
	go processor.Run(context.Background())

	router := httpapi.NewRouter(adminService, hubService, entryService, mailer, skillStore, st.Gossip, gossipCache, smHandlers)

	return &App{
		Config:       cfg,
		Provider:     provider,
		Store:        st,
		AdminService: adminService,
		HubService:   hubService,
		EntryService: entryService,
		Mailer:       mailer,
		HTTPHandler:  router,
	}, nil
}
