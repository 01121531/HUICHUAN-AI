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
  ArrowLeft01Icon,
  ArrowRight01Icon,
  RefreshIcon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatCaptureBytes } from '@/features/dataset-captures/lib'

import { getDatasetCaptureAccessAudits } from '../api'
import type { DatasetCaptureAccessAuditAction } from '../types'

const AUDIT_PAGE_SIZE = 20

export function DatasetCaptureAccessAuditSection() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [action, setAction] = useState<DatasetCaptureAccessAuditAction | ''>('')
  const [adminInput, setAdminInput] = useState('')
  const [admin, setAdmin] = useState('')
  const query = useQuery({
    queryKey: ['dataset-capture-access-audits', page, action, admin],
    queryFn: () =>
      getDatasetCaptureAccessAudits({
        page,
        pageSize: AUDIT_PAGE_SIZE,
        action: action || undefined,
        admin,
      }),
    placeholderData: (previous) => previous,
  })
  const data = query.data?.data
  const pages = useMemo(
    () => Math.max(1, Math.ceil((data?.total ?? 0) / AUDIT_PAGE_SIZE)),
    [data?.total]
  )

  const applyAdminFilter = () => {
    setPage(1)
    setAdmin(adminInput.trim())
  }

  return (
    <div className="space-y-3 border-t pt-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h4 className="text-sm font-semibold">{t('Capture access audit')}</h4>
          <p className="text-muted-foreground mt-1 text-sm">
            {t(
              'Track which administrator viewed or downloaded each captured record'
            )}
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="icon-sm"
          title={t('Refresh')}
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          <HugeiconsIcon
            icon={RefreshIcon}
            strokeWidth={2}
            className={query.isFetching ? 'animate-spin' : undefined}
          />
        </Button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <NativeSelect
          size="sm"
          value={action}
          aria-label={t('Access action')}
          onChange={(event) => {
            setPage(1)
            setAction(
              event.target.value as DatasetCaptureAccessAuditAction | ''
            )
          }}
        >
          <NativeSelectOption value="">{t('All actions')}</NativeSelectOption>
          <NativeSelectOption value="view">{t('Viewed')}</NativeSelectOption>
          <NativeSelectOption value="download">
            {t('Downloaded')}
          </NativeSelectOption>
        </NativeSelect>
        <Input
          className="w-52"
          value={adminInput}
          placeholder={t('Administrator username')}
          aria-label={t('Administrator username')}
          onChange={(event) => setAdminInput(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') applyAdminFilter()
          }}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={applyAdminFilter}
        >
          <HugeiconsIcon
            icon={Search01Icon}
            strokeWidth={2}
            data-icon="inline-start"
          />
          {t('Search')}
        </Button>
      </div>

      <div className="overflow-hidden rounded-md border">
        {query.isLoading ? (
          <div className="space-y-2 p-3">
            {Array.from({ length: 5 }, (_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : query.isError ? (
          <Empty className="min-h-48">
            <EmptyHeader>
              <EmptyTitle>
                {t('Failed to load capture access audits')}
              </EmptyTitle>
              <EmptyDescription>
                {t('Refresh the page and try again')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : !data || data.items.length === 0 ? (
          <Empty className="min-h-48">
            <EmptyHeader>
              <EmptyTitle>{t('No capture access audits')}</EmptyTitle>
              <EmptyDescription>
                {t('View and download activity will appear here')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table className="min-w-[1180px] table-fixed">
            <TableHeader>
              <TableRow>
                <TableHead className="w-40">{t('Accessed at')}</TableHead>
                <TableHead className="w-36">{t('Administrator')}</TableHead>
                <TableHead className="w-32">{t('Action')}</TableHead>
                <TableHead className="w-36">{t('Captured user')}</TableHead>
                <TableHead className="w-36">{t('Token')}</TableHead>
                <TableHead className="w-40">{t('Model and route')}</TableHead>
                <TableHead className="w-52">{t('Capture ID')}</TableHead>
                <TableHead className="w-40">{t('Session ID')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((entry) => (
                <TableRow key={`${entry.event_id}-${entry.capture_id}`}>
                  <TableCell className="whitespace-nowrap">
                    {dayjs.unix(entry.created_at).format('YYYY-MM-DD HH:mm:ss')}
                  </TableCell>
                  <TableCell>
                    <span
                      className="block truncate font-medium"
                      title={entry.operator_username}
                    >
                      {entry.operator_username}
                    </span>
                      <span
                        className="text-muted-foreground block truncate text-xs"
                        title={`${entry.auth_method} · ${entry.ip}`}
                      >
                        #{entry.operator_user_id} · {entry.auth_method} ·{' '}
                        {entry.ip}
                      </span>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-col items-start gap-1">
                      <Badge
                        variant={
                          entry.action === 'download' ? 'default' : 'secondary'
                        }
                      >
                        {entry.action === 'download'
                          ? t('Downloaded')
                          : t('Viewed')}
                      </Badge>
                      <span className="text-muted-foreground text-xs">
                        {entry.outcome === 'delivered'
                          ? t('Delivered')
                          : entry.outcome === 'failed'
                            ? t('Failed')
                            : t('Prepared')}
                        {entry.action === 'download' &&
                          ` · ${entry.record_count} · ${formatCaptureBytes(entry.bytes)}`}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <span
                      className="block truncate font-medium"
                      title={entry.username}
                    >
                      {entry.username}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      #{entry.user_id}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="block truncate" title={entry.token_name}>
                      {entry.token_name}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      #{entry.token_id}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span
                      className="block truncate font-medium"
                      title={entry.effective_model}
                    >
                      {entry.effective_model}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      {entry.user_group || t('Unknown')} · {t('Channel')} #
                      {entry.channel_id}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span
                      className="block truncate font-mono text-xs"
                      title={entry.capture_id}
                    >
                      {entry.capture_id}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      {dayjs
                        .unix(entry.capture_created_at)
                        .format('YYYY-MM-DD HH:mm:ss')}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span
                      className="block truncate font-mono text-xs"
                      title={entry.session_id}
                    >
                      {entry.session_id}
                    </span>
                    <span
                      className="text-muted-foreground block truncate text-xs"
                      title={entry.event_id}
                    >
                      {t('Event')} {entry.event_id}
                    </span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {data && data.total > AUDIT_PAGE_SIZE && (
          <div className="flex h-12 items-center justify-between border-t px-3">
            <span className="text-muted-foreground text-xs">
              {t('Page {{page}} of {{pages}}', { page, pages })}
            </span>
            <div className="flex gap-1">
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                title={t('Previous page')}
                disabled={page <= 1 || query.isFetching}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
              </Button>
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                title={t('Next page')}
                disabled={page >= pages || query.isFetching}
                onClick={() =>
                  setPage((current) => Math.min(pages, current + 1))
                }
              >
                <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} />
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
