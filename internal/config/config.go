package config

import (
	cryptoRand "crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// ==================== 子配置结构体 ====================

// DatabaseConfig 数据库连接参数
type DatabaseConfig struct {
	// 数据库文件路径，默认 ./data/nowen.db
	DBPath string `mapstructure:"db_path"`
	// SQLite WAL 模式，默认 true
	WALMode bool `mapstructure:"wal_mode"`
	// 繁忙超时（毫秒），默认 5000
	BusyTimeout int `mapstructure:"busy_timeout"`
	// 缓存大小（负数为KB），默认 -20000
	CacheSize int `mapstructure:"cache_size"`
	// 最大打开连接数，默认 1（SQLite 建议）
	MaxOpenConns int `mapstructure:"max_open_conns"`
	// 最大空闲连接数，默认 1
	MaxIdleConns int `mapstructure:"max_idle_conns"`
}

// SecretsConfig 敏感信息配置
type SecretsConfig struct {
	// JWT 签名密钥（必须修改默认值）
	JWTSecret string `mapstructure:"jwt_secret"`
}

// AppConfig 应用运行环境配置
type AppConfig struct {
	// 服务器对外显示名称（留空则使用主机名或 "fan-video"）
	ServerName string `mapstructure:"server_name"`
	// 服务器监听端口，默认 8080
	Port int `mapstructure:"port"`
	// 调试模式，默认 false
	Debug bool `mapstructure:"debug"`
	// 运行环境标识：development / production / testing
	Env string `mapstructure:"env"`
	// 数据目录，默认 ./data
	DataDir string `mapstructure:"data_dir"`
	// 前端静态文件目录，默认 ./web/dist
	WebDir string `mapstructure:"web_dir"`
	// FFmpeg 可执行文件路径
	FFmpegPath string `mapstructure:"ffmpeg_path"`
	// FFprobe 可执行文件路径
	FFprobePath string `mapstructure:"ffprobe_path"`
	// VAAPI 设备路径，如 /dev/dri/renderD128
	// 保留该字段作为 Linux Intel 核显的逃生门，不在 UI 暴露。
	VAAPIDevice string `mapstructure:"vaapi_device"`
	// 全量备份存储目录，默认 <data_dir>/backups（Docker 下随 /data 卷持久化）
	BackupDir string `mapstructure:"backup_dir"`
	// 允许的跨域来源列表
	CORSOrigins []string `mapstructure:"cors_origins"`
}

// LoggingConfig 日志记录设置
type LoggingConfig struct {
	// 日志级别: debug / info / warn / error
	Level string `mapstructure:"level"`
	// 日志输出格式: json / console
	Format string `mapstructure:"format"`
	// 日志输出文件路径，留空则输出到 stdout
	OutputPath string `mapstructure:"output_path"`
	// 错误日志输出路径，留空则输出到 stderr
	ErrorOutputPath string `mapstructure:"error_output_path"`
	// 是否启用日志文件轮转
	EnableRotation bool `mapstructure:"enable_rotation"`
	// 单个日志文件最大大小（MB），默认 100
	MaxSizeMB int `mapstructure:"max_size_mb"`
	// 日志文件最大保留天数，默认 30
	MaxAgeDays int `mapstructure:"max_age_days"`
	// 日志文件最大保留个数，默认 10
	MaxBackups int `mapstructure:"max_backups"`
}

// CacheConfig 缓存配置参数
type CacheConfig struct {
	// 转码缓存目录，默认 ./cache
	CacheDir string `mapstructure:"cache_dir"`
	// 缓存最大占用磁盘空间（MB），0 为不限制
	MaxDiskUsageMB int `mapstructure:"max_disk_usage_mb"`
	// 缓存文件过期时间（小时），0 为不过期
	TTLHours int `mapstructure:"ttl_hours"`
	// 是否启用自动清理过期缓存
	AutoCleanup bool `mapstructure:"auto_cleanup"`
	// 自动清理间隔（分钟），默认 60
	CleanupIntervalMin int `mapstructure:"cleanup_interval_min"`
}

// ==================== 主配置结构体 ====================

// SubtitleConfig 字幕预处理配置（本地提取/转换/清洗，无网络请求）
type SubtitleConfig struct {
	// 字幕预处理最大并发数（默认 1）
	Workers int `mapstructure:"workers"`

	// ==================== 字幕清洗配置 ====================
	// 是否启用字幕内容清洗（在字幕提取/转换后执行）
	SubCleanEnabled bool `mapstructure:"sub_clean_enabled"`
	// 去除 HTML 标签（<i>, <b>, <font> 等）
	SubCleanRemoveHTML bool `mapstructure:"sub_clean_remove_html"`
	// 去除 ASS 样式标签（{\an8}, {\pos()} 等）
	SubCleanRemoveASSStyle bool `mapstructure:"sub_clean_remove_ass_style"`
	// 统一标点符号（全角→半角，仅对非 CJK 文本生效）
	SubCleanNormalizePunct bool `mapstructure:"sub_clean_normalize_punct"`
	// 去除 SDH 标注（[音乐], (笑声), [门铃响] 等听障辅助描述）
	SubCleanRemoveSDH bool `mapstructure:"sub_clean_remove_sdh"`
	// 去除广告水印字幕（字幕组署名、网站地址等）
	SubCleanRemoveAds bool `mapstructure:"sub_clean_remove_ads"`
	// 合并过短的字幕条目（显示时长低于阈值时与相邻条目合并）
	SubCleanMergeShort bool `mapstructure:"sub_clean_merge_short"`
	// 拆分过长的字幕条目（超过最大字符数时按时间均分拆分）
	SubCleanSplitLong bool `mapstructure:"sub_clean_split_long"`
	// 处理前备份原始字幕文件（生成 .bak 文件）
	SubCleanBackup bool `mapstructure:"sub_clean_backup"`
	// 编码检测失败时的回退编码（如 "gbk"、"big5"、"shift_jis"）
	SubCleanFallbackEnc string `mapstructure:"sub_clean_fallback_enc"`
	// 全局时间轴偏移（毫秒，正数延后、负数提前）
	SubCleanTimeOffsetMs int64 `mapstructure:"sub_clean_time_offset_ms"`
	// 最小字幕显示时长（毫秒，低于此值的条目将被合并，默认 500）
	SubCleanMinDurationMs int64 `mapstructure:"sub_clean_min_duration_ms"`
	// 最大字幕显示时长（毫秒，超过此值的条目将被截断，默认 10000）
	SubCleanMaxDurationMs int64 `mapstructure:"sub_clean_max_duration_ms"`
	// 合并间隔阈值（毫秒，两条字幕间隔小于此值时可合并，默认 200）
	SubCleanMinGapMs int64 `mapstructure:"sub_clean_min_gap_ms"`
	// 每行最大字符数（用于拆分过长字幕，默认 42）
	SubCleanMaxCharsPerLine int `mapstructure:"sub_clean_max_chars_per_line"`
	// 每条字幕最大行数（默认 2）
	SubCleanMaxLinesPerCue int `mapstructure:"sub_clean_max_lines_per_cue"`
}

// RegistrationConfig 注册控制配置
type RegistrationConfig struct {
	// 是否允许公开注册，默认 false（仅管理员可创建用户）
	Enabled bool `mapstructure:"enabled"`
	// 邀请码（设置后注册时需提供正确的邀请码）
	InviteCode string `mapstructure:"invite_code"`
}

// StorageConfig 存储配置（支持本地、WebDAV、网盘等多种存储后端）
type StorageConfig struct {
	// ==================== WebDAV 存储配置 ====================
	WebDAV WebDAVConfig `mapstructure:"webdav"`

	// ==================== V2.3: Alist 聚合网盘配置 ====================
	// 通过 Alist HTTP API 对接阿里云盘 / 115 / 夸克 / 百度网盘 / OneDrive 等 20+ 网盘
	Alist AlistConfig `mapstructure:"alist"`

	// ==================== V2.3: S3 兼容对象存储配置 ====================
	// 对接 AWS S3 / MinIO / Cloudflare R2 / 阿里云 OSS / 腾讯云 COS 等
	S3 S3Config `mapstructure:"s3"`

	// ==================== 预留：未来扩展 ====================
	// OneDrive    OneDriveConfig    `mapstructure:"onedrive"`
}

// AlistConfig Alist 聚合网盘配置（V2.3）
//
// Alist 官网: https://alist.nn.ci/
// 认证模式：
//  1. Token 模式（推荐）：预先获取长期 Token，直接填入 Token 字段
//  2. 用户名密码模式：首次请求时调用 /api/auth/login 换取 Token
type AlistConfig struct {
	// 是否启用 Alist 存储
	Enabled bool `mapstructure:"enabled"`
	// Alist 服务器地址（如 https://alist.example.com）
	ServerURL string `mapstructure:"server_url"`
	// 用户名（Token 模式可不填）
	Username string `mapstructure:"username"`
	// 密码（Token 模式可不填）
	Password string `mapstructure:"password"`
	// 长期 Token（优先于用户名密码）
	Token string `mapstructure:"token"`
	// 基础路径（Alist 内的根目录，如 /aliyun/movies）
	BasePath string `mapstructure:"base_path"`
	// 连接超时（秒，默认 30）
	Timeout int `mapstructure:"timeout"`
	// 是否启用元数据缓存
	EnableCache bool `mapstructure:"enable_cache"`
	// 元数据缓存 TTL（小时，默认 12）
	CacheTTLHours int `mapstructure:"cache_ttl_hours"`
	// ReadAt 块缓存大小（MiB，默认 8，<=0 禁用）
	ReadBlockSizeMB int `mapstructure:"read_block_size_mb"`
	// ReadAt 块缓存最大块数（每文件，默认 4，<=0 禁用）
	ReadBlockCount int `mapstructure:"read_block_count"`
}

// S3Config S3 兼容对象存储配置（V2.3）
type S3Config struct {
	// 是否启用 S3 存储
	Enabled bool `mapstructure:"enabled"`
	// S3 Endpoint（如 https://s3.amazonaws.com、https://minio.example.com:9000）
	Endpoint string `mapstructure:"endpoint"`
	// 区域（AWS 必填，MinIO 可留空或 us-east-1）
	Region string `mapstructure:"region"`
	// Access Key
	AccessKey string `mapstructure:"access_key"`
	// Secret Key
	SecretKey string `mapstructure:"secret_key"`
	// Bucket 名称
	Bucket string `mapstructure:"bucket"`
	// 基础路径前缀（Object Key 前缀，如 media/）
	BasePath string `mapstructure:"base_path"`
	// 是否使用 Path-Style 寻址（MinIO 必开，AWS 默认 Virtual-Host-Style）
	PathStyle bool `mapstructure:"path_style"`
	// 连接超时（秒，默认 30）
	Timeout int `mapstructure:"timeout"`
	// 是否启用元数据缓存
	EnableCache bool `mapstructure:"enable_cache"`
	// 元数据缓存 TTL（小时，默认 24）
	CacheTTLHours int `mapstructure:"cache_ttl_hours"`
	// ReadAt 块缓存大小（MiB，默认 8，<=0 禁用）
	ReadBlockSizeMB int `mapstructure:"read_block_size_mb"`
	// ReadAt 块缓存最大块数（每文件，默认 4，<=0 禁用）
	ReadBlockCount int `mapstructure:"read_block_count"`
}

// WebDAVConfig WebDAV 远程存储配置
type WebDAVConfig struct {
	// 是否启用 WebDAV 存储
	Enabled bool `mapstructure:"enabled"`
	// WebDAV 服务器地址（如 https://dav.example.com）
	ServerURL string `mapstructure:"server_url"`
	// 用户名
	Username string `mapstructure:"username"`
	// 密码
	Password string `mapstructure:"password"`
	// 基础路径（服务器上的根目录，如 /media）
	BasePath string `mapstructure:"base_path"`
	// 连接超时（秒，默认 30）
	Timeout int `mapstructure:"timeout"`
	// 是否启用连接池
	EnablePool bool `mapstructure:"enable_pool"`
	// 连接池大小（默认 5）
	PoolSize int `mapstructure:"pool_size"`
	// 是否启用缓存（本地缓存远程文件元数据）
	EnableCache bool `mapstructure:"enable_cache"`
	// 缓存过期时间（小时，默认 24）
	CacheTTLHours int `mapstructure:"cache_ttl_hours"`
	// 最大重试次数（默认 3）
	MaxRetries int `mapstructure:"max_retries"`
	// 重试间隔（秒，默认 2）
	RetryInterval int `mapstructure:"retry_interval"`
	// V2.1: ReadAt 块缓存大小（MiB，默认 8，<=0 禁用）
	ReadBlockSizeMB int `mapstructure:"read_block_size_mb"`
	// V2.1: ReadAt 块缓存最大块数（每文件，默认 4，<=0 禁用）
	ReadBlockCount int `mapstructure:"read_block_count"`
}

// STRMConfig .strm 远程流全局配置
type STRMConfig struct {
	// 默认 User-Agent（Media 自身无 UA 时使用），留空则使用内置值
	DefaultUserAgent string `mapstructure:"default_user_agent"`
	// 默认 Referer（留空=不发送）
	DefaultReferer string `mapstructure:"default_referer"`
	// 代理远程流时的连接超时（秒，默认 30，仅影响首包握手；读取阶段不超时）
	ConnectTimeout int `mapstructure:"connect_timeout"`
	// 对 HLS (.m3u8) 子清单做 URL 重写，让分片继续走后端代理（解决跨域/鉴权透传）
	RewriteHLS bool `mapstructure:"rewrite_hls"`
	// 扫描时是否对直链 mp4/mkv 启动远程 FFprobe 拉元数据（慢但能得到真实时长/分辨率）
	RemoteProbe bool `mapstructure:"remote_probe"`
	// 远程 FFprobe 超时秒数（默认 8）
	RemoteProbeTimeout int `mapstructure:"remote_probe_timeout"`
	// 按域名白名单追加 UA，例如：{"115.com":"Mozilla/5.0 ..."}
	DomainUserAgents map[string]string `mapstructure:"domain_user_agents"`
	// 按域名白名单追加 Referer，例如：{"115.com":"https://115.com/"}
	DomainReferers map[string]string `mapstructure:"domain_referers"`
}

// Config 应用主配置（聚合所有子模块）
type Config struct {
	mu sync.RWMutex `mapstructure:"-"`

	// 子模块配置
	Database     DatabaseConfig     `mapstructure:"database"`
	Secrets      SecretsConfig      `mapstructure:"secrets"`
	App          AppConfig          `mapstructure:"app"`
	Logging      LoggingConfig      `mapstructure:"logging"`
	Cache        CacheConfig        `mapstructure:"cache"`
	Subtitle     SubtitleConfig     `mapstructure:"subtitle"`
	Registration RegistrationConfig `mapstructure:"registration"`
	Storage      StorageConfig      `mapstructure:"storage"`
	STRM         STRMConfig         `mapstructure:"strm"`

	// ==================== 兼容性字段（向后兼容旧的扁平配置） ====================
	// 以下字段用于兼容旧版 config.yaml 中的扁平 key，
	// 加载后会自动合并到对应的子模块中。

	// 旧版兼容 - 数据库
	DBPath string `mapstructure:"db_path"`
	// 旧版兼容 - 密钥
	JWTSecret string `mapstructure:"jwt_secret"`
	// 旧版兼容 - 应用
	Port        int      `mapstructure:"port"`
	Debug       bool     `mapstructure:"debug"`
	DataDir     string   `mapstructure:"data_dir"`
	WebDir      string   `mapstructure:"web_dir"`
	CacheDir    string   `mapstructure:"cache_dir"`
	FFmpegPath  string   `mapstructure:"ffmpeg_path"`
	FFprobePath string   `mapstructure:"ffprobe_path"`
	VAAPIDevice string   `mapstructure:"vaapi_device"`
	CORSOrigins []string `mapstructure:"cors_origins"`
}

// ==================== 加载逻辑 ====================

// Load 加载配置，支持以下方式（优先级从低到高）：
//  1. 内置默认值
//  2. 主配置文件 config.yaml（兼容旧版扁平格式）
//  3. config/ 目录下的分片配置文件（database.yaml, secrets.yaml 等）
//  4. 环境变量（NOWEN_ 前缀）
func Load() (*Config, error) {
	// 设置默认值
	setDefaults()

	// 配置文件搜索路径
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./data")
	viper.AddConfigPath("/etc/fan-video")

	// 环境变量
	viper.SetEnvPrefix("NOWEN")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// 1. 读取主配置文件（不存在也不报错）
	_ = viper.ReadInConfig()

	// 2. 合并 config/ 目录下的分片配置文件
	if err := mergeConfigDir(); err != nil {
		return nil, fmt.Errorf("加载分片配置文件失败: %w", err)
	}

	// 3. 反序列化
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 4. 向后兼容：将旧版扁平字段合并到子模块
	cfg.migrateFromFlatConfig()

	// 5. 确保目录存在
	for _, dir := range []string{cfg.App.DataDir, cfg.Cache.CacheDir, cfg.BackupDir()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}

	// 6. 处理 db_path 相对路径
	if !filepath.IsAbs(cfg.Database.DBPath) {
		cfg.Database.DBPath = filepath.Join(cfg.App.DataDir, filepath.Base(cfg.Database.DBPath))
	}

	// 7. 自动生成 JWT Secret（如果仍为默认值）
	//    为避免容器/进程每次重启导致已签发 token 全部失效（用户被踢回登录页），
	//    这里将自动生成的 secret 持久化到 data 目录，后续启动优先读取该文件。
	if cfg.Secrets.JWTSecret == "fan-video-secret-change-me" {
		cfg.Secrets.JWTSecret = loadOrCreatePersistedSecret(cfg.App.DataDir)
	}

	return cfg, nil
}

// loadOrCreatePersistedSecret 读取/生成持久化的 JWT Secret。
// 优先从 <dataDir>/.jwt_secret 读取；若不存在或内容为空，则生成 32 字节随机值并写盘。
// 写盘失败时回退到仅内存持有（打印告警但不终止进程）。
func loadOrCreatePersistedSecret(dataDir string) string {
	secretFile := filepath.Join(dataDir, ".jwt_secret")
	if b, err := os.ReadFile(secretFile); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) >= 16 {
			return s
		}
	}
	secret := generateRandomSecret(32)
	// 确保目录存在（loadConfig 已经 MkdirAll，但这里再兜底一次）
	_ = os.MkdirAll(dataDir, 0755)
	if err := os.WriteFile(secretFile, []byte(secret), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  JWT Secret 持久化失败（%v），本次使用内存随机值，下次重启将重新生成并导致所有登录态失效！\n", err)
	}
	return secret
}

// setDefaults 设置所有默认值
func setDefaults() {
	// ---- 数据库 ----
	viper.SetDefault("database.db_path", "./data/nowen.db")
	viper.SetDefault("database.wal_mode", true)
	viper.SetDefault("database.busy_timeout", 5000)
	viper.SetDefault("database.cache_size", -20000)
	viper.SetDefault("database.max_open_conns", 4)
	viper.SetDefault("database.max_idle_conns", 2)

	// ---- 密钥 ----
	viper.SetDefault("secrets.jwt_secret", "fan-video-secret-change-me")

	// ---- 应用 ----
	viper.SetDefault("app.server_name", "")
	viper.SetDefault("app.port", 8080)
	viper.SetDefault("app.debug", false)
	viper.SetDefault("app.env", "production")
	viper.SetDefault("app.data_dir", "./data")
	viper.SetDefault("app.web_dir", "./web/dist")
	viper.SetDefault("app.ffmpeg_path", "ffmpeg")
	viper.SetDefault("app.ffprobe_path", "ffprobe")
	viper.SetDefault("app.vaapi_device", "/dev/dri/renderD128")
	viper.SetDefault("app.backup_dir", "")
	viper.SetDefault("app.cors_origins", []string{})

	// ---- 日志 ----
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "console")
	viper.SetDefault("logging.output_path", "")
	viper.SetDefault("logging.error_output_path", "")
	viper.SetDefault("logging.enable_rotation", false)
	viper.SetDefault("logging.max_size_mb", 100)
	viper.SetDefault("logging.max_age_days", 30)
	viper.SetDefault("logging.max_backups", 10)

	// ---- 字幕预处理（提取/清洗）默认值 ----
	viper.SetDefault("subtitle.workers", 1)

	// 字幕清洗：默认行为偏保守，开启后也不会误杀
	viper.SetDefault("subtitle.sub_clean_enabled", false)
	viper.SetDefault("subtitle.sub_clean_remove_html", true)
	viper.SetDefault("subtitle.sub_clean_remove_ass_style", true)
	viper.SetDefault("subtitle.sub_clean_normalize_punct", false)
	viper.SetDefault("subtitle.sub_clean_remove_sdh", true)
	viper.SetDefault("subtitle.sub_clean_remove_ads", true)
	viper.SetDefault("subtitle.sub_clean_merge_short", true)
	viper.SetDefault("subtitle.sub_clean_split_long", true)
	viper.SetDefault("subtitle.sub_clean_backup", true)
	viper.SetDefault("subtitle.sub_clean_fallback_enc", "gbk")
	viper.SetDefault("subtitle.sub_clean_time_offset_ms", 0)
	viper.SetDefault("subtitle.sub_clean_min_duration_ms", 500)
	viper.SetDefault("subtitle.sub_clean_max_duration_ms", 10000)
	viper.SetDefault("subtitle.sub_clean_min_gap_ms", 200)
	viper.SetDefault("subtitle.sub_clean_max_chars_per_line", 42)
	viper.SetDefault("subtitle.sub_clean_max_lines_per_cue", 2)

	// ---- 缓存 ----
	viper.SetDefault("cache.cache_dir", "./cache")
	viper.SetDefault("cache.max_disk_usage_mb", 0)
	viper.SetDefault("cache.ttl_hours", 0)
	viper.SetDefault("cache.auto_cleanup", false)
	viper.SetDefault("cache.cleanup_interval_min", 60)

	// ---- 注册控制 ----
	viper.SetDefault("registration.enabled", false)
	viper.SetDefault("registration.invite_code", "")

	// ---- 存储配置 ----
	// WebDAV 存储配置
	viper.SetDefault("storage.webdav.enabled", false)
	viper.SetDefault("storage.webdav.server_url", "")
	viper.SetDefault("storage.webdav.username", "")
	viper.SetDefault("storage.webdav.password", "")
	viper.SetDefault("storage.webdav.base_path", "")
	viper.SetDefault("storage.webdav.timeout", 30)
	viper.SetDefault("storage.webdav.enable_pool", true)
	viper.SetDefault("storage.webdav.pool_size", 5)
	viper.SetDefault("storage.webdav.enable_cache", true)
	viper.SetDefault("storage.webdav.cache_ttl_hours", 24)
	viper.SetDefault("storage.webdav.max_retries", 3)
	viper.SetDefault("storage.webdav.retry_interval", 2)
	// V2.1: ReadAt 块缓存（播放器 seek 加速）
	viper.SetDefault("storage.webdav.read_block_size_mb", 8)
	viper.SetDefault("storage.webdav.read_block_count", 4)

	// V2.3: Alist 聚合网盘默认值
	viper.SetDefault("storage.alist.enabled", false)
	viper.SetDefault("storage.alist.server_url", "")
	viper.SetDefault("storage.alist.base_path", "/")
	viper.SetDefault("storage.alist.timeout", 30)
	viper.SetDefault("storage.alist.enable_cache", true)
	viper.SetDefault("storage.alist.cache_ttl_hours", 12)
	viper.SetDefault("storage.alist.read_block_size_mb", 8)
	viper.SetDefault("storage.alist.read_block_count", 4)

	// V2.3: S3 兼容对象存储默认值
	viper.SetDefault("storage.s3.enabled", false)
	viper.SetDefault("storage.s3.endpoint", "")
	viper.SetDefault("storage.s3.region", "us-east-1")
	viper.SetDefault("storage.s3.bucket", "")
	viper.SetDefault("storage.s3.base_path", "")
	viper.SetDefault("storage.s3.path_style", true)
	viper.SetDefault("storage.s3.timeout", 30)
	viper.SetDefault("storage.s3.enable_cache", true)
	viper.SetDefault("storage.s3.cache_ttl_hours", 24)
	viper.SetDefault("storage.s3.read_block_size_mb", 8)
	viper.SetDefault("storage.s3.read_block_count", 4)

	// ---- STRM 远程流 ----
	viper.SetDefault("strm.default_user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	viper.SetDefault("strm.default_referer", "")
	viper.SetDefault("strm.connect_timeout", 30)
	viper.SetDefault("strm.rewrite_hls", true)
	viper.SetDefault("strm.remote_probe", true)
	viper.SetDefault("strm.remote_probe_timeout", 8)

	// ---- 旧版兼容默认值（当使用扁平 key 时） ----
	viper.SetDefault("port", 8080)
	viper.SetDefault("debug", false)
	viper.SetDefault("data_dir", "./data")
	viper.SetDefault("cache_dir", "./cache")
	viper.SetDefault("web_dir", "./web/dist")
	viper.SetDefault("db_path", "./data/nowen.db")
	viper.SetDefault("jwt_secret", "fan-video-secret-change-me")
	viper.SetDefault("ffmpeg_path", "ffmpeg")
	viper.SetDefault("ffprobe_path", "ffprobe")
	viper.SetDefault("vaapi_device", "/dev/dri/renderD128")
}

// mergeConfigDir 合并 config/ 目录下的分片配置文件
func mergeConfigDir() error {
	// 搜索配置目录
	configDirs := []string{"./config", "./data/config", "/etc/fan-video/config"}

	for _, dir := range configDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		// 按照固定顺序加载分片文件，确保优先级可预测
		configFiles := []struct {
			name   string // 文件名（不含扩展名）
			prefix string // 在 viper 中的 key 前缀
		}{
			{name: "database", prefix: "database"},
			{name: "secrets", prefix: "secrets"},
			{name: "app", prefix: "app"},
			{name: "logging", prefix: "logging"},
			{name: "cache", prefix: "cache"},
			{name: "subtitle", prefix: "subtitle"},
			{name: "storage", prefix: "storage"},
		}

		for _, cf := range configFiles {
			filePath := filepath.Join(dir, cf.name+".yaml")
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				continue
			}

			subViper := viper.New()
			subViper.SetConfigFile(filePath)
			if err := subViper.ReadInConfig(); err != nil {
				return fmt.Errorf("读取 %s 失败: %w", filePath, err)
			}

			// 将分片配置写入主 viper 的对应前缀下
			// 注意：分片配置中的空值不应覆盖主配置文件中已存在的非空值，
			// 避免 config/secrets.yaml 中的空 tmdb_api_key 覆盖 config.yaml 中用户已保存的值
			for _, key := range subViper.AllKeys() {
				fullKey := cf.prefix + "." + key
				newVal := subViper.Get(key)
				existingVal := viper.Get(fullKey)
				// 仅当分片配置的值非空，或主配置中尚无该值时，才进行覆盖
				if !isEmptyValue(newVal) || existingVal == nil || isEmptyValue(existingVal) {
					viper.Set(fullKey, newVal)
				}
			}
		}
	}

	return nil
}

// isEmptyValue 判断配置值是否为"空"（空字符串、nil、空切片等）
// 用于 mergeConfigDir 中避免分片配置的空值覆盖主配置中已有的非空值
func isEmptyValue(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == ""
	case []interface{}:
		return len(val) == 0
	default:
		return false
	}
}

// migrateFromFlatConfig 将旧版扁平字段值合并到子模块配置中
// 规则：如果旧版字段有值且子模块字段为默认值，则使用旧版字段的值
func (c *Config) migrateFromFlatConfig() {
	// 数据库
	if c.DBPath != "" && c.DBPath != "./data/nowen.db" {
		c.Database.DBPath = c.DBPath
	}
	if c.Database.DBPath == "" {
		c.Database.DBPath = "./data/nowen.db"
	}

	// 密钥
	if c.JWTSecret != "" && c.JWTSecret != "fan-video-secret-change-me" {
		c.Secrets.JWTSecret = c.JWTSecret
	}
	if c.Secrets.JWTSecret == "" {
		c.Secrets.JWTSecret = "fan-video-secret-change-me"
	}
	// 应用
	// 注意：扁平字段仅在嵌套字段为零值/默认值时才生效（向后兼容）
	// 如果嵌套字段已有非默认值（说明用户通过新版格式或 API 设置过），则以嵌套字段为准
	if c.App.Port == 0 {
		if c.Port != 0 {
			c.App.Port = c.Port
		} else {
			c.App.Port = 8080
		}
	}
	if c.Debug && !c.App.Debug {
		c.App.Debug = true
	}
	if c.App.DataDir == "" {
		if c.DataDir != "" && c.DataDir != "./data" {
			c.App.DataDir = c.DataDir
		} else {
			c.App.DataDir = "./data"
		}
	}
	if c.App.WebDir == "" {
		if c.WebDir != "" && c.WebDir != "./web/dist" {
			c.App.WebDir = c.WebDir
		} else {
			c.App.WebDir = "./web/public"
		}
	}
	if c.App.FFmpegPath == "" {
		if c.FFmpegPath != "" && c.FFmpegPath != "ffmpeg" {
			c.App.FFmpegPath = c.FFmpegPath
		} else {
			c.App.FFmpegPath = "ffmpeg"
		}
	}
	if c.App.FFprobePath == "" {
		if c.FFprobePath != "" && c.FFprobePath != "ffprobe" {
			c.App.FFprobePath = c.FFprobePath
		} else {
			c.App.FFprobePath = "ffprobe"
		}
	}
	if c.App.VAAPIDevice == "" {
		if c.VAAPIDevice != "" {
			c.App.VAAPIDevice = c.VAAPIDevice
		} else {
			c.App.VAAPIDevice = "/dev/dri/renderD128"
		}
	}

	// 缓存
	if c.CacheDir != "" && c.CacheDir != "./cache" {
		c.Cache.CacheDir = c.CacheDir
	}
	if c.Cache.CacheDir == "" {
		c.Cache.CacheDir = "./cache"
	}
}

// ==================== 便捷访问方法（保持已有 API 兼容） ====================

// BackupDir 返回全量备份存储目录。
// 未显式配置时默认使用 <data_dir>/backups（Docker 下随数据卷持久化）。
func (c *Config) BackupDir() string {
	if strings.TrimSpace(c.App.BackupDir) != "" {
		return c.App.BackupDir
	}
	return filepath.Join(c.App.DataDir, "backups")
}

// IsDefaultJWTSecret 检查是否使用自动生成的 JWT Secret（未在配置文件中显式设置）
// 注意：由于 Load() 中会自动替换默认值，此方法现在检查是否为用户显式配置
func (c *Config) IsDefaultJWTSecret() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// 如果 viper 中原始值仍为默认值，说明用户未显式配置
	return viper.GetString("secrets.jwt_secret") == "fan-video-secret-change-me"
}

// SaveSTRMConfig 将当前 STRM 配置持久化到配置文件
func (c *Config) SaveSTRMConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sc := c.STRM
	viper.Set("strm.default_user_agent", sc.DefaultUserAgent)
	viper.Set("strm.default_referer", sc.DefaultReferer)
	viper.Set("strm.connect_timeout", sc.ConnectTimeout)
	viper.Set("strm.rewrite_hls", sc.RewriteHLS)
	viper.Set("strm.remote_probe", sc.RemoteProbe)
	viper.Set("strm.remote_probe_timeout", sc.RemoteProbeTimeout)
	viper.Set("strm.domain_user_agents", sc.DomainUserAgents)
	viper.Set("strm.domain_referers", sc.DomainReferers)
	return c.saveConfig()
}

// saveConfig 将当前配置写入配置文件
func (c *Config) saveConfig() error {
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		configFile = "config.yaml"
	}
	return viper.WriteConfigAs(configFile)
}

// updateSecretsFile 更新 config/secrets.yaml 分片文件中的指定字段
// 避免分片文件中的旧值在重启时覆盖用户通过 API 保存的新值
func (c *Config) updateSecretsFile(key, value string) {
	secretsDirs := []string{"./config", "./data/config", "/etc/fan-video/config"}
	for _, dir := range secretsDirs {
		filePath := filepath.Join(dir, "secrets.yaml")
		if _, err := os.Stat(filePath); err != nil {
			continue
		}
		subViper := viper.New()
		subViper.SetConfigFile(filePath)
		if err := subViper.ReadInConfig(); err != nil {
			continue
		}
		subViper.Set(key, value)
		_ = subViper.WriteConfigAs(filePath)
		return // 只更新第一个找到的文件
	}
}

// ==================== 数据库 DSN 构造 ====================

// generateRandomSecret 生成随机密钥字符串
func generateRandomSecret(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	// 使用 crypto/rand 生成安全随机数
	if _, err := cryptoRand.Read(b); err != nil {
		// 降级使用时间戳（极端情况）
		for i := range b {
			b[i] = charset[i%len(charset)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// GetDBDSN 返回 SQLite 连接字符串（含优化参数）
//
// 注意：项目使用的是 glebarez/sqlite（基于纯 Go 的 modernc.org/sqlite），
// 其 DSN 参数语法与 mattn/go-sqlite3 不同——必须使用 _pragma=name(value) 形式，
// 以前的 _journal_mode=WAL / _busy_timeout=5000 在该驱动下不会生效。
func (c *Config) GetDBDSN() string {
	dsn := c.Database.DBPath
	params := []string{}

	if c.Database.WALMode {
		params = append(params, "_pragma=journal_mode(WAL)")
		// WAL 下推荐 NORMAL，性能与持久性折中
		params = append(params, "_pragma=synchronous(NORMAL)")
	}
	if c.Database.BusyTimeout > 0 {
		params = append(params, fmt.Sprintf("_pragma=busy_timeout(%d)", c.Database.BusyTimeout))
	}
	if c.Database.CacheSize != 0 {
		params = append(params, fmt.Sprintf("_pragma=cache_size(%d)", c.Database.CacheSize))
	}

	if len(params) > 0 {
		dsn += "?" + strings.Join(params, "&")
	}
	return dsn
}
