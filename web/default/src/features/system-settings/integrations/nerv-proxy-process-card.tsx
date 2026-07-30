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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

import {
  getNERVProxyDashboard,
  getNERVProxyProcessStatus,
  startNERVProxyProcess,
  stopNERVProxyProcess,
} from '../api'
import type { NERVProxyDashboardSnapshot, NERVProxyProcessStatus } from '../types'

export function NERVProxyProcessCard() {
  const [home, setHome] = useState('')
  const [restoreConfig, setRestoreConfig] = useState(true)
  const normalizedHome = home.trim()

  const statusQuery = useQuery({
    queryKey: ['nerv-proxy-process-status'],
    queryFn: getNERVProxyProcessStatus,
    staleTime: 10 * 1000,
  })
  const dashboardQuery = useQuery({
    queryKey: ['nerv-proxy-dashboard'],
    queryFn: getNERVProxyDashboard,
    enabled: false,
  })
  const startMutation = useMutation({
    mutationFn: () => startNERVProxyProcess({ home: normalizedHome || undefined }),
    onSuccess: () => {
      void statusQuery.refetch()
    },
  })
  const stopMutation = useMutation({
    mutationFn: () => stopNERVProxyProcess({ restore_config: restoreConfig }),
    onSuccess: () => {
      void statusQuery.refetch()
    },
  })

  const status = statusQuery.data?.success
    ? (statusQuery.data.data ?? startMutation.data?.data?.status ?? stopMutation.data?.data?.status)
    : startMutation.data?.data?.status ?? stopMutation.data?.data?.status
  const actionFailed =
    startMutation.data?.success === false || stopMutation.data?.success === false
  const actionMessage = startMutation.data?.message || stopMutation.data?.message

  return (
    <Card>
      <CardHeader>
        <div className='flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between'>
          <div>
            <CardTitle>NERV 外置代理控制</CardTitle>
            <CardDescription>
              对应原项目 proxy_relay.py：启动本机 8080 代理和 8090 面板，作为 Codex
              与上游中转之间的独立代理层。
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
              disabled={startMutation.isPending || status?.running}
              onClick={() => {
                stopMutation.reset()
                startMutation.mutate()
              }}
            >
              {startMutation.isPending ? '启动中...' : '启动代理'}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={stopMutation.isPending || !status?.pid}
              onClick={() => {
                startMutation.reset()
                stopMutation.mutate()
              }}
            >
              {stopMutation.isPending ? '停止中...' : '停止代理'}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={!status?.dashboard_open || dashboardQuery.isFetching}
              onClick={() => {
                void dashboardQuery.refetch()
              }}
            >
              {dashboardQuery.isFetching ? '读取中...' : '读取面板'}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='grid gap-3 lg:grid-cols-[1fr_auto] lg:items-end'>
          <div className='space-y-2'>
            <div className='text-sm font-medium'>Codex Home</div>
            <Input
              value={home}
              onChange={(event) => setHome(event.target.value)}
              placeholder='留空自动查找；启动时会传给原 proxy_relay.py'
              autoComplete='off'
            />
          </div>
          <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
            <div>
              <div className='text-sm font-medium'>停止时还原配置</div>
              <div className='text-xs text-muted-foreground'>
                使用 config.toml.nerv-bak 还原并清理 bridge/skills
              </div>
            </div>
            <Switch checked={restoreConfig} onCheckedChange={setRestoreConfig} />
          </div>
        </div>

        {statusQuery.isError || statusQuery.data?.success === false ? (
          <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
            代理状态读取失败：{statusQuery.data?.message || '请稍后重试'}
          </div>
        ) : null}
        {actionFailed ? (
          <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
            操作失败：{actionMessage || '请检查 Python、端口占用和 Codex Home'}
          </div>
        ) : null}

        {status ? <ProxyProcessStatusView status={status} /> : null}
        {dashboardQuery.data ? (
          <ProxyDashboardPreview
            success={dashboardQuery.data.success}
            message={dashboardQuery.data.message}
            snapshot={dashboardQuery.data.data}
          />
        ) : null}
      </CardContent>
    </Card>
  )
}

function ProxyDashboardPreview({
  success,
  message,
  snapshot,
}: {
  success: boolean
  message: string
  snapshot?: NERVProxyDashboardSnapshot
}) {
  if (!success || !snapshot) {
    return (
      <div className='rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive'>
        面板读取失败：{message || '请确认代理面板已启动'}
      </div>
    )
  }

  if (!snapshot.available) {
    return (
      <div className='rounded-md border bg-muted/10 p-3 text-sm text-muted-foreground'>
        {snapshot.message || 'NERV 8090 面板未打开'}
      </div>
    )
  }

  return (
    <div className='space-y-3 rounded-md border bg-muted/10 p-3 text-sm'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <div className='font-medium'>NERV 8090 原生面板</div>
          <div className='break-all text-xs text-muted-foreground'>
            {snapshot.message} · {snapshot.url}
          </div>
        </div>
        <Badge variant='outline'>HTTP {snapshot.status_code || '未知'}</Badge>
      </div>
      {snapshot.html ? (
        <iframe
          title='NERV proxy dashboard'
          className='h-[520px] w-full rounded-md border bg-background'
          srcDoc={snapshot.html}
          sandbox='allow-scripts allow-same-origin'
        />
      ) : (
        <div className='rounded-md border bg-background p-3 text-muted-foreground'>
          面板没有返回可预览内容。
        </div>
      )}
    </div>
  )
}

function ProxyProcessStatusView({
  status,
}: {
  status: NERVProxyProcessStatus
}) {
  const rows = [
    ['进程状态', status.running ? `运行中：PID ${status.pid}` : '未运行', status.message],
    ['代理地址', status.listen_open ? '端口已打开' : '端口未打开', status.listen_url],
    [
      '面板地址',
      status.dashboard_open ? '端口已打开' : '端口未打开',
      status.dashboard_url,
    ],
    ['Python', status.python_command || '未找到', status.script_path || '暂无'],
    ['日志文件', status.log_path ? '可读取' : '暂无', status.log_path || '暂无'],
    ['Codex Home', status.codex_home || '自动查找', status.codex_home || '暂无'],
  ] as const

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap gap-2'>
        <Badge variant={status.running ? 'secondary' : 'outline'}>
          {status.message}
        </Badge>
        {status.started_at ? (
          <Badge variant='outline'>
            启动时间：{new Date(status.started_at * 1000).toLocaleString()}
          </Badge>
        ) : null}
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
      {status.log_tail ? (
        <div className='space-y-2'>
          <div className='text-sm font-medium'>最近日志</div>
          <Textarea
            className='min-h-32 font-mono text-xs'
            value={status.log_tail}
            readOnly
          />
        </div>
      ) : null}
    </div>
  )
}
