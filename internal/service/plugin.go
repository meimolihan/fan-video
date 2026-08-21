package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PluginService 可扩展插件系统
// 建立开放的插件架构，支持第三方开发者扩展功能
type PluginService struct {
	db        *gorm.DB
	logger    *zap.SugaredLogger
	mu        sync.RWMutex
	pluginDir string
	loaded    map[string]*LoadedPlugin
	registry  *PluginRegistry
}

// PluginInfo 插件信息（存储在数据库）
type PluginInfo struct {
	ID          string    `json:"id" gorm:"primaryKey;type:text"`
	Name        string    `json:"name" gorm:"type:text;not null"`
	Version     string    `json:"version" gorm:"type:text"`
	Author      string    `json:"author" gorm:"type:text"`
	Description string    `json:"description" gorm:"type:text"`
	Type        string    `json:"type" gorm:"type:text;not null"` // media_source / theme / player / metadata / notification
	EntryPoint  string    `json:"entry_point" gorm:"type:text"`   // 入口文件路径
	ConfigJSON  string    `json:"config_json" gorm:"type:text"`   // 插件配置（JSON）
	Enabled     bool      `json:"enabled" gorm:"default:false"`
	Installed   bool      `json:"installed" gorm:"default:true"`
	Homepage    string    `json:"homepage" gorm:"type:text"`
	License     string    `json:"license" gorm:"type:text"`
	MinVersion  string    `json:"min_version" gorm:"type:text"` // 最低兼容版本
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PluginManifest 插件清单文件（plugin.json）
type PluginManifest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Author      string            `json:"author"`
	Description string            `json:"description"`
	Type        string            `json:"type"`
	EntryPoint  string            `json:"entry_point"`
	Homepage    string            `json:"homepage"`
	License     string            `json:"license"`
	MinVersion  string            `json:"min_version"`
	Config      []PluginConfigDef `json:"config"`
	Hooks       []string          `json:"hooks"` // 注册的钩子点
	Permissions []string          `json:"permissions"`
}

// PluginConfigDef 插件配置项定义
type PluginConfigDef struct {
	Key         string      `json:"key"`
	Label       string      `json:"label"`
	Type        string      `json:"type"` // string / number / boolean / select
	Default     interface{} `json:"default"`
	Required    bool        `json:"required"`
	Options     []string    `json:"options,omitempty"` // select 类型的选项
	Description string      `json:"description"`
}

// LoadedPlugin 已加载的插件
type LoadedPlugin struct {
	Info     *PluginInfo
	Manifest *PluginManifest
	Config   map[string]interface{} // 运行时配置
}

// PluginHook 插件钩子接口
type PluginHook interface {
	OnMediaAdded(mediaID string, metadata map[string]interface{}) error
	OnMediaScraped(mediaID string, metadata map[string]interface{}) error
	OnPlaybackStart(userID, mediaID string) error
	OnPlaybackEnd(userID, mediaID string, position float64) error
	OnSearch(query string) ([]map[string]interface{}, error)
}

// PluginRegistry 插件注册表（管理钩子）
type PluginRegistry struct {
	mu    sync.RWMutex
	hooks map[string][]PluginHookHandler
}

// PluginHookHandler 钩子处理器
type PluginHookHandler struct {
	PluginID string
	Handler  func(data map[string]interface{}) (map[string]interface{}, error)
}

func NewPluginService(db *gorm.DB, pluginDir string, logger *zap.SugaredLogger) *PluginService {
	db.AutoMigrate(&PluginInfo{})

	if pluginDir == "" {
		pluginDir = "./data/plugins"
	}
	os.MkdirAll(pluginDir, 0755)

	svc := &PluginService{
		db:        db,
		logger:    logger,
		pluginDir: pluginDir,
		loaded:    make(map[string]*LoadedPlugin),
		registry: &PluginRegistry{
			hooks: make(map[string][]PluginHookHandler),
		},
	}

	// 启动时加载已启用的插件
	go svc.loadEnabledPlugins()

	return svc
}

// InstallPlugin 安装插件（从目录）
func (s *PluginService) InstallPlugin(pluginPath string) (*PluginInfo, error) {
	// 读取插件清单
	manifestPath := filepath.Join(pluginPath, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取插件清单失败: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析插件清单失败: %w", err)
	}

	if manifest.ID == "" || manifest.Name == "" {
		return nil, fmt.Errorf("插件清单缺少必要字段（id, name）")
	}

	// 复制插件到插件目录
	destDir := filepath.Join(s.pluginDir, manifest.ID)
	if err := copyDir(pluginPath, destDir); err != nil {
		return nil, fmt.Errorf("复制插件文件失败: %w", err)
	}

	// 保存到数据库
	info := &PluginInfo{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Author:      manifest.Author,
		Description: manifest.Description,
		Type:        manifest.Type,
		EntryPoint:  manifest.EntryPoint,
		Homepage:    manifest.Homepage,
		License:     manifest.License,
		MinVersion:  manifest.MinVersion,
		Enabled:     false,
		Installed:   true,
	}

	// 序列化默认配置
	if len(manifest.Config) > 0 {
		defaultConfig := make(map[string]interface{})
		for _, cfg := range manifest.Config {
			defaultConfig[cfg.Key] = cfg.Default
		}
		configJSON, _ := json.Marshal(defaultConfig)
		info.ConfigJSON = string(configJSON)
	}

	s.db.Save(info)

	s.logger.Infof("插件已安装: %s v%s", manifest.Name, manifest.Version)
	return info, nil
}

// UninstallPlugin 卸载插件
func (s *PluginService) UninstallPlugin(pluginID string) error {
	// 先禁用
	s.DisablePlugin(pluginID)

	// 删除文件
	pluginPath := filepath.Join(s.pluginDir, pluginID)
	os.RemoveAll(pluginPath)

	// 删除数据库记录
	s.db.Delete(&PluginInfo{}, "id = ?", pluginID)

	s.logger.Infof("插件已卸载: %s", pluginID)
	return nil
}

// EnablePlugin 启用插件
func (s *PluginService) EnablePlugin(pluginID string) error {
	var info PluginInfo
	if err := s.db.First(&info, "id = ?", pluginID).Error; err != nil {
		return fmt.Errorf("插件不存在: %s", pluginID)
	}

	info.Enabled = true
	s.db.Save(&info)

	// 加载插件
	if err := s.loadPlugin(&info); err != nil {
		s.logger.Warnf("加载插件失败: %s -> %v", pluginID, err)
		return err
	}

	s.logger.Infof("插件已启用: %s", info.Name)
	return nil
}

// DisablePlugin 禁用插件
func (s *PluginService) DisablePlugin(pluginID string) error {
	s.mu.Lock()
	delete(s.loaded, pluginID)
	s.mu.Unlock()

	// 移除钩子
	s.registry.mu.Lock()
	for hookName, handlers := range s.registry.hooks {
		var filtered []PluginHookHandler
		for _, h := range handlers {
			if h.PluginID != pluginID {
				filtered = append(filtered, h)
			}
		}
		s.registry.hooks[hookName] = filtered
	}
	s.registry.mu.Unlock()

	s.db.Model(&PluginInfo{}).Where("id = ?", pluginID).Update("enabled", false)

	s.logger.Infof("插件已禁用: %s", pluginID)
	return nil
}

// ListPlugins 获取所有插件
func (s *PluginService) ListPlugins() ([]PluginInfo, error) {
	var plugins []PluginInfo
	err := s.db.Order("created_at DESC").Find(&plugins).Error
	return plugins, err
}

// GetPlugin 获取插件详情
func (s *PluginService) GetPlugin(pluginID string) (*PluginInfo, *PluginManifest, error) {
	var info PluginInfo
	if err := s.db.First(&info, "id = ?", pluginID).Error; err != nil {
		return nil, nil, err
	}

	// 读取清单
	manifestPath := filepath.Join(s.pluginDir, pluginID, "plugin.json")
	var manifest *PluginManifest
	if data, err := os.ReadFile(manifestPath); err == nil {
		manifest = &PluginManifest{}
		json.Unmarshal(data, manifest)
	}

	return &info, manifest, nil
}

// UpdatePluginConfig 更新插件配置
func (s *PluginService) UpdatePluginConfig(pluginID string, config map[string]interface{}) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	return s.db.Model(&PluginInfo{}).Where("id = ?", pluginID).
		Update("config_json", string(configJSON)).Error
}

// ExecuteHook 执行钩子
func (s *PluginService) ExecuteHook(hookName string, data map[string]interface{}) []map[string]interface{} {
	s.registry.mu.RLock()
	handlers := s.registry.hooks[hookName]
	s.registry.mu.RUnlock()

	var results []map[string]interface{}
	for _, h := range handlers {
		result, err := h.Handler(data)
		if err != nil {
			s.logger.Warnf("插件钩子执行失败: %s/%s -> %v", h.PluginID, hookName, err)
			continue
		}
		if result != nil {
			results = append(results, result)
		}
	}
	return results
}

// ScanPluginDir 扫描插件目录发现新插件
func (s *PluginService) ScanPluginDir() ([]PluginManifest, error) {
	var discovered []PluginManifest

	entries, err := os.ReadDir(s.pluginDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(s.pluginDir, entry.Name(), "plugin.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		// 检查是否已安装
		var count int64
		s.db.Model(&PluginInfo{}).Where("id = ?", manifest.ID).Count(&count)
		if count == 0 {
			discovered = append(discovered, manifest)
		}
	}

	return discovered, nil
}

// ==================== 内部方法 ====================

func (s *PluginService) loadEnabledPlugins() {
	time.Sleep(2 * time.Second) // 等待服务启动

	var plugins []PluginInfo
	s.db.Where("enabled = ?", true).Find(&plugins)

	for i := range plugins {
		if err := s.loadPlugin(&plugins[i]); err != nil {
			s.logger.Warnf("加载插件失败: %s -> %v", plugins[i].Name, err)
		}
	}

	if len(plugins) > 0 {
		s.logger.Infof("已加载 %d 个插件", len(plugins))
	}
}

func (s *PluginService) loadPlugin(info *PluginInfo) error {
	// 读取清单
	manifestPath := filepath.Join(s.pluginDir, info.ID, "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("读取插件清单失败: %w", err)
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("解析插件清单失败: %w", err)
	}

	loaded := &LoadedPlugin{
		Info:     info,
		Manifest: &manifest,
	}

	s.mu.Lock()
	s.loaded[info.ID] = loaded
	s.mu.Unlock()

	s.logger.Debugf("插件已加载: %s v%s", info.Name, info.Version)
	return nil
}

// copyDir 复制目录
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(src, path)
		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	})
}
