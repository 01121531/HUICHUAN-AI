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

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { CopyButton } from '@/components/copy-button'

import { SettingsSection } from '../components/settings-section'

type NERVLabRecentEvent = {
  ts: number
  event: string
  target: string
  model: string
}

type NERVLabDefaults = {
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

export function NERVLabToolsSection({
  defaultValues,
}: NERVLabToolsSectionProps) {
  const recentEvents = useMemo(
    () => parseRecentEvents(defaultValues['nerv_stats.recent']),
    [defaultValues['nerv_stats.recent']]
  )

  const wslCommand = `python mcp_server.py --wsl`
  const dockerCommand = `python mcp_server.py --docker ${defaultValues['nerv_setting.docker_container'] || 'kali-tools'}`
  const sshCommand = `python mcp_server.py --kali ${defaultValues['nerv_setting.ssh_host'] || 'root@192.168.1.100'}`
  const directProxyCommand = `python direct_setup.py proxy`

  return (
    <SettingsSection title='NERV 工程工具'>
      <Alert>
        <AlertDescription>
          这里展示的是 NERV 原始项目的本机工程流程：部署桥接、验证配置、直连
          模式、代理模式和 MCP 后端。命令都在你的 Codex 本机执行，不在服务器上执行。
        </AlertDescription>
      </Alert>

      <div className='grid gap-4 lg:grid-cols-2'>
        <CommandCard
          title='桥接部署'
          description='把 bridge.md 和 skills 同步到本机 Codex 目录。'
          commands={[
            'python deploy.py apply',
            'python deploy.py status',
            'python deploy.py remove',
          ]}
        />

        <CommandCard
          title='配置验证'
          description='检查桥接文件、技能目录和 Codex 配置是否生效。'
          commands={['python verify.py']}
        />

        <CommandCard
          title='直连模式'
          description='不经过代理，直接把 Codex 指向 OpenAI 接口。'
          commands={[
            'python direct_setup.py apply',
            directProxyCommand,
            'python direct_setup.py remove',
          ]}
        />

        <CommandCard
          title='代理模式'
          description='自动注入、自动连接并提供本地代理与面板。'
          commands={[
            'python proxy_relay.py',
            '重新启动 Codex CLI 后输入 zxwn',
          ]}
        />

        <CommandCard
          title='MCP 后端'
          description={`对应工具后端配置：WSL=${defaultValues['nerv_setting.wsl_distro'] || 'kali-linux'}，Docker=${defaultValues['nerv_setting.docker_container'] || 'kali-tools'}。`}
          commands={[
            'python mcp_server.py',
            'python mcp_server.py --auto',
            wslCommand,
            dockerCommand,
            sshCommand,
            'python mcp_server.py --port 9000',
          ]}
        />

        <Card>
          <CardHeader>
            <CardTitle>最近事件</CardTitle>
            <CardDescription>从 memory.json 风格的事件缓存里读取最近 8 条。 </CardDescription>
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
      </div>
    </SettingsSection>
  )
}
