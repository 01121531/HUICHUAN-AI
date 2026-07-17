# HUICHUAN-AI

<p align="center">
  <img src="./web/default/public/logo.svg" alt="HUICHUAN-AI Logo" width="96" height="96" />
</p>

<p align="center">
  <strong>统一、可靠、可审计的 AI API 网关与数据快照平台</strong>
</p>

<p align="center">
  <a href="https://github.com/01121531/HUICHUAN-AI/releases"><img alt="Release" src="https://img.shields.io/github/v/release/01121531/HUICHUAN-AI?include_prereleases&sort=semver" /></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" />
  <img alt="React" src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black" />
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-AGPL--3.0-7C3AED" />
</p>

HUICHUAN-AI 提供多模型供应商接入、渠道路由、令牌管理、用量计费、在线升级、数据快照留存、管理员授权与访问审计能力，适用于个人、团队和 AI 应用的统一 API 管理场景。

## 当前版本

| 项目 | 值 |
| --- | --- |
| 版本 | [`v1.0.6`](https://github.com/01121531/HUICHUAN-AI/releases/tag/v1.0.6) |
| Go module | `github.com/01121531/HUICHUAN-AI` |
| 默认服务文件 | `HUICHUAN.service` |
| 默认前端 | `web/default` |

## 核心能力

- **统一模型网关**：兼容 OpenAI Chat/Responses、Anthropic Messages、Gemini、Claude、OpenRouter、Ollama、通义、智谱、火山、腾讯、百度等渠道。
- **渠道路由与重试**：支持优先级、权重、分组、模型映射、失败重试和渠道健康检测。
- **令牌与权限管理**：支持用户令牌、模型范围、额度、分组、管理员权限和 Root 特权。
- **用量与计费**：提供请求日志、Token 统计、模型倍率、分组倍率、余额、订阅、充值与账单能力。
- **数据快照**：按模型、用户和令牌范围保存完整成功的模型调用样本，采用响应优先的异步采集链路，支持筛选、查看、导出和删除。
- **访问审计**：管理员查看、下载、删除数据快照都会记录操作人、时间、动作、目标记录和交付状态。
- **在线升级**：支持 Windows、Linux、macOS 独立部署包的网页端版本检测与在线升级。
- **多语言界面**：支持简体中文、繁体中文、英文、法文、日文、俄文和越南文。
- **容器化部署**：支持 Docker、Docker Compose、systemd、Nginx/Caddy 反向代理等部署方式。

## 数据快照功能

数据快照默认关闭，可由 Root 在系统设置中开启，并分别选择模型、用户和令牌范围。采集逻辑以最终成功的上游请求与响应为准；失败重试、非 2xx、客户端取消、半截流或 Schema 校验失败的请求不会进入主数据集。

### 低延迟异步采集

`v1.0.6` 将数据快照链路调整为“客户端响应优先、后台异步持久化”：

- 流式 chunk 先写入客户端，成功后才复制到有界分段缓冲，不在 API 请求主链路写磁盘或 MySQL。
- 完整成功并确认交付后，仅向后台队列非阻塞投递一次；队列拥塞、磁盘或索引异常不会反向阻塞用户请求。
- 协议归一化、Schema 校验、HMAC、JSON 序列化、JSONL 写入和 MySQL 索引均由后台 worker 完成。
- 同时限制单条快照大小和全局在途内存；超过限制时整条丢弃，禁止产生半行 JSONL。
- MySQL 索引使用独立队列批量写入，失败时保留 JSONL，并支持后续索引重建。
- 系统设置提供队列、worker、内存、磁盘、索引批次、导出并发、读取限速和优雅关闭参数。
- 可配置异常邮件收件人、告警类型、静默窗口、连续丢弃阈值和恢复通知。
- 运行状态页面展示队列深度、在途字节、最近写入与丢弃量、磁盘空间及 JSONL/索引 P50、P95 延迟。

完整设计与验收方案见
[`docs/DATA-SNAPSHOT-PERFORMANCE-OPTIMIZATION-PLAN.md`](./docs/DATA-SNAPSHOT-PERFORMANCE-OPTIMIZATION-PLAN.md)。

### 支持协议

| 协议 | 端点 |
| --- | --- |
| OpenAI Chat | `POST /v1/chat/completions` |
| OpenAI Responses | `POST /v1/responses` |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `generateContent` / `streamGenerateContent` |

首版不保存 embedding、rerank、图像生成、音频转写和 WebSocket Realtime。

### JSONL 导出格式

导出的 JSONL 每行固定包含 11 个顶层字段：

```text
session_id
user_id_hash
model
user_agent
system_prompt
tools
messages
response
created_at
cwd
_meta
```

归一化规则：

- `messages` 统一为 Anthropic 风格 `{role, content}`。
- 系统消息移入 `system_prompt`。
- OpenAI function tools 转为 `{name, description, input_schema}`。
- 工具调用和工具结果统一为 `tool_use` / `tool_result` 内容块。
- URL、文件信息和内联 Base64 等多模态内容完整保留。
- 认证 Header、Cookie、API Key、渠道密钥和客户端 IP 不进入数据集。

### 存储结构

每个用户、令牌和会话使用独立 JSONL 文件，便于隔离、删除和导出：

```text
logs/datasets/
└── node-<node>/
    └── user-<user-id>/
        └── token-<token-id>/
            └── session-<session-id>.jsonl
```

页面不直接按 JSONL 文件展示，而是按用户名聚合，并支持按时间、模型、令牌、分组、渠道 ID、用户和内容关键字筛选。管理员可以勾选多条记录或多个用户，将结果合并导出为一个 JSONL 文件。

## 权限与审计

Root 可在用户管理中按管理员单独授权：

| 权限 | 能力 |
| --- | --- |
| `dataset_capture.view` | 查看用户聚合、筛选结果和完整记录详情 |
| `dataset_capture.download` | 导出选择结果，且必须同时具备查看权限 |

以下动作会进入审计：

- 查看数据快照列表与详情
- 下载数据快照 JSONL
- 删除数据快照会话文件
- 更新数据快照策略
- 更新管理员数据快照权限

访问审计不保存提示词、回复正文、工具参数、内容搜索词、认证信息或绝对文件路径。删除原始快照后，审计定位信息仍会保留。

## 数据库结构变化

应用启动时通过 GORM AutoMigrate 自动创建或更新相关表。对话正文保存在 JSONL 文件中，不写入业务数据库。

### `dataset_capture_indices`

可重建的数据快照元数据索引，用于用户聚合、筛选、分页、导出定位和删除定位。主要字段包括：

- `capture_id`
- `node`
- `file_id`
- `row`
- `user_id`
- `token_id`
- `token_scope`
- `user_group`
- `requested_model`
- `effective_model`
- `channel_id`
- `session_id`
- `captured_at`
- `record_size`

该表不保存提示词、回复正文、工具参数、认证信息、令牌密钥或绝对路径。

### `dataset_capture_access_audits`

保存管理员访问事件，包括管理员 ID、用户名、认证方式、IP、操作类型、交付状态、记录数量、导出字节数和时间。

### `dataset_capture_access_audit_items`

保存访问事件中的每条数据快照定位信息，包括 `capture_id`、用户名、令牌名、模型、分组、渠道 ID 和会话 ID。该表用于事后确认“谁在什么时候查看或下载了哪条数据”。

### `options`

新增或使用以下系统配置项：

| Key | 说明 |
| --- | --- |
| `DatasetCaptureEnabled` | 是否开启数据快照 |
| `DatasetCaptureModelMode` | `all` 或 `selected` |
| `DatasetCaptureModels` | 指定模型列表 JSON |
| `DatasetCapturePolicyV2` | 版本化的采集范围、性能保护和异常邮件策略 JSON |
| `DatasetCapturePermissionMigrated` | 旧版权限迁移标记 |

## 独立 Capture Proxy

其他项目可以只修改 API Base URL，通过独立代理复用同一套归一化器：

```powershell
$env:CAPTURE_PROXY_UPSTREAM = "https://api.openai.com"
$env:CAPTURE_PROXY_LISTEN = ":8080"
$env:DATASET_CAPTURE_HMAC_KEY = "replace-with-a-stable-secret"
$env:DATASET_CAPTURE_PATH = ".\logs\datasets\sample-{date}-{node}.jsonl"
go run ./cmd/dataset-capture-proxy
```

健康检查：

```http
GET /__capture/health
```

## 快速开始

### Docker 开发环境

```bash
git clone https://github.com/01121531/HUICHUAN-AI.git
cd HUICHUAN-AI
docker compose -f docker-compose.dev.yml up -d --build
```

访问：

```text
http://localhost:3000
```

### 本地开发

```bash
# 启动 PostgreSQL、Redis 和后端服务
make dev-api

# 启动默认前端
make dev-web
```

也可以单独启动前端：

```bash
cd web
bun install --frozen-lockfile
cd default
bun run dev
```

## 构建与验证

```bash
go vet ./...
go test ./... -count=1
go build -o .run/huichuan.exe .
cd web/default
bun run i18n:sync
bun run build:check
```

## 升级说明

升级前请备份数据库和 `logs/datasets`。Linux systemd 独立部署推荐在服务文件中设置：

```ini
[Service]
KillMode=process
TimeoutStopSec=35
```

这样在线升级辅助进程不会在主进程重启时被 `KillMode=control-group` 一起清理。`v1.0.5` 起即使旧服务模板导致辅助进程被 systemd 清理，新版本启动时也会自动识别“目标版本已经运行但状态停在 restarting”的情况，并把状态修复为 `succeeded`。

## v1.0.6 更新摘要

- 数据快照流式响应改为客户端优先写入，移除请求主链路中的同步 spool、JSONL 和 MySQL I/O。
- 新增有界分段缓冲、单条样本限制、全局在途内存限制和非阻塞完成任务队列。
- 将协议归一化、Schema 校验、HMAC、JSON 序列化及持久化全部迁移到后台 worker。
- JSONL 与 MySQL 索引解耦，增加批量 upsert、有限重试、索引重建和磁盘用量周期校准。
- 增加导出并发与读取限速、超时优雅排空以及跨平台磁盘剩余空间检测。
- 数据快照策略扩展到模型、用户、令牌、流式请求和多模态 Base64 范围。
- 新增运行状态、异常邮件、静默聚合、恢复通知及测试邮件接口。
- 重构系统设置中的数据快照页面，并补齐简体中文、繁体中文、英文、法文、日文、俄文和越南文。
- 新增可重复的 SSE A/B 压测脚本，输出首字节、chunk 间隔、完整耗时和进程资源差异。
- 发布 Linux amd64/arm64、macOS amd64/arm64 和 Windows amd64 独立部署包及 SHA-256 校验文件。

## v1.0.5 更新摘要

- 修复 Linux systemd 在线升级卡在 `94% / restarting` 的问题。
- 新增启动时升级状态自愈：当当前运行版本等于升级目标版本时，自动将中断的升级状态标记为成功。
- 更新 `HUICHUAN.service` 模板，增加 `KillMode=process` 和 `TimeoutStopSec=35`，避免升级辅助进程被 systemd 默认控制组清理。
- 修复 README 中文乱码并保留项目 Logo 展示。

## v1.0.4 更新摘要

- Linux/macOS 在线升级支持跨平台 Release 资产选择。
- Unix 独立部署升级时优先替换目标二进制，再等待服务管理器重启到新版本。
- 增加升级状态超时归一化，避免长期停留在活动状态。

## v1.0.3 更新摘要

- 在线升级扩展到 Linux amd64/arm64 与 macOS amd64/arm64 独立部署包。
- Release 构建产物命名统一为 `huichuan-ai-<version>-<platform>-<arch>`。
- 更新升级页面文案，去除“仅 Windows 64 位”的限制。

## v1.0.2 更新摘要

- 项目命名全面迁移为 HUICHUAN-AI / HUICHUAN。
- Go module 迁移为 `github.com/01121531/HUICHUAN-AI`。
- 删除 `.agents/skills` 以及不属于当前项目发布内容的设计文档。
- 修复 Go vet 发现的返回值、SSE 渲染和地址拼接问题。
- 修复数据快照访问审计，增强查看、下载和删除的留痕能力。
- 更新 README，记录数据快照和数据库结构变化。

## License

AGPL-3.0
