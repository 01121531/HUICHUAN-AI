# 数据快照对话完整性优化方案

## 1. 文档信息

- **适用项目**：HUICHUAN-AI
- **方案状态**：待实施
- **编写日期**：2026-07-18
- **实施约束**：本阶段只形成设计方案，不修改业务代码。
- **核心目标**：在不改变 API 对外响应语义和响应时序的前提下，保证数据快照能够完整表达一次模型调用中的系统提示词、用户输入、助手输出、推理内容、工具调用、工具结果和结束状态。

## 2. 背景与现状判断

### 2.1 当前数据并非只有用户内容

现有快照格式将对话上下文保存在 `messages`，将本次模型最终输出保存在 `response.content`。因此查看 `messages` 标签时只看到用户历史消息，并不代表快照没有 AI 输出；AI 输出目前位于独立的 `response` 标签中。

当前详情页存在展示割裂：

1. `Messages` 和 `Response` 分开显示，用户容易把上下文误认为完整对话。
2. `response.content` 只能表达最终文本，不能明确表达模型的 reasoning/thinking 内容。
3. 工具调用位于 `response.tool_use`，与助手文本没有统一的时间顺序。
4. 没有一套明确的完整性状态来区分“正常无文本但有工具调用”和“响应聚合失败”。

### 2.2 当前格式边界

当前 JSONL 保持与 `sample.jsonl` 兼容的 11 个顶层字段：

`session_id`、`user_id_hash`、`model`、`user_agent`、`system_prompt`、`tools`、`messages`、`response`、`created_at`、`cwd`、`_meta`。

当前 `response` 主要包含：

- `content`
- `stop_reason`
- `tool_use`
- `usage`

方案必须保持现有字段兼容，不得因为新增推理信息而破坏已有导出文件和旧版读取程序。

## 3. 目标与非目标

### 3.1 目标

1. 完整保留本次调用实际交付给客户端的助手文本。
2. 在供应商明确返回且允许记录时，保留 reasoning/thinking 内容及其顺序。
3. 统一 OpenAI Chat/Responses、Anthropic Messages、Gemini 的文本、推理、工具调用和工具结果。
4. 正确聚合非流式和流式响应，只有完整成功的响应进入主数据集。
5. 让详情页默认看到“完整对话时间线”，不再要求用户在多个标签之间寻找 AI 输出。
6. 在不等待磁盘、数据库、JSON Schema 校验或导出任务的情况下完成 API 响应。
7. 对缺失、截断、供应商不支持或客户端取消的内容给出可解释状态。

### 3.2 非目标

- 不伪造供应商没有返回的思考内容。
- 不从最终文本反推隐藏 reasoning。
- 不把认证信息、Cookie、API Key、渠道密钥或客户端 IP 写入样本正文。
- 不在本方案中改变模型路由、计费结算或客户端响应协议。
- 不要求所有模型都提供 reasoning；供应商未返回时记录为空或 `unsupported`。

## 4. 兼容数据模型

### 4.1 顶层字段保持不变

继续输出固定 11 个顶层字段。新增信息优先放入已有 `messages` 内容块和 `response` 的兼容扩展位置，避免改变顶层字段集合。

### 4.2 统一内容块

`messages[].content` 统一使用内容块数组。建议支持以下类型：

```json
{"type":"text","text":"..."}
{"type":"thinking","thinking":"..."}
{"type":"reasoning","text":"...","summary":"..."}
{"type":"tool_use","id":"call_1","name":"search","input":{}}
{"type":"tool_result","tool_use_id":"call_1","content":"...","is_error":false}
{"type":"image_url","url":"..."}
{"type":"input_file","file_id":"...","filename":"..."}
```

约束：

- `text` 表示对用户可见的文本。
- `thinking` 表示供应商明确标记的内部思考块；只有配置允许时保存正文。
- `reasoning` 表示 OpenAI Responses 等协议提供的推理摘要或推理事件。
- `tool_use` 和 `tool_result` 必须保留调用 ID，保证多轮工具链可还原。
- 多模态 URL、文件信息和内联 Base64 遵循现有保留/脱敏策略。
- 不改变旧记录；旧记录没有新增块时按历史格式读取。

### 4.3 响应扩展建议

为保持旧消费者兼容，`response.content` 继续保存最终可见文本，`response.tool_use` 继续保存聚合后的工具调用。同时允许在 `response` 内增加可选对象：

```json
{
  "content": "最终可见文本",
  "reasoning": {
    "content": "供应商返回的推理摘要或思考内容",
    "blocks": [],
    "visibility": "captured|redacted|unsupported|missing",
    "source": "provider_event|provider_field|none"
  },
  "stop_reason": "end_turn",
  "tool_use": {"input_already_merged": true, "calls": []},
  "usage": {}
}
```

该扩展必须使用 `omitempty` 或稳定的空对象策略，具体以 Schema 版本确定；旧版读取器只读取已知字段时仍能正常工作。

### 4.4 完整性元数据

建议在 `_meta` 增加可选字段，不改变已有字段含义：

```json
{
  "capture_status": "complete|incomplete|delivery_failed|provider_missing|redacted",
  "response_protocol": "openai-chat|openai-responses|anthropic|gemini",
  "reasoning_status": "captured|redacted|unsupported|missing|not_requested",
  "stream_terminated": true,
  "assistant_blocks": 3,
  "capture_warnings": []
}
```

状态含义必须由后端生成，前端不得根据字段是否为空自行推断成功或失败。

## 5. 供应商归一化规则

### 5.1 OpenAI Chat Completions

- 非流式：读取 `choices[].message.content`、`message.tool_calls`，兼容供应商扩展的 reasoning 字段。
- 流式：按 `choices[].delta` 顺序累加 `content`、`reasoning_content` 或等价字段；按 tool call index 合并 ID、名称和 arguments。
- 以 `finish_reason` 和完整 SSE 终止标志共同判断结束；只有收到完整终止事件才允许入库。

### 5.2 OpenAI Responses

- 读取 `response.output` 中的 message、reasoning、function/tool call 和 tool result。
- 处理 `response.output_text.delta`、reasoning delta、`response.completed`/`response.done` 等事件。
- 保留 item 顺序和 item ID，避免把推理、文本和工具调用错误拼接。

### 5.3 Anthropic Messages

- 读取 `content` 中的 `text`、`thinking`、`tool_use`、`tool_result`。
- 流式处理 `content_block_start`、`content_block_delta`、`content_block_stop`、`message_delta`、`message_stop`。
- 将 `thinking_delta` 与普通文本分开聚合，禁止把思考正文混入 `response.content`。

### 5.4 Gemini generateContent

- 读取 `candidates[].content.parts[]` 中的 `text`、`thought`/思考扩展、`functionCall` 和 `functionResponse`。
- 流式按 candidate、part 顺序合并，记录 `finishReason`。
- 对不同 SDK 或渠道返回的字段别名建立兼容映射，并保留原始结束原因。

### 5.5 不支持或未返回 reasoning 的模型

当供应商没有提供可验证的 reasoning/thinking 字段时：

- `reasoning_status = unsupported`：协议明确不提供。
- `reasoning_status = missing`：协议支持，但本次响应没有返回。
- `reasoning_status = redacted`：策略要求不保存正文，但确认供应商返回过。
- 禁止以空字符串伪装成“已捕获”。

## 6. 流式聚合与响应优先架构

### 6.1 强制事件顺序

每个流式事件必须遵循：

1. 先向客户端写入并 Flush 原始事件。
2. 确认客户端写入成功后，仅在内存中复制必要的增量片段。
3. 更新请求级聚合器，不进行磁盘、数据库、Schema 校验、HMAC 或邮件操作。
4. 收到终止事件后标记 `stream_terminated`。
5. 客户端响应完成后，非阻塞提交后台快照任务。

任何聚合失败都不得回写或延迟已交付给客户端的事件。

### 6.2 请求级聚合器

聚合器只保存不可变的协议事件和必要元数据：

- 最终成功 attempt 标识。
- 文本片段序列。
- reasoning/thinking 片段序列。
- 工具调用状态表。
- 工具结果关联表。
- usage、finish reason、终止状态。
- 客户端交付状态和告警码。

聚合器必须有单请求大小上限和全局内存水位；超限时停止复制快照数据，但继续透传响应，并在 `_meta.capture_warnings` 中记录 `memory_limit`。

### 6.3 非流式响应

非流式响应完成写入客户端后，再将响应正文交给后台任务。不得因为 JSON 归一化或写入 JSONL 失败而改变已经返回的 HTTP 状态码和响应体。

### 6.4 重试与取消

- 失败 attempt 只能进入临时状态，不进入主 JSONL。
- 最终成功 attempt 才能生成快照。
- 客户端取消、写入失败、SSE 半截或终止事件缺失时，标记 `delivery_failed` 或 `incomplete`，不写主数据集。
- 允许将脱敏计数写入指标或错误日志，但不得写入半条样本。

## 7. 后台持久化流程

后台 worker 按以下顺序执行：

1. 校验请求级聚合状态和最终成功 attempt。
2. 将协议事件归一化为统一内容块。
3. 生成兼容的 `response.content`、`response.reasoning` 和 `response.tool_use`。
4. 填充完整性元数据。
5. 执行 Schema 校验、JSON 序列化和数据快照索引写入。
6. 以单次原子追加写入对应用户、令牌、会话 JSONL 文件。
7. 更新索引和统计；任一持久化失败不得影响已完成的额度结算。

队列满、磁盘不足或 worker 异常时：

- API 请求立即返回或继续完成，不等待后台任务。
- 记录丢弃计数、最近错误和队列水位。
- 按现有数据快照邮件告警策略通知管理员。
- 不产生半行 JSONL，不覆盖已有有效记录。

## 8. 页面与查询体验

### 8.1 默认展示“完整对话”

详情页默认使用时间线展示：

1. System prompt
2. 用户消息
3. 助手 reasoning/thinking（显示“推理内容”标签和策略状态）
4. 助手可见文本
5. 工具调用
6. 工具结果
7. 最终结束原因和 usage

原有 `Messages`、`Response`、`Tools`、`Usage` 标签保留，用于原始结构查看和兼容操作。

### 8.2 明确空值状态

页面不得将空内容渲染成“没有 AI 输出”。应显示：

- `已捕获文本输出`
- `仅工具调用，无文本输出`
- `供应商未提供推理内容`
- `推理内容按策略隐藏`
- `响应不完整，未进入主数据集`

### 8.3 搜索和导出

- 内容搜索覆盖用户文本、助手文本、reasoning 摘要、工具名称和工具结果。
- 默认导出保持 11 个顶层字段；新增可选扩展字段原样保留。
- 导出说明中显示 reasoning 是否捕获、是否脱敏以及完整性状态。
- 不因页面默认隐藏 reasoning 而删除已保存数据。

## 9. 安全与隐私策略

1. Root 通过系统设置控制是否保存 reasoning 正文、仅保存摘要或完全不保存。
2. 对外部供应商返回的敏感推理内容，不进行未经配置的二次推断或扩写。
3. 访问、查看、下载和导出审计只记录定位信息与状态，不记录提示词、回复正文或 reasoning 正文。
4. 数据快照文件继续使用 `0600` 权限、临时文件原子替换和目录隔离。
5. reasoning 字段的内容搜索、导出和删除必须复用现有权限模型。

## 10. 分步实施计划

### 第 1 步：建立完整性判定和协议事件模型

**修改范围**：`pkg/datasetcapture` 类型、解析器和 Schema 测试。

**内容**：定义内容块、reasoning 状态、终止状态、工具调用关联和兼容序列化规则；不改变现有 API 输出。

**测试门禁**：旧 JSONL 回读、固定 11 个顶层字段、空文本工具调用、非法事件和半截流。

### 第 2 步：完善四类协议的非流式归一化

**内容**：补齐 OpenAI Chat/Responses、Anthropic、Gemini 的 reasoning、工具结果和多模态映射。

**测试门禁**：每个协议建立 golden fixture，逐字段比较文本、reasoning、工具 ID、finish reason 和 usage。

### 第 3 步：完善流式事件聚合

**内容**：按事件顺序聚合文本和 reasoning，处理终止事件、重复事件、乱序片段、工具参数增量和客户端取消。

**测试门禁**：SSE 多 chunk、空 chunk、重复终止、半截 SSE、长上下文和并发请求测试。

### 第 4 步：接入响应优先后台队列

**内容**：确认最后一次客户端 `Write/Flush` 早于快照任务提交；worker 完成归一化、校验、写盘和索引。

**测试门禁**：慢磁盘、慢数据库、队列满、worker panic、服务关闭排空和响应顺序测试。

### 第 5 步：改造详情页完整对话时间线

**内容**：默认合并 system、messages、reasoning、assistant、tool_use、tool_result；保留原始结构标签。

**测试门禁**：完整、无 reasoning、仅工具调用、缺失快照、旧格式和移动端布局。

### 第 6 步：上线开关、监控与渐进发布

**内容**：增加 reasoning 保存策略、完整性统计、告警阈值和灰度开关；先对合成请求和内部用户启用。

**测试门禁**：开关切换、权限校验、告警邮件、回滚到旧格式、生产构建和端到端请求。

## 11. 性能与 API 行为保证

### 11.1 对 API 响应的影响

在满足“先响应、后持久化”的架构约束后，主链路只增加：

- 每个流式事件一次轻量内存复制。
- 聚合器的计数、状态更新和边界检查。
- 一次非阻塞队列投递。

主链路禁止执行 JSONL 写入、MySQL 写入、Schema 校验、reasoning 大对象序列化、邮件发送和导出任务。

### 11.2 预期性能指标

以关闭数据快照为基线，在相同模型和并发下验收：

| 指标 | 目标 |
| --- | --- |
| 首 Token P95 增量 | 不超过 2 ms 或 2% |
| 流式 chunk P95 增量 | 不超过 1 ms |
| 最后一块交付 P95 增量 | 不超过 2 ms |
| API P95（后台写入期间） | 增量不超过 5% |
| 主链路磁盘/数据库写入 | 0 次 |
| 队列满时请求阻塞 | 0 ms，立即降级 |

reasoning 内容可能较大，必须通过单请求和全局内存上限保护；达到上限时只丢弃快照复制，不影响客户端响应。

## 12. 验收清单

- [ ] `messages`、`response.content` 和 reasoning 的语义和展示明确。
- [ ] OpenAI Chat/Responses、Anthropic、Gemini 非流式 golden tests 通过。
- [ ] 四类协议流式聚合和终止判断通过。
- [ ] 文本、推理、工具调用、工具结果顺序可还原。
- [ ] 半截流、客户端取消和失败重试不进入主 JSONL。
- [ ] 固定 11 个顶层字段保持兼容。
- [ ] 旧快照可读取，新增字段缺失不会导致页面崩溃。
- [ ] API 响应先于日志提交，队列满不阻塞请求。
- [ ] 详情页默认显示完整对话时间线。
- [ ] reasoning 状态、空值和仅工具调用状态可解释。
- [ ] `go test ./... -count=1`、`go vet ./...`、前端 `build:check` 和生产构建通过。
- [ ] 在具备 CGO 的 Linux/CI 环境执行 `go test -race ./...`。
- [ ] 使用合成流式请求完成端到端验证，并记录首 Token、chunk 和最后一块延迟。

## 13. 风险与回滚

| 风险 | 处理措施 |
| --- | --- |
| 供应商字段变更 | 保留原始 finish reason，增加协议 fixture 和未知字段告警 |
| reasoning 内容过大 | 单请求/全局内存上限，超限只丢弃快照复制 |
| 流式终止事件缺失 | 标记 `incomplete`，不写主 JSONL |
| 页面把扩展字段当必填 | 所有新增字段可选，前端按状态渲染 |
| 后台队列故障 | 非阻塞丢弃、指标、聚合邮件告警 |
| 误保存敏感内容 | Root 策略开关、默认最小化保存、审计不记录正文 |
| 新格式兼容性问题 | 保持 11 个顶层字段，扩展字段版本化，支持回滚读取器 |

回滚时关闭 reasoning 捕获开关即可；既有 11 字段样本不需要迁移，新增扩展字段由旧读取器忽略。

## 14. 最终结论

当前样本已经包含本次 AI 最终输出，主要问题是详情页展示割裂以及 reasoning 没有独立、可解释的采集状态。最佳优化方向不是重新抓取或延迟响应，而是：

1. 在响应透传后用请求级内存聚合器旁路复制事件。
2. 统一解析文本、reasoning、工具调用和工具结果。
3. 以完整终止事件和客户端交付状态作为入库前提。
4. 由后台队列完成归一化、校验和 JSONL 持久化。
5. 在页面默认展示完整时间线，并明确“未提供、已隐藏、缺失和不完整”等状态。

这样可以解决“看起来只有用户数据”的误解，并为确实缺少 AI 输出或推理内容的请求提供可验证的原因，同时不改变当前 API 的响应逻辑。
