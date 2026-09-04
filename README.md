# fan-video

轻量级媒体库管理服务，仅支持本地海报匹配。

> 📖 二进制构建、镜像推送、运行方式与常用命令：见 [BUILD.md](./BUILD.md)

## 功能特性

- ✅ 本地海报匹配（优先同名图片，失败自动提取视频第一帧）
- ✅ NFO 元数据解析
- ✅ 媒体库扫描与索引
- ✅ WebDAV 远程存储支持
- ❌ 无网络刮削
- ❌ 无 AI 功能

## 海报匹配规则

```
1. 视频目录同名图片（.jpg/.jpeg/.png/.webp）
   支持 -poster/-cover/-thumb 后缀

2. 任意子目录同名图片
   例：/电影/流浪地球.mp4 → /电影/封面图/流浪地球.jpg

3. 视频第一帧（兜底）
   当前两步均失败时，自动提取第1秒帧
```

## 快速开始

### Docker 部署

```bash
# 构建
docker build -t mobufan/fan-video:latest .

# 运行
docker run -d \
  --name fan-video \
  -p 8060:8080 \
  -v $(pwd)/data:/data \
  -v /path/to/media:/media:ro \
  fan-video
```

### 媒体库配置

编辑 `docker-compose.yml`：

```yaml
volumes:
  - ./data:/data
  - /your/media/path:/media:ro
```

## 项目结构

```
fan-video/
├── cmd/server-lite/     # 服务入口
├── internal/
│   ├── service/         # 核心服务（含海报匹配）
│   ├── handler/         # HTTP 处理器
│   ├── repository/      # 数据层
│   └── model/           # 数据模型
├── web/                 # 前端
├── config/              # 配置文件
└── docker-compose.yml   # Docker 部署配置
```

## 开发

```bash
# 本地构建
go build -o fan-video ./cmd/server-lite

# 运行
./fan-video
```

## 版本

- **v2.2** - 全面优化：
  - 统一 `.jpeg` 后缀支持
  - 统一海报/背景图命名规则的扩展名一致性
  - 更新 Dockerfile 注释
  - 项目说明文档完善
- **v2.1** - 精简重构，移除网络刮削和 AI 功能

## 项目状态

| 组件 | 状态 | 说明 |
|------|------|------|
| 本地海报匹配 | ✅ 完整 | 阶段1 同名 → 阶段1b 子目录 → 阶段3 通用 → 阶段4 视频首帧 |
| .jpeg 支持 | ✅ 完整 | 所有图片查找阶段均支持 |
| NFO 解析 | ✅ 完整 | Kodi/Emby/Jellyfin 风格 |
| WebDAV | ✅ 可用 | 远程存储可选配置 |
| 网络刮削 | ❌ 已移除 | TMDb、豆瓣、成人内容等 |
| AI 功能 | ❌ 已移除 | 精彩片段、章节生成等 |
| 媒体库扫描 | ✅ 完整 | 电影、剧集、剧集合集 |
| 流媒体服务 | ✅ 完整 | 直连、Remux、转码 |
| 文件管理 | ✅ 完整 | 批量重命名等 |
