package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"hijarr/internal/cache"
	"hijarr/internal/config"
	"hijarr/internal/logger"
	"hijarr/internal/maintenance"
	"hijarr/internal/migrations"
	"hijarr/internal/metrics"
	"hijarr/internal/proxy"
	"hijarr/internal/scheduler"
	"hijarr/internal/sonarr"
	"hijarr/internal/srn"
	"hijarr/internal/state"
	"hijarr/internal/web"

	"github.com/gin-gonic/gin"
	_ "go.uber.org/automaxprocs" // 自动按 cgroup CPU 配额设 GOMAXPROCS
)

func main() {
	logger.Init() // reads LOG_LEVEL env var; default INFO

	// 启动配置摘要
	fmt.Println("─── hijarr 启动配置 ───────────────────────────")
	fmt.Printf("  CACHE_DB_PATH          = %s\n", config.CacheDBPath)
	fmt.Printf("  BACKEND_SRN_URL        = %q\n", config.BackendSRNURL)
	fmt.Printf("  SONARR_URL             = %q\n", config.SonarrURL)
	fmt.Printf("  SONARR_SYNC_INTERVAL   = %v\n", config.SonarrSyncInterval)
	fmt.Printf("  SRN_PREFERRED_LANGUAGES= %v\n", config.SRNPreferredLanguages)
	fmt.Println("───────────────────────────────────────────────")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 初始化 SQLite 状态存储
	stateStore := state.GetStore(config.StateDBPath)

	// ── SRN 节点身份初始化 ──────────────────────────────────────────────────
	// 优先级：SRN_PRIV_KEY 环境变量 > global_state 数据库 > 自动生成（Warn）
	privHex := config.SRNPrivKey
	switch {
	case privHex != "":
		// 环境变量优先；同步写入数据库（供 migration 等内部查询使用）
		stateStore.SetIdentity("srn_priv_key", privHex)
		fmt.Printf("🔑 [SRN] 从环境变量 SRN_PRIV_KEY 加载节点密钥\n")
	case stateStore.GetIdentity("srn_priv_key") != "":
		privHex = stateStore.GetIdentity("srn_priv_key")
		fmt.Printf("🔑 [SRN] 从数据库加载节点密钥\n")
	default:
		// 自动生成：Warn 级别提示用户固化密钥
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			fmt.Printf("❌ [SRN] 密钥生成失败: %v\n", err)
			break
		}
		privHex = hex.EncodeToString(priv)
		stateStore.SetIdentity("srn_priv_key", privHex)
		// WARN：新容器/新数据库时身份会轮换，生产必须固化
		fmt.Printf("⚠️  [SRN][WARN] 未配置 SRN_PRIV_KEY，已自动生成临时密钥。\n")
		fmt.Printf("⚠️  [SRN][WARN] 若 DB 丢失或容器重建，节点身份将变更！\n")
		fmt.Printf("⚠️  [SRN][WARN] 请将以下私钥写入环境变量以固化身份：\n")
		fmt.Printf("⚠️  [SRN][WARN]   SRN_PRIV_KEY=%s\n", privHex)
	}
	if len(privHex) == 128 { // ed25519.PrivateKey = 64 bytes = 128 hex chars
		privBytes, _ := hex.DecodeString(privHex)
		nodePriv := ed25519.PrivateKey(privBytes)
		srn.SetNodeKey(nodePriv)
		pub := nodePriv.Public().(ed25519.PublicKey)
		pubHex := hex.EncodeToString(pub)
		fmt.Printf("🔑 [SRN] 节点公钥: %s\n", pubHex)
		web.SetNodePublicKey(pubHex)
	}

	// ── 维护任务与一次性迁移 ──
	mStore, _ := maintenance.NewTaskStore(config.StateDBPath)
	migrations.Wire(privHex)
	mRunner := maintenance.NewTaskRunner(mStore, migrations.GlobalRegistry())
	if err := mRunner.RunOneShotMigrations(ctx); err != nil {
		fmt.Printf("❌ [Maintenance] 一次性迁移失败: %v\n", err)
	}
	// 执行选配的社区任务 (默认开启协议与清理类任务)
	mRunner.RunCommunityTasks(ctx, []maintenance.TaskCategory{maintenance.CategoryProtocol, maintenance.CategoryCleanup})

	// 初始化 SQLite TMDB 缓存
	cache.InitTMDBCache(config.CacheDBPath)

	// 启动定时任务调度器
	sched := scheduler.New()

	// Sonarr direct sync (SRN subtitle pull and save)
	if config.SonarrURL != "" {
		sonarrClient := sonarr.NewClient(config.SonarrURL, config.SonarrAPIKey)
		sonarrJob := scheduler.NewSonarrSyncJob(sonarrClient)
		sched.Register(sonarrJob, config.SonarrSyncInterval)
		web.SetSonarrClient(sonarrClient)
		web.SetSonarrSearcher(sonarrJob)
		web.SetJobsInfo([]map[string]string{
			{"name": sonarrJob.Name(), "interval": config.SonarrSyncInterval.String()},
		})
	}

	go sched.Start(ctx)

	// 每 30 分钟打一次运营统计
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Println(metrics.Report())
			case <-ctx.Done():
				return
			}
		}
	}()

	r := gin.Default()
	web.RegisterRoutes(r)

	// 1. Torznab Proxy (Prowlarr)
	r.Any("/prowlarr/*path", proxy.TorznabProxy)
	r.Any("/prowlarr", proxy.TorznabProxy)

	// 2. TVDB MITM Proxy (Sonarr metadata overriding)
	r.Any("/sonarr/*path", proxy.TVDBMitmProxy)
	r.Any("/sonarr", proxy.TVDBMitmProxy)

	fmt.Printf("🚀 hijarr Go Proxy starting on :%s\n", config.Port)

	srv := &http.Server{
		Addr:    ":" + config.Port,
		Handler: r,
	}


	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Failed to start server: %v\n", err)
		}
	}()

	// Block until we receive our signal (CTRL+C)
	<-ctx.Done()
	fmt.Println("\n🛑 Shutting down server...")

	// Give it exactly 3 seconds to finish current requests
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	fmt.Println("✅ hijarr exited gracefully")
}
