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
import type { Table } from '@tanstack/react-table'
import { Download, FileSpreadsheet, Loader2, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Progress } from '@/components/ui/progress'

import {
  downloadUsageLogExportTask,
  exportUsageLogs,
  getUsageLogExportTask,
} from '../api'
import type { UsageLog } from '../data/schema'
import { buildApiParams } from '../lib/utils'
import type { UsageLogExportRequest, UsageLogExportTask } from '../types'

export function UsageLogExportActions({
  table,
  selectedLogs,
  total,
  isAdmin,
  searchParams,
}: {
  table: Table<UsageLog>
  selectedLogs: UsageLog[]
  total: number
  isAdmin: boolean
  searchParams: Record<string, unknown>
}) {
  const { t } = useTranslation()
  const [exporting, setExporting] = useState(false)
  const [backgroundTask, setBackgroundTask] =
    useState<UsageLogExportTask | null>(null)
  const [taskDialogOpen, setTaskDialogOpen] = useState(false)
  const [downloading, setDownloading] = useState(false)
  const filters = useMemo(() => {
    const params = buildApiParams({
      page: 1,
      pageSize: 1,
      searchParams,
      columnFilters: table.getState().columnFilters,
      isAdmin,
    })
    const { p: _page, page_size: _pageSize, ...exportFilters } = params
    void _page
    void _pageSize
    return exportFilters
  }, [isAdmin, searchParams, table])

  useEffect(() => {
    if (
      !backgroundTask?.task_id ||
      (backgroundTask.status !== 'pending' &&
        backgroundTask.status !== 'running')
    ) {
      return
    }
    let cancelled = false
    const poll = async () => {
      try {
        const task = await getUsageLogExportTask(
          backgroundTask.task_id,
          isAdmin
        )
        if (cancelled) return
        setBackgroundTask(task)
        if (task.status === 'succeeded') {
          toast.success(t('Background export completed'))
        } else if (task.status === 'failed') {
          toast.error(task.error || t('Background export failed'))
        }
      } catch {
        if (!cancelled) {
          toast.error(t('Failed to refresh export progress'))
        }
      }
    }
    const timer = window.setInterval(() => void poll(), 1500)
    void poll()
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [backgroundTask?.status, backgroundTask?.task_id, isAdmin, t])

  const runExport = async (
    selectionMode: 'selected' | 'filtered',
    format: 'csv' | 'xlsx'
  ) => {
    if (selectionMode === 'selected' && selectedLogs.length === 0) {
      toast.warning(t('Please select at least one log'))
      return
    }
    const request: UsageLogExportRequest = {
      selection_mode: selectionMode,
      format,
      filters,
      ...(selectionMode === 'selected'
        ? {
            ids: selectedLogs.map((log) => log.id),
            request_ids: selectedLogs
              .map((log) => log.request_id)
              .filter(Boolean),
          }
        : {}),
    }
    setExporting(true)
    try {
      const result = await exportUsageLogs(request, isAdmin)
      if (result.downloaded) {
        toast.success(t('Export completed'))
      } else if (result.requiresBackground) {
        if (result.task) {
          setBackgroundTask(result.task)
          setTaskDialogOpen(true)
        }
        toast.info(
          result.message ||
            t('The export is large and has been moved to a background task')
        )
      }
    } catch {
      toast.error(t('Export failed'))
    } finally {
      setExporting(false)
    }
  }

  const downloadBackgroundExport = async () => {
    if (!backgroundTask?.task_id) return
    const format =
      backgroundTask.state?.format || backgroundTask.result?.format || 'xlsx'
    setDownloading(true)
    try {
      await downloadUsageLogExportTask(backgroundTask.task_id, isAdmin, format)
      toast.success(t('Export downloaded'))
    } catch {
      toast.error(t('Export download failed'))
    } finally {
      setDownloading(false)
    }
  }

  const taskProgress = Math.min(
    100,
    Math.max(0, backgroundTask?.state?.progress || 0)
  )
  const taskPhase =
    backgroundTask?.state?.phase || backgroundTask?.status || 'pending'

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant='outline'
              size='sm'
              disabled={exporting}
              className='h-8 gap-1.5'
            />
          }
        >
          {exporting ? (
            <Loader2 className='size-3.5 animate-spin' />
          ) : (
            <Download className='size-3.5' />
          )}
          <span className='hidden sm:inline'>{t('Export')}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-64'>
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t('Selected logs')} ({selectedLogs.length})
            </DropdownMenuLabel>
            <DropdownMenuItem
              disabled={selectedLogs.length === 0}
              onClick={() => void runExport('selected', 'xlsx')}
            >
              <FileSpreadsheet className='mr-2 size-4' />
              XLSX
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={selectedLogs.length === 0}
              onClick={() => void runExport('selected', 'csv')}
            >
              <Download className='mr-2 size-4' />
              CSV
            </DropdownMenuItem>
            {selectedLogs.length > 0 && (
              <DropdownMenuItem onClick={() => table.resetRowSelection()}>
                <X className='mr-2 size-4' />
                {t('Clear selection')}
              </DropdownMenuItem>
            )}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t('All filtered logs')} ({total.toLocaleString()})
            </DropdownMenuLabel>
            <DropdownMenuItem
              disabled={total === 0}
              onClick={() => void runExport('filtered', 'xlsx')}
            >
              <FileSpreadsheet className='mr-2 size-4' />
              XLSX
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={total === 0}
              onClick={() => void runExport('filtered', 'csv')}
            >
              <Download className='mr-2 size-4' />
              CSV
            </DropdownMenuItem>
          </DropdownMenuGroup>
          {backgroundTask && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => setTaskDialogOpen(true)}>
                {backgroundTask.status === 'pending' ||
                backgroundTask.status === 'running' ? (
                  <Loader2 className='mr-2 size-4 animate-spin' />
                ) : (
                  <FileSpreadsheet className='mr-2 size-4' />
                )}
                {t('View background export')}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={taskDialogOpen} onOpenChange={setTaskDialogOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Background export')}</DialogTitle>
            <DialogDescription>
              {t(
                'Large exports run in the background and do not block API requests.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-3 py-2'>
            <div className='flex items-center justify-between gap-3 text-sm'>
              <span className='font-medium'>{t(taskPhase)}</span>
              <span className='text-muted-foreground tabular-nums'>
                {taskProgress}%
              </span>
            </div>
            <Progress value={taskProgress} />
            <div className='text-muted-foreground flex justify-between gap-3 text-xs'>
              <span>
                {t('{{processed}} of {{total}} log entries processed.', {
                  processed: backgroundTask?.state?.processed || 0,
                  total: backgroundTask?.state?.total || total,
                })}
              </span>
              {backgroundTask?.state?.expires_at ? (
                <span>
                  {t('Expires')}:{' '}
                  {new Date(
                    backgroundTask.state.expires_at * 1000
                  ).toLocaleString()}
                </span>
              ) : null}
            </div>
            {backgroundTask?.status === 'failed' && (
              <p className='text-destructive text-sm'>
                {backgroundTask.state?.error_code ||
                  backgroundTask.error ||
                  t('Background export failed')}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setTaskDialogOpen(false)}>
              {t('Close')}
            </Button>
            <Button
              disabled={
                backgroundTask?.status !== 'succeeded' ||
                backgroundTask?.state?.phase !== 'succeeded' ||
                downloading
              }
              onClick={() => void downloadBackgroundExport()}
            >
              {downloading ? (
                <Loader2 className='mr-2 size-4 animate-spin' />
              ) : (
                <Download className='mr-2 size-4' />
              )}
              {t('Download')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
