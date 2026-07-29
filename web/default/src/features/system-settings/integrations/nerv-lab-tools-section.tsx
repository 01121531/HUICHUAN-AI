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
import { useMemo } from 'react'

import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { SettingsSection } from '../components/settings-section'
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
  wsl: 'WSL 子系统',
  docker: '容器后端',
  ssh: '远程主机',
}

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
    description: '把 bridge.md 和 skills 同步到本机 Codex 目录。',
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
    commands: ['python proxy_relay.py', '重新启动 Codex CLI 后输入 zxwn'],
  },
]

function formatTime(value?: number) {
  if (!value) return '暂无'
  return new Date(value * 1000).toLocaleString()
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

export function NERVLabToolsSection({
  defaultValues,
}: NERVLabToolsSectionProps) {
  const recentEvents = useMemo(
    () => parseRecentEvents(defaultValues['nerv_stats.recent']),
    [defaultValues['nerv_stats.recent']]
  )

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
  const backend = defaultValues['nerv_setting.mcp_backend'] ?? 'auto'
  const mcpCommands = [
    'python mcp_server.py',
    'python mcp_server.py --auto',
    `python mcp_server.py --wsl ${defaultValues['nerv_setting.wsl_distro'] || 'kali-linux'}`,
    `python mcp_server.py --docker ${defaultValues['nerv_setting.docker_container'] || 'kali-tools'}`,
    `python mcp_server.py --kali ${defaultValues['nerv_setting.ssh_host'] || 'root@192.168.1.100'}`,
    'python mcp_server.py --port 9000',
  ]

  return (
    <SettingsSection title='NERV 工程工具'>
      <Alert>
        <AlertDescription>
          这里把 NERV 原项目的部署脚本、MCP 后端、工具库和技能库整理成中转站后台目录。
          页面里的英文命令、模型上下文协议标识和工具名都是技术标识，会按原项目保留。
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
              description='在 NERV 连接设置里切换 MCP 后端。'
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
                <div className='rounded-md border p-3'>桥接提示词 → NERV 连接设置</div>
                <div className='rounded-md border p-3'>篡改规则 → 响应篡改配置</div>
                <div className='rounded-md border p-3'>memory.json → NERV 记忆缓存与最近事件</div>
                <div className='rounded-md border p-3'>mcp_server.py → MCP 后端命令与工具目录</div>
                <div className='rounded-md border p-3'>skills / tools → 技能目录与工具目录</div>
              </CardContent>
            </Card>

            <RecentEventsCard recentEvents={recentEvents} />
          </div>
        </TabsContent>

        <TabsContent value='tools' className='mt-4 space-y-4'>
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
            {workflowCommands.map((item) => (
              <CommandCard
                key={item.title}
                title={item.title}
                description={item.description}
                commands={item.commands}
              />
            ))}
            <CommandCard
              title='MCP 后端'
              description={`对应工具后端配置：WSL=${defaultValues['nerv_setting.wsl_distro'] || 'kali-linux'}，容器=${defaultValues['nerv_setting.docker_container'] || 'kali-tools'}。`}
              commands={mcpCommands}
            />
          </div>
        </TabsContent>
      </Tabs>
    </SettingsSection>
  )
}
