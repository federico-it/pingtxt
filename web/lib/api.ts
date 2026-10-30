import useSWR, { type SWRConfiguration } from "swr"

export type Status = "pending" | "up" | "down" | "degraded" | "paused"
export type MonitorType = "http" | "keyword" | "tcp" | "ping" | "dns"

export interface Monitor {
  id: number
  name: string
  type: MonitorType
  target: string
  interval_sec: number
  timeout_ms: number
  retries: number
  method: string
  expected_status: string
  keyword: string
  keyword_invert: boolean
  dns_record: string
  dns_server: string
  dns_expected: string
  ignore_tls: boolean
  headers: Record<string, string>
  body: string
  locations: string[]
  paused: boolean
  status: Status
  status_since: number
  created_at: number
}

export interface ResultRow {
  id: number
  monitor_id: number
  location: string
  ts: number
  ok: boolean
  latency_ms: number
  status_code: number
  message: string
  cert_expiry: number
}

export interface Bar {
  t: number
  total: number
  ok: number
  avg_ms: number
}

export interface MonitorSummary extends Monitor {
  uptime_24h: number
  avg_ms_24h: number
  last: ResultRow | null
  bars: Bar[]
}

export interface LocationStatus {
  id: string
  name: string
  online: boolean
  last: ResultRow | null
}

export interface MonitorDetail extends MonitorSummary {
  uptime_7d: number
  uptime_30d: number
  location_status: LocationStatus[]
}

export interface Bucket {
  t: number
  location: string
  total: number
  ok: number
  avg_ms: number
  min_ms: number
  max_ms: number
}

export interface Series {
  from: number
  to: number
  size: number
  locations: { id: string; name: string }[]
  buckets: Bucket[]
}

export interface Incident {
  id: number
  monitor_id: number
  monitor_name: string
  started_at: number
  resolved_at: number
  cause: string
}

export interface Agent {
  id: number
  name: string
  location: string
  location_id: string
  version: string
  hostname: string
  platform: string
  remote_addr: string
  last_seen: number
  created_at: number
  online: boolean
}

export interface Location {
  id: string
  name: string
  online: boolean
  kind: "local" | "agent"
}

export type NotifierType =
  "webhook" | "discord" | "slack" | "telegram" | "ntfy" | "email"

export interface Notifier {
  id: number
  name: string
  type: NotifierType
  config: Record<string, string>
  enabled: boolean
  created_at: number
}

export interface Overview {
  monitors: number
  status: Partial<Record<Status, number>>
  agents: number
  agents_online: number
  open_incidents: number
  version: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string
  ) {
    super(message)
  }
}

export async function api<T = unknown>(
  path: string,
  init?: { method?: string; body?: unknown }
): Promise<T> {
  const res = await fetch(path, {
    method: init?.method ?? "GET",
    credentials: "same-origin",
    headers:
      init?.body !== undefined
        ? { "Content-Type": "application/json" }
        : undefined,
    body: init?.body !== undefined ? JSON.stringify(init.body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) {
    if (
      res.status === 401 &&
      typeof window !== "undefined" &&
      !path.startsWith("/api/auth/login")
    ) {
      const here = window.location.pathname
      // Full reload on purpose: drops all cached data of the old session.
      // eslint-disable-next-line @next/next/no-location-assign-relative-destination
      if (!here.startsWith("/login")) window.location.href = "/login/"
    }
    throw new ApiError(res.status, data?.error ?? res.statusText)
  }
  return data as T
}

export function useApi<T>(path: string | null, config?: SWRConfiguration<T>) {
  return useSWR<T>(path, (p: string) => api<T>(p), {
    refreshInterval: 10_000,
    ...config,
  })
}

export interface Identity {
  provider: string
  email: string
  created_at: number
}

export interface User {
  id: number
  email: string
  created_at: number
  has_password: boolean
  two_factor: boolean
  identities: Identity[]
}

export interface Me {
  user: User
  version: string
  recovery_codes_left: number
}

export type TokenScope = "read" | "write"

export interface ApiToken {
  id: number
  name: string
  hint: string
  scope: TokenScope
  created_at: number
  last_used_at: number
  expires_at: number
}

export interface LoginResult {
  two_factor?: boolean
  challenge?: string
}

export interface MonitorDefaults {
  interval_sec: number
  timeout_ms: number
  retries: number
}

export interface GeneralSettings {
  public_url: string
  local_name: string
  local_enabled: boolean
  retention_days: number
  monitor_defaults: MonitorDefaults
}

export type ProviderId = "github" | "google" | "oidc"

export interface ProviderSettings {
  id: ProviderId
  label: string
  enabled: boolean
  client_id: string
  client_secret: string
  client_secret_set: boolean
  callback_url: string
  name?: string
  issuer?: string
  scopes?: string
}

export interface AuthSettings {
  password_login: boolean
  allow_signup: boolean
  allowed_domains: string[]
  providers: ProviderSettings[]
}

export interface Settings {
  general: GeneralSettings
  auth: AuthSettings
  detected_url: string
}

export interface AuthOptions {
  password_login: boolean
  providers: { id: ProviderId; label: string }[]
}

export interface StatusPage {
  id: number
  slug: string
  title: string
  description: string
  domain: string
  published: boolean
  show_incidents: boolean
  monitor_ids: number[]
  created_at: number
}

export type PageStatus = "operational" | "degraded" | "partial_outage" | "major_outage" | "unknown"

export interface PublicMonitor {
  id: number
  name: string
  status: Status
  uptime: number
  uptime_24h: number
  bars: Bar[]
}

export interface PublicStatusPage {
  slug: string
  title: string
  description: string
  published: boolean
  status: PageStatus
  days: number
  monitors: PublicMonitor[]
  show_incidents: boolean
  incidents: { id: number; monitor_name: string; started_at: number; resolved_at: number }[]
  updated_at: number
}

/** Public address of a status page. */
export function statusPageURL(p: Pick<StatusPage, "slug" | "domain">, hubURL: string) {
  return p.domain ? `https://${p.domain}/` : `${hubURL.replace(/\/$/, "")}/status/${p.slug}/`
}
