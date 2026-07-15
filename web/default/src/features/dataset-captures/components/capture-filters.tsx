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
  FilterIcon,
  FilterResetIcon,
  Search01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import {
  type Dispatch,
  type SetStateAction,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DateTimePicker } from '@/components/datetime-picker'
import { MultiSelect, type Option } from '@/components/multi-select'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { captureFilterCount, cloneCaptureFilters } from '../lib'
import {
  EMPTY_CAPTURE_FILTERS,
  type CaptureFacets,
  type CaptureFilters,
} from '../types'

type CaptureFiltersProps = {
  value: CaptureFilters
  facets?: CaptureFacets
  disabled?: boolean
  onApply: (filters: CaptureFilters) => void
}

type FilterFieldsProps = {
  idPrefix: string
  draft: CaptureFilters
  setDraft: Dispatch<SetStateAction<CaptureFilters>>
  models: Option[]
  tokens: Option[]
  groups: Option[]
  channels: Option[]
  className: string
}

function FilterFields(props: FilterFieldsProps) {
  const { t } = useTranslation()
  return (
    <div className={props.className}>
      <div className='grid min-w-0 gap-1.5'>
        <Label htmlFor={`${props.idPrefix}-username`}>
          {t('Username or ID')}
        </Label>
        <Input
          id={`${props.idPrefix}-username`}
          value={props.draft.username}
          maxLength={200}
          placeholder={t('Search username')}
          onChange={(event) =>
            props.setDraft((current) => ({
              ...current,
              username: event.target.value,
            }))
          }
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label htmlFor={`${props.idPrefix}-content`}>
          {t('Captured content')}
        </Label>
        <Input
          id={`${props.idPrefix}-content`}
          value={props.draft.content}
          maxLength={200}
          placeholder={t('Search request or response content')}
          onChange={(event) =>
            props.setDraft((current) => ({
              ...current,
              content: event.target.value,
            }))
          }
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label>{t('Start time')}</Label>
        <DateTimePicker
          value={props.draft.startTime}
          onChange={(value) =>
            props.setDraft((current) => ({ ...current, startTime: value }))
          }
          placeholder={t('Any time')}
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label>{t('End time')}</Label>
        <DateTimePicker
          value={props.draft.endTime}
          onChange={(value) =>
            props.setDraft((current) => ({ ...current, endTime: value }))
          }
          placeholder={t('Any time')}
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label htmlFor={`${props.idPrefix}-models`}>{t('Models')}</Label>
        <MultiSelect
          id={`${props.idPrefix}-models`}
          options={props.models}
          selected={props.draft.models}
          onChange={(models) =>
            props.setDraft((current) => ({ ...current, models }))
          }
          placeholder={t('All models')}
          maxVisibleChips={1}
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label htmlFor={`${props.idPrefix}-tokens`}>{t('Tokens')}</Label>
        <MultiSelect
          id={`${props.idPrefix}-tokens`}
          options={props.tokens}
          selected={props.draft.tokenIds.map(String)}
          onChange={(values) =>
            props.setDraft((current) => ({
              ...current,
              tokenIds: values.map(Number),
            }))
          }
          placeholder={t('All tokens')}
          maxVisibleChips={1}
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label htmlFor={`${props.idPrefix}-groups`}>{t('Groups')}</Label>
        <MultiSelect
          id={`${props.idPrefix}-groups`}
          options={props.groups}
          selected={props.draft.groups}
          onChange={(groups) =>
            props.setDraft((current) => ({ ...current, groups }))
          }
          placeholder={t('All groups')}
          maxVisibleChips={1}
        />
      </div>
      <div className='grid min-w-0 gap-1.5'>
        <Label htmlFor={`${props.idPrefix}-channels`}>{t('Channel IDs')}</Label>
        <MultiSelect
          id={`${props.idPrefix}-channels`}
          options={props.channels}
          selected={props.draft.channelIds.map(String)}
          onChange={(values) =>
            props.setDraft((current) => ({
              ...current,
              channelIds: values.map(Number),
            }))
          }
          placeholder={t('All channels')}
          maxVisibleChips={1}
        />
      </div>
    </div>
  )
}

export function CaptureFiltersBar(props: CaptureFiltersProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(() => cloneCaptureFilters(props.value))
  const [mobileOpen, setMobileOpen] = useState(false)
  useEffect(() => setDraft(cloneCaptureFilters(props.value)), [props.value])

  const options = useMemo(
    () => ({
      models: (props.facets?.models ?? []).map((value) => ({
        value,
        label: value,
      })),
      tokens: (props.facets?.tokens ?? []).map((token) => ({
        value: String(token.id),
        label: `${token.name} (#${token.id})`,
      })),
      groups: (props.facets?.groups ?? []).map((value) => ({
        value,
        label: value,
      })),
      channels: (props.facets?.channel_ids ?? []).map((id) => ({
        value: String(id),
        label: `#${id}`,
      })),
    }),
    [props.facets]
  )
  const activeCount = captureFilterCount(props.value)

  const apply = (closeMobile = false) => {
    if (
      draft.startTime &&
      draft.endTime &&
      draft.startTime.getTime() > draft.endTime.getTime()
    ) {
      toast.error(t('Start time must not be after end time'))
      return
    }
    props.onApply(cloneCaptureFilters(draft))
    if (closeMobile) setMobileOpen(false)
  }
  const reset = (closeMobile = false) => {
    const empty = cloneCaptureFilters(EMPTY_CAPTURE_FILTERS)
    setDraft(empty)
    props.onApply(empty)
    if (closeMobile) setMobileOpen(false)
  }

  return (
    <>
      <div className='bg-muted/15 flex min-h-12 items-center gap-2 border-b px-3 md:hidden'>
        <div className='flex min-w-0 flex-1 items-center gap-2 text-sm font-medium'>
          <HugeiconsIcon icon={FilterIcon} strokeWidth={2} aria-hidden='true' />
          <span>{t('Capture filters')}</span>
          {activeCount > 0 && <Badge variant='secondary'>{activeCount}</Badge>}
        </div>
        <Button variant='outline' size='sm' onClick={() => setMobileOpen(true)}>
          <HugeiconsIcon
            icon={FilterIcon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Filter')}
        </Button>
      </div>
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent className='w-full gap-0 sm:max-w-md'>
          <SheetHeader className='border-b pr-12'>
            <SheetTitle>{t('Capture filters')}</SheetTitle>
            <SheetDescription className='sr-only'>
              {t('Capture filters')}
            </SheetDescription>
          </SheetHeader>
          <div className='min-h-0 flex-1 overflow-y-auto p-4'>
            <FilterFields
              idPrefix='capture-mobile'
              draft={draft}
              setDraft={setDraft}
              {...options}
              className='grid gap-4'
            />
          </div>
          <SheetFooter className='grid grid-cols-2 border-t'>
            <Button
              variant='outline'
              disabled={props.disabled}
              onClick={() => reset(true)}
            >
              <HugeiconsIcon
                icon={FilterResetIcon}
                strokeWidth={2}
                data-icon='inline-start'
              />
              {t('Reset')}
            </Button>
            <Button disabled={props.disabled} onClick={() => apply(true)}>
              <HugeiconsIcon
                icon={Search01Icon}
                strokeWidth={2}
                data-icon='inline-start'
              />
              {t('Apply filters')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <section className='bg-muted/15 hidden border-b px-4 py-3 md:block'>
        <div className='mb-3 flex min-h-8 items-center justify-between gap-3'>
          <div className='flex items-center gap-2 text-sm font-medium'>
            <HugeiconsIcon
              icon={FilterIcon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <span>{t('Capture filters')}</span>
            {activeCount > 0 && (
              <Badge variant='secondary'>
                {t('{{count}} active', { count: activeCount })}
              </Badge>
            )}
          </div>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              disabled={props.disabled}
              onClick={() => reset()}
            >
              <HugeiconsIcon
                icon={FilterResetIcon}
                strokeWidth={2}
                data-icon='inline-start'
              />
              {t('Reset')}
            </Button>
            <Button size='sm' disabled={props.disabled} onClick={() => apply()}>
              <HugeiconsIcon
                icon={Search01Icon}
                strokeWidth={2}
                data-icon='inline-start'
              />
              {t('Apply filters')}
            </Button>
          </div>
        </div>
        <FilterFields
          idPrefix='capture-desktop'
          draft={draft}
          setDraft={setDraft}
          {...options}
          className='grid grid-cols-2 gap-3 xl:grid-cols-3 2xl:grid-cols-4'
        />
      </section>
    </>
  )
}
