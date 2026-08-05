import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { History, RefreshCw, ServerCog } from 'lucide-react'
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
import { Textarea } from '@/components/ui/textarea'

import {
  createManagedProxy,
  batchCreateManagedProxies,
  createProxyGroup,
  checkManagedProxy,
  checkAllManagedProxies,
  checkProxyGroup,
  deleteManagedProxy,
  deleteProxyGroup,
  listGroupProxies,
  listProxyLogAnalyses,
  listProxyGroups,
  listProxyStateEvents,
  listProxyUpstreamAttempts,
  getProxyHealthSettings,
  resetManagedProxyObservation,
  setManagedProxyPaused,
  switchProxyGroup,
  updateManagedProxy,
  updateProxyGroup,
  updateProxyHealthSettings,
} from './proxy-management-api'
import type {
  ManagedProxy,
  ManagedProxyInput,
  ProxyBatchInput,
  ProxyGroup,
  ProxyGroupInput,
  ProxyHealthSettings,
  ProxyLogAnalysis,
  ProxyStateEvent,
  ProxyUpstreamAttempt,
} from './proxy-management-types'
import { ProxyPoolWorkspace } from './proxy-pool-workspace'

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
const DEFAULT_PROXY_HEALTH_SETTINGS: ProxyHealthSettings = {
  enabled: true,
  time: '08:00',
  timezone: 'Asia/Shanghai',
}

const proxyStatusLabels: Record<string, string> = {
  available: '正常',
  watching: '观察中',
  paused: '已暂停',
  cooling: '自动禁用（冷却中）',
  recovering: '恢复测试',
  unavailable: '已自动禁用',
  disabled: '已停用',
}

const proxyEventLabels: Record<string, string> = {
  health_status_changed: '健康状态变化',
  auto_paused: '耗时异常，已自动禁用',
  recovery_started: '开始恢复测试',
  recovery_failed: '恢复失败',
  recovery_succeeded: '恢复成功',
  health_unavailable: '连接失败，已自动禁用',
  manual_paused: '手动暂停',
  manual_resumed: '手动恢复',
  manual_recovery_requested: '手动恢复检测',
  manual_observation_reset: '清空观察计数',
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
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>
            {props.group ? '编辑代理分组' : '新增代理分组'}
          </DialogTitle>
          <DialogDescription>
            设置轮换、自动禁用、连接检测和冷却恢复参数。
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-5'>
          <section className='space-y-3 rounded-xl border p-4'>
            <div>
              <h3 className='text-sm font-semibold'>基础信息</h3>
              <p className='text-muted-foreground mt-1 text-xs'>
                用清晰的名称区分地区、供应商或用途。
              </p>
            </div>
            <div className='space-y-1.5'>
              <Label>分组名称</Label>
              <Input
                autoFocus
                value={form.name}
                placeholder='例如：海外高速代理'
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    name: event.target.value,
                  }))
                }
              />
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <label className='bg-muted/30 flex min-h-12 items-center justify-between gap-3 rounded-lg border px-3 text-sm'>
                <span>
                  <span className='block font-medium'>启用此分组</span>
                  <span className='text-muted-foreground text-xs'>
                    停用后不参与渠道请求
                  </span>
                </span>
                <Switch
                  checked={form.enabled}
                  onCheckedChange={(enabled) =>
                    setForm((current) => ({ ...current, enabled }))
                  }
                />
              </label>
              <label className='bg-muted/30 flex min-h-12 items-center justify-between gap-3 rounded-lg border px-3 text-sm'>
                <span>
                  <span className='block font-medium'>无代理时允许直连</span>
                  <span className='text-muted-foreground text-xs'>
                    建议仅在可接受直连时开启
                  </span>
                </span>
                <Switch
                  checked={form.allow_direct_fallback}
                  onCheckedChange={(allow_direct_fallback) =>
                    setForm((current) => ({
                      ...current,
                      allow_direct_fallback,
                    }))
                  }
                />
              </label>
            </div>
          </section>

          <section className='space-y-3 rounded-xl border p-4'>
            <div>
              <h3 className='text-sm font-semibold'>轮换与请求等待</h3>
              <p className='text-muted-foreground mt-1 text-xs'>
                达到请求次数或使用时长后自动切换，切换期间阻塞新请求。
              </p>
            </div>
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              <NumberField
                label='单代理最大请求数'
                value={form.max_requests}
                min={1}
                onChange={(v) => setNumber('max_requests', v)}
              />
              <NumberField
                label='最长使用时间（秒）'
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
            </div>
          </section>

          <section className='space-y-3 rounded-xl border p-4'>
            <div>
              <h3 className='text-sm font-semibold'>健康检测与超时判定</h3>
              <p className='text-muted-foreground mt-1 text-xs'>
                连接检测失败或通用日志红色次数达到阈值后自动停用代理。
              </p>
            </div>
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              <NumberField
                label='检测周期（秒）'
                value={form.health_check_interval}
                min={1}
                onChange={(v) => setNumber('health_check_interval', v)}
              />
              <NumberField
                label='检测失败阈值'
                value={form.health_failure_threshold}
                min={1}
                onChange={(v) => setNumber('health_failure_threshold', v)}
              />
              <NumberField
                label='连续红色阈值'
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
                label='窗口红色比例'
                value={form.window_timeout_ratio}
                min={0.01}
                step={0.01}
                onChange={(v) => setNumber('window_timeout_ratio', v)}
              />
            </div>
          </section>

          <section className='space-y-3 rounded-xl border p-4'>
            <div>
              <h3 className='text-sm font-semibold'>冷却与自动恢复</h3>
              <p className='text-muted-foreground mt-1 text-xs'>
                冷却到期后自动复检，连续通过指定次数后重新参与请求。
              </p>
            </div>
            <div className='grid gap-4 sm:grid-cols-3'>
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
          </section>
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

function BatchProxyDialog(props: {
  open: boolean
  saving: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: ProxyBatchInput) => void
}) {
  const [form, setForm] = useState<ProxyBatchInput>({
    proxies: '',
    default_protocol: 'socks5',
    name_prefix: '',
    enabled: true,
  })

  useEffect(() => {
    if (props.open) {
      setForm({
        proxies: '',
        default_protocol: 'socks5',
        name_prefix: '',
        enabled: true,
      })
    }
  }, [props.open])

  const inputCount = form.proxies
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean).length

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>批量添加代理</DialogTitle>
          <DialogDescription>
            每行填写一个代理，也可以使用英文逗号分隔。系统会自动去重，全部校验通过后一次写入。
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4'>
          <div className='grid gap-4 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label>无协议地址的默认协议</Label>
              <NativeSelect
                className='w-full'
                value={form.default_protocol}
                onChange={(event) =>
                  setForm((value) => ({
                    ...value,
                    default_protocol: event.target.value,
                  }))
                }
              >
                <NativeSelectOption value='http'>HTTP</NativeSelectOption>
                <NativeSelectOption value='https'>HTTPS</NativeSelectOption>
                <NativeSelectOption value='socks4'>SOCKS4</NativeSelectOption>
                <NativeSelectOption value='socks5'>SOCKS5</NativeSelectOption>
                <NativeSelectOption value='socks5h'>SOCKS5H</NativeSelectOption>
              </NativeSelect>
            </div>
            <div className='space-y-1.5'>
              <Label>名称前缀（可选）</Label>
              <Input
                value={form.name_prefix}
                placeholder='例如：香港出口'
                onChange={(event) =>
                  setForm((value) => ({
                    ...value,
                    name_prefix: event.target.value,
                  }))
                }
              />
            </div>
          </div>
          <div className='space-y-1.5'>
            <div className='flex items-center justify-between gap-3'>
              <Label>代理地址</Label>
              <span className='text-muted-foreground text-xs'>
                已填写 {inputCount} 条
              </span>
            </div>
            <Textarea
              className='min-h-64 font-mono text-sm'
              value={form.proxies}
              placeholder={[
                'socks5://user:password@127.0.0.1:1080',
                'http://user:password@127.0.0.2:8080',
                '127.0.0.3:1080',
                '127.0.0.4:1080:user:password',
              ].join('\n')}
              onChange={(event) =>
                setForm((value) => ({ ...value, proxies: event.target.value }))
              }
            />
            <p className='text-muted-foreground text-xs'>
              支持完整
              URL、主机:端口、用户名:密码@主机:端口、主机:端口:用户名:密码；单次最多
              1000 条。
            </p>
          </div>
          <label className='flex items-center justify-between gap-3 rounded-lg border p-3 text-sm'>
            <span>新增后立即启用</span>
            <Switch
              checked={form.enabled}
              onCheckedChange={(enabled) =>
                setForm((value) => ({ ...value, enabled }))
              }
            />
          </label>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            取消
          </Button>
          <Button
            disabled={props.saving || inputCount === 0 || inputCount > 1000}
            onClick={() => props.onSave(form)}
          >
            {props.saving ? '添加中…' : `批量添加（${inputCount}）`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type DeleteTarget =
  | { kind: 'group'; id: number; name: string; boundChannelCount: number }
  | { kind: 'proxy'; id: number; name: string }

function deleteTargetDescription(target: DeleteTarget | null) {
  if (!target) return ''
  if (target.kind === 'proxy') {
    return `确定删除“${target.name}”吗？此操作无法撤销。`
  }
  if (target.boundChannelCount > 0) {
    return `“${target.name}”当前被 ${target.boundChannelCount} 个渠道使用。确认后会自动取消这些渠道的代理池绑定，并删除分组内的全部代理。此操作无法撤销。`
  }
  return `确定删除“${target.name}”及分组内的全部代理吗？此操作无法撤销。`
}

export function ProxyManagementSection() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [selectedGroupId, setSelectedGroupId] = useState(0)
  const [selectedProxyId, setSelectedProxyId] = useState(0)
  const [groupDialogOpen, setGroupDialogOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<ProxyGroup | null>(null)
  const [proxyDialogOpen, setProxyDialogOpen] = useState(false)
  const [batchProxyDialogOpen, setBatchProxyDialogOpen] = useState(false)
  const [editingProxy, setEditingProxy] = useState<ManagedProxy | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DeleteTarget | null>(null)

  const groupsQuery = useQuery({
    queryKey: ['proxy-groups'],
    queryFn: async () => (await listProxyGroups()).data ?? [],
    refetchInterval: 2000,
  })
  const groups = groupsQuery.data ?? EMPTY_PROXY_GROUPS
  const healthSettingsQuery = useQuery({
    queryKey: ['proxy-health-settings'],
    queryFn: async () =>
      (await getProxyHealthSettings()).data ?? DEFAULT_PROXY_HEALTH_SETTINGS,
  })

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
    refetchInterval: 5000,
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
    queryClient.invalidateQueries({ queryKey: ['proxy-log-analyses'] })
    queryClient.invalidateQueries({ queryKey: ['proxy-state-events'] })
    queryClient.invalidateQueries({ queryKey: ['proxy-upstream-attempts'] })
  }

  const groupMutation = useMutation({
    mutationFn: async (data: ProxyGroupInput) =>
      editingGroup
        ? updateProxyGroup(editingGroup.id, data)
        : createProxyGroup(data),
    onSuccess: (response) => {
      toast.success(editingGroup ? '代理分组已更新' : '代理分组已创建')
      if (!editingGroup && response.data?.id) {
        setSelectedGroupId(response.data.id)
      }
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
  const batchProxyMutation = useMutation({
    mutationFn: (data: ProxyBatchInput) =>
      batchCreateManagedProxies(selectedGroupId, data),
    onSuccess: (response) => {
      if (!response.success) return
      const createdCount = response.data?.created_count ?? 0
      const skippedCount = response.data?.skipped_count ?? 0
      toast.success(
        skippedCount > 0
          ? `批量添加完成：新增 ${createdCount} 个，跳过重复 ${skippedCount} 个`
          : `批量添加完成：新增 ${createdCount} 个代理`
      )
      setBatchProxyDialogOpen(false)
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
  const checkAllMutation = useMutation({
    mutationFn: (groupId: number) =>
      groupId > 0 ? checkProxyGroup(groupId) : checkAllManagedProxies(),
    onSuccess: (response, groupId) => {
      let message = '已有代理检测任务正在运行'
      if (response.data?.created) {
        message =
          groupId > 0 ? '本组代理检测任务已启动' : '全部代理检测任务已启动'
      }
      toast.success(message)
      refresh()
    },
  })
  const healthSettingsMutation = useMutation({
    mutationFn: updateProxyHealthSettings,
    onSuccess: () => {
      toast.success('每日代理检测时间已保存')
      queryClient.invalidateQueries({ queryKey: ['proxy-health-settings'] })
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
  const resetObservationMutation = useMutation({
    mutationFn: (proxyId: number) => resetManagedProxyObservation(proxyId),
    onSuccess: () => {
      toast.success('当前观察计数已清空')
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
  const deleteMutation = useMutation({
    mutationFn: async (target: DeleteTarget) => {
      if (target.kind === 'group') return deleteProxyGroup(target.id)
      return deleteManagedProxy(target.id)
    },
    onSuccess: (response, target) => {
      if (!response.success) return
      const unboundChannelCount =
        target.kind === 'group'
          ? Number(
              (response.data as { unbound_channel_count?: number } | undefined)
                ?.unbound_channel_count ?? 0
            )
          : 0
      toast.success(
        unboundChannelCount > 0
          ? `代理分组已删除，同时取消了 ${unboundChannelCount} 个渠道的代理池绑定`
          : '已删除'
      )
      setDeleteTarget(null)
      refresh()
    },
  })

  const selectedGroup = useMemo(
    () => groups.find((group) => group.id === selectedGroupId) ?? null,
    [groups, selectedGroupId]
  )
  const proxies = proxiesQuery.data ?? EMPTY_MANAGED_PROXIES
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
    <section className='flex flex-col gap-4'>
      <Tabs defaultValue='groups'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <TabsList>
            <TabsTrigger value='groups'>
              <ServerCog />
              分组与代理
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
            disabled={groupsQuery.isFetching}
          >
            <RefreshCw className='mr-1.5 h-4 w-4' />
            刷新
          </Button>
        </div>

        <TabsContent value='groups' className='pt-3'>
          <ProxyPoolWorkspace
            groups={groups}
            selectedGroupId={selectedGroupId}
            selectedGroup={selectedGroup}
            proxies={proxies}
            switching={switchMutation.isPending}
            checking={checkMutation.isPending}
            checkingAll={checkAllMutation.isPending}
            savingHealthSettings={healthSettingsMutation.isPending}
            healthSettings={
              healthSettingsQuery.data ?? DEFAULT_PROXY_HEALTH_SETTINGS
            }
            pausing={pauseMutation.isPending}
            resetting={resetObservationMutation.isPending}
            onSelectGroup={setSelectedGroupId}
            onCreateGroup={() => {
              setEditingGroup(null)
              setGroupDialogOpen(true)
            }}
            onEditGroup={(group) => {
              setEditingGroup(group)
              setGroupDialogOpen(true)
            }}
            onDeleteGroup={(group) =>
              setDeleteTarget({
                kind: 'group',
                id: group.id,
                name: group.name,
                boundChannelCount: group.bound_channel_count,
              })
            }
            onSwitchGroup={(groupId) => switchMutation.mutate(groupId)}
            onBatchCreateProxy={() => setBatchProxyDialogOpen(true)}
            onCreateProxy={() => {
              setEditingProxy(null)
              setProxyDialogOpen(true)
            }}
            onCheckProxy={(proxyId) => checkMutation.mutate(proxyId)}
            onCheckGroup={(groupId) => checkAllMutation.mutate(groupId)}
            onCheckAll={() => checkAllMutation.mutate(0)}
            onSaveHealthSettings={(settings) =>
              healthSettingsMutation.mutate(settings)
            }
            onToggleProxy={(proxy) =>
              pauseMutation.mutate({
                proxyId: proxy.id,
                paused: proxy.status !== 'paused',
              })
            }
            onViewProxyLogs={(proxyId) =>
              navigate({
                to: '/usage-logs/$section',
                params: { section: 'common' },
                search: { page: 1, proxyId: String(proxyId) },
              })
            }
            onResetProxy={(proxyId) => resetObservationMutation.mutate(proxyId)}
            onEditProxy={(proxy) => {
              setEditingProxy(proxy)
              setProxyDialogOpen(true)
            }}
            onDeleteProxy={(proxy) =>
              setDeleteTarget({
                kind: 'proxy',
                id: proxy.id,
                name: proxy.name,
              })
            }
          />
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
      <BatchProxyDialog
        open={batchProxyDialogOpen}
        saving={batchProxyMutation.isPending}
        onOpenChange={setBatchProxyDialogOpen}
        onSave={(data) => batchProxyMutation.mutate(data)}
      />
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title='确认删除'
        desc={deleteTargetDescription(deleteTarget)}
        confirmText={
          deleteTarget?.kind === 'group' && deleteTarget.boundChannelCount > 0
            ? '解绑并删除'
            : '删除'
        }
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() =>
          deleteTarget && deleteMutation.mutate(deleteTarget)
        }
      />
    </section>
  )
}
