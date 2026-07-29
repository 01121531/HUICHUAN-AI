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

export type UsageLog = {
  id: number
  type: number
  user_id: number
  username: string
  token_name: string
  model_name: string
  channel: number
  channel_name: string
  prompt_tokens: number
  completion_tokens: number
  quota: number
  content: string
  request_id: string
  upstream_request_id: string
  created_at: number | string
  use_time: number
  is_stream: boolean
  other: string
  group?: string
  ip?: string
  status?: string | number
  task_id?: string
  mj_id?: string
  action?: string
  platform?: string
  prompt_en?: string
  finish_time?: number | string
  data?: unknown
  clear?: unknown
  [key: string]: unknown
}
