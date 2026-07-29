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
  ConfirmPaymentComplianceResponse,
  DatasetCaptureModelsResponse,
  DatasetCaptureRuntimeStatusResponse,
  DatasetCaptureSubjectsResponse,
  DatasetCaptureAccessAuditAction,
  DatasetCaptureAccessAuditResponse,
  DatasetCapturePolicy,
  DatasetCapturePolicyResponse,
  FetchUpstreamRatiosRequest,
  NERVBridgePromptResponse,
  LogCleanupTask,
  NERVSelfCheckResponse,
  SystemOptionsResponse,
  SystemTaskListResponse,
  SystemTaskResponse,
  UpdateOptionRequest,
  UpdateOptionResponse,
  UpstreamChannelsResponse,
  UpstreamRatiosResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  return res.data
}

export async function getNERVSelfCheck() {
  const res = await api.get<NERVSelfCheckResponse>('/api/nerv/self-check')
  return res.data
}

export async function getNERVBridgePrompt() {
  const res = await api.get<NERVBridgePromptResponse>(
    '/api/nerv/bridge-prompt'
  )
  return res.data
}

export async function getDatasetCapturePolicy() {
  const res = await api.get<DatasetCapturePolicyResponse>(
    '/api/dataset-capture-policy'
  )
  return res.data
}

export async function updateDatasetCapturePolicy(policy: DatasetCapturePolicy) {
  const res = await api.put<DatasetCapturePolicyResponse>(
    '/api/dataset-capture-policy',
    policy
  )
  return res.data
}

export async function getDatasetCaptureModels() {
  const res = await api.get<DatasetCaptureModelsResponse>(
    '/api/dataset-capture-policy/models'
  )
  return res.data
}

export async function getDatasetCaptureSubjects() {
  const res = await api.get<DatasetCaptureSubjectsResponse>(
    '/api/dataset-capture-policy/subjects'
  )
  return res.data
}

export async function getDatasetCaptureRuntimeStatus() {
  const res = await api.get<DatasetCaptureRuntimeStatusResponse>(
    '/api/dataset-capture-policy/status'
  )
  return res.data
}

export async function sendDatasetCaptureTestAlert() {
  const res = await api.post<{ success: boolean; message: string }>(
    '/api/dataset-capture-policy/test-alert'
  )
  return res.data
}

export async function getDatasetCaptureAccessAudits(params: {
  page: number
  pageSize: number
  action?: DatasetCaptureAccessAuditAction
  admin?: string
}) {
  const res = await api.get<DatasetCaptureAccessAuditResponse>(
    '/api/dataset-capture-policy/access-audits',
    {
      params: {
        page: params.page,
        page_size: params.pageSize,
        action: params.action || undefined,
        admin: params.admin || undefined,
      },
    }
  )
  return res.data
}

export async function confirmPaymentCompliance() {
  const res = await api.post<ConfirmPaymentComplianceResponse>(
    '/api/option/payment_compliance',
    { confirmed: true }
  )
  return res.data
}

export async function startLogCleanupTask(targetTimestamp: number) {
  const res = await api.post<SystemTaskResponse<LogCleanupTask>>(
    '/api/system-task/log-cleanup',
    null,
    {
      params: { target_timestamp: targetTimestamp },
    }
  )
  return res.data
}

export async function getCurrentLogCleanupTask() {
  const res = await api.get<SystemTaskResponse<LogCleanupTask | null>>(
    '/api/system-task/current',
    {
      params: { type: 'log_cleanup' },
    }
  )
  return res.data
}

export async function getSystemTask(taskId: string) {
  const res = await api.get<SystemTaskResponse<LogCleanupTask>>(
    `/api/system-task/${taskId}`
  )
  return res.data
}

export async function listSystemTasks(limit = 20) {
  const res = await api.get<SystemTaskListResponse>('/api/system-task/list', {
    params: { limit },
  })
  return res.data
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio'
  )
  return res.data
}

export async function getUpstreamChannels() {
  const res = await api.get<UpstreamChannelsResponse>(
    '/api/ratio_sync/channels'
  )
  return res.data
}

export async function fetchUpstreamRatios(request: FetchUpstreamRatiosRequest) {
  const res = await api.post<UpstreamRatiosResponse>(
    '/api/ratio_sync/fetch',
    request
  )
  return res.data
}
