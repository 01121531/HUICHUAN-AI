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
export type CaptureFilters = {
  startTime?: Date
  endTime?: Date
  models: string[]
  tokenIds: number[]
  groups: string[]
  channelIds: number[]
  username: string
  content: string
}

export type CaptureUser = {
  user_id: number
  username: string
  record_count: number
  token_count: number
  last_capture: number
  total_size: number
}

export type CaptureUsersPage = {
  items: CaptureUser[]
  total: number
  page: number
  page_size: number
  record_count: number
  total_size: number
  capture_enabled: boolean
  node: string
}

export type CaptureRecordSummary = {
  capture_id: string
  user_id: number
  username: string
  token_id: number
  token_name: string
  token_scope: string
  user_group: string
  requested_model: string
  effective_model: string
  channel_id: number
  session_id: string
  captured_at: number
  record_size: number
}

export type CaptureRecordsPage = {
  items: CaptureRecordSummary[]
  total: number
  page: number
  page_size: number
}

export type CaptureFacetToken = {
  id: number
  name: string
  scope: string
}

export type CaptureFacets = {
  models: string[]
  tokens: CaptureFacetToken[]
  groups: string[]
  channel_ids: number[]
}

export type CaptureRecord = {
  session_id: string
  user_id_hash: string | null
  model: string
  user_agent: string | null
  system_prompt: string
  tools: unknown[]
  messages: unknown[]
  response: {
    content: string | null
    stop_reason: string | null
    tool_use: unknown
    usage: unknown
  }
  created_at: string | null
  cwd: string | null
  _meta: Record<string, unknown>
}

export type CaptureRecordDetail = {
  metadata: CaptureRecordSummary
  record: CaptureRecord
}

export type CaptureExportSelection = {
  userIds: number[]
  captureIds: string[]
  allFiltered: boolean
  filters: CaptureFilters
}

export type CaptureDeleteItem = {
  capture_ids: string[]
  file_id?: string
  session_id?: string
  deleted_records: number
  success: boolean
  error?: string
}

export type CaptureDeleteResponse = {
  items: CaptureDeleteItem[]
  deleted_conversations: number
  deleted_records: number
}

export const EMPTY_CAPTURE_FILTERS: CaptureFilters = {
  models: [],
  tokenIds: [],
  groups: [],
  channelIds: [],
  username: '',
  content: '',
}
