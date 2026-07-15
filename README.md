<div align="center">
  <img src="web/default/public/logo.svg" alt="HUICHUAN-AI Logo" width="92" />

# HUICHUAN-AI

**统一、可靠、可审计的 AI API 网关与训练数据采集平台**

统一接入多家模型服务，集中管理渠道、令牌、路由、计费和训练数据。

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white)
![License](https://img.shields.io/badge/License-AGPL--3.0-7C3AED)

</div>

## 项目简介

HUICHUAN-AI 是面向个人、团队和 AI 应用的 API 管理平台。它将不同模型供应商统一到一套入口中，并提供渠道管理、令牌权限、请求路由、重试、用量统计和计费控制。

本分支在网关能力之上增加了训练数据采集系统。系统只保存完整成功的模型调用，将 OpenAI、Anthropic 和 Gemini 请求统一为固定 Schema，并提供按用户组织、组合筛选、权限隔离、审计、批量导出和整段对话删除能力。

## 核心能力

- **统一模型网关**：通过兼容 API 接入和切换不同模型服务。
- **智能路由与重试**：按照优先级、权重、分组和可用性选择渠道。
- **令牌与权限管理**：为用户、团队或应用签发独立令牌并限制模型和额度。
- **用量与计费控制**：集中管理定价、余额、配额、订阅和消费记录。
- **日志与可观测性**：查看请求状态、延迟、Token 用量和错误信息。
- **训练数据采集**：采集最终成功的上游请求与响应，统一输出训练 JSONL。
- **细粒度管理员授权**：Root 可逐个管理员授予采集数据查看和下载权限。
- **响应式管理界面**：支持桌面端、移动端、浅色与深色主题。
- **多语言界面**：覆盖简体中文、繁体中文、英文、法文、日文、俄文和越南文。

## 训练数据采集

### 支持的协议

- OpenAI `POST /v1/chat/completions`
- OpenAI `POST /v1/responses`
- Anthropic `POST /v1/messages`
- Gemini `generateContent` / `streamGenerateContent`

首版不采集 embedding、rerank、图像生成、音频、任务接口和 WebSocket Realtime。失败重试、非 2xx、客户端取消、不完整流以及 Schema 校验失败的请求不会写入主数据集。

### 数据组织

每个用户、令牌和会话使用独立文件：

```text
logs/datasets/
└── node-<node>/
    └── user-<user-id>/
        └── token-<token-id>/
            └── session-<session-id>.jsonl
```

同一对话中的每次成功调用追加为一行完整快照。页面不以文件为展示单位，而是按真实用户名分组，并显示真实用户 ID、令牌名称和令牌 ID。

导出文件每行严格保持以下 11 个顶层字段：

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

- 系统消息移入 `system_prompt`，消息统一为 Anthropic 风格的 `{role, content}`。
- OpenAI function tools 统一为 `{name, description, input_schema}`。
- 工具调用和工具结果统一为 `tool_use` / `tool_result` 内容块。
- URL、文件元数据和内联 Base64 等多模态内容完整保留。
- Token、缓存 Token 和结束原因统一映射到固定 `response` 结构。
- 认证 Header、Cookie、API Key、查询字符串、客户端 IP 和渠道密钥不会进入数据集。

### 管理页面

截获数据页面支持：

- 按用户名分组，展开后分页浏览该用户的截获记录。
- 按时间、最终模型、令牌、实际分组、最终渠道 ID、用户名和内容关键词组合筛选。
- 查看消息、响应、工具调用、Token 用量和原始记录详情。
- 勾选多个用户或多条记录，合并导出为一个经过严格校验的 JSONL 文件。
- Root 批量删除选中记录所属的完整对话文件。

内容搜索只在元数据筛选后的候选中执行，默认最多扫描 5,000 条记录，不建立持久化明文全文索引。

### 权限与审计

Root 在“用户管理 -> 编辑管理员 -> 管理员权限”中逐人分配：

| 权限 | 能力 |
| --- | --- |
| `dataset_capture.view` | 查看用户聚合、筛选结果和完整记录详情 |
| `dataset_capture.download` | 导出选择结果，且必须同时拥有查看权限 |

Root 隐式拥有全部能力；普通管理员默认没有采集数据权限；普通用户始终不能访问；删除和策略配置保持 Root-only。

以下操作写入使用记录/管理审计：

- `dataset_capture.view`
- `dataset_capture.download`
- `dataset_capture.delete`
- `dataset_capture.permission_update`
- `dataset_capture.policy_update`

审计记录不包含提示词、响应正文、工具参数、内容搜索词、认证信息或绝对文件路径。

### 启用与模型范围

采集默认关闭。Root 可在“系统设置 -> 维护设置 -> 截获数据”中开启，并选择“全部模型”或站点现有模型中的指定模型。策略在运行时生效，无需重启。

环境变量用于首次启动默认值和存储参数：

```env
DATASET_CAPTURE_ENABLED=false
DATASET_CAPTURE_PATH=./logs/datasets/sample-{date}-{node}.jsonl
DATASET_CAPTURE_HMAC_KEY=replace-with-a-stable-secret
DATASET_CAPTURE_QUEUE_SIZE=128
DATASET_CAPTURE_MAX_DISK_GB=10
```

准入使用客户端请求的站点模型；样本中的 `model` 保存渠道映射后的最终上游模型。生产环境必须设置稳定且足够强的 `DATASET_CAPTURE_HMAC_KEY`。

更完整的配置、管理 API、导出规则和代理使用说明见 [训练数据采集文档](docs/dataset-capture.md)，设计与维护基线见 [截获数据重构方案](docs/dataset-capture-redesign-plan.md)。

## 数据库结构变化

应用启动时通过 GORM `AutoMigrate` 创建或更新采集索引表。训练正文不写入业务数据库，仍保存在受控 JSONL 目录中。

### `dataset_capture_indices`

该表是可重建的非正文元数据索引，用于用户聚合、筛选、分页、导出定位和删除定位：

| 字段 | 用途 |
| --- | --- |
| `id` | 自增主键 |
| `capture_id` | 单条快照 opaque ID，唯一索引 |
| `node` | 写入节点标识 |
| `file_id` | 对话文件 opaque ID，与 `row` 组成唯一索引 |
| `row` | 快照在对话文件中的行号 |
| `user_id` | 站点用户 ID |
| `token_id` | 站点令牌 ID |
| `token_scope` | 令牌存储作用域 |
| `user_group` | 请求最终实际使用的分组 |
| `requested_model` | 客户端请求的站点模型 |
| `effective_model` | 映射后的最终上游模型 |
| `channel_id` | 最终成功 attempt 的渠道 ID |
| `session_id` | 16 位稳定会话标识 |
| `captured_at` | 捕获时间戳 |
| `record_size` | JSONL 行字节数 |

索引表不保存提示词、响应、工具参数、认证信息、令牌密钥或绝对路径。服务启动时扫描可信数据目录，补录可恢复记录并移除陈旧索引。

### `options`

新增或使用以下系统配置项：

| Key | 值 |
| --- | --- |
| `DatasetCaptureEnabled` | `true` / `false` |
| `DatasetCaptureModelMode` | `all` / `selected` |
| `DatasetCaptureModels` | JSON 字符串数组 |

三个配置通过事务统一保存，避免出现“指定模型但列表为空”等中间非法状态。

### 管理员权限数据

采集权限复用现有 `casbin_rule` 和 `authz_roles` 授权体系，不新增明文权限表。旧版本若存在 `DatasetCaptureAdminVisible=true`，启动时会一次性为当时的管理员迁移 `view + download`，并写入 `DatasetCapturePermissionMigrated=true` 标记；之后逐管理员授权成为唯一依据。

### 升级说明

1. 升级前备份主数据库和 `logs/datasets`。
2. 启动新版本，等待数据库自动迁移和采集索引校正完成。
3. 使用 Root 账号检查采集开关、模型范围和管理员权限。
4. 历史聚合 JSONL 保留原样，不自动改写为按用户、令牌和会话分区的目录。

## 独立 Capture Proxy

其他项目可只修改 API Base URL，通过独立代理复用同一归一化器：

```powershell
$env:CAPTURE_PROXY_UPSTREAM = "https://api.openai.com"
$env:CAPTURE_PROXY_LISTEN = ":8080"
$env:DATASET_CAPTURE_HMAC_KEY = "replace-with-a-stable-secret"
$env:DATASET_CAPTURE_PATH = ".\logs\datasets\sample-{date}-{node}.jsonl"
go run ./cmd/dataset-capture-proxy
```

健康检查：`GET /__capture/health`

Docker 启动：

```bash
docker compose -f docker-compose.capture-proxy.yml up -d --build
```

代理会透传请求方法、路径、请求体、认证信息、状态码和流式响应，但不会把认证信息写入数据集。

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go 1.25、Gin、GORM |
| 前端 | React 19、TypeScript、Rsbuild、Tailwind CSS、shadcn/ui |
| 数据库 | SQLite、MySQL、PostgreSQL |
| 缓存 | Redis |
| 权限 | Casbin + 应用权限资源 |
| 部署 | Docker、Docker Compose、Nginx/Caddy |

## 快速开始

### Docker 开发环境

环境要求：Docker 和 Docker Compose。

```bash
git clone https://github.com/01121531/HUICHUAN-AI.git
cd HUICHUAN-AI
docker compose -f docker-compose.dev.yml up -d --build
```

启动后访问：

```text
http://localhost:3000
```

查看状态与停止服务：

```bash
docker compose -f docker-compose.dev.yml ps
curl http://localhost:3000/api/status
docker compose -f docker-compose.dev.yml down
```

首次启动后按页面引导创建 Root 账号。公开部署前必须更换数据库密码、会话密钥、采集 HMAC 密钥及其他默认配置。

### 本地开发

环境要求：Go 1.25+、Bun、Docker。

```bash
# 启动 PostgreSQL、Redis 和后端服务
make dev-api

# 启动默认版前端
make dev-web
```

前端也可单独启动：

```bash
cd web
bun install --frozen-lockfile
cd default
bun run dev
```

## 验证

后端采集相关测试：

```bash
go test ./pkg/datasetcapture ./middleware ./controller ./router ./service/authz ./setting/dataset_capture_setting
go test ./service -run 'Test(ReconcileDatasetCaptureIndexAndContentSearch|BuildDatasetCaptureExport|DeleteDatasetCaptureConversations)'
```

前端构建：

```bash
cd web/default
bun run build
```

逐行校验导出的 JSONL：

```bash
go run ./cmd/dataset-validate ./logs/datasets/node-node/user-1/token-playground/session-0123456789abcdef.jsonl
```

## 项目结构

```text
HUICHUAN-AI/
├── cmd/                         # 独立采集代理与 JSONL 校验器
├── controller/                  # HTTP 接口控制器
├── model/                       # 数据模型与数据库逻辑
├── pkg/datasetcapture/          # 采集、归一化、Schema 与写入核心
├── pkg/systemupdate/            # 在线升级检查、校验、替换、健康验证与回滚
├── relay/                       # 模型请求转发与适配
├── router/                      # 路由定义
├── service/                     # 查询、导出、删除与索引服务
├── setting/                     # 运行时设置
├── docs/                        # 设计、ADR 和使用文档
├── web/default/                 # 默认 React 前端
├── docker-compose.dev.yml
└── main.go
```

## 网页端在线升级

Root 可以在“系统设置 → 运维设置 → 版本更新”中检查
[`01121531/HUICHUAN-AI`](https://github.com/01121531/HUICHUAN-AI)
的最新正式 Release。Windows 64 位独立 EXE 部署支持直接在线安装：

1. 服务端重新获取固定仓库的最新 Release，浏览器不能指定下载地址。
2. 下载匹配平台的 EXE 和 `checksums-windows.txt`，验证文件大小与 SHA-256。
3. 将当前 EXE 复制为临时更新辅助程序并暂存新版本。
4. Root 二次确认后进行受控重启；辅助程序备份旧版本并替换当前 EXE。
5. 新进程必须在规定时间内通过 `/api/status` 健康检查并报告目标版本，否则自动恢复旧版本。

在线安装仅接受 `vMAJOR.MINOR.PATCH` 格式的稳定版本。Docker、Kubernetes、
`go run`、非 Windows amd64、只读可执行目录和设置了 `VERSION` 环境覆盖的部署会显示明确的禁用原因，
这些环境应继续使用镜像或外部发布系统升级。

可选环境变量：

```env
# 显式关闭应用内在线升级
SYSTEM_UPDATE_ENABLED=false

# 私有仓库或需要提高 GitHub API 限额时使用；不会返回给前端
SYSTEM_UPDATE_GITHUB_TOKEN=

# 在线升级时等待当前服务优雅退出的秒数
SYSTEM_UPDATE_SHUTDOWN_TIMEOUT_SECONDS=30
```

更新状态保存在可执行文件相邻的 `.huichuan-update/state.json`，用于重启后恢复进度；
API 不会返回下载 URL、本地路径或认证信息。完整设计和状态机见
[`docs/adr/ADR-online-system-update.md`](docs/adr/ADR-online-system-update.md)。

## 数据与发布安全

- `.env`、数据库、日志、运行产物和 `data/` 已加入 Git 忽略规则。
- 不要提交真实采集 JSONL、用户数据、认证凭据、Cookie、Token、数据库备份或生产日志。
- 公开 Issue、PR 或日志前应移除用户名、路径、请求正文和其他敏感内容。
- 生产环境应限制 `logs/datasets` 的文件权限并纳入加密备份和生命周期管理。

## 许可证

本项目依据 [GNU Affero General Public License v3.0](LICENSE) 发布。部署、修改或分发时请遵守许可证要求。
