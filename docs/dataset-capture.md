# Sample 风格训练数据采集

本功能把完整成功的模型调用保存并导出为 JSONL，每行固定使用参考
`sample.jsonl` 的 11 个顶层字段。管理页面按用户、令牌和对话展示，JSONL 只作为
磁盘存储和下载格式。功能默认关闭，不影响未开启采集的实例。

支持以下接口：

- OpenAI `/v1/chat/completions`
- OpenAI `/v1/responses`
- Anthropic `/v1/messages`
- Gemini `generateContent` / `streamGenerateContent`

不采集 embedding、rerank、图像生成、音频、任务接口和 WebSocket Realtime。

## 在 New API 中启用

```env
DATASET_CAPTURE_ENABLED=true
DATASET_CAPTURE_PATH=./logs/datasets/sample-{date}-{node}.jsonl
DATASET_CAPTURE_HMAC_KEY=replace-with-a-stable-secret
DATASET_CAPTURE_QUEUE_SIZE=128
DATASET_CAPTURE_MAX_DISK_GB=10
```

`DATASET_CAPTURE_ENABLED` 是数据库中尚无对应配置时的初始默认值。Root 可以在
“系统设置 -> 站点与品牌 -> 截获数据”中运行时开启或关闭采集，无需重启服务。
关闭后不再创建新的采集会话，已经写入的历史数据不会被删除。

配置路径的目录部分是分区根目录。新记录写入：

```text
node-<node>/user-<user-id>/token-<token-id>/session-<session-id>.jsonl
```

同一用户、同一令牌、同一对话的每次成功调用追加为一行完整快照。Playground 使用
`token-playground`，无法识别用户或令牌的独立代理请求使用 `anonymous`。旧版
`sample-{date}-{node}.jsonl` 聚合文件保留原样并继续可浏览，不自动迁移。

采集器以最终成功的上游 attempt 为准。失败重试、非 2xx、客户端写入失败、
不完整 SSE 和无法归一化的数据不会写入主数据集。上游流无法安全复用时，会成对
回退到客户端请求与客户端响应，绝不混合两种协议。

认证 Header、Cookie、查询字符串、客户端 IP 和渠道密钥不会写入 JSONL。
请求正文及其中的文本、工具参数、图片、文件和 Base64 数据会完整保留。

客户端可以通过以下 Header 补充可选元数据：

```text
X-Client-Cwd: /workspace/project
```

用户 ID 使用 `DATASET_CAPTURE_HMAC_KEY` 做 HMAC-SHA256。会话 ID 使用请求中
的 `session_id` / `conversation_id`，缺失时使用请求 ID，最终生成 16 位稳定值。

## 运行策略与管理员权限

系统设置只控制采集运行方式：

```text
DatasetCaptureEnabled
DatasetCaptureModelMode = all | selected
DatasetCaptureModels = JSON string[]
```

“全部模型”允许所有受支持请求进入采集；“指定模型”从站点当前可路由模型目录中选择，
且不能保存空列表。准入匹配客户端请求的站点模型，JSONL 的 `model` 仍保存渠道映射后的
最终上游模型。已选择但后来下线的模型会保留并标记为不可用，不会被静默删除。

Root 在“用户管理 -> 编辑管理员 -> 管理员权限”中逐人分配：

- `dataset_capture.view`：查看用户名聚合、筛选结果和完整记录详情。
- `dataset_capture.download`：导出训练 JSONL；必须同时拥有 `view`。

Root 隐式拥有全部能力。管理员默认没有截获数据权限，普通用户始终无权访问。删除不作为
可分配权限，始终保持 Root-only。旧 `DatasetCaptureAdminVisible=true` 仅在升级时一次性
迁移现有管理员的查看与下载权限，迁移后不再参与鉴权。

## 管理页面、筛选与索引

“截获数据”页面以真实用户名为一级分类，展开后按需分页加载记录。页面显示真实用户名、
用户 ID、令牌名称和令牌 ID，不显示 JSONL 文件名、磁盘路径或 source row。

支持组合筛选：

- 开始和结束时间。
- 最终有效模型。
- 真实令牌 ID 和名称。
- 请求最终实际使用的分组。
- 最终成功 attempt 的渠道 ID。
- 用户名或用户 ID。
- system、messages、response 和 tool 内容关键词。

同一维度多值使用 OR，不同维度之间使用 AND。内容搜索不会建立持久化明文全文索引；服务端
先用元数据缩小范围，再读取最多 5,000 条候选。范围过宽时返回 422，要求继续增加筛选条件。

查询使用可重建的 `dataset_capture_indices` 元数据索引。索引不保存正文、认证信息或绝对路径。
服务启动时会扫描可信数据目录，补录历史记录并清除已删除文件的陈旧索引。旧记录无法恢复的
分组、请求模型或渠道 ID 显示“未知”。

## 选择、合并导出与删除

- 勾选用户名：选择该用户在当前完整筛选条件下的所有记录，不局限于当前页。
- 勾选记录：选择指定快照。
- 勾选筛选结果：选择当前筛选条件下的全部用户和记录。
- 用户与记录选择重叠时，服务端自动去重。

所有选中内容合并为一个 `.jsonl` 文件，同一快照一行。服务端按用户、令牌、会话、时间排序，
先生成权限为 `0600` 的临时文件，并逐行验证固定 Schema；全部成功后才开始下载，失败时不会
返回半个文件。页面使用的真实用户名、令牌、分组和渠道元数据不会增加到固定训练 Schema。

Root 可以勾选一条或多条记录后删除。删除粒度是记录所属的完整对话文件；同一对话选中多条
只删除一次。文件追加、索引回调、文件删除和索引清理使用同一文件锁。若同一会话以后产生新
成功请求，会重新创建文件并从第 1 行开始。

## 管理 API

```text
GET    /api/dataset-captures/users
GET    /api/dataset-captures/users/:user_id/records
GET    /api/dataset-captures/facets
GET    /api/dataset-captures/records/:capture_id
POST   /api/dataset-captures/export
DELETE /api/dataset-captures/records/batch

GET    /api/dataset-capture-policy
PUT    /api/dataset-capture-policy
GET    /api/dataset-capture-policy/models
```

所有内容读取、导出和删除都只接受 opaque ID，服务端每次从可信目录 allow-list 重新定位文件，
绝不接受客户端磁盘路径。

## 审计

- 打开完整详情：`dataset_capture.view`。
- 合并导出成功：`dataset_capture.download`。
- 每个完整对话删除成功：`dataset_capture.delete`。
- 管理员权限变化：`dataset_capture.permission_update`。
- 运行策略变化：`dataset_capture.policy_update`。

审计只记录操作人、对象 ID、数量、模型/时间条件、字节数和结果等非正文信息，不记录内容
关键词、提示词、响应、工具参数、认证 Header、Cookie、API Key 或绝对文件路径。

## 独立 Capture Proxy

代理完整透传请求和流式响应，其他项目只需修改 API Base URL。

```powershell
$env:CAPTURE_PROXY_UPSTREAM = "https://api.openai.com"
$env:CAPTURE_PROXY_LISTEN = ":8080"
$env:DATASET_CAPTURE_HMAC_KEY = "replace-with-a-stable-secret"
$env:DATASET_CAPTURE_PATH = ".\logs\datasets\sample-{date}-{node}.jsonl"
go run ./cmd/dataset-capture-proxy
```

随后把客户端 Base URL 改为 `http://127.0.0.1:8080`。原来的 Authorization
Header 或 API Key 保持不变，代理会向上游透传但不会记录。

健康检查地址为：

```text
GET /__capture/health
```

Docker 运行：

```powershell
docker compose -f docker-compose.capture-proxy.yml up --build
```

## 校验数据

```powershell
go run ./cmd/dataset-validate ./logs/datasets/node-node/user-1/token-playground/session-0123456789abcdef.jsonl
```

校验器逐行检查 JSON、固定顶层字段、固定 `_meta` 字段以及消息、工具和响应的
必填结构。它不会打印提示词或响应正文。

## 响应归一化

- OpenAI、Anthropic、Gemini 消息统一为 Anthropic 风格 content blocks。
- system/developer 消息移入 `system_prompt`。
- function tools 统一为 `name`、`description`、`input_schema`。
- 工具调用写入 `response.tool_use.calls`，包含 `id`、`name`、`input`。
- OpenAI cached tokens、Anthropic cache usage、Gemini cached content tokens 统一映射
  到参考样例的 `usage.cache`。
- 供应商结束原因写入 `_meta.raw_finish_reason`，标准结束原因写入
  `response.stop_reason`。
