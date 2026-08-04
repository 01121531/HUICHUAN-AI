export type ProxyGroup = {
  id: number
  name: string
  enabled: boolean
  current_proxy_id: number
  status: string
  max_requests: number
  max_duration_seconds: number
  switch_wait_seconds: number
  max_waiting_requests: number
  health_check_interval: number
  consecutive_timeout_threshold: number
  window_size: number
  window_timeout_ratio: number
  base_cooldown_seconds: number
  max_cooldown_seconds: number
  recovery_success_count: number
  allow_direct_fallback: boolean
  created_at: number
  updated_at: number
}

export type ManagedProxy = {
  id: number
  group_id: number
  name: string
  protocol: string
  host: string
  port: number
  username: string
  enabled: boolean
  status: string
  sort: number
  last_exit_ip: string
  last_check_at: number
  last_check_latency_ms: number
  health_failures: number
  consecutive_timeouts: number
  recovery_failures: number
  cooldown_until: number
  last_used_at: number
  total_requests: number
  total_timeouts: number
}

export type ProxyBinding = {
  id: number
  channel_id: number
  channel_name: string
  proxy_group_id: number
  proxy_group_name: string
  enabled: boolean
}

export type ProxyGroupInput = Omit<
  ProxyGroup,
  'id' | 'current_proxy_id' | 'status' | 'created_at' | 'updated_at'
>

export type ManagedProxyInput = {
  group_id: number
  name: string
  protocol: string
  host: string
  port: number
  username: string
  password?: string
  enabled: boolean
  sort: number
}

export type ProxyBindingInput = {
  proxy_group_id: number
  enabled: boolean
}

export type ProxyApiResponse<T = unknown> = {
  success: boolean
  message?: string
  data?: T
}
