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
import { AlertTriangle, Calculator } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { formatLogQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import type {
  BillingComponent,
  BillingSnapshotV1,
  LogOtherData,
} from '../types'

const MODE_KEYS: Record<string, string> = {
  per_token: 'Per-token',
  per_call: 'Per-call',
  tiered_expr: 'Dynamic Pricing',
  subscription: 'Subscription',
  free: 'Free',
  violation_fee: 'Violation Fee',
  unknown: 'Unknown',
}

function SnapshotValue({
  label,
  value,
  mono,
}: {
  label: string
  value: ReactNode
  mono?: boolean
}) {
  return (
    <div className='bg-background/70 min-w-0 rounded-md border px-2.5 py-2'>
      <div className='text-muted-foreground text-[11px]'>{label}</div>
      <div
        className={cn(
          'mt-0.5 min-w-0 text-xs font-medium break-all',
          mono && 'font-mono tabular-nums'
        )}
      >
        {value}
      </div>
    </div>
  )
}

function formatEffectiveUnitPrice(component: BillingComponent): string {
  const unitPrice = Number(component.unit_price_usd)
  const ratio = Number(component.ratio)

  // Rounding and multiplier adjustments are quota-only reconciliation rows,
  // not independently priced components.
  if (
    !Number.isFinite(unitPrice) ||
    !Number.isFinite(ratio) ||
    (unitPrice === 0 && component.subtotal_quota !== 0)
  ) {
    return '-'
  }

  const effectivePrice = unitPrice * ratio
  const digits = Math.abs(effectivePrice) >= 1 ? 6 : 8
  const formattedPrice = `$${effectivePrice.toLocaleString('en-US', {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  })}`
  const denominator =
    component.price_unit === 1_000_000
      ? 'M'
      : component.price_unit === 1_000
        ? 'K'
        : component.unit

  return `${formattedPrice}/${denominator}`
}

export function BillingSnapshotPanel({
  other,
  compact = false,
}: {
  other: LogOtherData | null
  compact?: boolean
}) {
  const { t } = useTranslation()
  const snapshot = other?.billing_snapshot_v1

  if (!snapshot) {
    return (
      <div className='border-border bg-muted/20 flex items-start gap-2 rounded-md border border-dashed p-3 text-xs'>
        <AlertTriangle className='mt-0.5 size-4 shrink-0 text-amber-500' />
        <div>
          <div className='font-medium'>{t('Legacy billing record')}</div>
          <div className='text-muted-foreground mt-0.5'>
            {t(
              'This historical log has no immutable billing snapshot. Displayed legacy pricing fields may be incomplete.'
            )}
          </div>
        </div>
      </div>
    )
  }

  if (snapshot.status !== 'complete') {
    return (
      <div className='border-destructive/30 bg-destructive/5 flex items-start gap-2 rounded-md border p-3 text-xs'>
        <AlertTriangle className='text-destructive mt-0.5 size-4 shrink-0' />
        <div>
          <div className='font-medium'>{t('Billing snapshot unavailable')}</div>
          <div className='text-muted-foreground mt-0.5 font-mono'>
            {snapshot.error_code || snapshot.status}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className='space-y-2.5'>
      <div className='flex flex-wrap items-center gap-2'>
        <Calculator className='text-primary size-4' />
        <StatusBadge
          label={t(MODE_KEYS[snapshot.mode] ?? 'Unknown')}
          variant='info'
          size='sm'
          copyable={false}
        />
        <StatusBadge
          label={snapshot.source || 'wallet'}
          variant={snapshot.source === 'subscription' ? 'success' : 'neutral'}
          size='sm'
          copyable={false}
        />
        <span className='text-muted-foreground text-[11px]'>
          {snapshot.version}
        </span>
      </div>

      <div
        className={cn(
          'grid gap-2',
          compact ? 'grid-cols-2' : 'grid-cols-2 lg:grid-cols-3'
        )}
      >
        <SnapshotValue
          label={t('Pre-consumed')}
          value={formatLogQuota(snapshot.pre_consumed_quota)}
          mono
        />
        <SnapshotValue
          label={t('Settlement Delta')}
          value={formatLogQuota(snapshot.settlement_delta)}
          mono
        />
        <SnapshotValue
          label={t('Final Charged')}
          value={formatLogQuota(snapshot.final_charged_quota)}
          mono
        />
      </div>

      {snapshot.components.length > 0 && (
        <div className='overflow-x-auto rounded-md border'>
          <table className='w-full min-w-[560px] text-left text-xs'>
            <thead className='bg-muted/50 text-muted-foreground'>
              <tr>
                <th className='px-2.5 py-2 font-medium'>{t('Component')}</th>
                <th className='px-2.5 py-2 font-medium'>{t('Quantity')}</th>
                <th className='px-2.5 py-2 font-medium'>
                  {t('Request-time Unit Price')}
                </th>
                <th className='px-2.5 py-2 text-right font-medium'>
                  {t('Subtotal')}
                </th>
              </tr>
            </thead>
            <tbody>
              {snapshot.components.map((component, index) => (
                <tr
                  key={`${component.kind}-${index}`}
                  className='border-t last:border-b-0'
                >
                  <td className='px-2.5 py-2 font-medium'>
                    {t(component.kind)}
                    {component.note && (
                      <div className='text-muted-foreground max-w-[240px] text-[10px] font-normal'>
                        {component.note}
                      </div>
                    )}
                  </td>
                  <td className='px-2.5 py-2 font-mono'>
                    {component.quantity.toLocaleString()} {component.unit}
                  </td>
                  <td className='px-2.5 py-2 font-mono'>
                    {formatEffectiveUnitPrice(component)}
                  </td>
                  <td className='px-2.5 py-2 text-right font-mono font-medium'>
                    {formatLogQuota(component.subtotal_quota)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {snapshot.mode === 'tiered_expr' && (
        <div className='text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-[11px]'>
          {snapshot.matched_tier && (
            <span>
              {t('Matched Tier')}: {snapshot.matched_tier}
            </span>
          )}
          {snapshot.tier_version && <span>{snapshot.tier_version}</span>}
          {snapshot.tier_hash && (
            <span className='font-mono'>
              {snapshot.tier_hash.slice(0, 12)}…
            </span>
          )}
        </div>
      )}
    </div>
  )
}

export function getBillingSnapshot(
  other: LogOtherData | null
): BillingSnapshotV1 | null {
  return other?.billing_snapshot_v1 ?? null
}
