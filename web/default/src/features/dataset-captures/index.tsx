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
  Database01Icon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  deleteCaptureRecords,
  exportCaptures,
  getCaptureFacets,
  listCaptureUsers,
} from './api'
import { CaptureBatchBar } from './components/capture-batch-bar'
import { CaptureDetailSheet } from './components/capture-detail-sheet'
import { CaptureFiltersBar } from './components/capture-filters'
import { CaptureUserGroup } from './components/capture-user-group'
import { cloneCaptureFilters } from './lib'
import {
  EMPTY_CAPTURE_FILTERS,
  type CaptureFilters,
  type CaptureRecordSummary,
  type CaptureUser,
} from './types'

const USER_PAGE_SIZE = 20
const EXPANDED_STORAGE_KEY = 'dataset-capture-expanded-users'

function initialExpandedUsers(): number[] {
  try {
    const stored = window.localStorage.getItem(EXPANDED_STORAGE_KEY)
    const parsed = stored ? (JSON.parse(stored) as unknown) : []
    return Array.isArray(parsed)
      ? parsed.filter((value): value is number => Number.isInteger(value))
      : []
  } catch {
    return []
  }
}

export function DatasetCaptures() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const authUser = useAuthStore((state) => state.auth.user)
  const canDownload = hasPermission(
    authUser,
    ADMIN_PERMISSION_RESOURCES.DATASET_CAPTURE,
    ADMIN_PERMISSION_ACTIONS.DOWNLOAD
  )
  const canDelete = authUser?.role === ROLE.SUPER_ADMIN
  const canSelect = canDownload || canDelete

  const [filters, setFilters] = useState<CaptureFilters>(() =>
    cloneCaptureFilters(EMPTY_CAPTURE_FILTERS)
  )
  const [page, setPage] = useState(1)
  const [expandedUsers, setExpandedUsers] =
    useState<number[]>(initialExpandedUsers)
  const [selectedUsers, setSelectedUsers] = useState<CaptureUser[]>([])
  const [selectedRecords, setSelectedRecords] = useState<
    CaptureRecordSummary[]
  >([])
  const [allFiltered, setAllFiltered] = useState(false)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [downloadingId, setDownloadingId] = useState<string | null>(null)
  const [exporting, setExporting] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  const usersQuery = useQuery({
    queryKey: ['dataset-capture-users', filters, page, USER_PAGE_SIZE],
    queryFn: () => listCaptureUsers(filters, page, USER_PAGE_SIZE),
  })
  const facetsQuery = useQuery({
    queryKey: ['dataset-capture-facets'],
    queryFn: getCaptureFacets,
  })
  const users = usersQuery.data?.items ?? []
  const pageCount = Math.max(
    1,
    Math.ceil((usersQuery.data?.total ?? 0) / USER_PAGE_SIZE)
  )
  const selectedUserIds = useMemo(
    () => new Set(selectedUsers.map((user) => user.user_id)),
    [selectedUsers]
  )
  const selectedCaptureIds = useMemo(
    () => new Set(selectedRecords.map((record) => record.capture_id)),
    [selectedRecords]
  )
  const selectionSummary = useMemo(() => {
    if (allFiltered) {
      return {
        userCount: usersQuery.data?.total ?? 0,
        recordCount: usersQuery.data?.record_count ?? 0,
        totalSize: usersQuery.data?.total_size ?? 0,
      }
    }
    const representedUsers = new Set(selectedUserIds)
    let recordCount = selectedUsers.reduce(
      (total, user) => total + user.record_count,
      0
    )
    let totalSize = selectedUsers.reduce(
      (total, user) => total + user.total_size,
      0
    )
    for (const record of selectedRecords) {
      representedUsers.add(record.user_id)
      if (!selectedUserIds.has(record.user_id)) {
        recordCount++
        totalSize += record.record_size
      }
    }
    return { userCount: representedUsers.size, recordCount, totalSize }
  }, [
    allFiltered,
    selectedRecords,
    selectedUserIds,
    selectedUsers,
    usersQuery.data,
  ])

  const clearSelection = () => {
    setAllFiltered(false)
    setSelectedUsers([])
    setSelectedRecords([])
  }
  const applyFilters = (next: CaptureFilters) => {
    setFilters(next)
    setPage(1)
    clearSelection()
  }
  const setUserOpen = (userId: number, open: boolean) => {
    setExpandedUsers((current) => {
      const next = open
        ? [...new Set([...current, userId])]
        : current.filter((id) => id !== userId)
      window.localStorage.setItem(EXPANDED_STORAGE_KEY, JSON.stringify(next))
      return next
    })
  }
  const selectUser = (user: CaptureUser, selected: boolean) => {
    setAllFiltered(false)
    setSelectedUsers((current) =>
      selected
        ? [...current.filter((item) => item.user_id !== user.user_id), user]
        : current.filter((item) => item.user_id !== user.user_id)
    )
    if (selected) {
      setSelectedRecords((current) =>
        current.filter((record) => record.user_id !== user.user_id)
      )
    }
  }
  const selectRecord = (record: CaptureRecordSummary, selected: boolean) => {
    setAllFiltered(false)
    setSelectedRecords((current) =>
      selected
        ? [
            ...current.filter((item) => item.capture_id !== record.capture_id),
            record,
          ]
        : current.filter((item) => item.capture_id !== record.capture_id)
    )
  }

  const runExport = async (record?: CaptureRecordSummary) => {
    setExporting(true)
    if (record) setDownloadingId(record.capture_id)
    try {
      await exportCaptures({
        userIds: record ? [] : selectedUsers.map((user) => user.user_id),
        captureIds: record
          ? [record.capture_id]
          : selectedRecords.map((item) => item.capture_id),
        allFiltered: record ? false : allFiltered,
        filters,
      })
      toast.success(t('Export started'))
    } catch {
      toast.error(t('Export failed'))
    } finally {
      setExporting(false)
      setDownloadingId(null)
    }
  }

  const runDelete = async () => {
    if (!allFiltered && selectedUsers.length === 0 && selectedRecords.length === 0)
      return
    setDeleting(true)
    try {
      const response = await deleteCaptureRecords({
        userIds: selectedUsers.map((user) => user.user_id),
        captureIds: selectedRecords.map((record) => record.capture_id),
        allFiltered,
        filters,
      })
      const failed = new Set(
        response.items
          .filter((item) => !item.success)
          .flatMap((item) => item.capture_ids)
      )
      setSelectedUsers([])
      setAllFiltered(false)
      setSelectedRecords((current) =>
        current.filter((record) => failed.has(record.capture_id))
      )
      setDeleteOpen(false)
      setDetailId(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['dataset-capture-users'] }),
        queryClient.invalidateQueries({
          queryKey: ['dataset-capture-user-records'],
        }),
        queryClient.invalidateQueries({ queryKey: ['dataset-capture-facets'] }),
      ])
      if (failed.size === 0) toast.success(t('Delete successful'))
      else toast.error(t('Some conversations could not be deleted'))
    } catch {
      toast.error(t('Delete failed'))
    } finally {
      setDeleting(false)
    }
  }

  const refresh = async () => {
    await Promise.all([
      usersQuery.refetch(),
      facetsQuery.refetch(),
      queryClient.invalidateQueries({
        queryKey: ['dataset-capture-user-records'],
      }),
    ])
  }
  const refreshing = usersQuery.isFetching || facetsQuery.isFetching
  const anySelected =
    allFiltered || selectedUsers.length > 0 || selectedRecords.length > 0

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>
          <span className='inline-flex min-w-0 items-center gap-2'>
            <span className='truncate'>{t('Dataset Captures')}</span>
            <Badge
              variant={
                usersQuery.data?.capture_enabled ? 'default' : 'secondary'
              }
              className='shrink-0'
            >
              {usersQuery.data?.capture_enabled ? t('Enabled') : t('Disabled')}
            </Badge>
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            size='sm'
            disabled={refreshing}
            onClick={() => void refresh()}
          >
            <HugeiconsIcon
              icon={RefreshIcon}
              strokeWidth={2}
              data-icon='inline-start'
              className={cn(refreshing && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col overflow-hidden rounded-md border'>
            <CaptureFiltersBar
              value={filters}
              facets={facetsQuery.data}
              disabled={usersQuery.isFetching}
              onApply={applyFilters}
            />

            <div className='flex min-h-12 shrink-0 items-center gap-3 border-b px-3 sm:px-4'>
              {canSelect && (
                <Checkbox
                  checked={allFiltered}
                  indeterminate={!allFiltered && anySelected}
                  onCheckedChange={(checked) => {
                    clearSelection()
                    setAllFiltered(checked === true)
                  }}
                  aria-label={t('Select all filtered records')}
                />
              )}
              <div className='min-w-0 flex-1 text-sm font-medium'>
                {t('{{users}} users, {{records}} records', {
                  users: usersQuery.data?.total ?? 0,
                  records: usersQuery.data?.record_count ?? 0,
                })}
              </div>
              <span className='text-muted-foreground hidden text-xs sm:block'>
                {usersQuery.data?.node}
              </span>
            </div>

            <div className='min-h-0 flex-1 overflow-auto pb-20'>
              {usersQuery.isLoading && (
                <div className='space-y-2 p-3'>
                  {['one', 'two', 'three', 'four', 'five'].map((key) => (
                    <Skeleton key={key} className='h-14 w-full' />
                  ))}
                </div>
              )}
              {usersQuery.isError && (
                <Empty className='h-full min-h-64 border-0'>
                  <EmptyHeader>
                    <EmptyMedia variant='icon'>
                      <HugeiconsIcon icon={Database01Icon} strokeWidth={2} />
                    </EmptyMedia>
                    <EmptyTitle>
                      {t('Failed to load dataset captures')}
                    </EmptyTitle>
                    <EmptyDescription>
                      {t('Try refreshing the page')}
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              )}
              {!usersQuery.isLoading &&
                !usersQuery.isError &&
                users.length === 0 && (
                  <Empty className='h-full min-h-64 border-0'>
                    <EmptyHeader>
                      <EmptyMedia variant='icon'>
                        <HugeiconsIcon icon={Database01Icon} strokeWidth={2} />
                      </EmptyMedia>
                      <EmptyTitle>{t('No captured users')}</EmptyTitle>
                      <EmptyDescription>
                        {t('No records match the current filters')}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                )}
              {users.map((user) => (
                <CaptureUserGroup
                  key={user.user_id}
                  user={user}
                  filters={filters}
                  open={expandedUsers.includes(user.user_id)}
                  canSelect={canSelect}
                  canDownload={canDownload}
                  userSelected={
                    allFiltered || selectedUserIds.has(user.user_id)
                  }
                  selectedCaptureIds={selectedCaptureIds}
                  downloadingId={downloadingId}
                  onOpenChange={(open) => setUserOpen(user.user_id, open)}
                  onUserSelect={selectUser}
                  onRecordSelect={selectRecord}
                  onView={setDetailId}
                  onDownload={(record) => void runExport(record)}
                />
              ))}
            </div>

            {usersQuery.data && usersQuery.data.total > USER_PAGE_SIZE && (
              <div className='flex h-12 shrink-0 items-center justify-between border-t px-3 sm:px-4'>
                <span className='text-muted-foreground text-xs'>
                  {t('Page {{page}} of {{pages}}', { page, pages: pageCount })}
                </span>
                <div className='flex gap-1'>
                  <Button
                    variant='outline'
                    size='icon-sm'
                    title={t('Previous page')}
                    disabled={page <= 1 || usersQuery.isFetching}
                    onClick={() =>
                      setPage((current) => Math.max(1, current - 1))
                    }
                  >
                    <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
                  </Button>
                  <Button
                    variant='outline'
                    size='icon-sm'
                    title={t('Next page')}
                    disabled={page >= pageCount || usersQuery.isFetching}
                    onClick={() =>
                      setPage((current) => Math.min(pageCount, current + 1))
                    }
                  >
                    <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} />
                  </Button>
                </div>
              </div>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <CaptureDetailSheet
        captureId={detailId}
        onOpenChange={(open) => !open && setDetailId(null)}
      />
      <CaptureBatchBar
        userCount={selectionSummary.userCount}
        recordCount={selectionSummary.recordCount}
        totalSize={selectionSummary.totalSize}
        canDownload={canDownload}
        canDelete={canDelete}
        exporting={exporting}
        deleting={deleting}
        onClear={clearSelection}
        onExport={() => void runExport()}
        onDelete={() => setDeleteOpen(true)}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t('Delete captured conversations')}
        desc={t(
          'Delete the complete conversations containing {{count}} selected records? This action cannot be undone.',
          { count: selectionSummary.recordCount }
        )}
        destructive
        isLoading={deleting}
        confirmText={t('Delete')}
        handleConfirm={() => void runDelete()}
      />
    </>
  )
}
