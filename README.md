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
| 版本 | [`v1.0.7`](https://github.com/01121531/HUICHUAN-AI/releases/tag/v1.0.7) |
| Go module | `github.com/01121531/HUICHUAN-AI` |
| 默认服务文件 | `HUICHUAN.service` |
| 默认前端 | `web/default` |

## 核心能力

- **统一模型网关**：兼容 OpenAI Chat/Responses、Anthropic Messages、Gemini、Claude、OpenRouter、Ollama、通义、智谱、火山、腾讯、百度等渠道。
- **渠道路由与重试**：支持优先级、权重、分组、模型映射、失败重试和渠道健康检测。
- **令牌与权限管理**：支持用户令牌、模型范围、额度、分组、管理员权限和 Root 特权。
- **用量与计费**：提供请求日志、Token 统计、模型倍率、分组倍率、余额、订阅、充值与账单能力。
- **数据快照**：按模型、用户和令牌范围保存完整成功的模型调用样本，采用响应优先的异步采集链路，支持筛选、查看、导出和删除。
- **访问审计与邮件提醒**：记录管理员查看、下载和删除行为，并可按操作人或数据所属用户发送异步邮件提醒。
- **在线升级**：支持 Windows、Linux、macOS 独立部署包的网页端版本检测与在线升级。
- **多语言界面**：支持简体中文、繁体中文、英文、法文、日文、俄文和越南文。
- **容器化部署**：支持 Docker、Docker Compose、systemd、Nginx/Caddy 反向代理等部署方式。

## 数据快照

数据快照默认关闭，可由 Root 在系统设置中开启，并分别选择模型、用户和令牌范围。采集逻辑以最终成功的上游请求与响应为准；失败重试、非 2xx、客户端取消、半截流或 Schema 校验失败的请求不会进入主数据集。

### 低延迟异步采集

数据快照采用“客户端响应优先、后台异步持久化”的处理方式：

- 流式 chunk 先写入客户端，再复制到有界分段缓冲。
- 完整成功并确认交付后，仅向后台队列非阻塞投递一次。
- 协议归一化、Schema 校验、HMAC、JSON 序列化、JSONL 写入和数据库索引均由后台 worker 完成。
- API 请求主链路不执行快照磁盘写入或 MySQL 索引写入。
- 队列、内存或磁盘资源不足时允许少量丢弃，优先保证用户 API 响应。
- MySQL 索引使用独立队列批量写入；索引失败时保留 JSONL，并支持后续重建。
- 大响应在后台转入权限为 `0600` 的临时 spool，处理完成后删除。

### 运行状态和性能保护

系统设置页面提供以下运行指标：

- 完成任务队列深度与容量
- 索引队列深度与容量
- 全局在途字节数
- 已写入与已丢弃数量
- JSONL 与索引失败数量
- 磁盘占用和剩余空间
- JSONL/索引 P50、P95 延迟
- 最近错误类型和时间
- 告警队列和邮件队列状态

所有性能参数标题均提供 `?` 帮助说明，展示参数用途、有效范围、默认值和安全约束。

以下参数支持“不限制”模式：

| 参数 | `0` 的含义 | 建议 |
| --- | --- | --- |
| 最小磁盘剩余空间 | 停用磁盘预留检查 | 仅在外部磁盘监控可靠时使用 |
| 快照存储上限 | 不限制快照目录总大小 | 可能持续增长直到文件系统空间耗尽 |
| 导出读取限速 | 不限制单个导出的读取速度 | 可能与 API 流量竞争磁盘吞吐 |

队列大小、worker 数、单条快照上限、在途内存、spool 阈值、索引批次和导出并发属于安全背压参数，必须保留有限值。

### 异常和访问邮件

系统设置支持两类异步邮件：

1. **运行异常邮件**
   - 队列溢出
   - 样本过大
   - 内存不足
   - 磁盘空间不足
   - JSONL、spool 或索引写入失败
   - 恢复通知
2. **数据访问邮件**
   - 管理员查看快照详情
   - 管理员下载快照导出
   - 可选择所有管理员或指定管理员
   - 可选择所有数据所属用户或指定用户
   - 操作人和数据所属用户范围同时设置为“指定”时，两者必须同时匹配

访问邮件只在查看或下载成功交付后进入后台邮件队列，不阻塞页面请求。邮件包含操作人、时间、动作、记录数量和快照定位信息，不包含提示词、回复正文、工具参数、令牌密钥或认证信息。

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

每个用户、令牌和会话使用独立 JSONL 文件：

```text
logs/datasets/
└── node-<node>/
    └── user-<user-id>/
        └── token-<token-id>/
            └── session-<session-id>.jsonl
```

页面不按 JSONL 文件展示，而是按用户名聚合，并支持按时间、模型、令牌、分组、渠道 ID、用户和内容关键字筛选。管理员可以勾选记录、用户或全部筛选结果，将多组数据合并导出为一个 JSONL 文件，也可以按相同选择范围批量删除完整会话。

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

## 数据库结构

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

保存访问事件中的每条数据快照定位信息，包括 `capture_id`、用户名、令牌名、模型、分组、渠道 ID 和会话 ID，用于确认“谁在什么时候查看或下载了哪条数据”。

### `options`

数据快照使用以下系统配置项：

| Key | 说明 |
| --- | --- |
| `DatasetCaptureEnabled` | 是否开启数据快照 |
| `DatasetCaptureModelMode` | `all` 或 `selected` |
| `DatasetCaptureModels` | 指定模型列表 JSON |
| `DatasetCapturePolicyV2` | 版本化的采集范围、性能保护和邮件策略 JSON |
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

设置 `DATASET_CAPTURE_MAX_DISK_GB=0` 可取消独立代理的快照存储总量上限。生产环境仍建议保留磁盘预留或外部磁盘告警。

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
go test ./... -count=1
go vet ./...

cd web/default
bun run i18n:sync
bun run build:check
```

## 在线升级

Windows、Linux 和 macOS 的独立部署包支持在“系统设置 → 运维 → 更新检查”中检测并执行在线升级。

升级前请：

1. 备份业务数据库和 `logs/datasets`。
2. 确认服务账户对安装目录和状态目录具有写权限。
3. 确认安装方式使用 GitHub Release 中与当前操作系统和架构匹配的产物。
4. 升级后检查数据库迁移、数据快照开关、模型范围、管理员权限和访问审计。

Docker、Kubernetes 或其他编排环境建议更新镜像并由编排系统完成滚动升级，不应在容器内部直接替换可执行文件。

## v1.0.7 更新摘要

- 为数据快照性能参数增加 `?` 帮助图标，说明作用、范围、默认值和安全限制。
- 为最小磁盘剩余空间、快照存储上限和导出限速增加“不限制”控制。
- 独立 Capture Proxy 支持 `DATASET_CAPTURE_MAX_DISK_GB=0`。
- 新增数据快照查看和下载邮件提醒。
- 支持按所有或指定管理员、所有或指定数据所属用户过滤访问提醒。
- 访问邮件通过有界后台队列发送，不阻塞查看和下载请求。
- 批量删除支持已选用户、已选记录和全部筛选结果，并复用导出选择语义。
- 补齐七种语言的参数帮助、访问提醒和“不限制”文案。
- 修复 README 中文编码损坏问题。

## License

[AGPL-3.0](./LICENSE)
