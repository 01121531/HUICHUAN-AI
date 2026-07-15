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
import type { CaptureFilters } from './types'

export function formatCaptureBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  }
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}

export function captureFilterCount(filters: CaptureFilters): number {
  return [
    filters.startTime,
    filters.endTime,
    filters.models.length > 0,
    filters.tokenIds.length > 0,
    filters.groups.length > 0,
    filters.channelIds.length > 0,
    filters.username.trim().length > 0,
    filters.content.trim().length > 0,
  ].filter(Boolean).length
}

export function cloneCaptureFilters(filters: CaptureFilters): CaptureFilters {
  return {
    ...filters,
    startTime: filters.startTime ? new Date(filters.startTime) : undefined,
    endTime: filters.endTime ? new Date(filters.endTime) : undefined,
    models: [...filters.models],
    tokenIds: [...filters.tokenIds],
    groups: [...filters.groups],
    channelIds: [...filters.channelIds],
  }
}
