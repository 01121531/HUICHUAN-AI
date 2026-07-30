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
import { useState } from 'react'

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
  applyNERVCodexConfig,
  applyNERVMCPConfig,
  getNERVCodexConfigStatus,
  removeNERVCodexConfig,
  removeNERVMCPConfig,
} from '../api'
import type { NERVCodexConfigStatus, NERVToolBackend } from '../types'

type NERVCodexConfigCardProps = {
  mcpBackend?: NERVToolBackend
  wslDistro?: string
  dockerContainer?: string
  sshHost?: string
}

export function NERVCodexConfigCard({
  mcpBackend = 'auto',
  wslDistro = '',
  dockerContainer = '',
  sshHost = '',
}: NERVCodexConfigCardProps) {
  const [home, setHome] = useState('')
  const normalizedHome = home.trim()
  const statusQuery = useQuery({
    queryKey: ['nerv-codex-config-status', normalizedHome],
    queryFn: () => getNERVCodexConfigStatus(normalizedHome || undefined),
    staleTime: 15 * 1000,
  })
  const applyMutation = useMutation({
    mutationFn: () => applyNERVCodexConfig({ home: normalizedHome || undefined }),
    onSuccess: () => {
      void statusQuery.refetch()
    },
  })
  const removeMutation = useMutation({
    mutationFn: () => removeNERVCodexConfig({ home: normalizedHome || undefined }),
    onSuccess: () => {
      void statusQuery.refetch()
    },
  })
  const applyMCPMutation = useMutation({
    mutationFn: () =>
      applyNERVMCPConfig({
        home: normalizedHome || undefined,
        backend: mcpBackend,
        wsl_distro: wslDistro || undefined,
        docker_container: dockerContainer || undefined,
        ssh_host: sshHost || undefined,
      }),
    onSuccess: () => {
      void statusQuery.refetch()
    },
  })
  const removeMCPMutation = useMutation({
    mutationFn: () => removeNERVMCPConfig({ home: normalizedHome || undefined }),
    onSuccess: () => {
      void statusQuery.refetch()
    },
  })

  const status = statusQuery.data?.success ? statusQuery.data.data : undefined
  const actionData =
    applyMutation.data?.data ??
    removeMutation.data?.data ??
    applyMCPMutation.data?.data ??
    removeMCPMutation.data?.data
  const actionFailed =
    applyMutation.data?.success === false ||
    removeMutation.data?.success === false ||
    applyMCPMutation.data?.success === false ||
    removeMCPMutation.data?.success === false
  const actionMessage =
    applyMutation.data?.message ||
    removeMutation.data?.message ||
    applyMCPMutation.data?.message ||
    removeMCPMutation.data?.message ||
    ''
  const resetActionMessages = () => {
    applyMutation.reset()
    removeMutation.reset()
    applyMCPMutation.reset()
    removeMCPMutation.reset()
  }

  return (
    <Card>
      <CardHeader>
        <div className='flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between'>
          <div>
            <CardTitle>Codex 一键部署</CardTitle>
            <CardDescription>
              对应原项目 deploy.py / direct_setup.py：备份 config.toml，写入桥接文件配置，
              复制 bridge.md、skills，并可一键写入 MCP 工具服务器配置。
            </CardDescription>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={statusQuery.isFetching}
              onClick={() => {
                void statusQuery.refetch()
              }}
            >
              {statusQuery.isFetching ? '检查中...' : '检查状态'}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={applyMutation.isPending}
              onClick={() => {
                resetActionMessages()
                applyMutation.mutate()
              }}
            >
              {applyMutation.isPending ? '部署中...' : '部署桥接'}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={removeMutation.isPending}
              onClick={() => {
                resetActionMessages()
                removeMutation.mutate()
              }}
            >
              {removeMutation.isPending ? '还原中...' : '还原桥接'}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={applyMCPMutation.isPending}
              onClick={() => {
                resetActionMessages()
                applyMCPMutation.mutate()
              }}
            >
              {applyMCPMutation.isPending ? '写入中...' : '写入 MCP'}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={removeMCPMutation.isPending}
              onClick={() => {
                resetActionMessages()
                removeMCPMutation.mutate()
              }}
            >
              {removeMCPMutation.isPending ? '还原中...' : '还原 MCP'}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='space-y-2'>
          <div className='text-sm font-medium'>Codex Home</div>
          <Input
            value={home}
            onChange={(event) => setHome(event.target.value)}
            placeholder='留空自动查找 CODEX_HOME 或 ~/.codex'
            autoComplete='off'
          />
          <div className='text-xs text-muted-foreground'>
            服务器端部署时通常是 /home/ubuntu/.codex；如果目标不在默认位置，可手动填写目录。
          </div>
        </div>

        {statusQuery.isError || statusQuery.data?.success === false ? (
          <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
            状态读取失败：{statusQuery.data?.message || '请稍后重试'}
          </div>
        ) : null}

        {status ? <CodexConfigStatusView status={status} /> : null}

        {actionData ? (
          <div className='rounded-md border bg-muted/10 p-3 text-sm'>
            <div className='font-medium'>
              {actionData.action === 'apply'
                ? '桥接部署结果'
                : actionData.action === 'remove'
                  ? '桥接还原结果'
                  : actionData.action === 'apply_mcp'
                    ? 'MCP 写入结果'
                    : 'MCP 还原结果'}
            </div>
            <div className='mt-2 flex flex-wrap gap-2'>
              {actionData.messages.map((message) => (
                <Badge key={message} variant='secondary'>
                  {message}
                </Badge>
              ))}
            </div>
          </div>
        ) : null}

        {actionFailed ? (
          <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
            操作失败：{actionMessage || '请检查 Codex Home 和服务器权限'}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function CodexConfigStatusView({ status }: { status: NERVCodexConfigStatus }) {
  const rows = [
    ['配置目录', status.found ? '已找到' : '未找到', status.home || '暂无'],
    ['配置文件', status.config_exists ? '存在' : '缺失', status.config_path || '暂无'],
    ['桥接配置', status.bridge_active ? '已写入' : '未写入', status.model_instructions_raw || '暂无'],
    ['桥接文件', status.bridge_exists ? '存在' : '缺失', 'bridge.md'],
    ['技能目录', status.skills_exists ? `${status.skill_count} 个技能` : '缺失', 'skills/'],
    ['备份文件', status.backup_exists ? '存在' : '暂无', 'config.toml.lab-bak'],
    ['MCP 配置', status.mcp_active ? '已写入' : '未写入', status.mcp_config_raw || '[mcp_servers.nerv_break]'],
    ['MCP 备份', status.mcp_backup_exists ? '存在' : '暂无', 'config.toml.mcp-bak'],
    ['MCP 脚本', status.asset_mcp_server_exists ? '可用' : '缺失', status.mcp_server_script_path || '暂无'],
    ['内置资产', status.asset_bridge_exists ? '可用' : '异常', status.asset_path || '暂无'],
  ] as const

  return (
    <div className='space-y-3'>
      <div className='flex flex-wrap items-center gap-2'>
        <Badge variant={status.bridge_active ? 'secondary' : 'outline'}>
          {status.message}
        </Badge>
        {status.mcp_active ? <Badge variant='secondary'>MCP 已写入</Badge> : null}
        {status.asset_skills_exists ? <Badge variant='outline'>内置 skills 可用</Badge> : null}
      </div>
      <div className='grid gap-2 md:grid-cols-2'>
        {rows.map(([label, value, detail]) => (
          <div key={label} className='rounded-md border bg-muted/10 p-3 text-sm'>
            <div className='flex items-center justify-between gap-2'>
              <span className='text-muted-foreground'>{label}</span>
              <span className='font-medium'>{value}</span>
            </div>
            <div className='mt-1 break-all font-mono text-xs text-muted-foreground'>
              {detail}
            </div>
          </div>
        ))}
      </div>
      {status.candidates.length > 0 ? (
        <div className='text-xs text-muted-foreground'>
          自动查找候选：{status.candidates.join('；')}
        </div>
      ) : null}
    </div>
  )
}
