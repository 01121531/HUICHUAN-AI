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
  Cancel01Icon,
  Delete02Icon,
  FileExportIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Spinner } from '@/components/ui/spinner'

import { formatCaptureBytes } from '../lib'

type CaptureBatchBarProps = {
  userCount: number
  recordCount: number
  totalSize: number
  canDownload: boolean
  canDelete: boolean
  exporting: boolean
  deleting: boolean
  onClear: () => void
  onExport: () => void
  onDelete: () => void
}

export function CaptureBatchBar(props: CaptureBatchBarProps) {
  const { t } = useTranslation()
  if (props.userCount === 0 && props.recordCount === 0) return null

  return (
    <div className='bg-background fixed bottom-4 left-1/2 z-40 flex w-[min(46rem,calc(100vw-1.5rem))] -translate-x-1/2 flex-wrap items-center gap-2 rounded-md border px-3 py-2 shadow-lg'>
      <Button
        variant='ghost'
        size='icon-sm'
        title={t('Clear selection')}
        onClick={props.onClear}
      >
        <HugeiconsIcon icon={Cancel01Icon} strokeWidth={2} />
      </Button>
      <div className='min-w-0 flex-1 text-xs sm:text-sm'>
        <span className='font-medium'>
          {t('{{users}} users, {{records}} records', {
            users: props.userCount,
            records: props.recordCount,
          })}
        </span>
        <span className='text-muted-foreground ml-2'>
          {formatCaptureBytes(props.totalSize)}
        </span>
      </div>
      <Separator orientation='vertical' className='hidden h-6 sm:block' />
      {props.canDelete && (
        <Button
          variant='destructive'
          size='sm'
          disabled={props.deleting}
          onClick={props.onDelete}
        >
          {props.deleting ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <HugeiconsIcon
              icon={Delete02Icon}
              strokeWidth={2}
              data-icon='inline-start'
            />
          )}
          {t('Delete selected')}
        </Button>
      )}
      {props.canDownload && (
        <Button size='sm' disabled={props.exporting} onClick={props.onExport}>
          {props.exporting ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <HugeiconsIcon
              icon={FileExportIcon}
              strokeWidth={2}
              data-icon='inline-start'
            />
          )}
          {t('Export JSONL')}
        </Button>
      )}
    </div>
  )
}
