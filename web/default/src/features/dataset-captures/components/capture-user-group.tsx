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
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Download01Icon,
  ViewIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

import { listCaptureUserRecords } from '../api'
import { formatCaptureBytes } from '../lib'
import type {
  CaptureFilters,
  CaptureRecordSummary,
  CaptureUser,
} from '../types'

const RECORD_PAGE_SIZE = 20

type CaptureUserGroupProps = {
  user: CaptureUser
  filters: CaptureFilters
  open: boolean
  canSelect: boolean
  canDownload: boolean
  userSelected: boolean
  selectedCaptureIds: Set<string>
  downloadingId: string | null
  onOpenChange: (open: boolean) => void
  onUserSelect: (user: CaptureUser, selected: boolean) => void
  onRecordSelect: (record: CaptureRecordSummary, selected: boolean) => void
  onView: (captureId: string) => void
  onDownload: (record: CaptureRecordSummary) => void
}

export function CaptureUserGroup(props: CaptureUserGroupProps) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  useEffect(() => setPage(1), [props.filters])

  const recordsQuery = useQuery({
    queryKey: [
      'dataset-capture-user-records',
      props.user.user_id,
      props.filters,
      page,
      RECORD_PAGE_SIZE,
    ],
    queryFn: () =>
      listCaptureUserRecords(
        props.user.user_id,
        props.filters,
        page,
        RECORD_PAGE_SIZE
      ),
    enabled: props.open,
  })
  const records = recordsQuery.data?.items ?? []
  const pageCount = Math.max(
    1,
    Math.ceil(
      (recordsQuery.data?.total ?? props.user.record_count) / RECORD_PAGE_SIZE
    )
  )

  return (
    <Collapsible
      open={props.open}
      onOpenChange={props.onOpenChange}
      className='border-b last:border-b-0'
    >
      <div className='hover:bg-muted/30 flex min-h-14 items-center gap-3 px-3 py-2 sm:px-4'>
        {props.canSelect && (
          <Checkbox
            checked={props.userSelected}
            onCheckedChange={(checked) =>
              props.onUserSelect(props.user, checked === true)
            }
            aria-label={t('Select user {{username}}', {
              username: props.user.username,
            })}
          />
        )}
        <CollapsibleTrigger className='focus-visible:ring-ring flex min-w-0 flex-1 items-center gap-3 rounded-sm text-left outline-none focus-visible:ring-2'>
          <HugeiconsIcon
            icon={ArrowDown01Icon}
            strokeWidth={2}
            className={cn(
              'size-4 shrink-0 transition-transform',
              !props.open && '-rotate-90'
            )}
            aria-hidden='true'
          />
          <span className='min-w-0 flex-1'>
            <span className='block truncate text-sm font-medium'>
              {props.user.username}
              <span className='text-muted-foreground ml-2 font-mono text-xs'>
                #{props.user.user_id}
              </span>
            </span>
            <span className='text-muted-foreground mt-0.5 block truncate text-xs'>
              {t('{{records}} records', { records: props.user.record_count })}
              {' · '}
              {t('{{tokens}} tokens', { tokens: props.user.token_count })}
              {' · '}
              {formatCaptureBytes(props.user.total_size)}
            </span>
          </span>
          <span className='text-muted-foreground hidden shrink-0 text-xs md:block'>
            {dayjs.unix(props.user.last_capture).format('YYYY-MM-DD HH:mm:ss')}
          </span>
        </CollapsibleTrigger>
      </div>

      <CollapsibleContent className='bg-muted/10 border-t'>
        {recordsQuery.isLoading && (
          <div className='space-y-2 p-3'>
            <Skeleton className='h-10 w-full' />
            <Skeleton className='h-10 w-full' />
            <Skeleton className='h-10 w-full' />
          </div>
        )}
        {recordsQuery.isError && (
          <div className='text-destructive px-4 py-6 text-sm'>
            {t('Failed to load capture records')}
          </div>
        )}
        {!recordsQuery.isLoading &&
          !recordsQuery.isError &&
          records.length === 0 && (
            <div className='text-muted-foreground px-4 py-6 text-center text-sm'>
              {t('No capture records')}
            </div>
          )}
        {records.length > 0 && (
          <div className='overflow-x-auto'>
            <Table className='table-fixed md:min-w-[920px] md:table-auto'>
              <TableHeader>
                <TableRow>
                  {props.canSelect && <TableHead className='w-10' />}
                  <TableHead className='w-24 md:w-auto'>{t('Captured at')}</TableHead>
                  <TableHead className='w-24 md:w-auto'>{t('Effective model')}</TableHead>
                  <TableHead className='w-24 md:w-auto'>{t('Token')}</TableHead>
                  <TableHead className='hidden md:table-cell'>{t('Group')}</TableHead>
                  <TableHead className='hidden md:table-cell'>{t('Channel ID')}</TableHead>
                  <TableHead className='hidden md:table-cell'>{t('Session ID')}</TableHead>
                  <TableHead className='w-20 text-right md:w-24'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {records.map((record) => {
                  const selected =
                    props.userSelected ||
                    props.selectedCaptureIds.has(record.capture_id)
                  return (
                    <TableRow
                      key={record.capture_id}
                      data-state={selected ? 'selected' : undefined}
                    >
                      {props.canSelect && (
                        <TableCell>
                          <Checkbox
                            checked={selected}
                            disabled={props.userSelected}
                            onCheckedChange={(checked) =>
                              props.onRecordSelect(record, checked === true)
                            }
                            aria-label={t('Select capture record')}
                          />
                        </TableCell>
                      )}
                      <TableCell className='whitespace-nowrap'>
                        <span className='md:hidden'>
                          {dayjs.unix(record.captured_at).format('MM-DD HH:mm')}
                        </span>
                        <span className='hidden md:inline'>
                          {dayjs.unix(record.captured_at).format('YYYY-MM-DD HH:mm:ss')}
                        </span>
                      </TableCell>
                      <TableCell
                        className='truncate font-medium'
                        title={record.effective_model}
                      >
                        {record.effective_model}
                      </TableCell>
                      <TableCell
                        className='truncate'
                        title={`${record.token_name} (#${record.token_id})`}
                      >
                        <span className='block truncate'>{record.token_name}</span>
                        <span className='text-muted-foreground hidden md:inline'>
                          #{record.token_id}
                        </span>
                      </TableCell>
                      <TableCell className='hidden md:table-cell'>
                        {record.user_group || t('Unknown')}
                      </TableCell>
                      <TableCell className='hidden md:table-cell'>
                        {record.channel_id > 0
                          ? `#${record.channel_id}`
                          : t('Unknown')}
                      </TableCell>
                      <TableCell
                        className='hidden max-w-36 truncate font-mono text-xs md:table-cell'
                        title={record.session_id}
                      >
                        {record.session_id}
                      </TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-1'>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            title={t('View record')}
                            onClick={() => props.onView(record.capture_id)}
                          >
                            <HugeiconsIcon icon={ViewIcon} strokeWidth={2} />
                          </Button>
                          {props.canDownload && (
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              title={t('Download record')}
                              disabled={
                                props.downloadingId === record.capture_id
                              }
                              onClick={() => props.onDownload(record)}
                            >
                              <HugeiconsIcon
                                icon={Download01Icon}
                                strokeWidth={2}
                              />
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
        {recordsQuery.data && recordsQuery.data.total > RECORD_PAGE_SIZE && (
          <div className='flex h-12 items-center justify-between border-t px-3'>
            <span className='text-muted-foreground text-xs'>
              {t('Page {{page}} of {{pages}}', { page, pages: pageCount })}
            </span>
            <div className='flex gap-1'>
              <Button
                variant='outline'
                size='icon-sm'
                title={t('Previous page')}
                disabled={page <= 1 || recordsQuery.isFetching}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
              </Button>
              <Button
                variant='outline'
                size='icon-sm'
                title={t('Next page')}
                disabled={page >= pageCount || recordsQuery.isFetching}
                onClick={() =>
                  setPage((current) => Math.min(pageCount, current + 1))
                }
              >
                <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} />
              </Button>
            </div>
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
