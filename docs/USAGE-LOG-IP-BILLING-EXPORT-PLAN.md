# 使用日志 IP、真实计价快照与表格导出方案

## 1. 文档目的

本文档用于指导 HUICHUAN-AI 使用日志能力的下一阶段建设，覆盖以下需求：

1. 由 Root 在系统设置中统一控制是否记录 API 请求 IP。
2. 消费日志和错误日志保存请求 IP。
3. Root、管理员可以查看所有用户的请求 IP，普通用户只能查看自己的请求 IP。
4. 使用日志列表直接展示完整 IP，不提供掩码或敏感信息隐藏开关。
5. 消费日志展示请求发生时的真实计价方式、价格快照和计算过程。
6. 支持将勾选日志或当前筛选条件下的全部日志导出为 XLSX、CSV。
7. 大批量导出使用后台任务，不能阻塞正常 API 请求。

本文档只定义设计、实施步骤、测试和验收标准，不代表相关功能已经完成。

---

## 2. 已确认的产品决策

### 2.1 IP 采集

- 使用 Root 系统级强制开关。
- 开关打开后，所有用户新产生的消费日志和错误日志统一记录请求 IP。
- 用户不能自行关闭系统级 IP 采集。
- 旧的个人 `record_ip_log` 配置仅保留数据兼容，不再决定是否记录 IP。
- 系统级 IP 采集默认关闭，由 Root 明确开启。
- 历史日志不补录 IP。

### 2.2 IP 查看权限

| 身份 | 可访问日志范围 | 可查看完整 IP |
| --- | --- | --- |
| Root | 所有用户日志 | 是 |
| 管理员 | 所有用户日志 | 是 |
| 普通用户 | 仅自己的日志 | 是 |
| 未登录用户 | 无 | 否 |

页面和导出文件均直接使用完整 IP，不提供掩码模式和“眼睛”切换。权限范围由后端强制限定：Root、管理员可以访问全部用户日志，普通用户只能访问自己的日志。

### 2.3 计价展示

- 保存并展示请求发生时的真实计价快照。
- 不读取当前模型价格重新计算历史费用。
- 支持按 Token、按次、动态阶梯、订阅、免费和附加费用等计价方式。
- 历史记录如果缺少完整快照，只进行兼容展示，并明确标记无法完整还原。

### 2.4 表格导出

- 同时支持 XLSX 和 CSV。
- 支持导出勾选的日志。
- 支持导出当前筛选条件下的全部日志。
- 导出范围不受当前分页限制。
- 大批量导出转为后台任务。
- Root、管理员可以导出全部用户日志。
- 普通用户只能导出自己的日志。

---

## 3. 最高设计原则：优先完整响应用户

新增的 IP、计价快照、日志序列化和表格导出能力，不得阻塞以下用户体验指标：

- 首字响应时间；
- 流式数据块转发；
- 最后一个流式数据块交付；
- 非流式响应正文交付；
- 上游模型实际生成速度。

总体原则：

```text
先完整交付模型响应
-> 再进行最终结算
-> 再非阻塞提交日志任务
-> 后台生成计价快照并写入日志
```

### 3.1 流式请求处理顺序

```text
请求进入
-> 身份、令牌、额度和路由校验
-> 执行必要的额度预扣
-> 请求上游模型
-> 优先向客户端转发每个流式数据块
-> 在数据写入客户端成功后，仅收集必要的 Usage 状态
-> 向客户端发送最后一个流式数据块
-> Flush 完成
-> 完成最终计费结算
-> 非阻塞提交使用日志任务
-> 后台生成真实计价快照
-> 后台写入消费日志
```

流式写入必须保持：

```go
n, err := writer.Write(data)
if n > 0 {
    usageCollector.TryObserve(data[:n])
}
return n, err
```

`TryObserve` 只允许执行有界内存操作，不得访问 MySQL、磁盘、SMTP 或执行复杂 JSON 序列化。

### 3.2 非流式请求处理顺序

```text
请求上游模型
-> 获得完整响应
-> 将响应写入客户端
-> Flush 完成
-> 完成最终计费结算
-> 非阻塞提交使用日志任务
-> 后台生成计价快照并写入日志
```

### 3.3 计费与日志分离

必须区分：

1. 实际额度结算：属于财务关键链路，不能因为日志队列已满而丢失。
2. 使用日志和计价快照：属于审计与展示链路，可以在响应完成后异步处理。

额度预扣仍然在请求上游前执行。最终响应交付后执行结算，随后把结算结果作为不可变任务载荷提交给日志队列。

### 3.4 非阻塞队列

请求处理协程不得等待日志队列：

```go
select {
case usageLogQueue <- job:
    // 提交成功
default:
    // 队列已满：记录指标并发送聚合告警，不阻塞用户请求
}
```

队列满、MySQL 异常、磁盘不足或后台任务异常时：

- 不修改已经成功交付给客户端的 API 响应；
- 不延迟后续流式数据块；
- 增加丢弃和失败指标；
- 首次异常触发管理员邮件；
- 静默窗口内聚合相同告警；
- 系统恢复后发送恢复通知。

---

## 4. 当前实现基础与主要差距

当前 `logs` 数据结构已经包含：

```go
Ip string `json:"ip"`
```

消费日志和错误日志也已经具备通过 `c.ClientIP()` 获取 IP 的能力，但是否记录由用户个人 `record_ip_log` 设置决定。

当前日志 `other` 中已经保存部分计价字段，包括：

- `model_ratio`
- `model_price`
- `completion_ratio`
- `group_ratio`
- `user_group_ratio`
- `cache_ratio`
- `cache_creation_ratio`
- `billing_mode`
- `matched_tier`
- `billing_source`
- 音频、图片、搜索和订阅相关字段

默认前端也已经具备部分计价详情展示，但仍存在以下差距：

1. 使用日志列表没有独立的请求 IP 列。
2. IP 采集不是 Root 统一控制。
3. 完整 IP 目前会直接包含在日志接口结果中，前端隐藏不等于数据没有下发。
4. 计价展示仍可能根据旧倍率字段重新推导，不是统一的不可变计价快照。
5. 缺少结构化的计费组成和最终结算过程。
6. 使用日志没有 XLSX、CSV 导出接口。
7. 表格没有跨分页选择和“导出全部筛选结果”能力。
8. 缺少日志导出后台任务、文件保留和下载鉴权。

---

## 5. 系统级 IP 采集

### 5.1 系统设置

在以下位置增加 Root 专属配置：

```text
系统设置
└── 运维
    └── 日志设置
        ├── 记录 API 请求 IP
        └── 可信代理 CIDR
```

建议配置键：

```text
UsageLogIPCaptureEnabled=false
TrustedProxyCIDRs=[]
```

系统开关使用内存中的不可变配置或原子值发布。请求主链路只读取内存，不访问数据库。

### 5.2 旧配置兼容

- 保留 `UserSetting.RecordIpLog` 字段，避免旧数据解析失败。
- 默认前端和经典前端移除个人“记录 IP 地址”开关。
- 更新个人设置时不再让 `record_ip_log` 影响日志采集。
- 后端记录条件只读取 `UsageLogIPCaptureEnabled`。
- 升级后系统开关默认关闭，不根据旧用户设置自动打开。

### 5.3 IP 获取与标准化

在认证和日志中间件能够访问到的请求入口解析一次 IP，并写入 Gin Context：

```text
usage_log_client_ip
```

处理要求：

1. 使用 `net.ParseIP` 标准化 IPv4、IPv6。
2. 去除端口、IPv6 Zone 等非地址部分。
3. 无法解析时保存空值，不保存原始恶意字符串。
4. 后续消费日志和错误日志复用 Context 中的值。
5. 不在请求结束时再次解析转发头。

### 5.4 可信代理

不得无条件信任客户端发送的：

- `X-Forwarded-For`
- `X-Real-IP`
- `Forwarded`

只有请求来源属于 Root 配置的可信代理 CIDR 时，才允许使用代理转发头。否则使用 TCP 连接来源地址。

部署时应确保 Nginx、Caddy、Cloudflare Tunnel 或负载均衡器覆盖而不是简单追加外部传入的伪造头。

---

## 6. IP 接口和页面

### 6.1 API 参数

日志接口不再提供 `ip_visibility` 参数。后端先按当前身份确定日志查询范围，再直接返回该范围内的完整 IP；普通用户接口始终由服务端限定为当前用户，不能通过查询参数访问其他用户日志。

### 6.2 完整值规则

- IPv4、IPv6 均按采集时的标准化完整值展示。
- 空值显示 `—`。
- 非法新值在采集入口丢弃，不制造虚假占位值。
- 历史记录不做掩码转换。

### 6.3 表格设计

使用日志新增“请求 IP”列：

- 消费日志、错误日志显示 IP。
- 其他日志显示 `—`。
- 完整 IP 可以复制。
- 列可以由用户隐藏，列显示状态沿用现有表格本地配置。
- 移动端放入“网络信息”区域。

页面不提供敏感信息“眼睛”按钮，也不在本地维护显示/隐藏状态。首次进入使用日志页面时记录一次完整 IP 访问审计，分页和筛选不重复制造审计记录。

### 6.4 IP 筛选

IP 筛选可作为同一阶段实现：

- Root、管理员可以筛选所有日志 IP。
- 普通用户只能筛选自己的日志。
- 支持精确 IP。
- 可选支持 IPv4/IPv6 前缀。
- 使用参数化查询，不直接拼接 SQL。
- 不允许任意通配符导致全表扫描。

---

## 7. 请求时真实计价快照

### 7.1 快照原则

计价快照必须满足：

1. 在本次请求最终结算结果确定后生成。
2. 只使用本次请求已经计算出的价格、倍率、Usage 和结算结果。
3. 不重新读取当前模型价格。
4. 不重新执行当前版本的价格表达式。
5. 修改系统价格后，历史日志显示保持不变。
6. 快照字段版本化，后续可以兼容演进。

### 7.2 存储位置

首版继续存入 `logs.other`，避免为每种计价组成增加大量主表列：

```json
{
  "billing_snapshot_v1": {
    "version": "v1",
    "status": "complete",
    "mode": "per_token",
    "source": "wallet",
    "requested_model": "claude-sonnet-5",
    "effective_model": "claude-sonnet-5",
    "base_currency": "USD",
    "quota_per_unit": 500000,
    "display_currency": "USD",
    "exchange_rate": 1,
    "group_ratio": 1,
    "user_group_ratio": null,
    "components": [],
    "pre_consumed_quota": 0,
    "settlement_delta": 3381,
    "final_charged_quota": 3381,
    "rounding": "round"
  }
}
```

### 7.3 计费组成

每个计费组成独立保存：

```json
{
  "kind": "input_tokens",
  "quantity": 91,
  "unit": "token",
  "unit_price_usd": 2,
  "price_unit": 1000000,
  "ratio": 1,
  "subtotal_quota": 91
}
```

支持的组成类型：

- 输入 Token；
- 输出 Token；
- 缓存读取；
- 缓存创建；
- 5 分钟缓存创建；
- 1 小时缓存创建；
- 音频输入；
- 音频输出；
- 图片输入；
- 图片生成；
- Web Search；
- File Search；
- 工具调用附加费；
- 按次费用；
- 动态阶梯费用；
- 订阅扣费；
- 违规附加费；
- 其他版本化扩展费用。

### 7.4 计费模式

统一枚举：

```text
per_token
per_call
tiered_expr
subscription
free
violation_fee
unknown
```

动态阶梯计价额外保存：

- 表达式版本；
- 表达式哈希；
- 命中的阶梯；
- 本次请求实际使用的计价输入；
- 每个费用组成；
- 最终舍入和结算结果。

为了避免泄露内部定价规则，普通用户导出的内容默认不包含动态表达式全文，只包含命中阶梯、价格组成和最终结果。Root、管理员也不需要通过使用日志导出表达式全文。

### 7.5 结算信息

快照需要区分：

- 预扣额度；
- 最终计算额度；
- 结算差额；
- 实际钱包扣费；
- 实际订阅扣费；
- 退款；
- 舍入方式；
- 溢出或钳制状态。

最终计费值以实际结算结果为准，而不是前端公式显示值。

### 7.6 历史记录

历史数据按以下状态展示：

| 状态 | 含义 |
| --- | --- |
| `complete` | 存在完整真实快照 |
| `legacy` | 只有旧倍率和价格字段，可兼容展示 |
| `missing` | 无法恢复完整计价过程 |

页面必须明确显示“真实计价快照”“历史兼容数据”或“无法完整还原”，不得把按当前价格计算的结果伪装成历史真实费用。

---

## 8. 计价页面设计

### 8.1 表格摘要

使用日志列表新增“计价方式”列，显示：

- 按 Token；
- 按次；
- 动态计价；
- 订阅；
- 免费；
- 违规扣费；
- 未知。

费用列继续显示最终实际扣费。计价方式只描述结算机制，不替代最终费用。

### 8.2 桌面详情

桌面端参考行内展开方式，点击日志行或展开箭头后显示：

```text
Request ID
Upstream Request ID
请求 IP
请求路径
请求模型
实际模型
计价方式
计费来源
请求时单价
Token 明细
计费过程
分组倍率
预扣额度
结算差额
最终实际扣费
计价快照状态
```

计算过程示例：

```text
输入：91 × $2 / 1,000,000
输出：658 × $10 / 1,000,000
缓存读取：0 × $0.2 / 1,000,000
小计：$0.006762
分组倍率：1x
实际扣费：$0.006762
```

### 8.3 移动端

移动端使用底部抽屉或全屏详情：

- 第一屏显示模型、时间、状态、最终费用。
- 第二层显示 Token 和计价组成。
- 网络信息单独显示 IP。
- 长 Request ID、IPv6 和计费公式允许换行与复制。

---

## 9. 日志选择与导出交互

### 9.1 行选择

表格增加复选框：

- 选择单条日志；
- 全选当前页；
- 选择当前筛选条件下的全部日志；
- 清除选择。

选择全部筛选结果时，不把所有 ID 加载到浏览器，而是保存：

```text
selection_mode=filtered
filters=<当前已应用筛选条件>
```

### 9.2 导出菜单

工具栏新增：

```text
导出
├── 导出勾选记录
│   ├── XLSX
│   └── CSV
└── 导出全部筛选结果
    ├── XLSX
    └── CSV
```

确认窗口显示：

- 导出范围；
- 预计记录数；
- 文件格式；
- IP 固定为完整值；
- 当前筛选条件；
- 是否将转入后台任务；
- 文件预计保留时间。

### 9.3 筛选一致性

导出必须复用使用日志查询条件：

- 时间范围；
- 日志类型；
- 模型；
- 令牌；
- 分组；
- 用户名；
- 渠道 ID；
- Request ID；
- Upstream Request ID；
- 可选的 IP。

后端接收并验证筛选条件快照，不读取浏览器当前页数据作为全部导出的数据源。

---

## 10. XLSX 输出设计

XLSX 建议使用三个工作表。

### 10.1 日志明细

每条日志一行：

- 时间；
- 日志类型；
- 用户 ID；
- 用户名；
- 令牌 ID；
- 令牌名称；
- 请求模型；
- 实际模型；
- 渠道 ID；
- 渠道名称；
- 分组；
- 请求 IP；
- Request ID；
- Upstream Request ID；
- 请求路径；
- 是否流式；
- 响应时间；
- 首字响应时间；
- 输入 Token；
- 输出 Token；
- 缓存读取 Token；
- 缓存写入 Token；
- 计价方式；
- 计费来源；
- 最终显示费用；
- 最终扣费额度；
- 计价快照状态；
- 日志摘要。

### 10.2 计价组成

每个费用组成一行：

- Request ID；
- 组成类型；
- 数量；
- 单位；
- 请求时单价；
- 价格单位；
- 倍率；
- 小计额度；
- 备注。

### 10.3 导出说明

记录：

- 导出任务 ID；
- 导出人；
- 导出时间；
- 导出范围；
- 筛选条件；
- IP 显示方式；
- 总记录数；
- 计价快照版本；
- HUICHUAN-AI 版本；
- 文件 SHA-256。

XLSX 采用流式写入，避免十万级导出时把整个工作簿保存在内存中。

---

## 11. CSV 输出设计

CSV 每条日志一行，动态计费组成压缩为：

```text
billing_formula
```

字段内容示例：

```text
输入 91 × $2/1M + 输出 658 × $10/1M；分组倍率 1x；最终 $0.006762
```

要求：

- 使用 UTF-8 BOM，保证中文 Excel 正常打开。
- 遵循 RFC 4180。
- 时间使用带时区的 RFC3339。
- Request ID、令牌名称等按文本导出。
- 换行、双引号、逗号必须正确转义。
- 防御 CSV Formula Injection。

对于以以下字符开头的单元格：

```text
=
+
-
@
```

导出时进行安全转义，防止 Excel 将用户内容解释为公式。

---

## 12. 同步导出与后台任务

### 12.1 默认阈值

建议默认配置：

```text
同步导出阈值：5000 条
单次最大导出行数：100000 条
导出并发数：1
导出文件保留时间：24 小时
导出查询批次：1000 条
```

单次最大导出行数允许 Root 设置为 `0`，表示不限制，但页面必须显示磁盘、MySQL 和生成时间风险。

### 12.2 后台任务

复用现有 `system_tasks`，新增任务类型：

```text
usage_log_export
```

任务阶段：

```text
pending
counting
exporting
finalizing
succeeded
failed
expired
```

任务状态包含：

- 总记录数；
- 已处理数；
- 进度；
- 当前阶段；
- 格式；
- 文件大小；
- SHA-256；
- 创建时间；
- 过期时间；
- 标准错误代码。

### 12.3 任务归属

任务必须记录：

- 创建人 ID；
- 创建人角色；
- 导出范围；
- IP 显示方式；
- 筛选条件；
- 文件格式。

查询任务和下载文件时重新校验当前用户身份。即使管理员生成文件后被降级为普通用户，也不能继续下载包含其他用户数据的文件。

### 12.4 导出文件

目录建议：

```text
logs/exports/usage-logs/<task-id>/
```

要求：

- 文件权限 `0600`。
- API 不返回绝对路径。
- 文件名不包含用户名、令牌、IP。
- 使用随机 `file_id` 下载。
- 先写 `.tmp`，完成后原子重命名。
- 失败任务删除所有半成品。
- 文件到期后自动删除。
- 下载响应使用安全的 `Content-Disposition`。

---

## 13. 导出性能保护

当前 MySQL、服务日志、数据快照和导出文件可能位于同一块磁盘，因此后台导出必须主动限制资源。

### 13.1 数据库读取

MySQL 使用游标分页：

```sql
SELECT ...
FROM logs
WHERE id > ?
  AND <filters>
ORDER BY id
LIMIT 1000;
```

禁止在大数据量导出中使用不断增大的 `OFFSET`。

### 13.2 资源限制

- XLSX、CSV 都使用流式写入。
- 默认最多一个后台导出任务执行。
- 导出使用独立的数据库连接并限制最大连接数。
- 可以配置导出读取速率。
- 定期检查剩余磁盘空间。
- 磁盘低于安全阈值时停止导出并删除临时文件。
- 导出任务不能使用 API 热路径的日志队列。
- 大文件生成不得占用无界内存。

### 13.3 对 API 的保护

必须确保：

- API 请求协程不参与文件生成。
- 导出任务不持有业务表长事务。
- 导出查询不锁定日志写入。
- XLSX 压缩并发受限。
- Go GC 压力异常时可以暂停新的后台导出任务。
- 导出失败不影响用户 API 响应和日志写入。

---

## 14. 权限与审计

新增操作标识：

```text
usage_log.ip_reveal
usage_log.export_create
usage_log.export_download
usage_log.export_failed
usage_log.ip_policy_update
```

审计记录：

- 操作人 ID；
- 操作人用户名；
- 操作人角色；
- 操作时间；
- 操作类型；
- 导出范围；
- 记录数量；
- 文件格式；
- IP 固定为完整值；
- 任务 ID；
- 文件大小；
- 成功或失败；
- 请求来源 IP。

审计中禁止保存：

- 被导出日志中的完整 IP；
- 请求提示词；
- 模型回复正文；
- 工具参数；
- 令牌密钥；
- 动态计价表达式全文；
- 文件绝对路径；
- 用户内容搜索词。

完整 IP 访问在首次进入使用日志页面时记录一次审计，不对每次分页请求重复制造大量审计日志。

---

## 15. API 设计建议

### 15.1 日志查询

```http
GET /api/log/?...
GET /api/log/self?...
```

### 15.2 创建导出

```http
POST /api/log/export
```

请求示例：

```json
{
  "format": "xlsx",
  "selection_mode": "filtered",
  "log_ids": [],
  "filters": {
    "start_timestamp": 0,
    "end_timestamp": 0,
    "type": 2,
    "model_name": "",
    "token_name": "",
    "group": "",
    "username": "",
    "channel": 0,
    "request_id": "",
    "upstream_request_id": ""
  }
}
```

后端不能信任请求中的 `username` 决定普通用户范围。普通用户必须强制追加当前认证用户 ID 条件。

### 15.3 查询导出任务

```http
GET /api/log/export/:task_id
```

### 15.4 下载文件

```http
GET /api/log/export/:task_id/download
```

下载前重新验证：

- 任务归属；
- 当前角色；
- 当前是否仍可访问任务中的日志范围；
- 文件是否过期；
- 文件哈希和大小是否有效。

---

## 16. 建议的数据结构

### 16.1 日志任务载荷

```go
type UsageLogJob struct {
    UserID            int
    Username          string
    TokenID           int
    TokenName         string
    ClientIP          string
    ModelName         string
    EffectiveModel    string
    ChannelID         int
    Group             string
    RequestID         string
    UpstreamRequestID string
    CreatedAt         int64
    LogType           int
    Usage             UsageSnapshot
    Billing           BillingSettlementSnapshot
}
```

该任务只能包含完成日志所需的不可变值，不持有 Gin Context、数据库事务、响应 Writer 或可变请求对象。

### 16.2 快照字段

```go
type BillingSnapshotV1 struct {
    Version             string
    Status              string
    Mode                string
    Source              string
    RequestedModel      string
    EffectiveModel      string
    BaseCurrency        string
    QuotaPerUnit        float64
    DisplayCurrency     string
    ExchangeRate        float64
    GroupRatio          float64
    UserGroupRatio      *float64
    Components          []BillingComponent
    PreConsumedQuota    int
    SettlementDelta     int
    FinalChargedQuota   int
    Rounding            string
    TierVersion         string
    TierHash            string
    MatchedTier         string
}
```

数值序列化时应避免二进制浮点展示误差。金额和中间结果优先使用十进制定点字符串或现有 Decimal 计算结果。

---

## 17. 失败策略

### 17.1 日志队列失败

- 不影响已成功交付的用户响应。
- 增加 `usage_log_dropped_total`。
- 记录脱敏错误类型。
- 触发聚合邮件告警。
- 不在 API 响应中暴露后台日志失败。

### 17.2 计价快照失败

- 实际额度结算结果必须保留。
- 可以写入最小消费日志，并设置：

```text
billing_snapshot_v1.status=failed
```

- 不使用当前价格伪造快照。
- 记录错误代码，不保存敏感原始错误。

### 17.3 导出失败

- 任务标记为 `failed`。
- 删除临时文件。
- 保留任务错误代码和结束时间。
- 页面允许用户按原筛选条件重新创建任务。
- 导出失败不能拖慢 API 请求。

---

## 18. 监控指标

新增或补充：

```text
usage_log_queue_depth
usage_log_queue_capacity
usage_log_jobs_submitted_total
usage_log_jobs_written_total
usage_log_jobs_dropped_total
usage_log_write_failed_total
usage_log_billing_snapshot_failed_total
usage_log_write_latency_ms
usage_log_ip_capture_enabled
usage_log_export_active
usage_log_export_rows_total
usage_log_export_failed_total
usage_log_export_bytes_total
usage_log_export_db_latency_ms
usage_log_export_disk_free_bytes
```

重点观察：

- API 首字响应 P50/P95/P99；
- 流式数据块转发 P95/P99；
- 完整响应交付 P95/P99；
- 日志队列水位；
- MySQL 日志插入延迟；
- 导出期间 API 延迟变化；
- 同盘 I/O 等待；
- Go GC 暂停时间。

---

## 19. 性能验收目标

在同环境、同请求、同上游的 A/B 测试中：

| 指标 | 目标 |
| --- | --- |
| 首 Token P95 增量 | 不超过 2 ms 或 2% |
| 流式数据块转发 P95 增量 | 不超过 1 ms |
| 最后一个数据块交付 P95 增量 | 不超过 2 ms |
| API 请求协程中的日志 MySQL 写入 | 0 次 |
| API 请求协程中的导出磁盘写入 | 0 次 |
| IP 解析 | 单次、有界、无数据库访问 |
| 日志队列满时 API 阻塞 | 0 ms |
| 后台导出并发默认值 | 1 |
| 导出期间 API P95 增量 | 不超过 5% |

性能目标不代表绝对零开销。主链路仍需要进行一次低成本 IP 读取、Usage 观察和非阻塞任务提交，但不得执行复杂快照构建、文件写入或日志数据库写入。

---

## 20. 分步实施计划

每一步完成后必须先测试，再进入下一步。

### 第一步：系统级 IP 采集

实施：

- 新增系统级 Root 开关。
- 新增可信代理配置。
- 移除个人 IP 采集开关的实际控制作用。
- 请求入口解析并标准化 IP。
- 消费日志和错误日志使用统一 IP。

测试：

- IPv4；
- IPv6；
- 直连；
- Nginx 可信代理；
- 非可信来源伪造转发头；
- 开关开启和关闭；
- 历史设置兼容；
- 请求主链路不查询用户 IP 设置。

### 第二步：异步使用日志流水线

实施：

- 定义不可变 `UsageLogJob`。
- 完整响应交付后非阻塞提交。
- 后台生成日志和计价快照。
- 队列指标、丢弃指标和告警。

测试：

- 流式最后一个数据块先于任务提交；
- 非流式响应先于任务提交；
- 队列满不阻塞；
- worker panic 不终止进程；
- MySQL 慢写不影响响应交付；
- 关闭服务时有界排空队列。

### 第三步：IP 权限和页面

实施：

- 日志 API 固定返回权限范围内的完整 IP。
- 表格和移动端增加 IP。
- 首次完整 IP 查看审计。

测试：

- Root；
- 管理员；
- 普通用户；
- 越权参数；
- 跨用户查询；
- IPv4/IPv6 完整值；
- 桌面和移动端。

### 第四步：真实计价快照

实施：

- 复用最终结算结果生成 `billing_snapshot_v1`。
- 支持所有现有计价模式。
- 详情展示真实公式。
- 历史记录兼容标记。

测试：

- 按 Token；
- 按次；
- 动态阶梯；
- 订阅；
- 免费；
- 缓存；
- 图片；
- 音频；
- Web/File Search；
- 预扣、补扣、退款；
- 舍入和额度钳制；
- 修改系统价格后历史快照不变。

### 第五步：勾选和小批量导出

实施：

- 表格复选框。
- 当前页和筛选结果选择。
- XLSX、CSV。
- IP 跟随敏感显示状态。

测试：

- 勾选单条和多条；
- 跨分页；
- 全部筛选结果；
- 普通用户范围强制限制；
- 中文和特殊字符；
- CSV 注入；
- Excel 打开验证；
- 导出数量与数据库一致。

### 第六步：后台大批量导出

实施：

- 新增 `usage_log_export` 系统任务。
- 状态和进度 API。
- 文件下载和过期清理。
- 并发、数据库和磁盘保护。

测试：

- 十万级日志；
- 任务失败；
- 服务重启；
- 文件过期；
- 权限降级；
- 磁盘不足；
- 多用户并发请求；
- 导出期间 API 压测。

### 第七步：全量验收

建议执行：

```powershell
go test ./... -count=1
go vet ./...
go test -race ./model ./service ./controller

cd web/default
bun run i18n:sync
bun run build:check
bun run build
```

补充执行：

- MySQL 集成测试；
- CSV 解析验证；
- XLSX 工作表和单元格验证；
- 浏览器桌面端验证；
- 移动端响应式验证；
- 浏览器控制台错误检查；
- 同上游 A/B 性能测试；
- 导出期间 API 并发压测。

---

## 21. 预计修改范围

后端可能涉及：

```text
common/
controller/log.go
model/log.go
model/option.go
model/system_task.go
router/api-router.go
service/log_info_generate.go
service/text_quota.go
service/system_task.go
```

建议新增：

```text
controller/usage_log_export.go
service/usage_log_export.go
service/usage_log_snapshot.go
service/usage_log_worker.go
pkg/usage_log_export/
```

前端可能涉及：

```text
web/default/src/features/usage-logs/api.ts
web/default/src/features/usage-logs/types.ts
web/default/src/features/usage-logs/data/schema.ts
web/default/src/features/usage-logs/components/usage-logs-table.tsx
web/default/src/features/usage-logs/components/common-logs-filter-bar.tsx
web/default/src/features/usage-logs/components/columns/common-logs-columns.tsx
web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx
web/default/src/features/system-settings/maintenance/log-settings-section.tsx
web/default/src/i18n/locales/
```

XLSX 建议使用：

```text
github.com/xuri/excelize/v2
```

CSV 使用 Go 标准库：

```text
encoding/csv
```

---

## 22. 最终验收标准

1. Root 可以统一开启或关闭消费日志、错误日志 IP 采集。
2. 用户无法绕过 Root 策略自行关闭系统级采集。
3. 页面直接展示权限范围内的完整 IP，不提供脱敏模式。
4. Root、管理员可以查看全部用户 IP。
5. 普通用户只能查看自己的 IP。
6. 越权 API 请求无法获得其他用户完整 IP。
7. 消费日志保存请求发生时的真实计价快照。
8. 修改当前价格不会改变历史计价展示。
9. 计价详情能够解释最终实际扣费。
10. 支持 XLSX、CSV。
11. 支持勾选导出和全部筛选结果导出。
12. 导出不受当前分页限制。
13. 普通用户无法导出其他用户日志。
14. 完整 IP 导出必须经过服务端权限校验。
15. 大批量导出转入后台任务。
16. 后台导出不会明显影响 API 首字和流式响应。
17. 队列满、MySQL 异常、磁盘不足不会反向阻塞已成功的用户响应。
18. 完整 IP 查看、导出创建和下载都有审计记录。
19. 导出临时文件具有 `0600` 权限并按期删除。
20. 所有后端测试、前端构建、文件解析和端到端验证通过。

---

## 23. 最终结论

本方案的核心不是简单地在使用日志中增加几个字段，而是建立一套：

```text
响应优先
+ 系统级 IP 策略
+ 服务端敏感数据控制
+ 不可变真实计价快照
+ 可审计的 XLSX/CSV 导出
+ 后台资源隔离
```

的完整使用日志体系。

所有新增持久化、计价快照构建和导出工作都必须位于用户响应交付之后。只要严格执行非阻塞队列、游标读取、流式文件写入、导出并发限制和同盘 I/O 保护，就可以在增加审计与导出能力的同时，将对用户 API 对话速度的影响控制到最低。
