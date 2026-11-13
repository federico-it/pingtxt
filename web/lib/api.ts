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
  | "udp"
  | "websocket"
  | "statuspage"

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

/** Where a step takes a value from: JSON path, response header or regex. */
export type ExtractFrom = "json" | "header" | "regex"

/** A value of a step's response saved as {{var}}. */
export interface Extract {
  var: string
  from: ExtractFrom
  path: string
}

/** An HTTP request made before an http/keyword check, e.g. a login. */
export interface Step {
  name?: string
  method: string
  url: string
  headers?: Record<string, string>
  body?: string
  expected_status?: string
  extract?: Extract[]
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
  /** http/keyword: requests made first (a login...), passing {{variables}} */
  steps: Step[]
  push_token: string
  grace_sec: number
  /** percent of failing locations that marks the monitor degraded; 0 = instance default */
  degraded_pct: number
  /** response time (ms) above which a location is slow; 0 = off, -1 = automatic */
  slow_ms: number
  /** ping: echo requests per check (1-20, default 3); 0 on other types */
  ping_count: number
  /** ping: down when at least this % of packets is lost; 0 = only when all are */
  ping_loss_pct: number
  /** 4 = IPv4 only, 6 = IPv6 only, 0 = either */
  /** 0 instance default, 1 IPv4 or IPv6, 4, 6, 46 IPv4 and IPv6 */
  ip_family: IPFamily
  /** diagnostics at the first failure: 0 instance default, 1 on, 2 off */
  diagnostics: 0 | 1 | 2
  locations: string[]
  /** 0 = no group */
  group_id: number
  tag_ids: number[]
  /** Monitors this one depends on: while one is down it sends no alerts. */
  depends_on: number[]
  /** Channels linked one by one, with their events (see ChannelLink). */
  notifiers: ChannelLink[]
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
  /** HTTP checks only, from hub and updated agents */
  timing?: Phases
}

/** Time per phase of an HTTP check, in ms. */
export interface Phases {
  dns_ms: number
  connect_ms: number
  tls_ms: number
  /** wait from the request being sent to the first byte */
  ttfb_ms: number
  /** buckets: average latency of the checks with phases */
  total_ms?: number
  /** buckets: how many checks the averages come from */
  n?: number
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
  /** Monitor it depends on that is down while this one fails too (no alerts of its own). */
  blocked_by?: { id: number; name: string; since: number } | null
  /** Open incident (down alert sent), with who acknowledged it. */
  incident?: {
    id: number
    started_at: number
    acked_at: number
    acked_by: string
    /** when its snooze ends (unix ms), 0 = not snoozed */
    snoozed_until?: number
    snoozed_by?: string
  } | null
  /** Why the monitor is down or degraded right now, one entry per problem. */
  reasons?: Reason[]
  /** reasons in one short line (status page monitors: worst component +N); older hubs leave it out */
  short_reason?: string
}

/** One problem of a monitor and the locations that report it. */
export interface Reason {
  kind: "down" | "degraded" | "slow"
  message: string
  locations: string[]
}

export interface LocationStatus {
  id: string
  name: string
  online: boolean
  last: ResultRow | null
  /** slow threshold in use at the location (ms), 0 = none */
  slow_ms: number
  /** ISO 3166-1 alpha-2 country of the location, "" = not set */
  country?: string
  /** last check passed but reported a partial problem (status page) */
  degraded?: boolean
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
  phases?: Phases
}

export interface Series {
  from: number
  to: number
  size: number
  locations: { id: string; name: string }[]
  buckets: Bucket[]
  /** the window and the one of the same length just before it */
  stats?: { current: PeriodStats; previous: PeriodStats }
}

/** Summary of one window of a monitor's history. */
export interface PeriodStats {
  from: number
  to: number
  /** 0..1, -1 = no checks */
  uptime: number
  avg_ms: number
  /** from single results only: `from` is later than the window when older ones were pruned */
  percentiles: { n: number; from: number; p50: number; p95: number; p99: number }
}

export interface Incident {
  id: number
  monitor_id: number
  monitor_name: string
  started_at: number
  resolved_at: number
  cause: string
  /** locations sent diagnostics for it */
  diagnostics: boolean
  /** when someone acknowledged it (unix ms), 0 = nobody: reminders and escalation stop */
  acked_at?: number
  /** email of who acknowledged it, "" = nobody */
  acked_by?: string
  /** when its snooze ends (unix ms; 0 or past = not snoozed): no reminders and no escalation until then */
  snoozed_until?: number
  /** email of who snoozed it */
  snoozed_by?: string
}

/** What a location found out about a failing check (see GET /api/incidents/{id}/diagnostics). */
export interface Diag {
  at: number
  host?: string
  port?: string
  dns?: { resolver: string; query: string; answers?: string[]; cname?: string; ms: number; error?: string }[]
  tcp?: { addr: string; ok: boolean; ms: number; error?: string }[]
  tls?: { addr: string; version?: string; subject?: string; issuer?: string; sans?: string[]; not_after?: number; error?: string }
  http?: { status: number; proto?: string; remote_addr?: string; redirects?: string[]; headers?: { name: string; value: string }[] }
  trace?: {
    proto: "tcp" | "udp"
    dest: string
    port: number
    reached: boolean
    error?: string
    hops?: { ttl: number; addr?: string; name?: string; rtt_ms?: number }[]
  }
  note?: string
  /** monitor type (left out by older agents on old diagnostics) */
  type?: string
  /** error of the failing check */
  check?: string
  /** first line the server sent (SMTP greeting) */
  banner?: string
  ms: number
}

/** What the diagnostics point at, worked out by the hub. Codes can be new: show text for unknown ones. */
export interface DiagFinding {
  code: string
  step: string
  level: "down" | "warn" | "ok"
  params?: Record<string, string>
  text: string
}

export interface Diagnostic {
  id: number
  monitor_id: number
  incident_id: number
  location: string
  location_name: string
  at: number
  data: Diag
  /** most telling first; missing on hubs older than findings */
  findings?: DiagFinding[]
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
  /** IPv6 connectivity: 0 unknown (older agent), 1 available, 2 not available */
  ipv6: 0 | 1 | 2
  last_seen: number
  created_at: number
  online: boolean
  /** send the agent_offline event when it stays disconnected */
  alert_offline: boolean
  /** when it disconnected (unix ms), 0 while connected or never connected */
  offline_since: number
  /** ISO 3166-1 alpha-2 country of the location, "" = not set */
  country?: string
  /** number of monitors that check from this agent */
  monitors: number
  /** version differs from the hub's */
  update_available: boolean
  /** the connected agent can replace its own binary */
  self_update: boolean
  /** why self_update is false (only for online agents) */
  no_update_reason?: string
  update_state?: "updating" | "failed"
  update_error?: string
}

/** The hub's own (built-in) location, listed with the agents. */
export interface LocalAgent {
  location_id: "local"
  name: string
  country: string
  /** the hub runs checks itself */
  enabled: boolean
  ipv6: 0 | 1 | 2
  monitors: number
  version: string
}

/** Why a monitor cannot get a new agent: IPv6 monitor and the agent has no
 * IPv6 (or has not reported it yet), Docker check, or localhost target. */
export type AgentSkipReason = "no_ipv6" | "ipv6_unknown" | "docker" | "loopback"

export interface AgentMonitor {
  id: number
  name: string
  type: string
  target: string
  reason?: AgentSkipReason
}

export interface AgentMonitorPlan {
  compatible: AgentMonitor[]
  skipped: AgentMonitor[]
  /** monitors that already check from this agent */
  already: number
}

/** 0 instance default (monitors only), 1 IPv4 or IPv6 (first to answer),
 * 4 IPv4 only, 6 IPv6 only, 46 IPv4 and IPv6 (both must work) */
export type IPFamily = 0 | 1 | 4 | 6 | 46

export interface Location {
  id: string
  name: string
  online: boolean
  kind: "local" | "agent"
  // IPv6 connectivity: 0 unknown (older agent), 1 available, 2 not available
  ipv6: 0 | 1 | 2
  /** agents only: when it disconnected (unix ms), 0 while online */
  offline_since?: number
  /** ISO 3166-1 alpha-2 country of the location, "" = not set */
  country?: string
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
  /** shown on the status pages that list one of its monitors */
  public?: boolean
  /** public message on status pages and in subscriber emails */
  announcement?: string
  /** email the subscribers of those pages (public windows only) */
  notify_subscribers?: boolean
  /** also email a reminder this many minutes before each occurrence, 0 = none */
  remind_min?: number
  active: boolean
  active_end?: number
  next_start?: number
  next_end?: number
}

export interface Group {
  id: number
  name: string
  description: string
  /** group this one is a subgroup of, 0 = top level */
  parent_id?: number
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
  | "webhook"
  | "discord"
  | "slack"
  | "telegram"
  | "ntfy"
  | "email"
  | "app"
  | "teams"
  | "pagerduty"
  | "opsgenie"
  | "pushover"
  | "gotify"
  | "matrix"
  | "twilio"

/** What a channel can be sent. agent_offline is about agents, not monitors:
 * it ignores the channel's scope and monitor links cannot pick it. */
export type NotifyEvent = "down" | "up" | "ack" | "degraded" | "cert_expiry" | "domain_expiry" | "agent_offline"

export const notifyEvents: NotifyEvent[] = ["down", "up", "ack", "degraded", "cert_expiry", "domain_expiry", "agent_offline"]

/** The events about a monitor, which monitor links can pick. */
export const monitorEvents: NotifyEvent[] = notifyEvents.filter((e) => e !== "agent_offline")

/** Events of a new channel: everything but degraded. */
export const defaultNotifyEvents: NotifyEvent[] = ["down", "up", "cert_expiry", "domain_expiry", "agent_offline"]

/**
 * A channel linked to one monitor with its own events. It wins over the
 * channel's scope: no events leaves the monitor out.
 */
export interface ChannelLink {
  monitor_id?: number
  notifier_id?: number
  events: NotifyEvent[]
}

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
  /** Every monitor, also future ones; otherwise the monitors of group_ids and tag_ids. */
  all_monitors: boolean
  group_ids: number[]
  tag_ids: number[]
  /** What the channel gets for the monitors of its scope. */
  events: NotifyEvent[]
  /** Monitors linked one by one. */
  monitors: ChannelLink[]
  created_at: number
}

/** Steps of the first-run checklist (GET /api/onboarding). */
export type OnboardingStepId =
  | "monitor"
  | "channel"
  | "public_url"
  | "agent"
  | "status_page"
  | "two_factor"
  | "backups"

export interface Onboarding {
  dismissed: boolean
  done: number
  total: number
  /** every step that is not optional is done */
  complete: boolean
  steps: { id: OnboardingStepId; done: boolean; optional?: boolean }[]
}

export interface Overview {
  monitors: number
  status: Partial<Record<Status, number>>
  agents: number
  agents_online: number
  /** agents offline longer than the grace period */
  agents_lost?: { id: number; name: string; offline_since: number }[]
  open_incidents: number
  version: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    /** the JSON body of the answer, if any */
    public data?: unknown
  ) {
    super(message)
  }
}

export async function api<T = unknown>(
  path: string,
  init?: { method?: string; body?: unknown }
): Promise<T> {
  const res = await hubFetch(path, {
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
    throw new ApiError(res.status, data?.error ?? res.statusText, data)
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
  /** ISO 3166-1 alpha-2 country of the hub's own location, "" = not set */
  local_country?: string
  local_enabled: boolean
  retention_days: number
  raw_retention_days: number
  monitor_defaults: MonitorDefaults
  expiry: ExpirySettings
  metrics: MetricsSettings
  /** default percent of failing locations that marks a monitor degraded */
  degraded_pct: number
  ip_family: Exclude<IPFamily, 0>
  /** collect diagnostics at the first failure of a check */
  diagnostics: boolean
  /** days the diagnostics of incidents are kept */
  diag_retention_days: number
  update_check: { disabled: boolean }
  /** days the audit log keeps its entries (30-3650) */
  audit_retention_days: number
}

export type AuditSource = "web" | "api" | "mcp" | "app" | "system" | "cli"
export const auditSources: AuditSource[] = ["web", "api", "mcp", "app", "system", "cli"]

/** One line of the audit log (admins only, see /api/audit). */
export interface AuditEntry {
  id: number
  at: number
  /** 0 = the hub, the command line or an unknown email */
  user_id: number
  user_email: string
  source: AuditSource
  /** API token (with its last characters) or OAuth app */
  credential: string
  /** MCP tool */
  via: string
  ip: string
  remote_addr: string
  user_agent: string
  /** <object>.<verb>, e.g. monitor.update */
  action: string
  target_type: string
  target_id: number
  target_name: string
  result: "ok" | "denied" | "failed"
  /** {"field": [before, after]}; keys starting with _ are details */
  changes?: Record<string, unknown>
  request_id: string
}

/** Anonymous usage stats (admins only), see /api/telemetry. */
export interface TelemetryStatus {
  enabled: boolean
  /** an admin answered the opt-in question */
  asked: boolean
  /** PINGTXT_DISABLE_TELEMETRY or a development build */
  forced_off: boolean
  /** exactly what the next hourly report sends (null when off) */
  report: {
    id: string
    version: string
    monitors: number
    agents: number
  } | null
  sent_at: number
  error?: string
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
  /** where the page name and logo link to (http or https), "" = no link */
  header_url: string
  /** show only the logo in the header, when there is one */
  hide_title: boolean
  /** the logo, only from GET /api/status-pages/{id}; changed with its own routes */
  logo?: PageLogo
  published: boolean
  show_incidents: boolean
  monitor_ids: number[]
  /** every monitor of these groups, current and future */
  group_ids: number[]
  /** email channel that sends incident updates to subscribers, 0 = off */
  subscribe_notifier_id: number
  /** visitors need a password; on save, false removes it */
  protected: boolean
  /** new visitors' password, write-only */
  password?: string
  /** look of the page, empty fields = default */
  theme?: PageTheme
  created_at: number
}

/** #rrggbb colors of one theme, missing or "" = default */
export interface PageColors {
  background?: string
  surface?: string
  text?: string
  muted?: string
  border?: string
  accent?: string
  up?: string
  degraded?: string
  down?: string
}

export interface PageLink {
  label: string
  url: string
}

/** Look of a status page; every field is optional. */
export interface PageTheme {
  mode?: "" | "light" | "dark"
  light?: PageColors
  dark?: PageColors
  font?: "" | "plex" | "figtree" | "atkinson" | "serif" | "system"
  size?: "" | "s" | "l"
  /** corner radius in px, undefined = 2 */
  radius?: number
  style?: "" | "solid" | "cards"
  density?: "" | "compact" | "airy"
  width?: "" | "wide"
  bars?: "" | "round" | "thin"
  header_style?: "" | "band"
  header_align?: "" | "center"
  tagline?: string
  header_links?: PageLink[]
  notice?: string
  footer_text?: string
  footer_links?: PageLink[]
  hide_powered_by?: boolean
  logo_favicon?: boolean
  css?: string
}

export type ReportStatus = "investigating" | "identified" | "monitoring" | "resolved"
export const reportStatuses: ReportStatus[] = ["investigating", "identified", "monitoring", "resolved"]
export type ReportImpact = "none" | "minor" | "major" | "critical"
export const reportImpacts: ReportImpact[] = ["none", "minor", "major", "critical"]

export interface PageIncidentUpdate {
  id: number
  incident_id: number
  status: ReportStatus
  message: string
  created_at: number
}

/** An incident written on a status page, with its timeline. */
export interface PageIncident {
  id: number
  page_id: number
  title: string
  impact: ReportImpact
  /** status of the latest update */
  status: ReportStatus
  created_at: number
  updated_at: number
  /** 0 = still open */
  resolved_at: number
  /** newest first */
  updates: PageIncidentUpdate[]
}

export interface PageSubscriber {
  id: number
  page_id: number
  email: string
  /** 0 = waiting for confirmation */
  confirmed_at: number
  created_at: number
}

/** Events a status page webhook can get. */
export const pageWebhookEvents = [
  "incident.created",
  "incident.updated",
  "incident.resolved",
  "page.status_changed",
  "monitor.status_changed",
  "maintenance.scheduled",
  "maintenance.reminder",
  "maintenance.in_progress",
  "maintenance.completed",
  "maintenance.cancelled",
] as const
export type PageWebhookEvent = (typeof pageWebhookEvents)[number]

export interface PageWebhook {
  id: number
  page_id: number
  url: string
  /** signs every request (HMAC-SHA256) */
  secret: string
  /** empty = every event */
  events: PageWebhookEvent[]
  enabled: boolean
  created_at: number
  /** latest delivery, 0 = never */
  last_at: number
  /** HTTP status of the latest delivery, 0 = no answer */
  last_status: number
  /** "" when it worked */
  last_error: string
}

export interface PageFeeds {
  atom: string
  rss: string
  /** the links carry the private key of a protected page */
  private: boolean
  published: boolean
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

/**
 * Logo of a status page. Files are next to the page JSON:
 * <page path>/logo/logo.svg, or logo.avif, logo.webp and logo.png.
 */
export interface PageLogo {
  svg: boolean
  width: number
  height: number
  /** version, appended as ?v= */
  v: number
}

export interface PublicStatusPage {
  slug: string
  title: string
  description: string
  /** where the page name and logo link to, "" = no link */
  header_url: string
  logo?: PageLogo
  hide_title: boolean
  /** set when the page does not use the default look */
  theme?: PageTheme
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
  /** incidents written on the page: open ones, then recently resolved */
  reports: PageIncident[]
  /** public maintenance windows covering a monitor of the page (missing on older hubs) */
  maintenances?: PublicMaintenance[]
  /** visitors can subscribe by email */
  subscribe: boolean
  updated_at: number
}

/** A public maintenance window: in progress, or its next occurrence. */
export interface PublicMaintenance {
  id: number
  title: string
  description: string
  status: "scheduled" | "in_progress"
  starts_at: number
  ends_at: number
  kind: MaintenanceKind
  /** monitors of the page that it covers */
  monitor_ids: number[]
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

/** Push notifications to the mobile app through the push relay. */
export interface PushSettings {
  enabled: boolean
  relay_url: string
  /** Id of this hub at the relay ("" until registered). */
  hub_id: string
  /** The hub has a token from the relay (never returned). */
  registered: boolean
  /** Phones registered by all users. */
  devices: number
}

export interface BackupSettings {
  enabled: boolean
  interval_hours: number
  keep: number
}

export type ReportKind = "weekly" | "monthly"

export interface ReportSettings {
  weekly: boolean
  monthly: boolean
  /** hour of the day (0-23, in timezone) the reports are sent */
  hour: number
  timezone: string
  /** email channels that receive the reports */
  notifier_ids: number[]
  /** only these groups; empty: every monitor */
  group_ids: number[]
}

export interface Reports extends ReportSettings {
  /** next report of each enabled kind (unix ms) */
  next: Partial<Record<ReportKind, number>>
  last: Partial<Record<ReportKind, { from: number; sent_at: number }>>
}

export interface Report {
  kind: ReportKind
  from: number
  to: number
  timezone: string
  uptime: number
  prev_uptime: number
  incidents: number
  downtime_ms: number
  monitors: number
  subject: string
  /** plain text body of the email */
  text: string
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
  const res = await hubFetch(path, {
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
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText, data)
  return data as T
}

/** A change of a configuration as code plan. */
export interface CodeChange {
  kind: "groups" | "tags" | "channels" | "monitors" | "maintenances" | "status_pages" | "settings"
  name: string
  action: "create" | "update" | "delete"
  fields?: string[]
}

export interface CodePlan {
  changes: CodeChange[]
  unchanged: Record<string, number>
  errors: string[]
  warnings: string[]
}

export interface CodeApplyResult {
  plan: CodePlan
  applied: boolean
  import?: ImportResult
  deleted: Record<string, number>
}

/** The hub's own configuration file (PINGTXT_CONFIG_FILE). */
export interface CodeSyncStatus {
  managed: boolean
  file?: string
  prune: boolean
  applied_at?: number
  checked_at?: number
  error?: string
  plan?: CodePlan
}

/**
 * Fired on window when the hub does not answer (no connection, or 502/503/504
 * from the hub or a proxy): the hub is probably restarting or updating.
 * components/hub-offline.tsx shows a waiting screen.
 */
export const HUB_OFFLINE_EVENT = "pingtxt:hub-offline"

function signalOffline() {
  if (typeof window !== "undefined") window.dispatchEvent(new Event(HUB_OFFLINE_EVENT))
}

async function hubFetch(path: string, init: RequestInit): Promise<Response> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch (err) {
    signalOffline()
    throw err
  }
  if (res.status === 502 || res.status === 503 || res.status === 504) signalOffline()
  return res
}
