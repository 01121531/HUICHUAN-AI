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
import { api } from '@/lib/api'

import type {
  CaptureDeleteResponse,
  CaptureExportSelection,
  CaptureFacets,
  CaptureFilters,
  CaptureRecordDetail,
  CaptureRecordsPage,
  CaptureUsersPage,
} from './types'

function unixSeconds(value: Date | undefined): number {
  return value ? Math.floor(value.getTime() / 1000) : 0
}

function appendMany(
  params: URLSearchParams,
  key: string,
  values: Array<string | number>
) {
  for (const value of values) params.append(key, String(value))
}

function captureFilterParams(filters: CaptureFilters): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.startTime) {
    params.set('start_time', String(unixSeconds(filters.startTime)))
  }
  if (filters.endTime) {
    params.set('end_time', String(unixSeconds(filters.endTime)))
  }
  appendMany(params, 'model', filters.models)
  appendMany(params, 'token_id', filters.tokenIds)
  appendMany(params, 'group', filters.groups)
  appendMany(params, 'channel_id', filters.channelIds)
  if (filters.username.trim()) params.set('username', filters.username.trim())
  if (filters.content.trim()) params.set('content', filters.content.trim())
  return params
}

function withQuery(path: string, params: URLSearchParams): string {
  const query = params.toString()
  return query ? `${path}?${query}` : path
}

function exportFilter(filters: CaptureFilters) {
  return {
    start_time: unixSeconds(filters.startTime),
    end_time: unixSeconds(filters.endTime),
    models: filters.models,
    token_ids: filters.tokenIds,
    groups: filters.groups,
    channel_ids: filters.channelIds,
    username: filters.username.trim(),
    content: filters.content.trim(),
  }
}

export async function listCaptureUsers(
  filters: CaptureFilters,
  page: number,
  pageSize: number
): Promise<CaptureUsersPage> {
  const params = captureFilterParams(filters)
  params.set('page', String(page))
  params.set('page_size', String(pageSize))
  const response = await api.get(
    withQuery('/api/dataset-captures/users', params)
  )
  return response.data.data as CaptureUsersPage
}

export async function listCaptureUserRecords(
  userId: number,
  filters: CaptureFilters,
  page: number,
  pageSize: number
): Promise<CaptureRecordsPage> {
  const params = captureFilterParams(filters)
  params.set('page', String(page))
  params.set('page_size', String(pageSize))
  const response = await api.get(
    withQuery(`/api/dataset-captures/users/${userId}/records`, params)
  )
  return response.data.data as CaptureRecordsPage
}

export async function getCaptureFacets(): Promise<CaptureFacets> {
  const response = await api.get('/api/dataset-captures/facets')
  return response.data.data as CaptureFacets
}

export async function getCaptureRecord(
  captureId: string
): Promise<CaptureRecordDetail> {
  const response = await api.get(
    `/api/dataset-captures/records/${encodeURIComponent(captureId)}`
  )
  return response.data.data as CaptureRecordDetail
}

function saveBlob(blob: Blob, filename: string) {
  const objectUrl = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(objectUrl)
}

function responseFilename(disposition: unknown): string {
  if (typeof disposition !== 'string') return 'dataset-capture.jsonl'
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
  if (encoded) return decodeURIComponent(encoded)
  return (
    disposition.match(/filename="?([^";]+)"?/i)?.[1] ?? 'dataset-capture.jsonl'
  )
}

export async function exportCaptures(
  selection: CaptureExportSelection
): Promise<void> {
  const response = await api.post(
    '/api/dataset-captures/export',
    {
      user_ids: selection.userIds,
      capture_ids: selection.captureIds,
      all_filtered: selection.allFiltered,
      filter: exportFilter(selection.filters),
    },
    { responseType: 'blob' }
  )
  saveBlob(
    response.data as Blob,
    responseFilename(response.headers['content-disposition'])
  )
}

export async function deleteCaptureRecords(
  selection: CaptureExportSelection
): Promise<CaptureDeleteResponse> {
  const response = await api.delete('/api/dataset-captures/records/batch', {
    data: {
      user_ids: selection.userIds,
      capture_ids: selection.captureIds,
      all_filtered: selection.allFiltered,
      filter: exportFilter(selection.filters),
    },
  })
  return response.data.data as CaptureDeleteResponse
}
