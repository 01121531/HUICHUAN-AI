import { api } from '@/lib/api'

import type {
  ManagedProxy,
  ManagedProxyInput,
  ProxyApiResponse,
  ProxyBinding,
  ProxyBindingInput,
  ProxyGroup,
  ProxyGroupInput,
} from './proxy-management-types'

export async function listProxyGroups() {
  const response =
    await api.get<ProxyApiResponse<ProxyGroup[]>>('/api/proxy/groups')
  return response.data
}

export async function createProxyGroup(data: ProxyGroupInput) {
  const response = await api.post<ProxyApiResponse<ProxyGroup>>(
    '/api/proxy/groups',
    data
  )
  return response.data
}

export async function updateProxyGroup(id: number, data: ProxyGroupInput) {
  const response = await api.put<ProxyApiResponse<ProxyGroup>>(
    `/api/proxy/groups/${id}`,
    data
  )
  return response.data
}

export async function deleteProxyGroup(id: number) {
  const response = await api.delete<ProxyApiResponse>(`/api/proxy/groups/${id}`)
  return response.data
}

export async function listGroupProxies(groupId: number) {
  const response = await api.get<ProxyApiResponse<ManagedProxy[]>>(
    `/api/proxy/groups/${groupId}/proxies`
  )
  return response.data
}

export async function createManagedProxy(data: ManagedProxyInput) {
  const response = await api.post<ProxyApiResponse<ManagedProxy>>(
    '/api/proxy/proxies',
    data
  )
  return response.data
}

export async function updateManagedProxy(id: number, data: ManagedProxyInput) {
  const response = await api.put<ProxyApiResponse<ManagedProxy>>(
    `/api/proxy/proxies/${id}`,
    data
  )
  return response.data
}

export async function deleteManagedProxy(id: number) {
  const response = await api.delete<ProxyApiResponse>(
    `/api/proxy/proxies/${id}`
  )
  return response.data
}

export async function listProxyBindings() {
  const response = await api.get<ProxyApiResponse<ProxyBinding[]>>(
    '/api/proxy/bindings'
  )
  return response.data
}

export async function upsertProxyBinding(
  channelId: number,
  data: ProxyBindingInput
) {
  const response = await api.put<ProxyApiResponse<ProxyBinding>>(
    `/api/proxy/bindings/${channelId}`,
    data
  )
  return response.data
}

export async function deleteProxyBinding(channelId: number) {
  const response = await api.delete<ProxyApiResponse>(
    `/api/proxy/bindings/${channelId}`
  )
  return response.data
}
