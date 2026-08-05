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
import {
  Activity,
  CalendarClock,
  CheckCircle2,
  FileText,
  Layers3,
  ListPlus,
  Network,
  Pause,
  Pencil,
  Play,
  Plus,
  RotateCcw,
  Shuffle,
  Trash2,
  TriangleAlert,
} from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'

import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type {
  ManagedProxy,
  ProxyGroup,
  ProxyHealthSettings,
} from './proxy-management-types'

const proxyStatusLabels: Record<string, string> = {
  available: '正常',
  watching: '观察中',
  paused: '已暂停',
  cooling: '冷却中',
  recovering: '恢复测试',
  unavailable: '已自动禁用',
  disabled: '已停用',
}

function proxyStatusLabel(proxy: ManagedProxy) {
  if (!proxy.enabled) return '手动停用'
  return proxyStatusLabels[proxy.status] ?? proxy.status ?? '正常'
}

function proxyGroupStatusLabel(group: ProxyGroup) {
  if (!group.enabled) return '已停用'
  if (group.status === 'switching') return '切换中'
  return '运行中'
}

function isProxyAvailable(proxy: ManagedProxy) {
  return (
    proxy.enabled &&
    (!proxy.status ||
      proxy.status === 'available' ||
      proxy.status === 'watching')
  )
}

type ProxyPoolWorkspaceProps = {
  groups: ProxyGroup[]
  selectedGroupId: number
  selectedGroup: ProxyGroup | null
  proxies: ManagedProxy[]
  switching: boolean
  checking: boolean
  checkingAll: boolean
  savingHealthSettings: boolean
  healthSettings: ProxyHealthSettings
  pausing: boolean
  resetting: boolean
  onSelectGroup: (groupId: number) => void
  onCreateGroup: () => void
  onEditGroup: (group: ProxyGroup) => void
  onDeleteGroup: (group: ProxyGroup) => void
  onSwitchGroup: (groupId: number) => void
  onBatchCreateProxy: () => void
  onCreateProxy: () => void
  onCheckProxy: (proxyId: number) => void
  onCheckGroup: (groupId: number) => void
  onCheckAll: () => void
  onSaveHealthSettings: (settings: { enabled: boolean; time: string }) => void
  onToggleProxy: (proxy: ManagedProxy) => void
  onViewProxyLogs: (proxyId: number) => void
  onResetProxy: (proxyId: number) => void
  onEditProxy: (proxy: ManagedProxy) => void
  onDeleteProxy: (proxy: ManagedProxy) => void
}

function SummaryCard(props: {
  title: string
  value: number
  description: string
  icon: ReactNode
  tone?: 'default' | 'success' | 'warning'
}) {
  let toneClass = 'bg-primary/10 text-primary'
  if (props.tone === 'success') {
    toneClass = 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
  } else if (props.tone === 'warning') {
    toneClass = 'bg-amber-500/10 text-amber-700 dark:text-amber-300'
  }

  return (
    <Card className='shadow-none'>
      <CardContent className='flex items-center gap-3 p-4'>
        <div
          className={`flex size-10 shrink-0 items-center justify-center rounded-xl ${toneClass}`}
        >
          {props.icon}
        </div>
        <div className='min-w-0'>
          <p className='text-muted-foreground text-xs'>{props.title}</p>
          <p className='mt-0.5 text-xl font-semibold tabular-nums'>
            {props.value}
          </p>
          <p className='text-muted-foreground truncate text-xs'>
            {props.description}
          </p>
        </div>
      </CardContent>
    </Card>
  )
}

export function ProxyPoolWorkspace(props: ProxyPoolWorkspaceProps) {
  const [dailyCheckEnabled, setDailyCheckEnabled] = useState(
    props.healthSettings.enabled
  )
  const [dailyCheckTime, setDailyCheckTime] = useState(
    props.healthSettings.time
  )

  useEffect(() => {
    setDailyCheckEnabled(props.healthSettings.enabled)
    setDailyCheckTime(props.healthSettings.time)
  }, [props.healthSettings.enabled, props.healthSettings.time])
  const totalProxyCount = props.groups.reduce(
    (total, group) => total + (group.proxy_count ?? 0),
    0
  )
  const availableProxyCount = props.groups.reduce(
    (total, group) => total + (group.available_proxy_count ?? 0),
    0
  )
  const unavailableProxyCount = props.groups.reduce(
    (total, group) => total + (group.unavailable_proxy_count ?? 0),
    0
  )
  const currentProxy = props.selectedGroup
    ? (props.proxies.find(
        (proxy) => proxy.id === props.selectedGroup?.current_proxy_id
      ) ?? null)
    : null

  return (
    <div className='space-y-4'>
      <Card className='shadow-none'>
        <CardContent className='flex flex-col gap-4 p-4 xl:flex-row xl:items-center xl:justify-between'>
          <div className='flex min-w-0 items-start gap-3'>
            <div className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl'>
              <CalendarClock className='size-5' />
            </div>
            <div>
              <p className='text-sm font-semibold'>代理健康检测</p>
              <p className='text-muted-foreground mt-1 text-xs leading-5'>
                每天按北京时间全量检测；连接失败的代理会立即自动禁用。也可随时手动检测全部代理。
              </p>
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <label className='flex h-9 items-center gap-2 rounded-md border px-3 text-sm'>
              <Switch
                checked={dailyCheckEnabled}
                onCheckedChange={setDailyCheckEnabled}
              />
              每日检测
            </label>
            <Input
              type='time'
              className='h-9 w-32'
              value={dailyCheckTime}
              disabled={!dailyCheckEnabled}
              aria-label='每日代理检测时间'
              onChange={(event) => setDailyCheckTime(event.target.value)}
            />
            <Button
              variant='outline'
              size='sm'
              disabled={props.savingHealthSettings || !dailyCheckTime}
              onClick={() =>
                props.onSaveHealthSettings({
                  enabled: dailyCheckEnabled,
                  time: dailyCheckTime,
                })
              }
            >
              保存时间
            </Button>
            <Button
              size='sm'
              disabled={props.checkingAll}
              onClick={props.onCheckAll}
            >
              <Activity />
              {props.checkingAll ? '检测任务启动中…' : '立即检测全部'}
            </Button>
          </div>
        </CardContent>
      </Card>
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        <SummaryCard
          title='代理分组'
          value={props.groups.length}
          description='按用途统一管理代理'
          icon={<Layers3 className='size-5' />}
        />
        <SummaryCard
          title='全部代理'
          value={totalProxyCount}
          description='所有分组中的代理总数'
          icon={<Network className='size-5' />}
        />
        <SummaryCard
          title='当前可用'
          value={availableProxyCount}
          description='可直接参与请求选择'
          icon={<CheckCircle2 className='size-5' />}
          tone='success'
        />
        <SummaryCard
          title='需关注'
          value={unavailableProxyCount}
          description='停用、冷却或已自动禁用'
          icon={<TriangleAlert className='size-5' />}
          tone='warning'
        />
      </div>

      <div className='grid items-start gap-4 xl:grid-cols-[18rem_minmax(0,1fr)]'>
        <Card className='overflow-hidden shadow-none xl:sticky xl:top-4'>
          <CardHeader className='flex flex-row items-start justify-between gap-3 border-b'>
            <div>
              <CardTitle className='text-base'>代理分组</CardTitle>
              <p className='text-muted-foreground mt-1 text-xs'>
                选择分组后管理其中的代理
              </p>
            </div>
            <Button size='sm' onClick={props.onCreateGroup}>
              <Plus />
              新建
            </Button>
          </CardHeader>
          <CardContent className='p-2'>
            {!props.groups.length ? (
              <div className='flex min-h-56 flex-col items-center justify-center px-5 text-center'>
                <div className='bg-muted flex size-11 items-center justify-center rounded-xl'>
                  <Layers3 className='text-muted-foreground size-5' />
                </div>
                <p className='mt-3 text-sm font-medium'>还没有代理分组</p>
                <p className='text-muted-foreground mt-1 text-xs leading-5'>
                  先创建分组，再向分组中添加代理并绑定渠道。
                </p>
                <Button
                  className='mt-4'
                  size='sm'
                  onClick={props.onCreateGroup}
                >
                  <Plus />
                  创建第一个分组
                </Button>
              </div>
            ) : (
              <ScrollArea className='h-72 xl:h-[min(34rem,62vh)]'>
                <div className='space-y-2 pr-2'>
                  {props.groups.map((group) => {
                    const selected = group.id === props.selectedGroupId
                    return (
                      <div
                        key={group.id}
                        className={`overflow-hidden rounded-xl border transition-colors ${
                          selected
                            ? 'border-primary/50 bg-primary/5'
                            : 'hover:bg-muted/40'
                        }`}
                      >
                        <button
                          type='button'
                          className='focus-visible:ring-ring w-full px-3 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset'
                          onClick={() => props.onSelectGroup(group.id)}
                        >
                          <div className='flex items-start justify-between gap-2'>
                            <span className='min-w-0 truncate text-sm font-semibold'>
                              {group.name}
                            </span>
                            <Badge
                              variant={group.enabled ? 'secondary' : 'outline'}
                              className='shrink-0'
                            >
                              {proxyGroupStatusLabel(group)}
                            </Badge>
                          </div>
                          <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs tabular-nums'>
                            <span>{group.proxy_count ?? 0} 个代理</span>
                            <span>
                              {group.available_proxy_count ?? 0} 个可用
                            </span>
                            <span>{group.bound_channel_count} 个渠道</span>
                          </div>
                        </button>
                        <div className='border-t px-2 py-1.5'>
                          <div className='flex items-center justify-end gap-1'>
                            <Button
                              variant='ghost'
                              size='sm'
                              onClick={() => props.onEditGroup(group)}
                            >
                              <Pencil />
                              编辑
                            </Button>
                            <Button
                              variant='ghost'
                              size='sm'
                              className='text-destructive hover:text-destructive'
                              onClick={() => props.onDeleteGroup(group)}
                            >
                              <Trash2 />
                              删除
                            </Button>
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </ScrollArea>
            )}
          </CardContent>
        </Card>

        {!props.selectedGroup ? (
          <Card className='shadow-none'>
            <CardContent className='flex min-h-96 flex-col items-center justify-center text-center'>
              <div className='bg-muted flex size-12 items-center justify-center rounded-xl'>
                <Network className='text-muted-foreground size-6' />
              </div>
              <h3 className='mt-4 text-base font-semibold'>请选择代理分组</h3>
              <p className='text-muted-foreground mt-1 max-w-sm text-sm'>
                选择左侧分组查看代理，或者新建一个分组开始配置。
              </p>
            </CardContent>
          </Card>
        ) : (
          <Card className='min-w-0 overflow-hidden shadow-none'>
            <CardHeader className='gap-4 border-b'>
              <div className='flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between'>
                <div className='min-w-0'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <CardTitle className='truncate text-lg'>
                      {props.selectedGroup.name}
                    </CardTitle>
                    <Badge
                      variant={
                        props.selectedGroup.enabled ? 'secondary' : 'outline'
                      }
                    >
                      {proxyGroupStatusLabel(props.selectedGroup)}
                    </Badge>
                  </div>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    当前代理：{currentProxy?.name ?? '尚未选择'}
                    {currentProxy ? `（#${currentProxy.id}）` : ''}
                  </p>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={props.checkingAll || !props.proxies.length}
                    onClick={() => {
                      if (props.selectedGroup) {
                        props.onCheckGroup(props.selectedGroup.id)
                      }
                    }}
                  >
                    <Activity />
                    检测本组
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={props.switching || props.proxies.length < 2}
                    onClick={() =>
                      props.selectedGroup &&
                      props.onSwitchGroup(props.selectedGroup.id)
                    }
                  >
                    <Shuffle />
                    立即切换
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      props.selectedGroup &&
                      props.onEditGroup(props.selectedGroup)
                    }
                  >
                    <Pencil />
                    编辑设置
                  </Button>
                  <Button
                    variant='destructive'
                    size='sm'
                    onClick={() =>
                      props.selectedGroup &&
                      props.onDeleteGroup(props.selectedGroup)
                    }
                  >
                    <Trash2 />
                    删除分组
                  </Button>
                </div>
              </div>

              <div className='grid gap-3 text-sm sm:grid-cols-2 2xl:grid-cols-4'>
                <div className='bg-muted/40 rounded-lg border px-3 py-2.5'>
                  <p className='text-muted-foreground text-xs'>轮换策略</p>
                  <p className='mt-1 font-medium tabular-nums'>
                    {props.selectedGroup.max_requests} 次 /{' '}
                    {props.selectedGroup.max_duration_seconds} 秒
                  </p>
                </div>
                <div className='bg-muted/40 rounded-lg border px-3 py-2.5'>
                  <p className='text-muted-foreground text-xs'>等待请求</p>
                  <p className='mt-1 font-medium tabular-nums'>
                    {props.selectedGroup.waiting_requests} /{' '}
                    {props.selectedGroup.max_waiting_requests}
                  </p>
                </div>
                <div className='bg-muted/40 rounded-lg border px-3 py-2.5'>
                  <p className='text-muted-foreground text-xs'>已绑定渠道</p>
                  <p className='mt-1 font-medium tabular-nums'>
                    {props.selectedGroup.bound_channel_count} 个
                  </p>
                </div>
                <div className='bg-muted/40 rounded-lg border px-3 py-2.5'>
                  <p className='text-muted-foreground text-xs'>无代理时</p>
                  <p className='mt-1 font-medium'>
                    {props.selectedGroup.allow_direct_fallback
                      ? '允许直连'
                      : '阻止请求'}
                  </p>
                </div>
              </div>
            </CardHeader>

            <CardContent className='space-y-4 p-4'>
              <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                <div>
                  <h3 className='text-sm font-semibold'>分组内代理</h3>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    密码不会回显；编辑时留空即可保留原密码。
                  </p>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={props.onBatchCreateProxy}
                  >
                    <ListPlus />
                    批量添加
                  </Button>
                  <Button size='sm' onClick={props.onCreateProxy}>
                    <Plus />
                    新增代理
                  </Button>
                </div>
              </div>

              <div className='overflow-x-auto rounded-xl border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>代理</TableHead>
                      <TableHead>连接地址</TableHead>
                      <TableHead>运行状态</TableHead>
                      <TableHead>最近质量</TableHead>
                      <TableHead>使用统计</TableHead>
                      <TableHead className='text-right'>操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {!props.proxies.length ? (
                      <TableRow>
                        <TableCell colSpan={6} className='h-64 text-center'>
                          <div className='flex flex-col items-center'>
                            <div className='bg-muted flex size-11 items-center justify-center rounded-xl'>
                              <Network className='text-muted-foreground size-5' />
                            </div>
                            <p className='mt-3 text-sm font-medium'>
                              当前分组还没有代理
                            </p>
                            <p className='text-muted-foreground mt-1 text-xs'>
                              可以单个添加，也可以一次批量导入多个代理。
                            </p>
                            <div className='mt-4 flex gap-2'>
                              <Button
                                variant='outline'
                                size='sm'
                                onClick={props.onBatchCreateProxy}
                              >
                                <ListPlus />
                                批量添加
                              </Button>
                              <Button size='sm' onClick={props.onCreateProxy}>
                                <Plus />
                                新增代理
                              </Button>
                            </div>
                          </div>
                        </TableCell>
                      </TableRow>
                    ) : (
                      props.proxies.map((proxy) => (
                        <TableRow key={proxy.id}>
                          <TableCell className='min-w-40'>
                            <div className='flex flex-wrap items-center gap-1.5'>
                              <span className='font-medium'>{proxy.name}</span>
                              {proxy.id ===
                                props.selectedGroup?.current_proxy_id && (
                                <Badge variant='default'>当前</Badge>
                              )}
                            </div>
                            <div className='text-muted-foreground mt-1 text-xs tabular-nums'>
                              ID {proxy.id} · 排序 {proxy.sort}
                            </div>
                          </TableCell>
                          <TableCell className='min-w-48'>
                            <div className='flex items-center gap-2'>
                              <Badge variant='outline' className='uppercase'>
                                {proxy.protocol}
                              </Badge>
                              <span className='font-mono text-xs'>
                                {proxy.host}:{proxy.port}
                              </span>
                            </div>
                            <div className='text-muted-foreground mt-1 font-mono text-xs'>
                              出口 {proxy.last_exit_ip || '尚未检测'}
                            </div>
                          </TableCell>
                          <TableCell className='min-w-36'>
                            <Badge
                              variant={
                                isProxyAvailable(proxy)
                                  ? 'secondary'
                                  : 'destructive'
                              }
                              className={
                                isProxyAvailable(proxy)
                                  ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
                                  : undefined
                              }
                            >
                              {proxyStatusLabel(proxy)}
                            </Badge>
                            <div className='text-muted-foreground mt-1 text-xs tabular-nums'>
                              {proxy.last_check_at > 0
                                ? `检测 ${proxy.last_check_latency_ms} ms`
                                : '尚未检测'}
                            </div>
                          </TableCell>
                          <TableCell className='min-w-44 text-xs tabular-nums'>
                            <div>
                              首字{' '}
                              {proxy.last_frt_ms > 0
                                ? `${proxy.last_frt_ms} ms`
                                : '—'}
                              {' · '}TPS{' '}
                              {proxy.last_tps > 0
                                ? proxy.last_tps.toFixed(1)
                                : '—'}
                            </div>
                            <div className='text-muted-foreground mt-1'>
                              窗口红色率{' '}
                              {proxy.window_samples > 0
                                ? `${Math.round(proxy.window_timeout_ratio * 100)}%（${proxy.window_timeouts}/${proxy.window_samples}）`
                                : '暂无样本'}
                            </div>
                          </TableCell>
                          <TableCell className='min-w-32 text-xs tabular-nums'>
                            <div>请求 {proxy.total_requests}</div>
                            <div className='text-muted-foreground mt-1'>
                              红色 {proxy.total_timeouts} · 连续{' '}
                              {proxy.consecutive_timeouts}
                            </div>
                          </TableCell>
                          <TableCell className='text-right'>
                            <div className='flex items-center justify-end gap-1'>
                              <Button
                                variant='outline'
                                size='sm'
                                disabled={props.checking}
                                onClick={() => props.onCheckProxy(proxy.id)}
                              >
                                <Activity />
                                检测
                              </Button>
                              <Button
                                variant='ghost'
                                size='icon-sm'
                                aria-label='编辑代理'
                                title='编辑代理'
                                onClick={() => props.onEditProxy(proxy)}
                              >
                                <Pencil />
                              </Button>
                              <DataTableRowActionMenu ariaLabel='更多代理操作'>
                                <DropdownMenuItem
                                  disabled={props.pausing}
                                  onClick={() => props.onToggleProxy(proxy)}
                                >
                                  {proxy.status === 'paused' ? (
                                    <Play />
                                  ) : (
                                    <Pause />
                                  )}
                                  {proxy.status === 'paused'
                                    ? '恢复代理'
                                    : '暂停代理'}
                                </DropdownMenuItem>
                                <DropdownMenuItem
                                  onClick={() =>
                                    props.onViewProxyLogs(proxy.id)
                                  }
                                >
                                  <FileText />
                                  查看通用日志
                                </DropdownMenuItem>
                                <DropdownMenuItem
                                  disabled={props.resetting}
                                  onClick={() => props.onResetProxy(proxy.id)}
                                >
                                  <RotateCcw />
                                  清空观察计数
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  variant='destructive'
                                  onClick={() => props.onDeleteProxy(proxy)}
                                >
                                  <Trash2 />
                                  删除代理
                                </DropdownMenuItem>
                              </DataTableRowActionMenu>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}
