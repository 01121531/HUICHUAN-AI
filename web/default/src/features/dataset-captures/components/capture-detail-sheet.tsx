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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import dayjs from '@/lib/dayjs'

import { getCaptureRecord } from '../api'
import { formatCaptureBytes } from '../lib'

type CaptureDetailSheetProps = {
  captureId: string | null
  onOpenChange: (open: boolean) => void
}

function JsonBlock(props: { value: unknown }) {
  return (
    <pre className='bg-muted/35 overflow-auto rounded-md border p-3 font-mono text-xs leading-5 [overflow-wrap:anywhere] whitespace-pre-wrap'>
      {JSON.stringify(props.value, null, 2)}
    </pre>
  )
}

type ContentBlock = Record<string, unknown>

function asContentBlocks(value: unknown): ContentBlock[] {
  if (!Array.isArray(value)) return []
  return value.filter(
    (item): item is ContentBlock =>
      Boolean(item) && typeof item === 'object' && !Array.isArray(item)
  )
}

function blockLabel(block: ContentBlock, fallback: string) {
  const type = typeof block.type === 'string' ? block.type : ''
  if (type === 'tool_use') return 'Tool call'
  if (type === 'tool_result') return 'Tool result'
  if (type === 'thinking' || type === 'reasoning') return 'Reasoning'
  if (type === 'image_url' || type === 'media') return 'Media'
  return fallback
}

function blockText(block: ContentBlock) {
  for (const key of ['text', 'thinking', 'content']) {
    const value = block[key]
    if (typeof value === 'string' && value) return value
  }
  return ''
}

function ContentBlocks(props: { value: unknown; emptyLabel: string }) {
  const { t } = useTranslation()
  const blocks = asContentBlocks(props.value)
  if (blocks.length === 0) {
    return <p className='text-muted-foreground text-sm'>{props.emptyLabel}</p>
  }
  return (
    <div className='space-y-2'>
      {blocks.map((block, index) => {
        const text = blockText(block)
        return (
          <div
            key={`${String(block.type ?? 'block')}-${index}`}
            className='bg-muted/35 rounded-md border p-3'
          >
            <div className='text-muted-foreground mb-1 text-[11px] font-medium uppercase tracking-wide'>
              {t(blockLabel(block, 'Content'))}
            </div>
            {text ? (
              <p className='text-sm whitespace-pre-wrap [overflow-wrap:anywhere]'>
                {text}
              </p>
            ) : (
              <JsonBlock value={block} />
            )}
          </div>
        )
      })}
    </div>
  )
}

function ConversationTimeline(props: {
  data: NonNullable<ReturnType<typeof useCaptureDetail>['data']>
}) {
  const { t } = useTranslation()
  const { record } = props.data
  const messages = Array.isArray(record.messages) ? record.messages : []
  const reasoning = record.response.reasoning
  const toolUse =
    record.response.tool_use && typeof record.response.tool_use === 'object'
      ? (record.response.tool_use as { calls?: unknown[] })
      : undefined
  const calls = Array.isArray(toolUse?.calls) ? toolUse.calls : []
  const meta = record._meta
  const reasoningStatus = String(
    meta.reasoning_status ?? reasoning?.visibility ?? 'not_requested'
  )
  const captureStatus = String(meta.capture_status ?? 'complete')
  const statusLabelKeys: Record<string, string> = {
    complete: 'Complete',
    incomplete: 'Incomplete',
    delivery_failed: 'Delivery failed',
    provider_missing: 'Provider missing',
    redacted: 'Redacted',
  }
  const reasoningStatusLabelKeys: Record<string, string> = {
    captured: 'Captured',
    redacted: 'Redacted',
    unsupported: 'Unsupported',
    missing: 'Missing',
    not_requested: 'Not requested',
  }
  const protocolLabelKeys: Record<string, string> = {
    'openai-chat': 'OpenAI Chat',
    'openai-responses': 'OpenAI Responses',
    anthropic: 'Anthropic Messages',
    gemini: 'Gemini',
  }
  const roleLabelKeys: Record<string, string> = {
    user: 'User',
    assistant: 'Assistant',
    tool: 'Tool',
    system: 'System',
  }
  const stopReasonLabelKeys: Record<string, string> = {
    end_turn: 'End turn',
    tool_use: 'Tool use',
    max_tokens: 'Max tokens',
    stop_sequence: 'Stop sequence',
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <h3 className='text-sm font-semibold'>{t('Complete conversation')}</h3>
        <Badge variant='outline'>
          {t('Capture status')}: {t(statusLabelKeys[captureStatus] ?? 'Unknown')}
        </Badge>
        {meta.response_protocol ? (
          <Badge variant='secondary'>
            {t(
              protocolLabelKeys[meta.response_protocol] ??
                meta.response_protocol
            )}
          </Badge>
        ) : null}
      </div>

      {record.system_prompt ? (
        <section className='space-y-2'>
          <h4 className='text-muted-foreground text-xs font-medium uppercase tracking-wide'>
            {t('System prompt')}
          </h4>
          <div className='bg-muted/35 rounded-md border p-3 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]'>
            {record.system_prompt}
          </div>
        </section>
      ) : null}

      <section className='space-y-3'>
        {messages.map((message, index) => {
          const value =
            message && typeof message === 'object'
              ? (message as { role?: unknown; content?: unknown })
              : {}
          const role = typeof value.role === 'string' ? value.role : 'message'
          return (
            <div key={`${role}-${index}`} className='rounded-md border p-3'>
              <div className='mb-2 flex items-center gap-2'>
                <Badge variant={role === 'assistant' ? 'default' : 'secondary'}>
                  {t(roleLabelKeys[role] ?? 'Message')}
                </Badge>
                <span className='text-muted-foreground text-xs'>
                  {t('Conversation context')}
                </span>
              </div>
              <ContentBlocks
                value={value.content}
                emptyLabel={t('This message has no visible content')}
              />
            </div>
          )
        })}
      </section>

      <section className='rounded-md border p-3'>
        <div className='mb-2 flex flex-wrap items-center gap-2'>
          <Badge variant='outline'>{t('Reasoning')}</Badge>
          <span className='text-muted-foreground text-xs'>
            {t('Reasoning status')}: {t(
              reasoningStatusLabelKeys[reasoningStatus] ?? 'Unknown'
            )}
          </span>
        </div>
        {reasoning ? (
          <ContentBlocks
            value={reasoning.blocks}
            emptyLabel={reasoning.content ?? t('No reasoning content returned')}
          />
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('Reasoning status')}: {t(
              reasoningStatusLabelKeys[reasoningStatus] ?? 'Unknown'
            )}
          </p>
        )}
      </section>

      <section className='rounded-md border p-3'>
        <div className='mb-2 flex flex-wrap items-center gap-2'>
          <Badge>{t('Assistant output')}</Badge>
          {record.response.stop_reason ? (
            <span className='text-muted-foreground text-xs'>
              {t('Stop reason')}:{' '}
              {t(
                stopReasonLabelKeys[record.response.stop_reason] ??
                  record.response.stop_reason
              )}
            </span>
          ) : null}
        </div>
        {record.response.content ? (
          <p className='text-sm whitespace-pre-wrap [overflow-wrap:anywhere]'>
            {record.response.content}
          </p>
        ) : calls.length > 0 ? (
          <p className='text-muted-foreground text-sm'>
            {t('This response contains tool calls but no visible text')}
          </p>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('No assistant output was returned')}
          </p>
        )}
      </section>

      {calls.length > 0 ? (
        <section className='space-y-2'>
          <h4 className='text-muted-foreground text-xs font-medium uppercase tracking-wide'>
            {t('Tool calls and results')}
          </h4>
          <JsonBlock value={calls} />
        </section>
      ) : null}
    </div>
  )
}

function Overview(props: {
  data: NonNullable<ReturnType<typeof useCaptureDetail>['data']>
}) {
  const { t } = useTranslation()
  const metadata = props.data.metadata
  const rows = [
    [t('Username'), `${metadata.username} (#${metadata.user_id})`],
    [t('Token'), `${metadata.token_name} (#${metadata.token_id})`],
    [t('Group'), metadata.user_group || t('Unknown')],
    [
      t('Channel ID'),
      metadata.channel_id > 0 ? `#${metadata.channel_id}` : t('Unknown'),
    ],
    [t('Requested model'), metadata.requested_model || t('Unknown')],
    [t('Effective model'), metadata.effective_model],
    [t('Session ID'), metadata.session_id],
    [
      t('Captured at'),
      dayjs.unix(metadata.captured_at).format('YYYY-MM-DD HH:mm:ss'),
    ],
    [t('Record size'), formatCaptureBytes(metadata.record_size)],
  ]
  return (
    <dl className='divide-y rounded-md border'>
      {rows.map(([label, value]) => (
        <div
          key={label}
          className='grid gap-1 px-3 py-2.5 sm:grid-cols-[9rem_minmax(0,1fr)]'
        >
          <dt className='text-muted-foreground text-xs'>{label}</dt>
          <dd className='min-w-0 text-sm font-medium break-words'>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function useCaptureDetail(captureId: string | null) {
  return useQuery({
    queryKey: ['dataset-capture-record', captureId],
    queryFn: () => getCaptureRecord(captureId ?? ''),
    enabled: Boolean(captureId),
  })
}

export function CaptureDetailSheet(props: CaptureDetailSheetProps) {
  const { t } = useTranslation()
  const detailQuery = useCaptureDetail(props.captureId)
  const data = detailQuery.data

  return (
    <Sheet open={Boolean(props.captureId)} onOpenChange={props.onOpenChange}>
      <SheetContent className='w-full gap-0 sm:max-w-3xl'>
        <SheetHeader className='border-b pr-12'>
          <div className='flex min-w-0 items-center gap-2'>
            <SheetTitle>{t('Capture record')}</SheetTitle>
            {data && (
              <Badge variant='outline'>{data.metadata.effective_model}</Badge>
            )}
          </div>
          <SheetDescription className='truncate font-mono text-xs'>
            {data?.metadata.capture_id ?? props.captureId ?? ''}
          </SheetDescription>
        </SheetHeader>

        {detailQuery.isLoading && (
          <div className='space-y-3 p-4'>
            <Skeleton className='h-8 w-56' />
            <Skeleton className='h-48 w-full' />
            <Skeleton className='h-32 w-full' />
          </div>
        )}
        {detailQuery.isError && (
          <div className='text-destructive p-4 text-sm'>
            {t('Failed to load capture record')}
          </div>
        )}
        {data && (
          <Tabs defaultValue='conversation' className='min-h-0 flex-1 gap-0'>
            <div className='overflow-x-auto border-b px-4 py-2'>
              <TabsList variant='line'>
                <TabsTrigger value='conversation'>
                  {t('Conversation')}
                </TabsTrigger>
                <TabsTrigger value='overview'>{t('Overview')}</TabsTrigger>
                <TabsTrigger value='messages'>{t('Messages')}</TabsTrigger>
                <TabsTrigger value='response'>{t('Response')}</TabsTrigger>
                <TabsTrigger value='tools'>{t('Tools')}</TabsTrigger>
                <TabsTrigger value='usage'>{t('Usage')}</TabsTrigger>
                <TabsTrigger value='raw'>{t('Raw data')}</TabsTrigger>
              </TabsList>
            </div>
            <ScrollArea className='min-h-0 flex-1'>
              <div className='p-4'>
                <TabsContent value='conversation'>
                  <ConversationTimeline data={data} />
                </TabsContent>
                <TabsContent value='overview'>
                  <Overview data={data} />
                </TabsContent>
                <TabsContent value='messages' className='space-y-3'>
                  {data.record.system_prompt && (
                    <section>
                      <h3 className='mb-2 text-sm font-medium'>
                        {t('System prompt')}
                      </h3>
                      <div className='bg-muted/35 rounded-md border p-3 text-sm whitespace-pre-wrap'>
                        {data.record.system_prompt}
                      </div>
                    </section>
                  )}
                  <JsonBlock value={data.record.messages} />
                </TabsContent>
                <TabsContent value='response'>
                  <JsonBlock value={data.record.response} />
                </TabsContent>
                <TabsContent value='tools'>
                  <JsonBlock
                    value={{
                      tools: data.record.tools,
                      tool_use: data.record.response.tool_use,
                    }}
                  />
                </TabsContent>
                <TabsContent value='usage'>
                  <JsonBlock value={data.record.response.usage} />
                </TabsContent>
                <TabsContent value='raw'>
                  <JsonBlock value={data.record} />
                </TabsContent>
              </div>
            </ScrollArea>
          </Tabs>
        )}
      </SheetContent>
    </Sheet>
  )
}
