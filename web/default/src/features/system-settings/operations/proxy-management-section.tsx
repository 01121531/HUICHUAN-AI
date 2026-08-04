import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  History,
  Link2,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  ServerCog,
  Shuffle,
  Trash2,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getChannels } from '@/features/channels/api'

import { SettingsSection } from '../components/settings-section'
import {
  createManagedProxy,
  createProxyGroup,
  checkManagedProxy,
  deleteManagedProxy,
  deleteProxyBinding,
  deleteProxyGroup,
  listGroupProxies,
  listProxyLogAnalyses,
  listProxyBindings,
  listProxyGroups,
  listProxyStateEvents,
  listProxyUpstreamAttempts,
  setManagedProxyPaused,
  switchProxyGroup,
  updateManagedProxy,
  updateProxyGroup,
  upsertProxyBinding,
} from './proxy-management-api'
import type {
  ManagedProxy,
  ManagedProxyInput,
  ProxyBinding,
  ProxyGroup,
  ProxyGroupInput,
  ProxyLogAnalysis,
  ProxyStateEvent,
  ProxyUpstreamAttempt,
} from './proxy-management-types'

const defaultGroupInput = (): ProxyGroupInput => ({
  name: '',
  enabled: true,
  max_requests: 500,
  max_duration_seconds: 1800,
  switch_wait_seconds: 30,
  max_waiting_requests: 500,
  health_check_interval: 300,
  health_failure_threshold: 2,
  consecutive_timeout_threshold: 3,
  window_size: 10,
  window_timeout_ratio: 0.6,
  base_cooldown_seconds: 600,
  max_cooldown_seconds: 7200,
  recovery_success_count: 2,
  allow_direct_fallback: false,
})

const EMPTY_PROXY_GROUPS: ProxyGroup[] = []
const EMPTY_MANAGED_PROXIES: ManagedProxy[] = []

const proxyStatusLabels: Record<string, string> = {
  available: '正常',
  watching: '观察中',
  paused: '已暂停',
  cooling: '冷却中',
  recovering: '恢复测试',
  unavailable: '不可用',
  disabled: '已停用',
}

function proxyStatusLabel(proxy: ManagedProxy) {
  if (!proxy.enabled) return '手动停用'
  return proxyStatusLabels[proxy.status] ?? proxy.status ?? '正常'
}

const proxyEventLabels: Record<string, string> = {
  health_status_changed: '健康状态变化',
  auto_paused: '自动暂停',
  recovery_started: '开始恢复测试',
  recovery_failed: '恢复失败',
  recovery_succeeded: '恢复成功',
  health_unavailable: '连接检测不可用',
  manual_paused: '手动暂停',
  manual_resumed: '手动恢复',
  manual_recovery_requested: '手动恢复检测',
}

const proxyAttemptResultLabels: Record<string, string> = {
  success: '请求成功',
  http_error: '上游 HTTP 错误',
  network_error: '网络错误',
  invalid_response: '响应异常',
}

const proxyReasonLabels: Record<string, string> = {
  first_response: '首字响应超过 10 秒',
  duration: '小输出总耗时达到 30 秒',
  throughput: '大输出 TPS 低于 15',
  request_failed: '检测请求失败',
  client_init_failed: '代理客户端初始化失败',
  invalid_check_url: '检测地址无效',
  http_status_400: '检测接口返回 400',
  response_read_failed: '检测响应读取失败',
  invalid_exit_ip: '未取得有效出口 IP',
  exit_ip_mismatch: '出口 IP 与预期不一致',
  manual: '管理员手动操作',
}

function proxyReasonLabel(reason: string) {
  if (!reason) return '—'
  return reason
    .split(',')
    .map((item) =>
      item.startsWith('http_status_')
        ? `检测接口返回 ${item.slice('http_status_'.length)}`
        : (proxyReasonLabels[item] ?? item)
    )
    .join('；')
}

function formatProxyTime(timestamp: number) {
  if (!timestamp) return '—'
  return new Date(timestamp * 1000).toLocaleString('zh-CN', { hour12: false })
}

function proxyAnalysisResultLabel(analysis: ProxyLogAnalysis) {
  if (analysis.is_timeout) return '红色 / 超时'
  if (analysis.counted) return '正常'
  return '忽略业务错误'
}

function groupToInput(group: ProxyGroup): ProxyGroupInput {
  return {
    name: group.name,
    enabled: group.enabled,
    max_requests: group.max_requests,
    max_duration_seconds: group.max_duration_seconds,
    switch_wait_seconds: group.switch_wait_seconds,
    max_waiting_requests: group.max_waiting_requests,
    health_check_interval: group.health_check_interval,
    health_failure_threshold: group.health_failure_threshold,
    consecutive_timeout_threshold: group.consecutive_timeout_threshold,
    window_size: group.window_size,
    window_timeout_ratio: group.window_timeout_ratio,
    base_cooldown_seconds: group.base_cooldown_seconds,
    max_cooldown_seconds: group.max_cooldown_seconds,
    recovery_success_count: group.recovery_success_count,
    allow_direct_fallback: group.allow_direct_fallback,
  }
}

function NumberField(props: {
  label: string
  value: number
  min?: number
  step?: number
  onChange: (value: number) => void
}) {
  return (
    <div className='space-y-1.5'>
      <Label>{props.label}</Label>
      <Input
        type='number'
        min={props.min ?? 0}
        step={props.step ?? 1}
        value={props.value}
        onChange={(event) => props.onChange(Number(event.target.value))}
      />
    </div>
  )
}

function GroupDialog(props: {
  open: boolean
  group: ProxyGroup | null
  saving: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: ProxyGroupInput) => void
}) {
  const [form, setForm] = useState<ProxyGroupInput>(defaultGroupInput)

  useEffect(() => {
    if (props.open) {
      setForm(props.group ? groupToInput(props.group) : defaultGroupInput())
    }
  }, [props.open, props.group])

  const setNumber = (key: keyof ProxyGroupInput, value: number) =>
    setForm((current) => ({ ...current, [key]: value }))

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {props.group ? '编辑代理分组' : '新增代理分组'}
          </DialogTitle>
          <DialogDescription>
            设置轮换、红色判定、连接检测和冷却恢复参数。
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4'>
          <div className='space-y-1.5'>
            <Label>分组名称</Label>
            <Input
              value={form.name}
              placeholder='例如：海外高速代理'
              onChange={(event) =>
                setForm((current) => ({ ...current, name: event.target.value }))
              }
            />
          </div>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
            <NumberField
              label='单代理最大请求数'
              value={form.max_requests}
              min={1}
              onChange={(v) => setNumber('max_requests', v)}
            />
            <NumberField
              label='单代理最长使用时间（秒）'
              value={form.max_duration_seconds}
              min={1}
              onChange={(v) => setNumber('max_duration_seconds', v)}
            />
            <NumberField
              label='切换等待上限（秒）'
              value={form.switch_wait_seconds}
              min={1}
              onChange={(v) => setNumber('switch_wait_seconds', v)}
            />
            <NumberField
              label='最大等待请求数'
              value={form.max_waiting_requests}
              min={1}
              onChange={(v) => setNumber('max_waiting_requests', v)}
            />
            <NumberField
              label='健康检测周期（秒）'
              value={form.health_check_interval}
              min={1}
              onChange={(v) => setNumber('health_check_interval', v)}
            />
            <NumberField
              label='连续检测失败阈值'
              value={form.health_failure_threshold}
              min={1}
              onChange={(v) => setNumber('health_failure_threshold', v)}
            />
            <NumberField
              label='连续超时阈值'
              value={form.consecutive_timeout_threshold}
              min={1}
              onChange={(v) => setNumber('consecutive_timeout_threshold', v)}
            />
            <NumberField
              label='滑动窗口大小'
              value={form.window_size}
              min={1}
              onChange={(v) => setNumber('window_size', v)}
            />
            <NumberField
              label='窗口超时比例'
              value={form.window_timeout_ratio}
              min={0.01}
              step={0.01}
              onChange={(v) => setNumber('window_timeout_ratio', v)}
            />
            <NumberField
              label='基础冷却时间（秒）'
              value={form.base_cooldown_seconds}
              min={1}
              onChange={(v) => setNumber('base_cooldown_seconds', v)}
            />
            <NumberField
              label='最大冷却时间（秒）'
              value={form.max_cooldown_seconds}
              min={1}
              onChange={(v) => setNumber('max_cooldown_seconds', v)}
            />
            <NumberField
              label='恢复所需成功次数'
              value={form.recovery_success_count}
              min={1}
              onChange={(v) => setNumber('recovery_success_count', v)}
            />
          </div>
          <div className='grid gap-3 rounded-lg border p-3 sm:grid-cols-2'>
            <label className='flex items-center justify-between gap-3 text-sm'>
              <span>启用此分组</span>
              <Switch
                checked={form.enabled}
                onCheckedChange={(enabled) =>
                  setForm((current) => ({ ...current, enabled }))
                }
              />
            </label>
            <label className='flex items-center justify-between gap-3 text-sm'>
              <span>无代理时允许直连</span>
              <Switch
                checked={form.allow_direct_fallback}
                onCheckedChange={(allow_direct_fallback) =>
                  setForm((current) => ({ ...current, allow_direct_fallback }))
                }
              />
            </label>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={props.saving || !form.name.trim()}
            onClick={() => props.onSave(form)}
          >
            {props.saving ? '保存中…' : '保存分组'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ProxyDialog(props: {
  open: boolean
  groupId: number
  proxy: ManagedProxy | null
  saving: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: ManagedProxyInput) => void
}) {
  const [form, setForm] = useState<ManagedProxyInput>({
    group_id: props.groupId,
    name: '',
    protocol: 'socks5',
    host: '',
    port: 1080,
    username: '',
    password: '',
    enabled: true,
    sort: 0,
    expected_exit_ip: '',
  })

  useEffect(() => {
    if (!props.open) return
    setForm(
      props.proxy
        ? {
            group_id: props.proxy.group_id,
            name: props.proxy.name,
            protocol: props.proxy.protocol,
            host: props.proxy.host,
            port: props.proxy.port,
            username: props.proxy.username ?? '',
            password: '',
            enabled: props.proxy.enabled,
            sort: props.proxy.sort,
            expected_exit_ip: props.proxy.expected_exit_ip ?? '',
          }
        : {
            group_id: props.groupId,
            name: '',
            protocol: 'socks5',
            host: '',
            port: 1080,
            username: '',
            password: '',
            enabled: true,
            sort: 0,
            expected_exit_ip: '',
          }
    )
  }, [props.open, props.proxy, props.groupId])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{props.proxy ? '编辑代理' : '新增代理'}</DialogTitle>
          <DialogDescription>
            密码保存后不会在页面或普通日志中显示。
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-4 sm:grid-cols-2'>
          <div className='space-y-1.5 sm:col-span-2'>
            <Label>代理名称</Label>
            <Input
              value={form.name}
              placeholder='例如：香港出口 1'
              onChange={(e) => setForm((v) => ({ ...v, name: e.target.value }))}
            />
          </div>
          <div className='space-y-1.5'>
            <Label>协议</Label>
            <NativeSelect
              className='w-full'
              value={form.protocol}
              onChange={(e) =>
                setForm((v) => ({ ...v, protocol: e.target.value }))
              }
            >
              <NativeSelectOption value='http'>HTTP</NativeSelectOption>
              <NativeSelectOption value='https'>HTTPS</NativeSelectOption>
              <NativeSelectOption value='socks4'>SOCKS4</NativeSelectOption>
              <NativeSelectOption value='socks5'>SOCKS5</NativeSelectOption>
              <NativeSelectOption value='socks5h'>SOCKS5H</NativeSelectOption>
            </NativeSelect>
          </div>
          <NumberField
            label='排序'
            value={form.sort}
            onChange={(sort) => setForm((v) => ({ ...v, sort }))}
          />
          <div className='space-y-1.5'>
            <Label>主机或 IP</Label>
            <Input
              value={form.host}
              placeholder='127.0.0.1'
              onChange={(e) => setForm((v) => ({ ...v, host: e.target.value }))}
            />
          </div>
          <NumberField
            label='端口'
            value={form.port}
            min={1}
            onChange={(port) => setForm((v) => ({ ...v, port }))}
          />
          <div className='space-y-1.5 sm:col-span-2'>
            <Label>预期出口 IP（可选）</Label>
            <Input
              value={form.expected_exit_ip}
              placeholder='支持单个 IP、CIDR 或逗号分隔多个值'
              onChange={(e) =>
                setForm((v) => ({ ...v, expected_exit_ip: e.target.value }))
              }
            />
          </div>
          <div className='space-y-1.5'>
            <Label>用户名</Label>
            <Input
              autoComplete='off'
              value={form.username}
              onChange={(e) =>
                setForm((v) => ({ ...v, username: e.target.value }))
              }
            />
          </div>
          <div className='space-y-1.5'>
            <Label>{props.proxy ? '新密码（留空不修改）' : '密码'}</Label>
            <Input
              type='password'
              autoComplete='new-password'
              value={form.password}
              onChange={(e) =>
                setForm((v) => ({ ...v, password: e.target.value }))
              }
            />
          </div>
          <label className='flex items-center justify-between gap-3 rounded-lg border p-3 text-sm sm:col-span-2'>
            <span>启用此代理</span>
            <Switch
              checked={form.enabled}
              onCheckedChange={(enabled) => setForm((v) => ({ ...v, enabled }))}
            />
          </label>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={
              props.saving ||
              !form.name.trim() ||
              !form.host.trim() ||
              form.port < 1
            }
            onClick={() => props.onSave(form)}
          >
            {props.saving ? '保存中…' : '保存代理'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function BindingDialog(props: {
  open: boolean
  groups: ProxyGroup[]
  channels: Array<{ id: number; name: string }>
  saving: boolean
  onOpenChange: (open: boolean) => void
  onSave: (channelId: number, groupId: number) => void
}) {
  const [channelId, setChannelId] = useState(0)
  const [groupId, setGroupId] = useState(0)

  useEffect(() => {
    if (!props.open) return
    setChannelId(props.channels[0]?.id ?? 0)
    setGroupId(props.groups[0]?.id ?? 0)
  }, [props.open, props.channels, props.groups])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>绑定渠道到代理组</DialogTitle>
          <DialogDescription>
            绑定后，该渠道不再使用列表序号，而是使用具有稳定 ID 的代理实体。
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4'>
          <div className='space-y-1.5'>
            <Label>渠道</Label>
            <NativeSelect
              className='w-full'
              value={String(channelId)}
              onChange={(e) => setChannelId(Number(e.target.value))}
            >
              {props.channels.map((channel) => (
                <NativeSelectOption key={channel.id} value={channel.id}>
                  {channel.name}（#{channel.id}）
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className='space-y-1.5'>
            <Label>代理分组</Label>
            <NativeSelect
              className='w-full'
              value={String(groupId)}
              onChange={(e) => setGroupId(Number(e.target.value))}
            >
              {props.groups.map((group) => (
                <NativeSelectOption key={group.id} value={group.id}>
                  {group.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={props.saving || channelId <= 0 || groupId <= 0}
            onClick={() => props.onSave(channelId, groupId)}
          >
            {props.saving ? '绑定中…' : '确认绑定'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type DeleteTarget =
  | { kind: 'group'; id: number; name: string }
  | { kind: 'proxy'; id: number; name: string }
  | { kind: 'binding'; id: number; name: string }

export function ProxyManagementSection() {
  const queryClient = useQueryClient()
  const [selectedGroupId, setSelectedGroupId] = useState(0)
  const [selectedProxyId, setSelectedProxyId] = useState(0)
  const [groupDialogOpen, setGroupDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<ProxyGroup | null>(null)
  const [proxyDialogOpen, setProxyDialogOpen] = useState(false)
  const [editingProxy, setEditingProxy] = useState<ManagedProxy | null>(null)
  const [bindingDialogOpen, setBindingDialogOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)

  const groupsQuery = useQuery({
    queryKey: ['proxy-groups'],
    queryFn: async () => (await listProxyGroups()).data ?? [],
    refetchInterval: 2000,
  })
  const groups = groupsQuery.data ?? EMPTY_PROXY_GROUPS

  useEffect(() => {
    if (!groups.length) {
      setSelectedGroupId(0)
    } else if (!groups.some((group) => group.id === selectedGroupId)) {
      setSelectedGroupId(groups[0].id)
    }
  }, [groups, selectedGroupId])

  const proxiesQuery = useQuery({
    queryKey: ['managed-proxies', selectedGroupId],
    queryFn: async () => (await listGroupProxies(selectedGroupId)).data ?? [],
    enabled: selectedGroupId > 0,
  })
  const bindingsQuery = useQuery({
    queryKey: ['proxy-bindings'],
    queryFn: async () => (await listProxyBindings()).data ?? [],
  })
  const channelsQuery = useQuery({
    queryKey: ['proxy-binding-channels'],
    queryFn: async () =>
      (await getChannels({ p: 1, page_size: 1000 })).data?.items ?? [],
  })
  const analysesQuery = useQuery({
    queryKey: ['proxy-log-analyses', selectedProxyId],
    queryFn: async () =>
      (await listProxyLogAnalyses(selectedProxyId, 50)).data ?? [],
    enabled: selectedProxyId > 0,
  })
  const eventsQuery = useQuery({
    queryKey: ['proxy-state-events', selectedProxyId],
    queryFn: async () =>
      (await listProxyStateEvents(selectedProxyId, 50)).data ?? [],
    enabled: selectedProxyId > 0,
  })
  const attemptsQuery = useQuery({
    queryKey: ['proxy-upstream-attempts', selectedProxyId],
    queryFn: async () =>
      (await listProxyUpstreamAttempts(selectedProxyId, 50)).data ?? [],
    enabled: selectedProxyId > 0,
  })

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['proxy-groups'] })
    queryClient.invalidateQueries({ queryKey: ['managed-proxies'] })
    queryClient.invalidateQueries({ queryKey: ['proxy-bindings'] })
    queryClient.invalidateQueries({ queryKey: ['proxy-log-analyses'] })
    queryClient.invalidateQueries({ queryKey: ['proxy-state-events'] })
    queryClient.invalidateQueries({ queryKey: ['proxy-upstream-attempts'] })
  }

  const groupMutation = useMutation({
    mutationFn: async (data: ProxyGroupInput) =>
      editingGroup
        ? updateProxyGroup(editingGroup.id, data)
        : createProxyGroup(data),
    onSuccess: () => {
      toast.success(editingGroup ? '代理分组已更新' : '代理分组已创建')
      setGroupDialogOpen(false)
      refresh()
    },
  })
  const proxyMutation = useMutation({
    mutationFn: async (data: ManagedProxyInput) =>
      editingProxy
        ? updateManagedProxy(editingProxy.id, data)
        : createManagedProxy(data),
    onSuccess: () => {
      toast.success(editingProxy ? '代理已更新' : '代理已创建')
      setProxyDialogOpen(false)
      refresh()
    },
  })
  const checkMutation = useMutation({
    mutationFn: (proxyId: number) => checkManagedProxy(proxyId),
    onSuccess: (response) => {
      const result = response.data
      if (result?.success) {
        toast.success(
          `检测成功：出口 ${result.exit_ip || '未知'}，耗时 ${result.latency_ms} ms`
        )
      } else {
        toast.error(`检测失败：${result?.failure_reason || '未知原因'}`)
      }
      refresh()
    },
  })
  const pauseMutation = useMutation({
    mutationFn: ({ proxyId, paused }: { proxyId: number; paused: boolean }) =>
      setManagedProxyPaused(proxyId, paused),
    onSuccess: (_response, variables) => {
      toast.success(variables.paused ? '代理已暂停' : '代理已恢复')
      refresh()
    },
  })
  const switchMutation = useMutation({
    mutationFn: (groupId: number) => switchProxyGroup(groupId),
    onSuccess: (response) => {
      toast.success(`已切换到代理 ID ${response.data?.current_proxy_id || '—'}`)
      refresh()
    },
  })
  const bindingMutation = useMutation({
    mutationFn: ({
      channelId,
      groupId,
    }: {
      channelId: number
      groupId: number
    }) =>
      upsertProxyBinding(channelId, { proxy_group_id: groupId, enabled: true }),
    onSuccess: () => {
      toast.success('渠道绑定已保存')
      setBindingDialogOpen(false)
      refresh()
    },
  })
  const deleteMutation = useMutation({
    mutationFn: async (target: DeleteTarget) => {
      if (target.kind === 'group') return deleteProxyGroup(target.id)
      if (target.kind === 'proxy') return deleteManagedProxy(target.id)
      return deleteProxyBinding(target.id)
    },
    onSuccess: () => {
      toast.success('已删除')
      setDeleteTarget(null)
      refresh()
    },
  })

  const selectedGroup = useMemo(
    () => groups.find((group) => group.id === selectedGroupId) ?? null,
    [groups, selectedGroupId]
  )
  const proxies = proxiesQuery.data ?? EMPTY_MANAGED_PROXIES
  const bindings = bindingsQuery.data ?? []
  const analyses = analysesQuery.data ?? []
  const events = eventsQuery.data ?? []
  const attempts = attemptsQuery.data ?? []

  useEffect(() => {
    if (!proxies.length) {
      setSelectedProxyId(0)
    } else if (!proxies.some((proxy) => proxy.id === selectedProxyId)) {
      setSelectedProxyId(proxies[0].id)
    }
  }, [proxies, selectedProxyId])

  return (
    <SettingsSection title='代理管理与自动切换'>
      <div className='grid gap-3 sm:grid-cols-3'>
        <Card>
          <CardHeader className='pb-2'>
            <CardTitle className='text-sm font-medium'>代理分组</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold'>
            {groups.length}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className='pb-2'>
            <CardTitle className='text-sm font-medium'>当前分组代理</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold'>
            {proxies.length}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className='pb-2'>
            <CardTitle className='text-sm font-medium'>已绑定渠道</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold'>
            {bindings.length}
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue='groups'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <TabsList>
            <TabsTrigger value='groups'>
              <ServerCog />
              分组与代理
            </TabsTrigger>
            <TabsTrigger value='bindings'>
              <Link2 />
              渠道绑定
            </TabsTrigger>
            <TabsTrigger value='records'>
              <History />
              状态与记录
            </TabsTrigger>
          </TabsList>
          <Button
            variant='outline'
            size='sm'
            onClick={refresh}
            disabled={groupsQuery.isFetching || bindingsQuery.isFetching}
          >
            <RefreshCw className='mr-1.5 h-4 w-4' />
            刷新
          </Button>
        </div>

        <TabsContent value='groups' className='space-y-4 pt-3'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <NativeSelect
              className='min-w-64'
              value={String(selectedGroupId)}
              onChange={(e) => setSelectedGroupId(Number(e.target.value))}
            >
              {!groups.length && (
                <NativeSelectOption value='0'>暂无代理分组</NativeSelectOption>
              )}
              {groups.map((group) => (
                <NativeSelectOption key={group.id} value={group.id}>
                  {group.name}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <div className='flex gap-2'>
              {selectedGroup && (
                <>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={switchMutation.isPending || proxies.length < 2}
                    onClick={() => switchMutation.mutate(selectedGroup.id)}
                  >
                    <Shuffle className='mr-1.5 h-4 w-4' />
                    立即切换
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => {
                      setEditingGroup(selectedGroup)
                      setGroupDialogOpen(true)
                    }}
                  >
                    <Pencil className='mr-1.5 h-4 w-4' />
                    编辑分组
                  </Button>
                </>
              )}
              <Button
                size='sm'
                onClick={() => {
                  setEditingGroup(null)
                  setGroupDialogOpen(true)
                }}
              >
                <Plus className='mr-1.5 h-4 w-4' />
                新增分组
              </Button>
            </div>
          </div>

          {selectedGroup && (
            <div className='grid gap-3 rounded-lg border p-3 text-sm sm:grid-cols-2 lg:grid-cols-4'>
              <div>
                <span className='text-muted-foreground'>状态：</span>
                {selectedGroup.enabled ? '启用' : '停用'}
              </div>
              <div>
                <span className='text-muted-foreground'>当前代理 ID：</span>
                {selectedGroup.current_proxy_id || '尚未选择'}
              </div>
              <div>
                <span className='text-muted-foreground'>轮换：</span>
                {selectedGroup.max_requests} 次 /{' '}
                {selectedGroup.max_duration_seconds} 秒
              </div>
              <div>
                <span className='text-muted-foreground'>直连回退：</span>
                {selectedGroup.allow_direct_fallback ? '允许' : '禁止'}
              </div>
            </div>
          )}

          {selectedGroup && (
            <div className='grid gap-3 rounded-lg border border-dashed p-3 text-sm sm:grid-cols-2 lg:grid-cols-4'>
              <div>
                <span className='text-muted-foreground'>全局等待请求：</span>
                {selectedGroup.waiting_requests} /{' '}
                {selectedGroup.max_waiting_requests}
              </div>
              <div>
                <span className='text-muted-foreground'>最近等待超时：</span>
                {selectedGroup.nearest_wait_remaining_seconds > 0
                  ? `${selectedGroup.nearest_wait_remaining_seconds} 秒`
                  : '无等待请求'}
              </div>
              <div>
                <span className='text-muted-foreground'>最长剩余等待：</span>
                {selectedGroup.longest_wait_remaining_seconds > 0
                  ? `${selectedGroup.longest_wait_remaining_seconds} 秒`
                  : '—'}
              </div>
              <div>
                <span className='text-muted-foreground'>切换状态：</span>
                {selectedGroup.status === 'switching'
                  ? '正在切换'
                  : '可接收请求'}
              </div>
            </div>
          )}

          <div className='flex items-center justify-between'>
            <p className='text-muted-foreground text-sm'>
              代理密码不会回显；修改时留空即可保留原密码。
            </p>
            <Button
              size='sm'
              disabled={!selectedGroup}
              onClick={() => {
                setEditingProxy(null)
                setProxyDialogOpen(true)
              }}
            >
              <Plus className='mr-1.5 h-4 w-4' />
              新增代理
            </Button>
          </div>
          <div className='overflow-x-auto rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>协议</TableHead>
                  <TableHead>地址</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>连续红色</TableHead>
                  <TableHead>窗口红色率</TableHead>
                  <TableHead>最近指标</TableHead>
                  <TableHead>累计请求 / 红色</TableHead>
                  <TableHead className='text-right'>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {!proxies.length ? (
                  <TableRow>
                    <TableCell
                      colSpan={9}
                      className='text-muted-foreground py-10 text-center'
                    >
                      当前分组还没有代理
                    </TableCell>
                  </TableRow>
                ) : (
                  proxies.map((proxy) => (
                    <TableRow key={proxy.id}>
                      <TableCell className='font-medium'>
                        {proxy.name}
                        <div className='text-muted-foreground text-xs'>
                          ID {proxy.id}
                        </div>
                      </TableCell>
                      <TableCell className='uppercase'>
                        {proxy.protocol}
                      </TableCell>
                      <TableCell className='font-mono'>
                        {proxy.host}:{proxy.port}
                      </TableCell>
                      <TableCell className='whitespace-nowrap'>
                        {proxyStatusLabel(proxy)}
                        <div className='text-muted-foreground text-xs'>
                          {proxy.last_check_at > 0
                            ? `检测 ${proxy.last_check_latency_ms} ms`
                            : '尚未检测'}
                        </div>
                        {proxy.last_exit_ip && (
                          <div className='text-muted-foreground font-mono text-xs'>
                            {proxy.last_exit_ip}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>{proxy.consecutive_timeouts}</TableCell>
                      <TableCell>
                        {proxy.window_samples > 0
                          ? `${Math.round(proxy.window_timeout_ratio * 100)}%（${proxy.window_timeouts}/${proxy.window_samples}）`
                          : '暂无样本'}
                      </TableCell>
                      <TableCell className='whitespace-nowrap'>
                        {proxy.last_frt_ms > 0
                          ? `首字 ${proxy.last_frt_ms} ms`
                          : '首字 —'}
                        <div className='text-muted-foreground text-xs'>
                          {proxy.last_tps > 0
                            ? `TPS ${proxy.last_tps.toFixed(1)}`
                            : 'TPS —'}
                        </div>
                      </TableCell>
                      <TableCell>
                        {proxy.total_requests} / {proxy.total_timeouts}
                      </TableCell>
                      <TableCell className='text-right'>
                        <div className='flex justify-end gap-1'>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label='立即检测代理'
                            disabled={checkMutation.isPending}
                            onClick={() => checkMutation.mutate(proxy.id)}
                          >
                            <Activity />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label={
                              proxy.status === 'paused'
                                ? '恢复代理'
                                : '暂停代理'
                            }
                            disabled={pauseMutation.isPending}
                            onClick={() =>
                              pauseMutation.mutate({
                                proxyId: proxy.id,
                                paused: proxy.status !== 'paused',
                              })
                            }
                          >
                            {proxy.status === 'paused' ? <Play /> : <Pause />}
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label='编辑代理'
                            onClick={() => {
                              setEditingProxy(proxy)
                              setProxyDialogOpen(true)
                            }}
                          >
                            <Pencil />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label='删除代理'
                            onClick={() =>
                              setDeleteTarget({
                                kind: 'proxy',
                                id: proxy.id,
                                name: proxy.name,
                              })
                            }
                          >
                            <Trash2 />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          {selectedGroup && (
            <Button
              variant='destructive'
              size='sm'
              onClick={() =>
                setDeleteTarget({
                  kind: 'group',
                  id: selectedGroup.id,
                  name: selectedGroup.name,
                })
              }
            >
              删除当前分组
            </Button>
          )}
        </TabsContent>

        <TabsContent value='bindings' className='space-y-4 pt-3'>
          <div className='flex items-center justify-between gap-3'>
            <p className='text-muted-foreground text-sm'>
              每个渠道只能绑定一个代理组，重复绑定会更新原设置。
            </p>
            <Button
              size='sm'
              disabled={!groups.length || !channelsQuery.data?.length}
              onClick={() => setBindingDialogOpen(true)}
            >
              <Plus className='mr-1.5 h-4 w-4' />
              绑定渠道
            </Button>
          </div>
          <div className='rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>渠道</TableHead>
                  <TableHead>代理分组</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead className='text-right'>操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {!bindings.length ? (
                  <TableRow>
                    <TableCell
                      colSpan={4}
                      className='text-muted-foreground py-10 text-center'
                    >
                      暂无渠道绑定
                    </TableCell>
                  </TableRow>
                ) : (
                  bindings.map((binding: ProxyBinding) => (
                    <TableRow key={binding.id}>
                      <TableCell className='font-medium'>
                        {binding.channel_name || `渠道 #${binding.channel_id}`}
                        <div className='text-muted-foreground text-xs'>
                          ID {binding.channel_id}
                        </div>
                      </TableCell>
                      <TableCell>
                        {binding.proxy_group_name ||
                          `分组 #${binding.proxy_group_id}`}
                      </TableCell>
                      <TableCell>{binding.enabled ? '启用' : '停用'}</TableCell>
                      <TableCell className='text-right'>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label='解除绑定'
                          onClick={() =>
                            setDeleteTarget({
                              kind: 'binding',
                              id: binding.channel_id,
                              name:
                                binding.channel_name ||
                                `渠道 #${binding.channel_id}`,
                            })
                          }
                        >
                          <Trash2 />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </TabsContent>

        <TabsContent value='records' className='space-y-4 pt-3'>
          <div className='flex flex-wrap items-end gap-3'>
            <div className='min-w-64 space-y-1.5'>
              <Label>查看代理</Label>
              <NativeSelect
                className='w-full'
                value={String(selectedProxyId)}
                onChange={(event) =>
                  setSelectedProxyId(Number(event.target.value))
                }
              >
                {!proxies.length && (
                  <NativeSelectOption value='0'>
                    当前分组暂无代理
                  </NativeSelectOption>
                )}
                {proxies.map((proxy) => (
                  <NativeSelectOption key={proxy.id} value={proxy.id}>
                    {proxy.name}（#{proxy.id}）
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            <p className='text-muted-foreground text-sm'>
              记录来自通用日志红色判定和连接检测，不包含代理密码。
            </p>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className='text-base'>最近真实上游尝试</CardTitle>
            </CardHeader>
            <CardContent className='overflow-x-auto p-0'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>发起时间</TableHead>
                    <TableHead>请求 / 次序</TableHead>
                    <TableHead>渠道</TableHead>
                    <TableHead>结果</TableHead>
                    <TableHead>耗时</TableHead>
                    <TableHead>上游请求 ID</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {!attempts.length ? (
                    <TableRow>
                      <TableCell
                        colSpan={6}
                        className='text-muted-foreground py-8 text-center'
                      >
                        暂无真实上游尝试记录
                      </TableCell>
                    </TableRow>
                  ) : (
                    attempts.map((attempt: ProxyUpstreamAttempt) => (
                      <TableRow key={attempt.id}>
                        <TableCell className='whitespace-nowrap'>
                          {new Date(attempt.started_at_ms).toLocaleString(
                            'zh-CN',
                            { hour12: false }
                          )}
                        </TableCell>
                        <TableCell>
                          <div className='max-w-48 truncate font-mono text-xs'>
                            {attempt.request_id}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            第 {attempt.attempt_sequence} 次 · 重试索引{' '}
                            {attempt.retry_index}
                          </div>
                        </TableCell>
                        <TableCell>{attempt.channel_id || '—'}</TableCell>
                        <TableCell
                          className={
                            attempt.result === 'success'
                              ? 'text-success font-medium'
                              : 'text-destructive font-medium'
                          }
                        >
                          {proxyAttemptResultLabels[attempt.result] ??
                            attempt.result}
                          <div className='text-muted-foreground text-xs font-normal'>
                            {attempt.http_status
                              ? `HTTP ${attempt.http_status}`
                              : proxyReasonLabel(attempt.failure_reason)}
                          </div>
                        </TableCell>
                        <TableCell className='whitespace-nowrap'>
                          {attempt.duration_ms} ms
                        </TableCell>
                        <TableCell className='max-w-48 truncate font-mono text-xs'>
                          {attempt.upstream_request_id || '—'}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className='text-base'>状态变更记录</CardTitle>
            </CardHeader>
            <CardContent className='overflow-x-auto p-0'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>时间</TableHead>
                    <TableHead>事件</TableHead>
                    <TableHead>状态变化</TableHead>
                    <TableHead>原因</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {!events.length ? (
                    <TableRow>
                      <TableCell
                        colSpan={4}
                        className='text-muted-foreground py-8 text-center'
                      >
                        暂无状态变更记录
                      </TableCell>
                    </TableRow>
                  ) : (
                    events.map((event: ProxyStateEvent) => (
                      <TableRow key={event.id}>
                        <TableCell className='whitespace-nowrap'>
                          {formatProxyTime(event.created_at)}
                        </TableCell>
                        <TableCell>
                          {proxyEventLabels[event.event_type] ??
                            event.event_type}
                        </TableCell>
                        <TableCell className='whitespace-nowrap'>
                          {proxyStatusLabels[event.from_status] ??
                            event.from_status ??
                            '—'}{' '}
                          →{' '}
                          {proxyStatusLabels[event.to_status] ??
                            event.to_status ??
                            '—'}
                        </TableCell>
                        <TableCell>{proxyReasonLabel(event.reason)}</TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className='text-base'>最近通用日志判定</CardTitle>
            </CardHeader>
            <CardContent className='overflow-x-auto p-0'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>请求时间</TableHead>
                    <TableHead>日志</TableHead>
                    <TableHead>结果</TableHead>
                    <TableHead>首字</TableHead>
                    <TableHead>总耗时 / TPS</TableHead>
                    <TableHead>原因</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {!analyses.length ? (
                    <TableRow>
                      <TableCell
                        colSpan={6}
                        className='text-muted-foreground py-8 text-center'
                      >
                        暂无已分析日志
                      </TableCell>
                    </TableRow>
                  ) : (
                    analyses.map((analysis: ProxyLogAnalysis) => (
                      <TableRow key={analysis.id}>
                        <TableCell className='whitespace-nowrap'>
                          {formatProxyTime(analysis.log_created_at)}
                        </TableCell>
                        <TableCell>
                          {analysis.log_type === 5 ? '错误日志' : '消费日志'}
                        </TableCell>
                        <TableCell
                          className={
                            analysis.is_timeout
                              ? 'text-destructive font-medium'
                              : 'text-success font-medium'
                          }
                        >
                          {proxyAnalysisResultLabel(analysis)}
                        </TableCell>
                        <TableCell>
                          {analysis.first_response_time_ms > 0
                            ? `${analysis.first_response_time_ms} ms`
                            : '—'}
                        </TableCell>
                        <TableCell className='whitespace-nowrap'>
                          {analysis.use_time_seconds} 秒
                          <div className='text-muted-foreground text-xs'>
                            {analysis.tokens_per_second > 0
                              ? `TPS ${analysis.tokens_per_second.toFixed(1)}`
                              : 'TPS —'}
                          </div>
                        </TableCell>
                        <TableCell>
                          {proxyReasonLabel(analysis.reason)}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <GroupDialog
        open={groupDialogOpen}
        group={editingGroup}
        saving={groupMutation.isPending}
        onOpenChange={setGroupDialogOpen}
        onSave={(data) => groupMutation.mutate(data)}
      />
      <ProxyDialog
        open={proxyDialogOpen}
        groupId={selectedGroupId}
        proxy={editingProxy}
        saving={proxyMutation.isPending}
        onOpenChange={setProxyDialogOpen}
        onSave={(data) => proxyMutation.mutate(data)}
      />
      <BindingDialog
        open={bindingDialogOpen}
        groups={groups}
        channels={(channelsQuery.data ?? []).map((channel) => ({
          id: channel.id,
          name: channel.name,
        }))}
        saving={bindingMutation.isPending}
        onOpenChange={setBindingDialogOpen}
        onSave={(channelId, groupId) =>
          bindingMutation.mutate({ channelId, groupId })
        }
      />
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title='确认删除'
        desc={`确定删除“${deleteTarget?.name ?? ''}”吗？此操作无法撤销。`}
        confirmText='删除'
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() =>
          deleteTarget && deleteMutation.mutate(deleteTarget)
        }
      />
    </SettingsSection>
  )
}
