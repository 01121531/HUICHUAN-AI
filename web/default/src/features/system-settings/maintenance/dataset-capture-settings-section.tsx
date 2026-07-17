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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Database,
  Gauge,
  HardDrive,
  Mail,
  RefreshCw,
} from 'lucide-react'
import { useEffect, useMemo } from 'react'
import {
  type FieldPathByValue,
  type UseFormReturn,
  useForm,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { MultiSelect } from '@/components/multi-select'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'

import {
  getDatasetCaptureModels,
  getDatasetCapturePolicy,
  getDatasetCaptureRuntimeStatus,
  getDatasetCaptureSubjects,
  sendDatasetCaptureTestAlert,
  updateDatasetCapturePolicy,
} from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type {
  DatasetCaptureActivityWindow,
  DatasetCaptureModelMode,
  DatasetCapturePolicy,
  DatasetCaptureScopeMode,
} from '../types'
import { DatasetCaptureAccessAuditSection } from './dataset-capture-access-audit-section'

const EMPTY_VALUES: string[] = []
const ALERT_TYPES = [
  'queue_full',
  'inflight_bytes_exceeded',
  'sample_too_large',
  'disk_low',
  'disk_limit_reached',
  'jsonl_write_failed',
  'index_write_failed',
  'spool_write_failed',
  'worker_panic',
] as const

const DEFAULT_POLICY: DatasetCapturePolicy = {
  version: 2,
  enabled: false,
  model_mode: 'all',
  models: [],
  user_mode: 'all',
  user_ids: [],
  token_mode: 'all',
  token_ids: [],
  capture_stream: true,
  preserve_multimodal_base64: true,
  performance: {
    queue_size: 1024,
    workers: 2,
    buffer_segment_kb: 64,
    max_sample_mb: 100,
    max_inflight_mb: 512,
    spool_threshold_mb: 2,
    index_queue_size: 2048,
    index_batch_size: 50,
    index_flush_interval_ms: 1000,
    min_free_disk_gb: 2,
    max_disk_gb: 10,
    export_concurrency: 1,
    export_read_mbps: 32,
  },
  alerts: {
    enabled: false,
    recipients: [],
    types: [...ALERT_TYPES],
    silence_minutes: 10,
    alert_after_drops: 1,
    send_recovery: true,
  },
}

function createDatasetCaptureSchema(translate: (key: string) => string) {
  return z
    .object({
      version: z.number().int().min(2).max(2),
      enabled: z.boolean(),
      model_mode: z.enum(['all', 'selected']),
      models: z.array(z.string()),
      user_mode: z.enum(['all', 'selected']),
      user_ids: z.array(z.number().int().positive()),
      token_mode: z.enum(['all', 'selected']),
      token_ids: z.array(z.number().int().positive()),
      capture_stream: z.boolean(),
      preserve_multimodal_base64: z.boolean(),
      performance: z.object({
        queue_size: z.number().int().min(16).max(65536),
        workers: z.number().int().min(1).max(32),
        buffer_segment_kb: z.number().int().min(4).max(1024),
        max_sample_mb: z.number().int().min(1).max(1024),
        max_inflight_mb: z.number().int().min(16).max(65536),
        spool_threshold_mb: z.number().int().min(1).max(1024),
        index_queue_size: z.number().int().min(16).max(131072),
        index_batch_size: z.number().int().min(1).max(1000),
        index_flush_interval_ms: z.number().int().min(100).max(60000),
        min_free_disk_gb: z.number().int().min(0).max(10240),
        max_disk_gb: z.number().int().min(1).max(1048576),
        export_concurrency: z.number().int().min(1).max(8),
        export_read_mbps: z.number().int().min(0).max(1024),
      }),
      alerts: z.object({
        enabled: z.boolean(),
        recipients: z.array(z.email()),
        types: z.array(z.string()),
        silence_minutes: z.number().int().min(1).max(1440),
        alert_after_drops: z.number().int().min(1).max(1000000),
        send_recovery: z.boolean(),
      }),
    })
    .superRefine((value, context) => {
      if (value.model_mode === 'selected' && value.models.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['models'],
          message: translate('Select at least one model'),
        })
      }
      if (value.user_mode === 'selected' && value.user_ids.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['user_ids'],
          message: translate('Select at least one user'),
        })
      }
      if (value.token_mode === 'selected' && value.token_ids.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['token_ids'],
          message: translate('Select at least one token'),
        })
      }
      if (value.alerts.enabled && value.alerts.recipients.length === 0) {
        context.addIssue({
          code: 'custom',
          path: ['alerts', 'recipients'],
          message: translate('Add at least one alert recipient'),
        })
      }
      if (
        value.performance.spool_threshold_mb > value.performance.max_sample_mb
      ) {
        context.addIssue({
          code: 'custom',
          path: ['performance', 'spool_threshold_mb'],
          message: translate('Spool threshold cannot exceed sample limit'),
        })
      }
    })
}

type DatasetCaptureFormValues = z.infer<
  ReturnType<typeof createDatasetCaptureSchema>
>
type NumberPath = FieldPathByValue<DatasetCaptureFormValues, number>

function normalizePolicy(policy: DatasetCapturePolicy): DatasetCapturePolicy {
  return {
    ...DEFAULT_POLICY,
    ...policy,
    models: Array.isArray(policy.models) ? policy.models : [],
    user_ids: Array.isArray(policy.user_ids) ? policy.user_ids : [],
    token_ids: Array.isArray(policy.token_ids) ? policy.token_ids : [],
    performance: { ...DEFAULT_POLICY.performance, ...policy.performance },
    alerts: {
      ...DEFAULT_POLICY.alerts,
      ...policy.alerts,
      recipients: Array.isArray(policy.alerts?.recipients)
        ? policy.alerts.recipients
        : [],
      types: Array.isArray(policy.alerts?.types)
        ? policy.alerts.types
        : [...ALERT_TYPES],
    },
  }
}

function Subsection({
  title,
  description,
}: {
  title: string
  description?: string
}) {
  return (
    <div
      data-settings-form-span='full'
      className='border-border/70 border-t pt-6 first:border-t-0 first:pt-0'
    >
      <h4 className='text-sm font-semibold'>{title}</h4>
      {description ? (
        <p className='text-muted-foreground mt-1 text-xs'>{description}</p>
      ) : null}
    </div>
  )
}

function ScopeToggle({
  value,
  onChange,
  allLabel,
  selectedLabel,
  ariaLabel,
}: {
  value: DatasetCaptureScopeMode
  onChange: (value: DatasetCaptureScopeMode) => void
  allLabel: string
  selectedLabel: string
  ariaLabel: string
}) {
  return (
    <ToggleGroup
      value={[value]}
      onValueChange={(values) => {
        const next = values.find((item) => item !== value)
        if (next === 'all' || next === 'selected') onChange(next)
      }}
      variant='outline'
      spacing={0}
      aria-label={ariaLabel}
    >
      <ToggleGroupItem value='all'>{allLabel}</ToggleGroupItem>
      <ToggleGroupItem value='selected'>{selectedLabel}</ToggleGroupItem>
    </ToggleGroup>
  )
}

function NumberField({
  form,
  name,
  label,
  description,
  min,
  max,
}: {
  form: UseFormReturn<DatasetCaptureFormValues>
  name: NumberPath
  label: string
  description?: string
  min: number
  max: number
}) {
  return (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={min}
              max={max}
              value={field.value}
              onBlur={field.onBlur}
              onChange={(event) => field.onChange(event.target.valueAsNumber)}
            />
          </FormControl>
          {description ? (
            <FormDescription>{description}</FormDescription>
          ) : null}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1
  )
  return `${(value / 1024 ** index).toFixed(index > 1 ? 1 : 0)} ${units[index]}`
}

function windowDropped(window?: DatasetCaptureActivityWindow) {
  if (!window) return 0
  return (
    window.dropped_queue_full +
    window.dropped_sample_too_large +
    window.dropped_inflight_limit +
    window.build_failed +
    window.jsonl_write_failed +
    window.disk_limit_dropped +
    window.disk_low_dropped
  )
}

export function DatasetCaptureSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const schema = useMemo(() => createDatasetCaptureSchema(t), [t])
  const policyQuery = useQuery({
    queryKey: ['dataset-capture-policy'],
    queryFn: getDatasetCapturePolicy,
  })
  const modelsQuery = useQuery({
    queryKey: ['dataset-capture-policy-models'],
    queryFn: getDatasetCaptureModels,
  })
  const subjectsQuery = useQuery({
    queryKey: ['dataset-capture-policy-subjects'],
    queryFn: getDatasetCaptureSubjects,
  })
  const statusQuery = useQuery({
    queryKey: ['dataset-capture-runtime-status'],
    queryFn: getDatasetCaptureRuntimeStatus,
    refetchInterval: 5000,
  })
  const form = useForm<DatasetCaptureFormValues>({
    resolver: zodResolver(schema),
    defaultValues: DEFAULT_POLICY,
  })

  useEffect(() => {
    if (policyQuery.data?.data) {
      form.reset(normalizePolicy(policyQuery.data.data))
    }
  }, [form, policyQuery.data?.data])

  const mutation = useMutation({
    mutationFn: updateDatasetCapturePolicy,
    onSuccess: (result) => {
      form.reset(normalizePolicy(result.data))
      void queryClient.invalidateQueries({
        queryKey: ['dataset-capture-policy-models'],
      })
      void queryClient.invalidateQueries({
        queryKey: ['dataset-capture-policy-subjects'],
      })
      void queryClient.invalidateQueries({
        queryKey: ['dataset-capture-runtime-status'],
      })
      toast.success(t('Setting updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update setting'))
    },
  })
  const testAlertMutation = useMutation({
    mutationFn: sendDatasetCaptureTestAlert,
    onSuccess: (result) => toast.success(result.message),
    onError: (error: Error) =>
      toast.error(error.message || t('Failed to send test alert')),
  })

  const catalogModels = Array.isArray(modelsQuery.data?.data?.models)
    ? modelsQuery.data.data.models
    : []
  const modelOptions = catalogModels.map((model) => ({
    value: model.id,
    label: model.available ? model.id : `${model.id} (${t('Unavailable')})`,
  }))
  const availableModels = catalogModels
    .filter((model) => model.available)
    .map((model) => model.id)
  const userOptions = (subjectsQuery.data?.data?.users ?? []).map((user) => ({
    value: String(user.id),
    label: `${user.username} (#${user.id})`,
  }))
  const tokenOptions = (subjectsQuery.data?.data?.tokens ?? []).map(
    (token) => ({
      value: String(token.id),
      label: `${token.name || t('Unnamed token')} (#${token.id}) · ${token.username || `#${token.user_id}`}`,
    })
  )
  const alertTypeOptions = ALERT_TYPES.map((type) => ({
    value: type,
    label: t(`Dataset capture alert ${type}`),
  }))

  const selectedModelMode = form.watch('model_mode')
  const selectedUserMode = form.watch('user_mode')
  const selectedTokenMode = form.watch('token_mode')
  const alertsEnabled = form.watch('alerts.enabled')
  const selectedModels = form.watch('models') ?? EMPTY_VALUES
  const unavailableModels = selectedModels.filter(
    (model) => !availableModels.includes(model)
  )
  const runtime = statusQuery.data?.data
  const queuePercent = runtime?.writer.queue_capacity
    ? Math.min(
        100,
        (runtime.writer.queue_depth / runtime.writer.queue_capacity) * 100
      )
    : 0
  const dropped = runtime
    ? runtime.writer.dropped_queue_full +
      runtime.writer.dropped_sample_too_large +
      runtime.writer.dropped_inflight_limit +
      runtime.writer.disk_limit_dropped +
      runtime.writer.disk_low_dropped
    : 0

  if (
    policyQuery.isLoading ||
    modelsQuery.isLoading ||
    subjectsQuery.isLoading
  ) {
    return (
      <SettingsSection title={t('Dataset Capture')}>
        <div className='space-y-4'>
          <Skeleton className='h-16 w-full' />
          <Skeleton className='h-32 w-full' />
          <Skeleton className='h-32 w-full' />
        </div>
      </SettingsSection>
    )
  }

  if (policyQuery.isError || modelsQuery.isError || subjectsQuery.isError) {
    return (
      <SettingsSection title={t('Dataset Capture')}>
        <Alert variant='destructive'>
          <AlertTitle>{t('Failed to load capture policy')}</AlertTitle>
          <AlertDescription>
            {t('Refresh the page and try again')}
          </AlertDescription>
        </Alert>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Dataset Capture')}>
      <div className='border-border/70 grid overflow-hidden rounded-md border sm:grid-cols-2 xl:grid-cols-4'>
        <div className='border-border/70 flex min-w-0 items-center gap-3 border-b p-3 sm:border-r xl:border-b-0'>
          <Activity className='text-muted-foreground size-4 shrink-0' />
          <div className='min-w-0'>
            <p className='text-muted-foreground text-xs'>
              {t('Runtime status')}
            </p>
            <div className='mt-1 flex items-center gap-2'>
              <Badge variant={runtime?.enabled ? 'default' : 'secondary'}>
                {runtime?.enabled ? t('Enabled') : t('Disabled')}
              </Badge>
              <span className='truncate text-xs'>{runtime?.node ?? '-'}</span>
            </div>
          </div>
        </div>
        <div className='border-border/70 min-w-0 border-b p-3 xl:border-r xl:border-b-0'>
          <div className='flex items-center justify-between gap-2 text-xs'>
            <span className='text-muted-foreground flex items-center gap-2'>
              <Gauge className='size-4' /> {t('Background queue')}
            </span>
            <span className='font-mono'>
              {runtime?.writer.queue_depth ?? 0}/
              {runtime?.writer.queue_capacity ?? 0}
            </span>
          </div>
          <Progress value={queuePercent} className='mt-2 h-1.5' />
        </div>
        <div className='border-border/70 min-w-0 border-b p-3 sm:border-r sm:border-b-0'>
          <p className='text-muted-foreground flex items-center gap-2 text-xs'>
            <Database className='size-4' /> {t('Snapshot throughput')}
          </p>
          <p className='mt-1 font-mono text-sm'>
            {runtime?.writer.written ?? 0} {t('written')} · {dropped}{' '}
            {t('dropped')}
          </p>
        </div>
        <div className='min-w-0 p-3'>
          <div className='flex items-start justify-between gap-2'>
            <div>
              <p className='text-muted-foreground flex items-center gap-2 text-xs'>
                <HardDrive className='size-4' /> {t('Snapshot storage')}
              </p>
              <p className='mt-1 font-mono text-sm'>
                {formatBytes(runtime?.writer.disk_bytes ?? 0)} /{' '}
                {formatBytes(runtime?.writer.free_disk_bytes ?? 0)} {t('free')}
              </p>
            </div>
            <Button
              type='button'
              variant='ghost'
              size='icon-sm'
              title={t('Refresh')}
              onClick={() => void statusQuery.refetch()}
            >
              <RefreshCw className='size-4' />
            </Button>
          </div>
        </div>
        {[
          {
            label: `${t('Runtime status')} · ${t('1 minute')}`,
            window: runtime?.writer.last_minute,
          },
          {
            label: `${t('Runtime status')} · ${t('5 minutes')}`,
            window: runtime?.writer.last_five_minutes,
          },
        ].map((item) => {
          const window = item.window
          const droppedInWindow = windowDropped(window)
          const reasons = [
            [
              t('Dataset capture alert queue_full'),
              window?.dropped_queue_full ?? 0,
            ],
            [
              t('Dataset capture alert sample_too_large'),
              window?.dropped_sample_too_large ?? 0,
            ],
            [
              t('Dataset capture alert inflight_bytes_exceeded'),
              window?.dropped_inflight_limit ?? 0,
            ],
            ['build_failed', window?.build_failed ?? 0],
            [
              t('Dataset capture alert jsonl_write_failed'),
              window?.jsonl_write_failed ?? 0,
            ],
            [
              t('Dataset capture alert disk_low'),
              (window?.disk_limit_dropped ?? 0) +
                (window?.disk_low_dropped ?? 0),
            ],
          ].filter((reason) => Number(reason[1]) > 0)
          return (
            <div
              key={item.label}
              className='border-border/70 col-span-full min-w-0 border-t p-3 sm:col-span-1 sm:even:border-l'
            >
              <p className='text-muted-foreground text-xs'>{item.label}</p>
              <p className='mt-1 font-mono text-sm'>
                {window?.submitted ?? 0} {t('Submitted')} ·{' '}
                {window?.written ?? 0} {t('written')} · {droppedInWindow}{' '}
                {t('dropped')}
              </p>
              {reasons.length > 0 ? (
                <p className='text-muted-foreground mt-1 truncate text-xs'>
                  {reasons
                    .map(([label, count]) => `${label}: ${count}`)
                    .join(' · ')}
                </p>
              ) : null}
            </div>
          )
        })}
      </div>

      <Form {...form}>
        <SettingsForm
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          <SettingsPageFormActions
            onSave={form.handleSubmit((values) => mutation.mutate(values))}
            onReset={() =>
              form.reset(
                normalizePolicy(policyQuery.data?.data ?? DEFAULT_POLICY)
              )
            }
            isSaving={mutation.isPending}
            isSaveDisabled={!form.formState.isDirty}
            isResetDisabled={!form.formState.isDirty}
          />

          <Subsection
            title={t('Capture policy')}
            description={t(
              'Only complete successful requests inside this scope are saved'
            )}
          />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable data snapshots')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Process snapshots after the client response is delivered'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='model_mode'
            render={({ field }) => (
              <FormItem data-settings-form-span='full'>
                <div className='text-sm font-medium'>
                  {t('Capture model scope')}
                </div>
                <FormControl>
                  <ScopeToggle
                    value={field.value}
                    onChange={(value) =>
                      field.onChange(value as DatasetCaptureModelMode)
                    }
                    allLabel={t('All models')}
                    selectedLabel={t('Selected models')}
                    ariaLabel={t('Capture model scope')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {selectedModelMode === 'selected' ? (
            <FormField
              control={form.control}
              name='models'
              render={({ field }) => (
                <FormItem data-settings-form-span='full'>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <FormLabel>{t('Site models')}</FormLabel>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      onClick={() => field.onChange(availableModels)}
                    >
                      {t('Select all available models')}
                    </Button>
                  </div>
                  <FormControl>
                    <MultiSelect
                      options={modelOptions}
                      selected={field.value ?? EMPTY_VALUES}
                      onChange={field.onChange}
                      placeholder={t('Select site models...')}
                      maxVisibleChips={6}
                    />
                  </FormControl>
                  {unavailableModels.length > 0 ? (
                    <p className='text-warning-foreground text-xs'>
                      {t('Unavailable selected models: {{models}}', {
                        models: unavailableModels.join(', '),
                      })}
                    </p>
                  ) : null}
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}
          <FormField
            control={form.control}
            name='user_mode'
            render={({ field }) => (
              <FormItem>
                <div className='text-sm font-medium'>{t('User scope')}</div>
                <FormControl>
                  <ScopeToggle
                    value={field.value}
                    onChange={field.onChange}
                    allLabel={t('All users')}
                    selectedLabel={t('Selected users')}
                    ariaLabel={t('User scope')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='token_mode'
            render={({ field }) => (
              <FormItem>
                <div className='text-sm font-medium'>{t('Token scope')}</div>
                <FormControl>
                  <ScopeToggle
                    value={field.value}
                    onChange={field.onChange}
                    allLabel={t('All tokens')}
                    selectedLabel={t('Selected tokens')}
                    ariaLabel={t('Token scope')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          {selectedUserMode === 'selected' ? (
            <FormField
              control={form.control}
              name='user_ids'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Selected users')}</FormLabel>
                  <FormControl>
                    <MultiSelect
                      options={userOptions}
                      selected={(field.value ?? []).map(String)}
                      onChange={(values) => field.onChange(values.map(Number))}
                      placeholder={t('Select users...')}
                      maxVisibleChips={4}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}
          {selectedTokenMode === 'selected' ? (
            <FormField
              control={form.control}
              name='token_ids'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Selected tokens')}</FormLabel>
                  <FormControl>
                    <MultiSelect
                      options={tokenOptions}
                      selected={(field.value ?? []).map(String)}
                      onChange={(values) => field.onChange(values.map(Number))}
                      placeholder={t('Select tokens...')}
                      maxVisibleChips={4}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          ) : null}
          <FormField
            control={form.control}
            name='capture_stream'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Capture streaming requests')}</FormLabel>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='preserve_multimodal_base64'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Preserve multimodal Base64')}</FormLabel>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <Subsection
            title={t('Performance protection')}
            description={t(
              'Bound memory, disk and database work without blocking API responses'
            )}
          />
          <NumberField
            form={form}
            name='performance.queue_size'
            label={t('Completion queue size')}
            min={16}
            max={65536}
          />
          <NumberField
            form={form}
            name='performance.workers'
            label={t('Normalization workers')}
            min={1}
            max={32}
          />
          <NumberField
            form={form}
            name='performance.buffer_segment_kb'
            label={t('Buffer segment (KB)')}
            min={4}
            max={1024}
          />
          <NumberField
            form={form}
            name='performance.max_sample_mb'
            label={t('Maximum snapshot (MB)')}
            min={1}
            max={1024}
          />
          <NumberField
            form={form}
            name='performance.max_inflight_mb'
            label={t('Maximum in-flight memory (MB)')}
            min={16}
            max={65536}
          />
          <NumberField
            form={form}
            name='performance.spool_threshold_mb'
            label={t('Background spool threshold (MB)')}
            min={1}
            max={1024}
          />
          <NumberField
            form={form}
            name='performance.index_queue_size'
            label={t('Index queue size')}
            min={16}
            max={131072}
          />
          <NumberField
            form={form}
            name='performance.index_batch_size'
            label={t('Index batch size')}
            min={1}
            max={1000}
          />
          <NumberField
            form={form}
            name='performance.index_flush_interval_ms'
            label={t('Index flush interval (ms)')}
            min={100}
            max={60000}
          />
          <NumberField
            form={form}
            name='performance.min_free_disk_gb'
            label={t('Minimum free disk (GB)')}
            min={0}
            max={10240}
          />
          <NumberField
            form={form}
            name='performance.max_disk_gb'
            label={t('Maximum snapshot storage (GB)')}
            min={1}
            max={1048576}
          />
          <NumberField
            form={form}
            name='performance.export_concurrency'
            label={t('Export concurrency')}
            min={1}
            max={8}
          />
          <NumberField
            form={form}
            name='performance.export_read_mbps'
            label={t('Export read limit (MB/s)')}
            min={0}
            max={1024}
            description={t('Set to 0 for no export speed limit')}
          />

          <Subsection
            title={t('Exception email alerts')}
            description={t(
              'The first failure is sent immediately and repeated failures are aggregated'
            )}
          />
          <FormField
            control={form.control}
            name='alerts.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable snapshot exception emails')}</FormLabel>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          {alertsEnabled ? (
            <>
              <FormField
                control={form.control}
                name='alerts.recipients'
                render={({ field }) => (
                  <FormItem data-settings-form-span='full'>
                    <FormLabel>{t('Alert recipients')}</FormLabel>
                    <FormControl>
                      <MultiSelect
                        options={[]}
                        selected={field.value ?? EMPTY_VALUES}
                        onChange={field.onChange}
                        allowCreate
                        placeholder={t('Enter email addresses...')}
                        maxVisibleChips={5}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='alerts.types'
                render={({ field }) => (
                  <FormItem data-settings-form-span='full'>
                    <FormLabel>{t('Alert types')}</FormLabel>
                    <FormControl>
                      <MultiSelect
                        options={alertTypeOptions}
                        selected={field.value ?? EMPTY_VALUES}
                        onChange={field.onChange}
                        placeholder={t('Select alert types...')}
                        maxVisibleChips={5}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <NumberField
                form={form}
                name='alerts.silence_minutes'
                label={t('Alert silence window (minutes)')}
                min={1}
                max={1440}
              />
              <NumberField
                form={form}
                name='alerts.alert_after_drops'
                label={t('Alert after dropped snapshots')}
                min={1}
                max={1000000}
              />
              <FormField
                control={form.control}
                name='alerts.send_recovery'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Send recovery email')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <div data-settings-form-span='full' className='flex justify-end'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => testAlertMutation.mutate()}
                  disabled={
                    testAlertMutation.isPending || form.formState.isDirty
                  }
                >
                  <Mail className='size-4' />
                  {t('Send test email')}
                </Button>
              </div>
            </>
          ) : null}
        </SettingsForm>
      </Form>

      <div className='border-border/70 border-t pt-6'>
        <DatasetCaptureAccessAuditSection />
      </div>
    </SettingsSection>
  )
}
