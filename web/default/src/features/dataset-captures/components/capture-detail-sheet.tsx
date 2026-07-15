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
          <Tabs defaultValue='overview' className='min-h-0 flex-1 gap-0'>
            <div className='overflow-x-auto border-b px-4 py-2'>
              <TabsList variant='line'>
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
