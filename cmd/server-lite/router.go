package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/fan-video/fan-video/internal/config"
	"github.com/fan-video/fan-video/internal/embedded"
	"github.com/fan-video/fan-video/internal/handler"
	"github.com/fan-video/fan-video/internal/middleware"
	"github.com/fan-video/fan-video/internal/pwa"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/fan-video/fan-video/internal/serverprofile"
	"github.com/fan-video/fan-video/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func buildRouter(
	cfg *config.Config,
	services *service.Services,
	playbackSessions *service.PlaybackSessionService,
	handlers *handler.Handlers,
	repos *repository.Repositories,
	appVer string,
	logger *zap.SugaredLogger,
) *gin.Engine {
	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	// gin.Default() = Logger + Recovery；此处显式构建并在两者之间插入 Gzip，
	// 使 Gzip 位于 Recovery 外层，panic 时错误响应同样完整经过 gzip 流。
	r := gin.New()
	// 生产模式下 GIN 访问日志（含管理面板轮询）不进入 journal，
	// 避免高频请求日志将 systemctl status 的启动横幅挤出 20 行日志窗口。
	if cfg.App.Debug {
		r.Use(gin.Logger())
	} else {
		gin.DefaultWriter = io.Discard
	}
	r.Use(middleware.Gzip())
	r.Use(gin.Recovery())
	corsOrigins := append([]string{
		"tauri://localhost",
		"http://tauri.localhost",
		"https://tauri.localhost",
	}, cfg.App.CORSOrigins...)
	r.Use(middleware.CORS(corsOrigins...))
	r.Use(middleware.Security())
	r.Use(middleware.RateLimitWithConfig(middleware.RateLimitConfig{
		MaxRequests:           3000,
		Window:                time.Minute,
		ExcludePaths:          []string{"/api/ws"},
		ExcludeImageEndpoints: true,
	}))
	// PJAX 局部刷新：识别 X-PJAX 请求头，非 API 的 SPA 导航 GET 返回页面主体片段，
	// 普通请求返回完整页面。保持与完整页面复用同一份 index.html，不重写业务模板。
	r.Use(middleware.PJAX(cfg.App.WebDir))
	// Build assets carry content-hashed filenames (index-<hash>.js etc.) and are
	// therefore immutable: cache them for a year so repeat visits never re-download.
	// /assets/sw.js 与 /assets/manifest.json 由 registerPWAAndAssets 单独处理
	// （内嵌内容 + PWA 专用缓存头），不落入 immutable 缓存。
	r.Use(func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			return
		}
		switch c.Request.URL.Path {
		case "/assets/sw.js":
			c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		case "/assets/manifest.json":
			c.Header("Cache-Control", "no-cache")
		default:
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
	})

	if cfg.Secrets.JWTSecret == "" {
		logger.Fatal("JWT Secret 未配置或自动生成失败，无法启动")
	}
	jwtMiddleware := middleware.JWTAuthWithValidator(cfg.Secrets.JWTSecret, services.Auth.ValidateTokenVersion)
	jwtRefreshMiddleware := middleware.JWTAuthAllowExpired(cfg.Secrets.JWTSecret, services.Auth.ValidateTokenVersion)
	wsMiddleware := middleware.WSAuth(cfg.Secrets.JWTSecret, services.Auth.ValidateTokenVersion)
	profileRuntime := serverprofile.NewLiteRuntime(cfg)
	executionRepo := repository.NewTranscodeExecutionRepo(repos.DB())
	taskCenterService := service.NewTaskCenterService(
		services.Library,
		executionRepo,
		logger,
	)
	taskActionDispatcher := service.NewTaskActionDispatcher(
		services.ArtifactMaintenance,
		services.WSHub,
		logger,
	)
	taskCenterHandler := handler.NewTaskCenterHandler(taskCenterService, taskActionDispatcher, logger)
	taskCenterHandler.SetAuditService(services.User)
	legacyRetirementHandler := handler.NewLegacySourceRetirementHandler(
		service.NewLegacySourceRetirementService(executionRepo),
		logger,
	)
	legacyRetirementHandler.SetAuditService(services.User)
	runtimeHistoryHandler := handler.NewRuntimeHistoryHandler(
		service.NewRuntimeHistoryService(repository.NewRuntimeHistoryRepo(repos.DB()), logger),
		logger,
	)
	playbackPlanHandler := handler.NewPlaybackPlanHandler(services.Stream, logger)
	playbackSessionHandler := handler.NewPlaybackSessionHandler(
		playbackSessions,
		services.Permission,
		repos.Media,
		logger,
	)

	// Local Media Analysis is a core Lite capability and is deliberately
	// initialized without AIService. It works offline using FFmpeg only.
	mediaAnalysisService := service.NewMediaAnalysisService(
		cfg,
		repos.Media,
		repos.VideoHighlight,
		repos.AIAnalysisTask,
		logger,
	)
	mediaAnalysisService.SetWSHub(services.WSHub)
	mediaAnalysisHandler := handler.NewMediaAnalysisHandler(mediaAnalysisService, logger)

	startMaintenanceJobs(repos)
	webRoot := embedded.Resolve(cfg.App.WebDir)
	registerPublicRoutes(r, cfg, handlers, profileRuntime, appVer, jwtMiddleware, jwtRefreshMiddleware, wsMiddleware, webRoot)
	registerCoreAPI(r, cfg, services, handlers, playbackPlanHandler, playbackSessionHandler, mediaAnalysisHandler, mediaAnalysisService, repos, jwtMiddleware)
	registerAdminAPI(r, cfg, handlers, taskCenterHandler, runtimeHistoryHandler, jwtMiddleware)
	r.POST("/api/admin/tasks/:kind/:id/:action", jwtMiddleware, middleware.AdminOnly(), taskCenterHandler.Action)
	r.GET("/api/admin/legacy-source-retirement/:source", jwtMiddleware, middleware.AdminOnly(), legacyRetirementHandler.Report)
	r.POST("/api/admin/legacy-source-retirement/:source/decisions", jwtMiddleware, middleware.AdminOnly(), legacyRetirementHandler.Review)
	r.POST("/api/admin/legacy-source-retirement/:source/removal-plans", jwtMiddleware, middleware.AdminOnly(), legacyRetirementHandler.PrepareRemovalPlan)
	r.GET("/api/admin/legacy-source-retirement/:source/isolation", jwtMiddleware, middleware.AdminOnly(), legacyRetirementHandler.IsolationState)
	r.POST("/api/admin/legacy-source-retirement/:source/isolations", jwtMiddleware, middleware.AdminOnly(), legacyRetirementHandler.Isolate)
	r.POST("/api/admin/legacy-source-retirement/:source/isolation-rollbacks", jwtMiddleware, middleware.AdminOnly(), legacyRetirementHandler.RollbackIsolation)

	// Pulse 已从客户端移除。旧书签或旧浏览器地址必须在服务端直接退出该页面，
	// 避免尚未完成 Service Worker 升级的旧前端继续渲染历史页面。
	redirectLegacyPulse := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusTemporaryRedirect, "/admin")
	}
	r.GET("/pulse", redirectLegacyPulse)
	r.GET("/pulse/*path", redirectLegacyPulse)

	registerPWAAndAssets(r, cfg.App.WebDir, webRoot)
	r.NoRoute(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		serveFrontendFile(c, webRoot, "index.html", "")
	})
	return r
}

func startMaintenanceJobs(repos *repository.Repositories) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		_ = repos.LoginLog.CleanOlderThan(90)
		for range ticker.C {
			_ = repos.LoginLog.CleanOlderThan(90)
		}
	}()
}

func registerPublicRoutes(
	r *gin.Engine,
	cfg *config.Config,
	handlers *handler.Handlers,
	profileRuntime serverprofile.LiteRuntime,
	appVer string,
	jwtMiddleware gin.HandlerFunc,
	jwtRefreshMiddleware gin.HandlerFunc,
	wsMiddleware gin.HandlerFunc,
	webRoot http.FileSystem,
) {
	auth := r.Group("/api/auth")
	auth.POST("/login", handlers.Auth.Login)
	auth.GET("/status", handlers.Auth.Status)
	auth.POST("/register", middleware.RateLimit(10), handlers.Auth.Register)
	auth.POST("/refresh", jwtRefreshMiddleware, handlers.Auth.RefreshToken)
	auth.PUT("/password", jwtMiddleware, handlers.Auth.ChangePassword)

	writeCapabilities := func(c *gin.Context) {
		manifest := profileRuntime.Manifest(cfg)
		c.JSON(http.StatusOK, gin.H{"data": manifest})
	}
	r.GET("/api/capabilities", writeCapabilities)

	r.GET("/api/health", func(c *gin.Context) {
		manifest := profileRuntime.Manifest(cfg)
		features := manifest.LegacyFeatures(cfg)
		data := gin.H{
			"status":         "ok",
			"version":        appVer,
			"server_name":    cfg.App.ServerName,
			"profile":        manifest.Profile,
			"schema_version": manifest.SchemaVersion,
			"capabilities":   manifest.Capabilities,
			"go":             runtime.Version(),
			"os":             runtime.GOOS,
			"arch":           runtime.GOARCH,
			"port":           cfg.App.Port,
			"listen_addr":    fmt.Sprintf(":%d", cfg.App.Port),
			"lan_ips":        getLocalIPv4Addresses(),
			"features":       features,
		}
		c.JSON(http.StatusOK, gin.H{
			"status":         data["status"],
			"version":        data["version"],
			"server_name":    data["server_name"],
			"profile":        data["profile"],
			"schema_version": data["schema_version"],
			"capabilities":   data["capabilities"],
			"go":             data["go"],
			"os":             data["os"],
			"arch":           data["arch"],
			"port":           data["port"],
			"listen_addr":    data["listen_addr"],
			"lan_ips":        data["lan_ips"],
			"features":       features,
			"data":           data,
		})
	})

	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Content-Type", "image/png")
		c.Header("Cache-Control", "public, max-age=604800")
		serveFrontendFile(c, webRoot, "assets/icon-192.png", "")
	})
	r.GET("/webmanifest", func(c *gin.Context) {
		c.Header("Content-Type", "application/manifest+json")
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "application/manifest+json", pwa.ManifestJSON())
	})
	r.GET("/manifest.json", func(c *gin.Context) {
		c.Header("Content-Type", "application/manifest+json")
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "application/manifest+json", pwa.ManifestJSON())
	})
	// PWA 资源同时挂到 /assets/ 下（见 registerPWAAndAssets）：反代/防火墙普遍放行
	// 该路径，而顶层 /sw.js、/webmanifest 常被拦成 403。Service-Worker-Allowed: / 让
	// /assets/sw.js 的 scope 仍是站点根目录，PWA 行为与原先完全一致。
	r.GET("/sw.js", func(c *gin.Context) {
		c.Header("Service-Worker-Allowed", "/")
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Data(http.StatusOK, "text/javascript", pwa.SWJS())
	})
	r.GET("/api/ws", wsMiddleware, handlers.WS.HandleWebSocket)
}

func getLocalIPv4Addresses() []string {
	ips := make([]string, 0, 4)
	interfaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				ips = append(ips, ipv4.String())
			}
		}
	}
	return ips
}
