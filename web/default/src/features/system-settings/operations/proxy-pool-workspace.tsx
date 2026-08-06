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
  ListChecks,
  ListPlus,
  LoaderCircle,
  Network,
  Pause,
  Pencil,
  Play,
  Plus,
  RotateCcw,
  Shuffle,
  Trash2,
  TriangleAlert,
  X,
} from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'

import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Progress } from '@/components/ui/progress'
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

import { ProxyLatencyTrendChart } from './proxy-latency-trend-chart'
import type {
  ManagedProxy,
  ProxyGroup,
  ProxyHealthSettings,
  ProxyHealthSystemTask,
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

function proxyHealthCheckLabel(proxy: ManagedProxy) {
  if (proxy.last_check_at <= 0) return '尚未检测'
  if (proxy.last_health_error) return '连接检测失败'
  return `检测 ${proxy.last_check_latency_ms} ms`
}

type ProxyPoolWorkspaceProps = {
  groups: ProxyGroup[]
  selectedGroupId: number
  selectedGroup: ProxyGroup | null
  proxies: ManagedProxy[]
  switching: boolean
  checking: boolean
  checkingAll: boolean
  healthCheckTask: ProxyHealthSystemTask | null | undefined
  savingHealthSettings: boolean
  healthSettings: ProxyHealthSettings
  pausing: boolean
  resetting: boolean
  batchOperating: boolean
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
  onBatchCheck: (proxies: ManagedProxy[]) => void
  onBatchSetPaused: (proxies: ManagedProxy[], paused: boolean) => void
  onBatchReset: (proxies: ManagedProxy[]) => void
  onBatchDelete: (proxies: ManagedProxy[]) => void
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

function ProxyGroupManagerDialog(props: {
  open: boolean
  groups: ProxyGroup[]
  selectedGroupId: number
  onOpenChange: (open: boolean) => void
  onSelect: (groupId: number) => void
  onCreate: () => void
  onEdit: (group: ProxyGroup) => void
  onDelete: (group: ProxyGroup) => void
}) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[85vh] sm:max-w-2xl'>
        <DialogHeader>
          <div className='flex items-start justify-between gap-4 pr-8'>
            <div>
              <DialogTitle>管理代理分组</DialogTitle>
              <DialogDescription className='mt-1'>
                在这里选择、新建、编辑或删除代理分组。
              </DialogDescription>
            </div>
            <Button
              size='sm'
              onClick={() => {
                props.onOpenChange(false)
                props.onCreate()
              }}
            >
              <Plus />
              新建分组
            </Button>
          </div>
        </DialogHeader>

        {!props.groups.length ? (
          <div className='flex min-h-64 flex-col items-center justify-center rounded-xl border border-dashed text-center'>
            <div className='bg-muted flex size-11 items-center justify-center rounded-xl'>
              <Layers3 className='text-muted-foreground size-5' />
            </div>
            <p className='mt-3 text-sm font-medium'>还没有代理分组</p>
            <p className='text-muted-foreground mt-1 text-xs'>
              创建分组后即可集中添加和管理代理。
            </p>
          </div>
        ) : (
          <ScrollArea className='max-h-[60vh] pr-3'>
            <div className='space-y-2'>
              {props.groups.map((group) => {
                const selected = group.id === props.selectedGroupId
                return (
                  <div
                    key={group.id}
                    className={`flex flex-col gap-3 rounded-xl border p-3 sm:flex-row sm:items-center sm:justify-between ${
                      selected ? 'border-primary/50 bg-primary/5' : ''
                    }`}
                  >
                    <button
                      type='button'
                      className='focus-visible:ring-ring min-w-0 flex-1 rounded-lg text-left outline-none focus-visible:ring-2'
                      onClick={() => {
                        props.onSelect(group.id)
                        props.onOpenChange(false)
                      }}
                    >
                      <div className='flex flex-wrap items-center gap-2'>
                        <span className='truncate text-sm font-semibold'>
                          {group.name}
                        </span>
                        {selected && <Badge>当前选择</Badge>}
                        <Badge
                          variant={group.enabled ? 'secondary' : 'outline'}
                        >
                          {proxyGroupStatusLabel(group)}
                        </Badge>
                      </div>
                      <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs tabular-nums'>
                        <span>{group.proxy_count ?? 0} 个代理</span>
                        <span>{group.available_proxy_count ?? 0} 个可用</span>
                        <span>{group.bound_channel_count} 个渠道</span>
                      </div>
                    </button>
                    <div className='flex shrink-0 items-center gap-1 self-end sm:self-auto'>
                      {!selected && (
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => {
                            props.onSelect(group.id)
                            props.onOpenChange(false)
                          }}
                        >
                          选择
                        </Button>
                      )}
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => {
                          props.onOpenChange(false)
                          props.onEdit(group)
                        }}
                      >
                        <Pencil />
                        编辑
                      </Button>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='text-destructive hover:text-destructive'
                        onClick={() => {
                          props.onOpenChange(false)
                          props.onDelete(group)
                        }}
                      >
                        <Trash2 />
                        删除
                      </Button>
                    </div>
                  </div>
                )
              })}
            </div>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  )
}

export function ProxyPoolWorkspace(props: ProxyPoolWorkspaceProps) {
  const [groupManagerOpen, setGroupManagerOpen] = useState(false)
  const [selectionMode, setSelectionMode] = useState(false)
  const [selectedProxyIds, setSelectedProxyIds] = useState<Set<number>>(
    new Set()
  )
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
  useEffect(() => {
    setSelectionMode(false)
    setSelectedProxyIds(new Set())
  }, [props.selectedGroupId])
  useEffect(() => {
    const availableIds = new Set(props.proxies.map((proxy) => proxy.id))
    setSelectedProxyIds((current) => {
      const next = new Set(
        [...current].filter((proxyId) => availableIds.has(proxyId))
      )
      if (next.size === current.size) return current
      return next
    })
  }, [props.proxies])
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
  const healthCheckProgress = Math.min(
    100,
    Math.max(
      0,
      props.healthCheckTask?.status === 'succeeded'
        ? 100
        : (props.healthCheckTask?.state?.progress ?? 0)
    )
  )
  const healthCheckProcessed =
    props.healthCheckTask?.state?.processed ??
    props.healthCheckTask?.result?.checked ??
    0
  const healthCheckTotal = props.healthCheckTask?.state?.total ?? 0
  const healthCheckScope =
    (props.healthCheckTask?.payload?.group_id ?? 0) > 0
      ? '分组代理检测'
      : '全部代理检测'
  let healthCheckButtonLabel = '立即检测全部'
  if (props.checkingAll) {
    healthCheckButtonLabel =
      props.healthCheckTask?.status === 'running'
        ? `检测中 ${healthCheckProgress}%`
        : '等待检测任务…'
  }
  let healthCheckStatusLabel = `${healthCheckProcessed} / ${healthCheckTotal || healthCheckProcessed}，${healthCheckProgress}%`
  if (props.healthCheckTask?.status === 'pending') {
    healthCheckStatusLabel = '等待执行'
  } else if (props.healthCheckTask?.status === 'failed') {
    healthCheckStatusLabel = `检测失败：${props.healthCheckTask.error || '未知原因'}`
  }
  const currentProxy = props.selectedGroup
    ? (props.proxies.find(
        (proxy) => proxy.id === props.selectedGroup?.current_proxy_id
      ) ?? null)
    : null
  const selectedProxies = props.proxies.filter((proxy) =>
    selectedProxyIds.has(proxy.id)
  )
  const connectionFailedProxies = props.proxies.filter(
    (proxy) => proxy.last_health_error.trim() !== ''
  )
  const allProxiesSelected =
    props.proxies.length > 0 && selectedProxyIds.size === props.proxies.length

  const toggleProxySelection = (proxyId: number, selected: boolean) => {
    setSelectedProxyIds((current) => {
      const next = new Set(current)
      if (selected) {
        next.add(proxyId)
      } else {
        next.delete(proxyId)
      }
      return next
    })
  }

  const closeSelectionMode = () => {
    setSelectionMode(false)
    setSelectedProxyIds(new Set())
  }

  return (
    <div className='space-y-4'>
      <Card className='shadow-none'>
        <CardContent className='space-y-4 p-4'>
          <div className='flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between'>
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
                {props.checkingAll ? (
                  <LoaderCircle className='animate-spin' />
                ) : (
                  <Activity />
                )}
                {healthCheckButtonLabel}
              </Button>
            </div>
          </div>
          {props.healthCheckTask && (
            <div className='bg-muted/40 rounded-lg border px-3 py-3'>
              <div className='mb-2 flex flex-wrap items-center justify-between gap-2 text-xs'>
                <span className='font-medium'>{healthCheckScope}</span>
                <span className='text-muted-foreground tabular-nums'>
                  {healthCheckStatusLabel}
                </span>
              </div>
              <Progress value={healthCheckProgress} />
              {props.healthCheckTask.status === 'succeeded' &&
                props.healthCheckTask.result && (
                  <p className='text-muted-foreground mt-2 text-xs tabular-nums'>
                    检测完成：正常 {props.healthCheckTask.result.healthy}{' '}
                    个，失败 {props.healthCheckTask.result.failed} 个，自动禁用{' '}
                    {props.healthCheckTask.result.unavailable} 个。
                  </p>
                )}
            </div>
          )}
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

      <div className='space-y-4'>
        <div className='bg-card flex flex-col gap-3 rounded-xl border px-4 py-3 sm:flex-row sm:items-center sm:justify-between'>
          <div className='flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center'>
            <div className='flex shrink-0 items-center gap-2'>
              <Layers3 className='text-muted-foreground size-4' />
              <span className='text-sm font-medium'>当前分组</span>
            </div>
            <NativeSelect
              className='w-full sm:max-w-72'
              value={
                props.selectedGroupId > 0 ? String(props.selectedGroupId) : ''
              }
              disabled={!props.groups.length}
              aria-label='选择代理分组'
              onChange={(event) =>
                props.onSelectGroup(Number(event.target.value))
              }
            >
              {!props.groups.length && (
                <NativeSelectOption value=''>暂无代理分组</NativeSelectOption>
              )}
              {props.groups.map((group) => (
                <NativeSelectOption key={group.id} value={String(group.id)}>
                  {group.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            {props.selectedGroup && (
              <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs tabular-nums'>
                <Badge
                  variant={
                    props.selectedGroup.enabled ? 'secondary' : 'outline'
                  }
                >
                  {proxyGroupStatusLabel(props.selectedGroup)}
                </Badge>
                <span>{props.selectedGroup.proxy_count ?? 0} 个代理</span>
                <span>
                  {props.selectedGroup.available_proxy_count ?? 0} 个可用
                </span>
                <span>{props.selectedGroup.bound_channel_count} 个渠道</span>
              </div>
            )}
          </div>
          <div className='flex shrink-0 flex-wrap gap-2'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => setGroupManagerOpen(true)}
            >
              <Layers3 />
              管理分组
            </Button>
            <Button size='sm' onClick={props.onCreateGroup}>
              <Plus />
              新建分组
            </Button>
          </div>
        </div>

        {!props.selectedGroup ? (
          <Card className='shadow-none'>
            <CardContent className='flex min-h-96 flex-col items-center justify-center text-center'>
              <div className='bg-muted flex size-12 items-center justify-center rounded-xl'>
                <Network className='text-muted-foreground size-6' />
              </div>
              <h3 className='mt-4 text-base font-semibold'>请选择代理分组</h3>
              <p className='text-muted-foreground mt-1 max-w-sm text-sm'>
                使用上方分组选择器查看代理，或者新建一个分组开始配置。
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
                    variant={selectionMode ? 'secondary' : 'outline'}
                    size='sm'
                    disabled={!props.proxies.length || props.batchOperating}
                    onClick={() => {
                      if (selectionMode) {
                        closeSelectionMode()
                      } else {
                        setSelectionMode(true)
                      }
                    }}
                  >
                    {selectionMode ? <X /> : <ListChecks />}
                    {selectionMode ? '退出选择' : '选择代理'}
                  </Button>
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
                    {props.checkingAll ? (
                      <LoaderCircle className='animate-spin' />
                    ) : (
                      <Activity />
                    )}
                    {props.checkingAll ? '等待检测完成…' : '检测本组'}
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
              <ProxyLatencyTrendChart
                groupId={props.selectedGroup.id}
                proxies={props.proxies}
              />

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

              {selectionMode && (
                <div className='border-primary/20 bg-primary/5 flex flex-col gap-3 rounded-xl border px-4 py-3 lg:flex-row lg:items-center lg:justify-between'>
                  <div className='flex items-center gap-3'>
                    <Checkbox
                      checked={allProxiesSelected}
                      indeterminate={
                        selectedProxyIds.size > 0 && !allProxiesSelected
                      }
                      disabled={props.batchOperating}
                      aria-label='选择当前分组全部代理'
                      onCheckedChange={(checked) =>
                        setSelectedProxyIds(
                          checked
                            ? new Set(props.proxies.map((proxy) => proxy.id))
                            : new Set()
                        )
                      }
                    />
                    <div>
                      <p className='text-sm font-medium'>
                        {props.batchOperating
                          ? '正在处理批量操作…'
                          : `已选择 ${selectedProxyIds.size} 个代理`}
                      </p>
                      <p className='text-muted-foreground text-xs'>
                        可批量检测、恢复正常、暂停、清空观察计数或删除。
                      </p>
                      <Button
                        variant='link'
                        size='sm'
                        className='mt-1 h-auto px-0 py-1'
                        disabled={
                          !connectionFailedProxies.length ||
                          props.batchOperating
                        }
                        onClick={() =>
                          setSelectedProxyIds(
                            new Set(
                              connectionFailedProxies.map((proxy) => proxy.id)
                            )
                          )
                        }
                      >
                        选择连接失败的代理（{connectionFailedProxies.length}）
                      </Button>
                    </div>
                  </div>
                  <div className='flex flex-wrap gap-2'>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={!selectedProxies.length || props.batchOperating}
                      onClick={() => props.onBatchCheck(selectedProxies)}
                    >
                      <Activity />
                      检测
                    </Button>
                    <Button
                      size='sm'
                      disabled={!selectedProxies.length || props.batchOperating}
                      onClick={() =>
                        props.onBatchSetPaused(selectedProxies, false)
                      }
                    >
                      <Play />
                      恢复正常
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={!selectedProxies.length || props.batchOperating}
                      onClick={() =>
                        props.onBatchSetPaused(selectedProxies, true)
                      }
                    >
                      <Pause />
                      暂停
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={!selectedProxies.length || props.batchOperating}
                      onClick={() => props.onBatchReset(selectedProxies)}
                    >
                      <RotateCcw />
                      清空计数
                    </Button>
                    <Button
                      variant='destructive'
                      size='sm'
                      disabled={!selectedProxies.length || props.batchOperating}
                      onClick={() => props.onBatchDelete(selectedProxies)}
                    >
                      <Trash2 />
                      删除
                    </Button>
                  </div>
                </div>
              )}

              <div className='overflow-x-auto rounded-xl border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      {selectionMode && (
                        <TableHead className='w-12'>
                          <Checkbox
                            checked={allProxiesSelected}
                            indeterminate={
                              selectedProxyIds.size > 0 && !allProxiesSelected
                            }
                            disabled={props.batchOperating}
                            aria-label='选择当前分组全部代理'
                            onCheckedChange={(checked) =>
                              setSelectedProxyIds(
                                checked
                                  ? new Set(
                                      props.proxies.map((proxy) => proxy.id)
                                    )
                                  : new Set()
                              )
                            }
                          />
                        </TableHead>
                      )}
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
                        <TableCell
                          colSpan={selectionMode ? 7 : 6}
                          className='h-64 text-center'
                        >
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
                          {selectionMode && (
                            <TableCell className='w-12'>
                              <Checkbox
                                checked={selectedProxyIds.has(proxy.id)}
                                disabled={props.batchOperating}
                                aria-label={`选择代理 ${proxy.name}`}
                                onCheckedChange={(checked) =>
                                  toggleProxySelection(proxy.id, !!checked)
                                }
                              />
                            </TableCell>
                          )}
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
                              {proxyHealthCheckLabel(proxy)}
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
      <ProxyGroupManagerDialog
        open={groupManagerOpen}
        groups={props.groups}
        selectedGroupId={props.selectedGroupId}
        onOpenChange={setGroupManagerOpen}
        onSelect={props.onSelectGroup}
        onCreate={props.onCreateGroup}
        onEdit={props.onEditGroup}
        onDelete={props.onDeleteGroup}
      />
    </div>
  )
}
