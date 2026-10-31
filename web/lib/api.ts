import useSWR, { type SWRConfiguration } from "swr"

export type Status =
  "pending" | "up" | "down" | "degraded" | "paused" | "maintenance"
export type MonitorType =
  | "http"
  | "keyword"
  | "tcp"
  | "ping"
  | "dns"
  | "push"
  | "postgres"
  | "mysql"
  | "redis"
  | "mongodb"
  | "docker"
  | "grpc"
  | "smtp"
  | "mcp"

export type AssertionOp =
  | "eq"
  | "ne"
  | "contains"
  | "not_contains"
  | "exists"
  | "not_exists"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "matches"

/** A rule on the JSON body of an http/keyword response. */
export interface Assertion {
  path: string
  op: AssertionOp
  value?: string
}

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
  assertions: Assertion[]
  push_token: string
  grace_sec: number
  /** percent of failing locations that marks the monitor degraded; 0 = instance default */
  degraded_pct: number
  locations: string[]
  /** 0 = no group */
  group_id: number
  tag_ids: number[]
  cert_expiry_alert: boolean
  domain_expiry_alert: boolean
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
  maint: boolean
}

export interface Bar {
  t: number
  total: number
  ok: number // checks taken while the monitor was up or degraded
  degraded?: number // checks taken while the monitor was degraded
  maint: number
  avg_ms: number
}

export interface ExpiryCheck {
  monitor_id: number
  kind: "cert" | "domain"
  /** host name (cert) or registrable domain */
  subject: string
  /** unix ms, 0 = unknown */
  expires_at: number
  /** certificate issuer or registrar */
  issuer: string
  source: "tls" | "rdap" | "whois" | ""
  error: string
  checked_at: number
}

export interface MonitorSummary extends Monitor {
  uptime_24h: number
  avg_ms_24h: number
  last: ResultRow | null
  bars: Bar[]
  expiry: ExpiryCheck[]
  maintenance: { id: number; name: string; end_at: number } | null
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
  push_url?: string
}

export interface Bucket {
  t: number
  location: string
  total: number
  ok: number
  maint: number
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
  /** version differs from the hub's */
  update_available: boolean
  /** the connected agent can replace its own binary */
  self_update: boolean
  /** why self_update is false (only for online agents) */
  no_update_reason?: string
  update_state?: "updating" | "failed"
  update_error?: string
}

export interface Location {
  id: string
  name: string
  online: boolean
  kind: "local" | "agent"
}

export type MaintenanceKind = "once" | "daily" | "weekly" | "monthly"

export interface Maintenance {
  id: number
  name: string
  description: string
  all_monitors: boolean
  monitor_ids: number[]
  /** every monitor of these groups */
  group_ids: number[]
  /** every monitor with one of these tags */
  tag_ids: number[]
  kind: MaintenanceKind
  start_at: number
  end_at: number
  start_time: string
  duration_min: number
  weekdays: number[]
  month_days: number[]
  timezone: string
  enabled: boolean
  created_at: number
  active: boolean
  active_end?: number
  next_start?: number
  next_end?: number
}

export interface Group {
  id: number
  name: string
  description: string
  monitors: number
  created_at: number
}

export const tagColors = [
  "",
  "red",
  "orange",
  "yellow",
  "green",
  "cyan",
  "blue",
  "purple",
  "pink",
  "gray",
] as const
export type TagColor = (typeof tagColors)[number]

export interface Tag {
  id: number
  name: string
  color: TagColor
  monitors: number
  created_at: number
}

export type NotifierType =
  "webhook" | "discord" | "slack" | "telegram" | "ntfy" | "email"

export interface Notifier {
  id: number
  name: string
  type: NotifierType
  config: Record<string, string>
  enabled: boolean
  /** Minutes of downtime before the first alert (0 = right away). */
  delay_min: number
  /** Repeat the alert every this many minutes while down (0 = once). */
  repeat_min: number
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

/**
 * True when the logged-in user can change things (admin role), false for
 * read-only users, undefined while loading.
 */
export function useIsAdmin(): boolean | undefined {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 60_000 })
  return me.data ? me.data.user.role === "admin" : undefined
}

export interface Identity {
  provider: string
  email: string
  created_at: number
}

export type Role = "admin" | "viewer"

/** Labels and hints are in messages: users.roles.<role>. */
export const roles: Role[] = ["admin", "viewer"]

export interface User {
  id: number
  email: string
  role: Role
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
  raw_retention_days: number
  monitor_defaults: MonitorDefaults
  expiry: ExpirySettings
  metrics: MetricsSettings
  /** default percent of failing locations that marks a monitor degraded */
  degraded_pct: number
  update_check: { disabled: boolean }
}

export interface ReleaseInfo {
  version: string
  name: string
  /** release notes on GitHub */
  url: string
  published_at: number
}

export interface VersionStatus {
  current: string
  /** on in the settings and running a release build */
  check_enabled: boolean
  check_disabled: boolean
  latest: ReleaseInfo | null
  update_available: boolean
  checked_at: number
  error?: string
}

export interface MetricsSettings {
  /** serve /metrics without an API token */
  public: boolean
}

export interface ExpirySettings {
  /** days before expiry when an alert is sent; empty = off */
  cert_days: number[]
  domain_days: number[]
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
  default_role: Role
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
  /** every monitor of these groups, current and future */
  group_ids: number[]
  created_at: number
}

export type PageStatus =
  "operational" | "degraded" | "partial_outage" | "major_outage" | "unknown"

export interface PublicMonitor {
  id: number
  name: string
  /** section title, "" = no section */
  group: string
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
  incidents: {
    id: number
    monitor_name: string
    started_at: number
    resolved_at: number
  }[]
  updated_at: number
}

/** Public address of a status page. */
export function statusPageURL(
  p: Pick<StatusPage, "slug" | "domain">,
  hubURL: string
) {
  return p.domain
    ? `https://${p.domain}/`
    : `${hubURL.replace(/\/$/, "")}/status/${p.slug}/`
}

export type BackupKind =
  "auto" | "manual" | "pre-import" | "pre-restore" | "upload"

export interface BackupFile {
  name: string
  kind: BackupKind
  size: number
  created_at: number
}

export interface BackupSettings {
  enabled: boolean
  interval_hours: number
  keep: number
}

export interface Backups {
  settings: BackupSettings
  backups: BackupFile[]
  dir: string
  /** a backup staged to replace the database at the next start */
  pending_restore: { name: string; staged_at: number } | null
  /** database kind: sqlite, postgres or mysql (MariaDB/MySQL) */
  driver: "sqlite" | "postgres" | "mysql"
  /** backup files are made by pingtxt only with SQLite */
  file_backups: boolean
}

export interface ImportResult {
  mode: "merge" | "replace"
  backup?: string
  created: Record<string, number>
  updated: Record<string, number>
  deleted: Record<string, number>
  skipped: Record<string, number>
  settings: boolean
  warnings: string[]
}

/** Summary of a configuration export file, read in the browser before importing. */
export interface ConfigFile {
  format: string
  version: number
  hub_version: string
  exported_at: string
  secrets: boolean
  monitors?: unknown[]
  groups?: unknown[]
  tags?: unknown[]
  notifiers?: unknown[]
  maintenances?: unknown[]
  status_pages?: unknown[]
  agents?: unknown[]
}

/** Sends a raw body (file upload) and parses the JSON answer. */
export async function apiRaw<T = unknown>(
  path: string,
  body: BodyInit,
  contentType: string
): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": contentType },
    body,
  })
  const text = await res.text()
  let data: { error?: string } | null = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = null
  }
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText)
  return data as T
}
