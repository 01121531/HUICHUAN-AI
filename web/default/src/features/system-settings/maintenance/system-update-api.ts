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

export type SystemUpdateCapability = {
  supported: boolean
  reason?: string
  platform: string
  arch: string
}

export type SystemUpdateRelease = {
  id: number
  tag_name: string
  name?: string
  body?: string
  html_url?: string
  published_at?: string
  current_version: string
  update_available: boolean
  installable: boolean
  reason?: string
  asset_name?: string
}

export type SystemUpdatePhase =
  | 'idle'
  | 'downloading'
  | 'verifying'
  | 'staged'
  | 'restarting'
  | 'validating'
  | 'succeeded'
  | 'failed'
  | 'rolling_back'
  | 'rolled_back'

export type SystemUpdateState = {
  task_id?: string
  phase: SystemUpdatePhase
  progress: number
  current_version?: string
  target_version?: string
  release_id?: number
  message_code?: string
  error_code?: string
  started_at?: number
  updated_at?: number
  completed_at?: number
  restart_required: boolean
}

type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export async function getSystemUpdateCapability() {
  const response = await api.get<ApiResponse<SystemUpdateCapability>>(
    '/api/system-update/capability',
    { skipErrorHandler: true }
  )
  return response.data.data
}

export async function getLatestSystemUpdate() {
  const response = await api.get<ApiResponse<SystemUpdateRelease>>(
    '/api/system-update/latest',
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data.data
}

export async function getSystemUpdateStatus() {
  const response = await api.get<ApiResponse<SystemUpdateState>>(
    '/api/system-update/status',
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data.data
}

export async function startSystemUpdate(releaseId: number) {
  const response = await api.post<ApiResponse<SystemUpdateState>>(
    '/api/system-update',
    { release_id: releaseId },
    { skipErrorHandler: true }
  )
  return response.data.data
}
