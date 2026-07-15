# ADR: Windows 独立部署在线升级

## 状态

已接受，首版实现范围为 Windows amd64 独立可执行文件。

## 背景

系统设置中的版本检查原本由浏览器直接请求 GitHub Release，只能打开发布页，无法校验、替换和重启当前服务。Windows 不允许正在运行的可执行文件覆盖自身，因此在线升级必须由当前进程之外的辅助进程完成。

## 领域模型

- **更新来源**：固定为 `01121531/HUICHUAN-AI` 的 GitHub Release，不接受客户端提供下载 URL。
- **发布版本**：使用 `vMAJOR.MINOR.PATCH` 标签。非 SemVer Release 可以查看，但不可在线安装。
- **更新任务**：一次从下载、校验、暂存、重启、验证到成功或回滚的完整操作。
- **更新计划**：只保存在服务器本地，包含当前可执行文件、暂存文件、备份文件和重启参数。
- **更新状态**：不包含凭据和绝对路径，供 Root 用户在重启前后查询。

## 决策

1. 在线升级 API 仅允许 Root 调用，前端按钮可见性不代替后端鉴权。
2. 首版仅支持 Windows amd64 单节点独立 EXE；Docker、Kubernetes、`go run`、只读目录和其他平台返回明确的不支持原因。
3. 只能安装固定仓库的最新正式 Release，客户端只提交 `release_id`，服务端必须重新获取并绑定该 Release。
4. Release 必须包含匹配平台的 `.exe` 和 `checksums-windows.txt`。下载有大小上限，SHA-256 不匹配时禁止进入替换阶段。
5. 同一进程同一时间只能存在一个更新任务。更新采用以下状态机：

   `idle -> downloading -> verifying -> staged -> restarting -> validating -> succeeded`

   替换后健康检查失败时：

   `validating -> rolling_back -> rolled_back`

   替换前失败时直接进入 `failed`。
6. 当前进程将自身复制为临时辅助程序。辅助程序等待当前 PID 退出后，备份旧 EXE、原子替换、按原工作目录和参数启动新版本，并轮询 `/api/status`。新版本未在超时内返回目标版本时终止新进程并恢复旧版本。
7. 状态使用可执行文件相邻的 `.huichuan-update/state.json` 跨进程保存；写入使用临时文件加原子重命名。API 不返回文件路径、下载 URL、令牌或请求头。
8. 升级只能由 Root 手动点击并二次确认，不自动安装。更新请求写入管理审计；异步阶段写服务器日志和持久化状态。
9. 发布工作流必须使用完整 Go 包路径注入 `common.Version`，并只为 SemVer 标签生成可在线安装的资产。

## 不变量与执行边界

| 不变量 | 执行边界 |
| --- | --- |
| 非 Root 不能查询或启动在线升级 | `middleware.RootAuth()` |
| 客户端不能控制下载地址或本地路径 | controller + `pkg/systemupdate` |
| 校验失败绝不替换当前 EXE | `pkg/systemupdate` 下载/校验阶段 |
| 替换前必须保留可恢复备份 | Windows helper |
| 新版本健康检查失败必须尝试回滚 | Windows helper |
| 容器和不支持平台不能出现可点击安装按钮 | capability API + frontend |
| 并发更新只能成功创建一个任务 | manager mutex + 本地状态 |

## API

- `GET /api/system-update/capability`
- `GET /api/system-update/latest`
- `GET /api/system-update/status`
- `POST /api/system-update`，请求体：`{"release_id": 123}`

## 后果

- Windows 独立部署可以在网页端完成带校验、重启验证和失败回滚的升级。
- 容器和多节点部署仍应由 Compose、Kubernetes 或外部发布系统滚动升级，避免应用修改自身镜像或同时更新错误节点。
- 第一个可在线安装的版本必须以新的 SemVer 标签重新发布；历史 `api` 标签只作为发布记录展示。

## 验收标准

1. Root 可以检查最新版本、确认安装并观察下载到重连的完整状态。
2. 非 Root 请求四个 API 均被拒绝。
3. 非 SemVer、缺少资产、超限或 SHA-256 不匹配均不会替换文件。
4. 辅助程序在隔离目录中可成功替换并启动健康版本；健康失败可恢复旧文件。
5. 发布二进制的 `/api/status` 版本与 Release 标签一致。
6. 前端刷新或服务重启后仍可恢复并显示最后一次更新状态。
