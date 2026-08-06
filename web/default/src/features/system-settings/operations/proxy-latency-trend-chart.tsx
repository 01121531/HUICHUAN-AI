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
import { useQuery } from '@tanstack/react-query'
import { Activity, ChartNoAxesCombined, RefreshCw } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import {
  CartesianGrid,
  ComposedChart,
  Line,
  ReferenceLine,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ChartContainer, type ChartConfig } from '@/components/ui/chart'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'

import { getProxyTrend } from './proxy-management-api'
import type {
  ManagedProxy,
  ProxyTrendPoint,
  ProxyTrendResponse,
} from './proxy-management-types'

const EMPTY_TREND: ProxyTrendResponse = {
  group_id: 0,
  proxy_id: 0,
  limit: 100,
  sample_count: 0,
  current_score: 0,
  average_score: 0,
  timeout_count: 0,
  points: [],
}

const chartConfig = {
  score: { label: '健康分', color: 'var(--chart-1)' },
  key_latency_ms: { label: '关键延迟', color: 'var(--chart-2)' },
  timeout_score: { label: '超时请求', color: 'var(--destructive)' },
} satisfies ChartConfig

function formatLatency(milliseconds: number) {
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return '—'
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(milliseconds < 10_000 ? 1 : 0)} 秒`
}

function scoreBand(score: number) {
  if (score >= 90) return '优秀'
  if (score >= 75) return '良好'
  if (score >= 60) return '偏慢'
  return '超时'
}

function scoreTone(score: number) {
  if (score >= 90) return 'text-emerald-600 dark:text-emerald-400'
  if (score >= 75) return 'text-primary'
  if (score >= 60) return 'text-amber-600 dark:text-amber-400'
  return 'text-destructive'
}

function timeoutReason(reason: string) {
  if (!reason) return '—'
  return reason
    .split(',')
    .map((item) => {
      if (item === 'first_response') return '首字延迟超时'
      if (item === 'duration') return '总耗时超时'
      if (item === 'throughput') return 'TPS 过低'
      return item
    })
    .join('、')
}

function formatPointTime(timestamp: number, includeDate = false) {
  return new Date(timestamp * 1000).toLocaleString('zh-CN', {
    ...(includeDate
      ? { month: '2-digit' as const, day: '2-digit' as const }
      : {}),
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

function TrendTooltip(props: {
  active?: boolean
  payload?: Array<{ payload?: ProxyTrendPoint & { proxy_name: string } }>
}) {
  const point = props.payload?.[0]?.payload
  if (!props.active || !point) return null

  return (
    <div className='border-border/60 bg-background min-w-64 rounded-xl border p-3 text-xs shadow-xl'>
      <div className='flex items-center justify-between gap-3'>
        <p className='font-medium'>{formatPointTime(point.timestamp, true)}</p>
        {point.is_timeout && <Badge variant='destructive'>超时</Badge>}
      </div>
      <p className='text-muted-foreground mt-1'>
        {point.proxy_name}（#{point.proxy_id}）
      </p>
      <div className='mt-3 grid grid-cols-2 gap-x-5 gap-y-2 tabular-nums'>
        <span className='text-muted-foreground'>健康分</span>
        <span className={`text-right font-semibold ${scoreTone(point.score)}`}>
          {point.score} · {scoreBand(point.score)}
        </span>
        <span className='text-muted-foreground'>关键延迟</span>
        <span className='text-right'>
          {formatLatency(point.key_latency_ms)}
        </span>
        <span className='text-muted-foreground'>首字延迟</span>
        <span className='text-right'>
          {formatLatency(point.first_response_time_ms)}
        </span>
        <span className='text-muted-foreground'>总耗时</span>
        <span className='text-right'>
          {formatLatency(point.total_duration_ms)}
        </span>
        <span className='text-muted-foreground'>TPS</span>
        <span className='text-right'>
          {point.tokens_per_second > 0
            ? point.tokens_per_second.toFixed(2)
            : '—'}
        </span>
        <span className='text-muted-foreground'>判定原因</span>
        <span className='max-w-44 text-right'>
          {timeoutReason(point.reason)}
        </span>
      </div>
    </div>
  )
}

function TrendMetric(props: {
  label: string
  value: string
  detail: string
  valueClassName?: string
}) {
  return (
    <div className='bg-muted/35 rounded-lg border px-3 py-2.5'>
      <p className='text-muted-foreground text-xs'>{props.label}</p>
      <p
        className={`mt-1 text-lg font-semibold tabular-nums ${props.valueClassName ?? ''}`}
      >
        {props.value}
      </p>
      <p className='text-muted-foreground mt-0.5 text-xs'>{props.detail}</p>
    </div>
  )
}

export function ProxyLatencyTrendChart(props: {
  groupId: number
  proxies: ManagedProxy[]
}) {
  const [scope, setScope] = useState<'group' | 'proxy'>('group')
  const [selectedProxyId, setSelectedProxyId] = useState(0)

  useEffect(() => {
    if (!props.proxies.some((proxy) => proxy.id === selectedProxyId)) {
      setSelectedProxyId(props.proxies[0]?.id ?? 0)
    }
  }, [props.proxies, selectedProxyId])

  const requestedProxyId = scope === 'proxy' ? selectedProxyId : 0
  const trendQuery = useQuery({
    queryKey: ['proxy-trend', props.groupId, requestedProxyId],
    queryFn: async () =>
      (await getProxyTrend(props.groupId, requestedProxyId, 100)).data ??
      EMPTY_TREND,
    enabled: props.groupId > 0 && (scope === 'group' || requestedProxyId > 0),
    refetchInterval: 15_000,
  })

  const proxyNames = useMemo(
    () => new Map(props.proxies.map((proxy) => [proxy.id, proxy.name])),
    [props.proxies]
  )
  const trend = trendQuery.data ?? EMPTY_TREND
  const chartData = useMemo(
    () =>
      trend.points.map((point) => ({
        ...point,
        proxy_name: proxyNames.get(point.proxy_id) ?? `代理 ${point.proxy_id}`,
        timeout_score: point.is_timeout ? point.score : null,
      })),
    [proxyNames, trend.points]
  )

  return (
    <section
      className='overflow-hidden rounded-xl border'
      aria-labelledby='proxy-trend-title'
    >
      <div className='flex flex-col gap-3 border-b px-4 py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex min-w-0 items-start gap-3'>
          <div className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <ChartNoAxesCombined className='size-4' />
          </div>
          <div>
            <h3 id='proxy-trend-title' className='text-sm font-semibold'>
              请求延迟与健康分趋势
            </h3>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              最近 100 次有效请求，每 15 秒自动刷新；红点表示已判定超时。
            </p>
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <div
            className='bg-muted flex rounded-lg p-1'
            aria-label='趋势查看范围'
          >
            <Button
              variant={scope === 'group' ? 'secondary' : 'ghost'}
              size='sm'
              className='h-8'
              onClick={() => setScope('group')}
            >
              分组汇总
            </Button>
            <Button
              variant={scope === 'proxy' ? 'secondary' : 'ghost'}
              size='sm'
              className='h-8'
              disabled={!props.proxies.length}
              onClick={() => setScope('proxy')}
            >
              单个代理
            </Button>
          </div>
          {scope === 'proxy' && (
            <NativeSelect
              className='w-full sm:w-52'
              value={selectedProxyId > 0 ? String(selectedProxyId) : ''}
              aria-label='选择趋势代理'
              onChange={(event) =>
                setSelectedProxyId(Number(event.target.value))
              }
            >
              {!props.proxies.length && (
                <NativeSelectOption value=''>暂无代理</NativeSelectOption>
              )}
              {props.proxies.map((proxy) => (
                <NativeSelectOption key={proxy.id} value={String(proxy.id)}>
                  {proxy.name}（#{proxy.id}）
                </NativeSelectOption>
              ))}
            </NativeSelect>
          )}
          <Button
            variant='outline'
            size='icon-sm'
            aria-label='立即刷新趋势图'
            disabled={trendQuery.isFetching}
            onClick={() => trendQuery.refetch()}
          >
            <RefreshCw
              className={trendQuery.isFetching ? 'animate-spin' : ''}
            />
          </Button>
        </div>
      </div>

      <div className='p-4'>
        {trendQuery.isPending && (
          <div className='bg-muted/20 flex h-96 items-center justify-center rounded-lg border border-dashed'>
            <div className='text-muted-foreground flex items-center gap-2 text-sm'>
              <RefreshCw className='size-4 animate-spin' />
              正在加载趋势数据…
            </div>
          </div>
        )}
        {!trendQuery.isPending && trendQuery.isError && (
          <div className='bg-destructive/5 flex h-96 flex-col items-center justify-center rounded-lg border border-dashed text-center'>
            <Activity className='text-destructive size-6' />
            <p className='mt-3 text-sm font-medium'>趋势数据加载失败</p>
            <Button
              className='mt-3'
              variant='outline'
              size='sm'
              onClick={() => trendQuery.refetch()}
            >
              重新加载
            </Button>
          </div>
        )}
        {!trendQuery.isPending && !trendQuery.isError && !chartData.length && (
          <div className='bg-muted/20 flex h-96 flex-col items-center justify-center rounded-lg border border-dashed text-center'>
            <ChartNoAxesCombined className='text-muted-foreground size-7' />
            <p className='mt-3 text-sm font-medium'>暂无有效请求数据</p>
            <p className='text-muted-foreground mt-1 text-xs'>
              产生计分请求后，这里会显示健康分与关键延迟趋势。
            </p>
          </div>
        )}
        {!trendQuery.isPending &&
          !trendQuery.isError &&
          chartData.length > 0 && (
            <>
              <div className='mb-4 grid gap-3 sm:grid-cols-3'>
                <TrendMetric
                  label='当前健康分'
                  value={String(trend.current_score)}
                  detail={scoreBand(trend.current_score)}
                  valueClassName={scoreTone(trend.current_score)}
                />
                <TrendMetric
                  label='平均健康分'
                  value={trend.average_score.toFixed(1)}
                  detail={`${trend.sample_count} 次有效请求`}
                />
                <TrendMetric
                  label='超时次数'
                  value={String(trend.timeout_count)}
                  detail={`占比 ${((trend.timeout_count / trend.sample_count) * 100).toFixed(1)}%`}
                  valueClassName={
                    trend.timeout_count > 0 ? 'text-destructive' : undefined
                  }
                />
              </div>
              <ChartContainer
                config={chartConfig}
                className='aspect-auto h-80 w-full sm:h-96'
                initialDimension={{ width: 800, height: 384 }}
              >
                <ComposedChart data={chartData} margin={{ left: 4, right: 4 }}>
                  <CartesianGrid vertical={false} />
                  <XAxis
                    dataKey='timestamp'
                    tickLine={false}
                    axisLine={false}
                    minTickGap={30}
                    tickFormatter={(value: number) => formatPointTime(value)}
                  />
                  <YAxis
                    yAxisId='score'
                    domain={[0, 100]}
                    ticks={[0, 20, 40, 60, 80, 100]}
                    tickLine={false}
                    axisLine={false}
                    width={34}
                  />
                  <YAxis
                    yAxisId='latency'
                    orientation='right'
                    domain={[0, 'auto']}
                    tickLine={false}
                    axisLine={false}
                    width={48}
                    tickFormatter={(value: number) => formatLatency(value)}
                  />
                  <Tooltip content={<TrendTooltip />} />
                  <ReferenceLine
                    yAxisId='score'
                    y={60}
                    stroke='var(--destructive)'
                    strokeDasharray='4 4'
                    label={{
                      value: '超时警戒',
                      fill: 'var(--destructive)',
                      fontSize: 11,
                    }}
                  />
                  <Line
                    yAxisId='score'
                    type='monotone'
                    dataKey='score'
                    name='健康分'
                    stroke='var(--color-score)'
                    strokeWidth={2}
                    dot={false}
                    activeDot={{ r: 4 }}
                    isAnimationActive={false}
                  />
                  <Line
                    yAxisId='latency'
                    type='monotone'
                    dataKey='key_latency_ms'
                    name='关键延迟'
                    stroke='var(--color-key_latency_ms)'
                    strokeWidth={2}
                    strokeDasharray='5 4'
                    dot={false}
                    activeDot={{ r: 4 }}
                    isAnimationActive={false}
                  />
                  <Line
                    yAxisId='score'
                    type='linear'
                    dataKey='timeout_score'
                    name='超时请求'
                    stroke='transparent'
                    strokeWidth={0}
                    dot={{
                      r: 4,
                      fill: 'var(--destructive)',
                      stroke: 'var(--background)',
                      strokeWidth: 2,
                    }}
                    activeDot={{ r: 5, fill: 'var(--destructive)' }}
                    connectNulls={false}
                    isAnimationActive={false}
                  />
                </ComposedChart>
              </ChartContainer>
              <div className='text-muted-foreground mt-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs'>
                <span className='flex items-center gap-1.5'>
                  <span className='size-2 rounded-full bg-(--chart-1)' />
                  健康分（左轴）
                </span>
                <span className='flex items-center gap-1.5'>
                  <span className='size-2 rounded-full bg-(--chart-2)' />
                  关键延迟（右轴）
                </span>
                <span className='flex items-center gap-1.5'>
                  <span className='bg-destructive size-2 rounded-full' />
                  超时请求
                </span>
                <span>分数：90+ 优秀 · 75+ 良好 · 60+ 偏慢 · 低于 60 超时</span>
              </div>
            </>
          )}
      </div>
    </section>
  )
}
