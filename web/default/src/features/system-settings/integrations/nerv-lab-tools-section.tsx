/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import { getNERVSelfCheck, getNERVTools, runNERVTool } from '../api'
import { SettingsSection } from '../components/settings-section'
import type {
  NERVSelfCheckData,
  NERVMemoryKernel,
  NERVToolBackend,
  NERVToolCatalogItem,
  NERVToolRunResult,
} from '../types'
import {
  nervSkillGroupOrder,
  nervSkills,
  nervToolCategories,
  type NERVSkillItem,
  type NERVToolCategory,
} from './nerv-catalog'

type NERVLabRecentEvent = {
  ts: number
  event: string
  target: string
  model: string
}

type NERVLabDefaults = {
  'nerv_setting.mcp_backend': 'auto' | 'local' | 'wsl' | 'docker' | 'ssh'
  'nerv_setting.wsl_distro': string
  'nerv_setting.docker_container': string
  'nerv_setting.ssh_host': string
  'nerv_stats.recent': string
}

type NERVLabToolsSectionProps = {
  defaultValues: NERVLabDefaults
}

const eventLabels: Record<string, string> = {
  inject_chat: '对话注入',
  inject_responses: '响应接口注入',
  tamper_text: '文本篡改',
  tamper_chat: '对话篡改',
  tamper_responses: '响应接口篡改',
}

const backendLabels: Record<NERVLabDefaults['nerv_setting.mcp_backend'], string> = {
  auto: '自动选择',
  local: '本机工具',
  wsl: 'Windows 子系统（WSL）',
  docker: '容器后端（当前不用，仅兼容原项目）',
  ssh: '远程主机',
}

const memoryCategoryLabels: Record<string, string> = {
  total: '总学习',
  crack: '破解',
  reverse: '逆向',
  pentest: '渗透',
  tamper: '篡改',
  general: '通用',
}


const bundledNERVAssetPath = '/opt/new-api/build/HUICHUAN-AI/nerv/5.6-JAILBREAK-NERV'
const sourceNERVAssetPath = 'nerv/5.6-JAILBREAK-NERV'

const integrationChecklist = [
  {
    source: '原项目 bridge.md',
    status: '已接入',
    entry: 'NERV 连接设置 / 桥接提示词',
    evidence: 'service/nerv_bridge.go',
  },
  {
    source: 'proxy_relay.py 注入与篡改',
    status: '链路已融合，脚本已内置',
    entry: '聊天补全、响应接口、Codex 响应接口；原脚本位于内置资产包',
    evidence: 'service/nerv_bridge.go / nerv/5.6-JAILBREAK-NERV/proxy_relay.py',
  },
  {
    source: 'memory.json',
    status: '已融合到数据库选项',
    entry: 'NERV 记忆内核 / 最近事件',
    evidence: 'nerv_stats.*',
  },
  {
    source: '原项目工具清单 tools/tools.json',
    status: '已目录化，原文件已内置',
    entry: 'NERV 工程工具 / 工具目录',
    evidence: 'nerv-catalog.ts + nerv/5.6-JAILBREAK-NERV/tools/tools.json',
  },
  {
    source: '原项目技能目录 skills/*',
    status: '已目录化，原文件已内置',
    entry: 'NERV 工程工具 / 技能目录',
    evidence: 'nerv-catalog.ts + nerv/5.6-JAILBREAK-NERV/skills/*',
  },
  {
    source: 'deploy.py / verify.py / direct_setup.py',
    status: '脚本已内置，命令已接入',
    entry: 'NERV 工程工具 / 快捷命令',
    evidence: 'nerv/5.6-JAILBREAK-NERV/deploy.py / verify.py / direct_setup.py',
  },
  {
    source: 'mcp_server.py',
    status: '脚本已内置，配置已接入',
    entry: 'NERV 连接设置 / 工具后端（MCP）',
    evidence: 'nerv/5.6-JAILBREAK-NERV/mcp_server.py + auto/local/WSL/远程主机',
  },
]

const workflowCommands = [
  {
    title: '一键实验室菜单',
    description: '对应原项目 scripts/lab.bat 的本机菜单入口。',
    commands: [
      'scripts\\lab.bat start',
      'scripts\\lab.bat status',
      'scripts\\lab.bat stop',
      'scripts\\lab.bat tools-check',
    ],
  },
  {
    title: '桥接部署',
    description: '把桥接文件和技能目录同步到本机 Codex 目录。',
    commands: [
      'python deploy.py apply',
      'python deploy.py status',
      'python deploy.py remove',
    ],
  },
  {
    title: '配置验证',
    description: '检查桥接文件、技能目录和 Codex 配置是否生效。',
    commands: ['python verify.py'],
  },
  {
    title: '直连模式',
    description: '不经过代理，直接把 Codex 指向 OpenAI 接口。',
    commands: [
      'python direct_setup.py apply',
      'python direct_setup.py proxy',
      'python direct_setup.py remove',
    ],
  },
  {
    title: '代理模式',
    description: '自动注入、自动连接并提供本地代理与面板。',
    commands: ['python proxy_relay.py', '重启 Codex 命令行（Codex CLI）后输入 zxwn'],
  },
]

function formatTime(value?: number) {
  if (!value) return '暂无'
  return new Date(value * 1000).toLocaleString()
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }
  return `${size.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
}

function topMemoryEntries(values?: Record<string, number>, limit = 8) {
  return Object.entries(values ?? {})
    .filter(([, count]) => count > 0)
    .sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0]))
    .slice(0, limit)
}

function quoteTomlString(value: string) {
  return `"${value.replaceAll('\\', '\\\\').replaceAll('"', '\\"')}"`
}

function buildMCPConfigSnippet({
  assetPath,
  backend,
  wslDistro,
  dockerContainer,
  sshHost,
}: {
  assetPath: string
  backend: NERVLabDefaults['nerv_setting.mcp_backend']
  wslDistro: string
  dockerContainer: string
  sshHost: string
}) {
  const scriptPath = `${assetPath.replace(/[\\/]+$/, '')}/mcp_server.py`
  const args = [scriptPath]
  if (wslDistro) {
    args.push('--wsl-distro', wslDistro)
  }
  if (backend === 'auto') {
    args.push('--auto')
  } else if (backend === 'wsl') {
    args.push('--wsl')
  } else if (backend === 'docker') {
    args.push('--docker', dockerContainer || 'kali-tools')
  } else if (backend === 'ssh') {
    args.push('--kali', sshHost || 'root@192.168.1.100')
  }

  return [
    '[mcp_servers.nerv_break]',
    'command = "python"',
    `args = [${args.map(quoteTomlString).join(', ')}]`,
  ].join('\n')
}

function toolAvailabilityLabel(item: { checkable: boolean; available: boolean }) {
  if (item.available) return '可用'
  if (item.checkable) return '缺失'
  return '需参数'
}

function parseRecentEvents(raw: string): NERVLabRecentEvent[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item): item is NERVLabRecentEvent => {
      return (
        item &&
        typeof item === 'object' &&
        typeof item.ts === 'number' &&
        typeof item.event === 'string'
      )
    })
  } catch {
    return []
  }
}

function CommandRow({ command }: { command: string }) {
  return (
    <div className='flex items-center justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2 font-mono text-xs'>
      <span className='break-all'>{command}</span>
      <CopyButton
        value={command}
        tooltip='复制命令'
        successTooltip='已复制命令'
        className='shrink-0'
      />
    </div>
  )
}

function CommandCard({
  title,
  description,
  commands,
}: {
  title: string
  description: string
  commands: string[]
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className='space-y-2'>
        {commands.map((command) => (
          <CommandRow key={command} command={command} />
        ))}
      </CardContent>
    </Card>
  )
}

function OverviewCard({
  label,
  value,
  description,
}: {
  label: string
  value: string | number
  description: string
}) {
  return (
    <Card>
      <CardHeader className='pb-2'>
        <CardDescription>{label}</CardDescription>
        <CardTitle className='text-2xl'>{value}</CardTitle>
      </CardHeader>
      <CardContent className='text-sm text-muted-foreground'>
        {description}
      </CardContent>
    </Card>
  )
}

function SelfCheckCard({
  data,
  isLoading,
  isError,
  errorMessage,
  isRefreshing,
  onRefresh,
}: {
  data?: NERVSelfCheckData
  isLoading: boolean
  isError: boolean
  errorMessage?: string
  isRefreshing: boolean
  onRefresh: () => void
}) {
  const okCount = data?.checks.filter((item) => item.ok).length ?? 0
  const totalCount = data?.checks.length ?? 0
  let content = (
    <div className='rounded-md border bg-muted/20 p-3 text-sm text-muted-foreground'>
      暂无自检数据。
    </div>
  )

  if (isLoading) {
    content = (
      <div className='rounded-md border bg-muted/20 p-3 text-sm text-muted-foreground'>
        正在读取服务器自检结果...
      </div>
    )
  } else if (isError) {
    content = (
      <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
        自检接口读取失败：{errorMessage || '请稍后重试'}
      </div>
    )
  } else if (data) {
    content = (
      <>
        <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
          <OverviewCard
            label='自检通过'
            value={`${okCount}/${totalCount}`}
            description='通过项越多，说明融合状态越完整。'
          />
          <OverviewCard
            label='服务器资产'
            value={data.assets.exists ? '已找到' : '未找到'}
            description={data.assets.base_path || '暂无路径'}
          />
          <OverviewCard
            label='资产文件'
            value={data.assets.file_count}
            description={`总大小 ${formatBytes(data.assets.total_size_bytes)}`}
          />
          <OverviewCard
            label='工具 / 技能'
            value={`${data.catalog.tool_count}/${data.catalog.skill_count}`}
            description={`${data.catalog.category_count} 个工具类别，${data.catalog.skill_dir_count} 个技能目录。`}
          />
          <OverviewCard
            label='本机可用工具'
            value={`${data.catalog.tool_available}/${data.catalog.tool_count}`}
            description={`缺失 ${data.catalog.tool_missing} 个，需参数 ${data.catalog.tool_uncheckable} 个；可通过 WSL 或远程主机补足。`}
          />
          <OverviewCard
            label='篡改规则'
            value={
              data.config.tamper_rule_invalid > 0
                ? `${data.config.tamper_rule_invalid} 个异常`
                : '格式正常'
            }
            description={`${data.config.tamper_rule_count} 条规则已加载；原项目 ${data.config.bundled_rule_source || 'direct_setup.py'} 可导入 ${data.config.bundled_rule_count ?? 0} 条。`}
          />
        </div>

        <div className='grid gap-4 lg:grid-cols-3'>
          <div className='space-y-2'>
            <div className='text-sm font-semibold'>检查项</div>
            <div className='grid gap-2'>
              {data.checks.map((item) => (
                <div
                  key={item.key}
                  className='flex items-center justify-between gap-3 rounded-md border bg-muted/10 px-3 py-2 text-sm'
                >
                  <span>{item.message}</span>
                  <Badge variant={item.ok ? 'secondary' : 'destructive'}>
                    {item.ok ? '通过' : '异常'}
                  </Badge>
                </div>
              ))}
              {data.config.tamper_rule_errors?.map((item) => (
                <div
                  key={`${item.line}-${item.pattern}`}
                  className='rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-xs text-destructive'
                >
                  <div className='font-medium'>第 {item.line} 条规则异常</div>
                  <div className='mt-1 break-all font-mono'>{item.pattern}</div>
                  <div className='mt-1 break-all'>{item.error}</div>
                </div>
              ))}
            </div>
          </div>

          <div className='space-y-2'>
            <div className='text-sm font-semibold'>核心文件</div>
            <div className='grid gap-2'>
              {data.assets.required_files.map((item) => (
                <div
                  key={item.path}
                  className='flex items-center justify-between gap-3 rounded-md border bg-muted/10 px-3 py-2 text-sm'
                >
                  <span className='break-all font-mono text-xs'>
                    {item.path}
                  </span>
                  <Badge variant={item.exists ? 'outline' : 'destructive'}>
                    {item.exists ? formatBytes(item.size) : '缺失'}
                  </Badge>
                </div>
              ))}
            </div>
          </div>

          <div className='space-y-2'>
            <div className='text-sm font-semibold'>工具可用性</div>
            <div className='grid gap-2'>
              {(data.catalog.tool_availability ?? [])
                .slice(0, 12)
                .map((item) => (
                <div
                  key={item.name}
                  className='flex items-center justify-between gap-3 rounded-md border bg-muted/10 px-3 py-2 text-sm'
                >
                  <span className='min-w-0'>
                    <span className='block truncate font-medium'>
                      {item.name}
                    </span>
                    <span className='font-mono text-xs text-muted-foreground'>
                      {item.binary || item.category}
                    </span>
                  </span>
                  <Badge variant={item.available ? 'outline' : 'destructive'}>
                    {toolAvailabilityLabel(item)}
                  </Badge>
                </div>
              ))}
            </div>
          </div>
        </div>
      </>
    )
  }

  return (
    <Card>
      <CardHeader>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>服务器自检</CardTitle>
            <CardDescription>
              直接读取服务器运行环境，检查 NERV 资产、工具目录、技能目录和中转站配置。
            </CardDescription>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={isLoading || isRefreshing}
            onClick={onRefresh}
          >
            {isRefreshing ? '检测中...' : '重新检测'}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>{content}</CardContent>
    </Card>
  )
}

function ToolCategoryCard({ category }: { category: NERVToolCategory }) {
  return (
    <Card>
      <CardHeader>
        <div className='flex flex-wrap items-center gap-2'>
          <CardTitle>{category.label}</CardTitle>
          <Badge variant='secondary'>{category.tools.length} 个工具</Badge>
          <Badge variant='outline'>{category.key}</Badge>
        </div>
        <CardDescription>{category.description}</CardDescription>
      </CardHeader>
      <CardContent className='space-y-3'>
        {category.tools.map((tool) => (
          <div key={tool.name} className='rounded-md border bg-muted/10 p-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div>
                <div className='font-medium'>{tool.description}</div>
                <div className='font-mono text-xs text-muted-foreground'>
                  {tool.name}
                </div>
              </div>
              {tool.params.length > 0 ? (
                <div className='flex flex-wrap gap-1'>
                  {tool.params.map((param) => (
                    <Badge key={param} variant='outline'>
                      {param}
                    </Badge>
                  ))}
                </div>
              ) : null}
            </div>
            <div className='mt-2 flex items-center justify-between gap-2 rounded bg-background px-2 py-1.5 font-mono text-xs'>
              <span className='break-all'>{tool.commandTemplate}</span>
              <CopyButton
                value={tool.commandTemplate}
                tooltip='复制模板'
                successTooltip='已复制模板'
                className='shrink-0'
              />
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function SkillGroupCard({
  group,
  skills,
}: {
  group: string
  skills: NERVSkillItem[]
}) {
  return (
    <Card>
      <CardHeader>
        <div className='flex flex-wrap items-center gap-2'>
          <CardTitle>{group}</CardTitle>
          <Badge variant='secondary'>{skills.length} 个技能</Badge>
        </div>
      </CardHeader>
      <CardContent className='grid gap-3 md:grid-cols-2'>
        {skills.map((skill) => (
          <div key={skill.id} className='rounded-md border bg-muted/10 p-3'>
            <div className='flex flex-wrap items-center gap-2'>
              <div className='font-medium'>{skill.title}</div>
              <Badge variant='outline'>{skill.id}</Badge>
            </div>
            <div className='mt-1 text-sm text-muted-foreground'>
              {skill.summary}
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}

function RecentEventsCard({ recentEvents }: { recentEvents: NERVLabRecentEvent[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>最近事件</CardTitle>
        <CardDescription>从中转站的 NERV 记忆缓存里读取最近 8 条。</CardDescription>
      </CardHeader>
      <CardContent className='space-y-2'>
        {recentEvents.length > 0 ? (
          recentEvents.slice(0, 8).map((item) => (
            <div
              key={`${item.ts}-${item.event}-${item.target}-${item.model}`}
              className='rounded-md border bg-muted/20 p-3 text-sm'
            >
              <div className='flex items-center justify-between gap-2'>
                <span className='font-medium'>
                  {eventLabels[item.event] ?? item.event}
                </span>
                <span className='text-xs text-muted-foreground'>
                  {formatTime(item.ts)}
                </span>
              </div>
              <div className='mt-1 text-xs text-muted-foreground'>
                范围：{item.target || '暂无'} · 模型：{item.model || '暂无'}
              </div>
            </div>
          ))
        ) : (
          <div className='rounded-md border bg-muted/20 p-3 text-sm text-muted-foreground'>
            暂无最近事件记录。
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function MemoryKernelCard({ memory }: { memory?: NERVMemoryKernel }) {
  const stats = memory?.stats ?? {}
  const topPatterns = topMemoryEntries(memory?.patterns, 10)
  const topTechniques = topMemoryEntries(memory?.techniques, 6)
  const successes = memory?.successes ?? []

  return (
    <Card>
      <CardHeader>
        <CardTitle>NERV 记忆学习内核</CardTitle>
        <CardDescription>
          对应原项目 memory.json：记录分类统计、关键词模式、技术路径和最近学习样本。
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
          {['total', 'pentest', 'reverse', 'crack', 'tamper', 'general'].map(
            (key) => (
              <div key={key} className='rounded-md border bg-muted/10 p-3'>
                <div className='text-xs text-muted-foreground'>
                  {memoryCategoryLabels[key] ?? key}
                </div>
                <div className='mt-1 text-2xl font-semibold'>
                  {stats[key] ?? 0}
                </div>
              </div>
            )
          )}
        </div>

        <div className='grid gap-4 lg:grid-cols-2'>
          <div className='space-y-2'>
            <div className='text-sm font-semibold'>高频关键词</div>
            <div className='flex flex-wrap gap-2'>
              {topPatterns.length > 0 ? (
                topPatterns.map(([key, count]) => (
                  <Badge key={key} variant='outline'>
                    {key} × {count}
                  </Badge>
                ))
              ) : (
                <span className='text-sm text-muted-foreground'>暂无</span>
              )}
            </div>
          </div>
          <div className='space-y-2'>
            <div className='text-sm font-semibold'>技术路径</div>
            <div className='flex flex-wrap gap-2'>
              {topTechniques.length > 0 ? (
                topTechniques.map(([key, count]) => (
                  <Badge key={key} variant='secondary'>
                    {key} × {count}
                  </Badge>
                ))
              ) : (
                <span className='text-sm text-muted-foreground'>暂无</span>
              )}
            </div>
          </div>
        </div>

        <div className='space-y-2'>
          <div className='text-sm font-semibold'>最近学习样本</div>
          {successes.length > 0 ? (
            successes
              .slice(-8)
              .reverse()
              .map((item) => (
                <div
                  key={`${item.hash}-${item.ts}`}
                  className='rounded-md border bg-muted/10 p-3 text-sm'
                >
                  <div className='flex flex-wrap items-center gap-2'>
                    <Badge variant='outline'>
                      {memoryCategoryLabels[item.category] ?? item.category}
                    </Badge>
                    <span className='text-xs text-muted-foreground'>
                      {item.technique || item.event} · {item.model || '暂无模型'}
                    </span>
                  </div>
                  <div className='mt-2 line-clamp-2 text-muted-foreground'>
                    {item.user || item.result || '暂无摘要'}
                  </div>
                </div>
              ))
          ) : (
            <div className='rounded-md border bg-muted/20 p-3 text-sm text-muted-foreground'>
              暂无学习样本。启用 NERV 后，请求注入和篡改命中会自动写入这里。
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function buildToolOutput(result: NERVToolRunResult) {
  return [
    `退出码：${result.exit_code}`,
    `耗时：${result.duration_ms} ms`,
    `超时：${result.timed_out ? '是' : '否'}`,
    '',
    '--- 标准输出 ---',
    result.stdout || '（空）',
    '',
    '--- 错误输出 ---',
    result.stderr || '（空）',
  ].join('\n')
}

function ToolExecutionPanel({
  defaultBackend,
}: {
  defaultBackend: NERVToolBackend
}) {
  const [selectedName, setSelectedName] = useState('')
  const [args, setArgs] = useState<Record<string, string>>({})
  const [backend, setBackend] = useState<NERVToolBackend>(defaultBackend)
  const [timeoutSeconds, setTimeoutSeconds] = useState('30')
  const [lastResult, setLastResult] = useState<NERVToolRunResult | null>(null)

  const toolsQuery = useQuery({
    queryKey: ['nerv-tools'],
    queryFn: getNERVTools,
    staleTime: 30 * 1000,
  })

  const tools = useMemo(
    () => (toolsQuery.data?.success ? toolsQuery.data.data?.tools ?? [] : []),
    [toolsQuery.data]
  )
  const selectedTool = useMemo<NERVToolCatalogItem | undefined>(() => {
    if (selectedName) {
      return tools.find((tool) => tool.name === selectedName)
    }
    return tools[0]
  }, [selectedName, tools])

  const runMutation = useMutation({
    mutationFn: runNERVTool,
    onSuccess: (response) => {
      if (response.success && response.data) {
        setLastResult(response.data)
      }
    },
  })

  const runSelectedTool = () => {
    if (!selectedTool) return
    const normalizedArgs = Object.fromEntries(
      selectedTool.params.map((param) => [param, args[param] ?? ''])
    )
    const parsedTimeout = Number.parseInt(timeoutSeconds, 10)
    runMutation.mutate({
      name: selectedTool.name,
      args: normalizedArgs,
      backend,
      timeout_seconds: Number.isFinite(parsedTimeout) ? parsedTimeout : 30,
    })
  }

  return (
    <Card>
      <CardHeader>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>NERV 工具执行</CardTitle>
            <CardDescription>
              只允许执行原项目工具目录里的命令模板；后台接口仅 Root 可用，并限制超时和输出长度。
            </CardDescription>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => {
              void toolsQuery.refetch()
            }}
            disabled={toolsQuery.isFetching}
          >
            {toolsQuery.isFetching ? '刷新中...' : '刷新工具'}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        {toolsQuery.isError || toolsQuery.data?.success === false ? (
          <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
            工具目录读取失败：{toolsQuery.data?.message || '请稍后重试'}
          </div>
        ) : null}

        <div className='grid gap-4 lg:grid-cols-3'>
          <div className='space-y-2'>
            <div className='text-sm font-medium'>选择工具</div>
            <NativeSelect
              value={selectedTool?.name ?? ''}
              disabled={tools.length === 0}
              onChange={(event) => {
                setSelectedName(event.target.value)
                setArgs({})
                setLastResult(null)
              }}
            >
              {tools.map((tool) => (
                <NativeSelectOption key={tool.name} value={tool.name}>
                  {tool.name}（{tool.category}）
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>

          <div className='space-y-2'>
            <div className='text-sm font-medium'>执行后端</div>
            <NativeSelect
              value={backend}
              onChange={(event) =>
                setBackend(event.target.value as NERVToolBackend)
              }
            >
              <NativeSelectOption value='auto'>自动选择</NativeSelectOption>
              <NativeSelectOption value='local'>本机工具</NativeSelectOption>
              <NativeSelectOption value='wsl'>
                Windows 子系统（WSL）
              </NativeSelectOption>
              <NativeSelectOption value='ssh'>远程主机</NativeSelectOption>
              <NativeSelectOption value='docker'>
                容器后端（当前服务器不使用）
              </NativeSelectOption>
            </NativeSelect>
          </div>

          <div className='space-y-2'>
            <div className='text-sm font-medium'>超时秒数</div>
            <Input
              value={timeoutSeconds}
              inputMode='numeric'
              onChange={(event) => setTimeoutSeconds(event.target.value)}
              placeholder='30'
            />
          </div>
        </div>

        {selectedTool ? (
          <div className='space-y-3 rounded-md border bg-muted/10 p-3'>
            <div className='flex flex-wrap items-center gap-2'>
              <Badge variant={selectedTool.available ? 'outline' : 'secondary'}>
                {selectedTool.available ? '本机可用' : '可能需后端环境'}
              </Badge>
              <Badge variant='outline'>{selectedTool.category}</Badge>
              <span className='text-sm text-muted-foreground'>
                {selectedTool.description}
              </span>
            </div>
            <div className='flex items-center justify-between gap-3 rounded bg-background px-3 py-2 font-mono text-xs'>
              <span className='break-all'>{selectedTool.command}</span>
              <CopyButton
                value={selectedTool.command}
                tooltip='复制模板'
                successTooltip='已复制模板'
                className='shrink-0'
              />
            </div>

            <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-3'>
              {selectedTool.params.map((param) => (
                <div key={param} className='space-y-1.5'>
                  <div className='text-xs font-medium'>{param}</div>
                  <Input
                    value={args[param] ?? ''}
                    onChange={(event) =>
                      setArgs((current) => ({
                        ...current,
                        [param]: event.target.value,
                      }))
                    }
                    placeholder='可留空'
                    autoComplete='off'
                  />
                </div>
              ))}
            </div>

            <Button
              type='button'
              onClick={runSelectedTool}
              disabled={runMutation.isPending}
            >
              {runMutation.isPending ? '执行中...' : '执行工具模板'}
            </Button>
          </div>
        ) : (
          <div className='rounded-md border bg-muted/20 p-3 text-sm text-muted-foreground'>
            正在读取工具目录...
          </div>
        )}

        {runMutation.data?.success === false ? (
          <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
            执行失败：{runMutation.data.message || '请检查参数和后端环境'}
          </div>
        ) : null}

        {lastResult ? (
          <div className='space-y-2 rounded-md border p-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='text-sm font-semibold'>执行结果</div>
              <div className='flex gap-2'>
                <CopyButton
                  value={lastResult.command}
                  tooltip='复制实际命令'
                  successTooltip='已复制命令'
                />
                <CopyButton
                  value={buildToolOutput(lastResult)}
                  tooltip='复制输出'
                  successTooltip='已复制输出'
                />
              </div>
            </div>
            <div className='rounded bg-muted/20 px-3 py-2 font-mono text-xs'>
              {lastResult.command}
            </div>
            <Textarea
              readOnly
              className='min-h-64 font-mono text-xs'
              value={buildToolOutput(lastResult)}
            />
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

export function NERVLabToolsSection({
  defaultValues,
}: NERVLabToolsSectionProps) {
  const selfCheckQuery = useQuery({
    queryKey: ['nerv-self-check'],
    queryFn: getNERVSelfCheck,
    staleTime: 30 * 1000,
  })
  const recentRaw = defaultValues['nerv_stats.recent']
  const recentEvents = useMemo(() => parseRecentEvents(recentRaw), [recentRaw])

  const skillGroups = useMemo(() => {
    const grouped = new Map<string, NERVSkillItem[]>()
    for (const skill of nervSkills) {
      grouped.set(skill.group, [...(grouped.get(skill.group) ?? []), skill])
    }
    return [...grouped.entries()].sort(([left], [right]) => {
      const leftIndex = nervSkillGroupOrder.indexOf(left)
      const rightIndex = nervSkillGroupOrder.indexOf(right)
      return (
        (leftIndex === -1 ? 999 : leftIndex) -
        (rightIndex === -1 ? 999 : rightIndex)
      )
    })
  }, [])

  const toolCount = nervToolCategories.reduce(
    (total, category) => total + category.tools.length,
    0
  )
  const selfCheckData = selfCheckQuery.data?.success
    ? selfCheckQuery.data.data
    : undefined
  const selfCheckError =
    selfCheckQuery.isError || selfCheckQuery.data?.success === false
  const backend = defaultValues['nerv_setting.mcp_backend'] ?? 'auto'
  const runtimeAssetPath = selfCheckData?.assets.exists
    ? selfCheckData.assets.base_path
    : bundledNERVAssetPath
  const mcpConfigSnippet = buildMCPConfigSnippet({
    assetPath: runtimeAssetPath,
    backend,
    wslDistro: defaultValues['nerv_setting.wsl_distro'],
    dockerContainer: defaultValues['nerv_setting.docker_container'],
    sshHost: defaultValues['nerv_setting.ssh_host'],
  })
  const mcpCommands = [
    'python mcp_server.py',
    'python mcp_server.py --auto',
    `python mcp_server.py --wsl --wsl-distro ${defaultValues['nerv_setting.wsl_distro'] || 'kali-linux'}`,
    `python mcp_server.py --kali ${defaultValues['nerv_setting.ssh_host'] || 'root@192.168.1.100'}`,
    'python mcp_server.py --port 9000',
  ]

  return (
    <SettingsSection title='NERV 工程工具'>
      <Alert>
        <AlertDescription>
          这里把 NERV 原项目的部署脚本、模型上下文协议（MCP）后端、工具库和技能库整理成中转站后台目录。
          页面里的英文主要是命令、接口标识、文件路径和工具名，属于复制给程序使用的技术值，
          不能翻译；当前服务器部署不使用容器。
        </AlertDescription>
      </Alert>

      <Tabs defaultValue='overview' className='mt-4'>
        <TabsList className='grid w-full grid-cols-4'>
          <TabsTrigger value='overview'>总览</TabsTrigger>
          <TabsTrigger value='tools'>工具目录</TabsTrigger>
          <TabsTrigger value='skills'>技能目录</TabsTrigger>
          <TabsTrigger value='commands'>快捷命令</TabsTrigger>
        </TabsList>

        <TabsContent value='overview' className='mt-4 space-y-4'>
          <SelfCheckCard
            data={selfCheckData}
            isLoading={selfCheckQuery.isLoading}
            isError={selfCheckError}
            errorMessage={selfCheckQuery.data?.message}
            isRefreshing={selfCheckQuery.isFetching}
            onRefresh={() => {
              void selfCheckQuery.refetch()
            }}
          />

          <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
            <OverviewCard
              label='工具类别'
              value={nervToolCategories.length}
              description='来自原项目 tools/tools.json。'
            />
            <OverviewCard
              label='工具模板'
              value={toolCount}
              description='按类别展示命令模板和参数。'
            />
            <OverviewCard
              label='技能目录'
              value={nervSkills.length}
              description='来自原项目 skills 目录。'
            />
            <OverviewCard
              label='当前后端'
              value={backendLabels[backend] ?? backend}
              description='在 NERV 连接设置里切换工具后端（MCP）。'
            />
            <OverviewCard
              label='内置资产'
              value='已随源码部署'
              description={runtimeAssetPath}
            />
          </div>

          <div className='grid gap-4 lg:grid-cols-2'>
            <Card>
              <CardHeader>
                <CardTitle>模式映射</CardTitle>
                <CardDescription>
                  原项目功能与当前中转站的结合位置。
                </CardDescription>
              </CardHeader>
              <CardContent className='space-y-2 text-sm'>
                {integrationChecklist.map((item) => (
                  <div key={item.entry} className='rounded-md border p-3'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='font-medium'>{item.entry}</span>
                      <Badge variant='secondary'>{item.status}</Badge>
                    </div>
                    <div className='mt-1 text-muted-foreground'>
                      来源：{item.source}
                    </div>
                    <div className='mt-1 font-mono text-xs text-muted-foreground'>
                      代码位置：{item.evidence}
                    </div>
                  </div>
                ))}
              </CardContent>
            </Card>

            <div className='space-y-4'>
              <MemoryKernelCard memory={selfCheckData?.memory} />
              <RecentEventsCard recentEvents={recentEvents} />
            </div>
          </div>
        </TabsContent>

        <TabsContent value='tools' className='mt-4 space-y-4'>
          <ToolExecutionPanel defaultBackend={backend} />
          <div className='grid gap-4 xl:grid-cols-2'>
            {nervToolCategories.map((category) => (
              <ToolCategoryCard key={category.key} category={category} />
            ))}
          </div>
        </TabsContent>

        <TabsContent value='skills' className='mt-4 space-y-4'>
          {skillGroups.map(([group, skills]) => (
            <SkillGroupCard key={group} group={group} skills={skills} />
          ))}
        </TabsContent>

        <TabsContent value='commands' className='mt-4 space-y-4'>
          <div className='grid gap-4 lg:grid-cols-2'>
            <CommandCard
              title='内置资产路径'
              description='原 NERV 项目文件已随 HUICHUAN 源码部署到服务器构建目录。'
              commands={[
                `cd ${runtimeAssetPath}`,
                `ls ${runtimeAssetPath}`,
                `cd ${sourceNERVAssetPath}`,
              ]}
            />
            <CommandCard
              title='Codex 模型上下文协议（MCP）配置片段'
              description='对应原项目 config/mcp_config.txt，可复制到 Codex 配置里的 mcp_servers 区域。'
              commands={[mcpConfigSnippet]}
            />
            {workflowCommands.map((item) => (
              <CommandCard
                key={item.title}
                title={item.title}
                description={item.description}
                commands={item.commands}
              />
            ))}
            <CommandCard
              title='工具后端（MCP）'
              description={`对应工具后端配置：WSL=${defaultValues['nerv_setting.wsl_distro'] || 'kali-linux'}；如需远程工具主机，可在 NERV 连接设置里填写远程地址。当前服务器不使用容器。`}
              commands={mcpCommands}
            />
          </div>
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}
