"use client"

import { useEffect, useState, useSyncExternalStore } from "react"
import useSWR from "swr"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { LanguagePicker } from "@/components/language-picker"
import { StatusPageView, useForcedMode } from "@/components/status-page-view"
import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, ApiError, type PageTheme, type PublicStatusPage } from "@/lib/api"
import { customCSS, themeCSS } from "@/lib/page-theme"
import { useNow } from "@/hooks/use-now"
import { cn } from "@/lib/utils"

const noop = () => () => {}

/**
 * The page is served at /status/<slug>/ on the hub, at / on a custom domain
 * (the hub resolves the page from the host) and at /status/?slug= in dev.
 */
function endpoint(): string {
  const m = window.location.pathname.match(/^\/status\/([a-z0-9-]+)\/?$/)
  const slug = m?.[1] ?? new URLSearchParams(window.location.search).get("slug")
  return slug ? `/api/public/status/${encodeURIComponent(slug)}` : "/api/public/status"
}

export default function PublicStatusPage() {
  const path = useSyncExternalStore(noop, endpoint, () => null)
  const now = useNow()
  const t = useTranslations("statusPublic")
  const tc = useTranslations("common")
  const { data, error, mutate } = useSWR<PublicStatusPage>(path, (p: string) => api<PublicStatusPage>(p), {
    refreshInterval: 60_000,
  })
  const locked = error instanceof ApiError && error.status === 403 && (error.data as { password_required?: boolean } | undefined)?.password_required
  const lockedTitle = locked ? ((error.data as { title?: string }).title ?? "") : ""
  const lockedTheme = locked ? (error.data as { theme?: PageTheme }).theme : undefined

  // Lets browsers and feed readers find the feeds of the page.
  const feed = data && path ? `${path}/feed.atom` : undefined
  useEffect(() => {
    if (!feed || !path) return
    const links = [
      { href: feed, type: "application/atom+xml" },
      { href: `${path}/feed.rss`, type: "application/rss+xml" },
    ].map(({ href, type }) => {
      const l = document.createElement("link")
      l.rel = "alternate"
      l.type = type
      l.href = href
      l.title = document.title
      document.head.appendChild(l)
      return l
    })
    return () => links.forEach((l) => l.remove())
  }, [feed, path])

  useEffect(() => {
    if (data) document.title = data.title
    else if (lockedTitle) document.title = lockedTitle
  }, [data, lockedTitle])

  if (locked && path) {
    return <PasswordForm endpoint={`${path}/unlock`} title={lockedTitle} theme={lockedTheme} onUnlocked={() => mutate()} />
  }
  if (error) {
    return (
      <div className="flex min-h-svh flex-col items-center justify-center gap-2 px-4 text-center">
        <div className="text-xs text-muted-foreground">
          <span className="text-primary">$</span> curl status
        </div>
        <div>{error instanceof ApiError && error.status === 404 ? t("notFound") : `! ${error.message}`}</div>
      </div>
    )
  }
  if (!data) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        <span>
          {tc("loading")}<span className="cursor-blink">_</span>
        </span>
      </div>
    )
  }

  return (
    <StatusPageView
      data={data}
      now={now}
      logoBase={path ? `${path}/logo/` : undefined}
      feed={feed}
      notice={<SubscriptionNotice />}
      subscribe={data.subscribe && path ? <SubscribeForm endpoint={`${path}/subscribe`} /> : null}
    />
  )
}

/** Shown instead of a password protected page until the visitor unlocks it. */
function PasswordForm({ endpoint, title, theme, onUnlocked }: { endpoint: string; title: string; theme?: PageTheme; onUnlocked: () => void }) {
  const t = useTranslations("statusPublic.locked")
  useForcedMode(theme?.mode ?? "")
  const css = themeCSS(theme, ":root")
  const custom = customCSS(theme, false)
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      await api(endpoint, { method: "POST", body: { password } })
      onUnlocked()
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) setError(t("wrong"))
      else setError(err instanceof Error ? err.message : t("error"))
      setBusy(false)
    }
  }

  return (
    <div className="pt-page pt-locked mx-auto flex min-h-svh w-full max-w-3xl flex-col px-4 sm:px-6" data-pt-style={theme?.style || undefined}>
      {css && <style>{css}</style>}
      {custom && <style>{custom}</style>}
      <header className="flex items-center gap-4 border-b border-dashed py-3">
        <h1 className="min-w-0 truncate font-semibold tracking-tight">
          <span className="text-primary">&gt;</span> {title}
        </h1>
        <div className="ml-auto flex items-center gap-1">
          <LanguagePicker />
          {!theme?.mode && <ThemeToggle />}
        </div>
      </header>
      <main className="flex flex-1 items-center justify-center py-8">
        <form onSubmit={submit} className="grid w-full max-w-sm gap-3">
          <div className="text-xs text-muted-foreground">
            <span className="text-primary">$</span> unlock status
          </div>
          <div className="font-medium">{t("title")}</div>
          <p className="text-xs text-muted-foreground">{t("hint")}</p>
          <Input
            type="password"
            required
            autoFocus
            maxLength={72}
            autoComplete="current-password"
            aria-label={t("password")}
            placeholder={t("password")}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <Button type="submit" disabled={busy || !password}>
            {busy ? t("checking") : t("submit")}
          </Button>
          {error && <p className="text-xs text-down">! {error}</p>}
        </form>
      </main>
    </div>
  )
}

function SubscribeForm({ endpoint }: { endpoint: string }) {
  const t = useTranslations("statusPublic.subscribe")
  const [email, setEmail] = useState("")
  const [state, setState] = useState<"idle" | "busy" | "sent">("idle")
  const [error, setError] = useState("")

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setState("busy")
    setError("")
    try {
      await api(endpoint, { method: "POST", body: { email } })
      setState("sent")
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"))
      setState("idle")
    }
  }

  return (
    <Section title={t("title")} className="pt-section pt-subscribe">
      {state === "sent" ? (
        <p className="text-up">{t("sent", { email })}</p>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-2 sm:flex-row">
          <Input
            type="email"
            required
            maxLength={254}
            autoComplete="email"
            aria-label={t("email")}
            placeholder={t("placeholder")}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="sm:max-w-xs"
          />
          <Button type="submit" disabled={state === "busy"}>
            {state === "busy" ? t("sending") : t("submit")}
          </Button>
        </form>
      )}
      {error && <p className="mt-2 text-xs text-down">! {error}</p>}
      <p className="mt-2 text-xs text-muted-foreground">{t("hint")}</p>
    </Section>
  )
}

/** Handles the confirm and unsubscribe links of the emails (?confirm=, ?unsubscribe=). */
function SubscriptionNotice() {
  const t = useTranslations("statusPublic.subscribe")
  const [notice, setNotice] = useState<{ ok: boolean; text: string } | null>(null)

  useEffect(() => {
    const q = new URLSearchParams(window.location.search)
    const action = q.has("confirm") ? "confirm" : q.has("unsubscribe") ? "unsubscribe" : null
    if (!action) return
    const token = q.get(action) ?? ""
    q.delete(action)
    const rest = q.toString()
    window.history.replaceState(null, "", window.location.pathname + (rest ? `?${rest}` : ""))
    const req = /^[0-9a-f]{48}$/.test(token)
      ? api<{ email: string }>(`/api/public/subscriptions/${token}/${action}`, { method: "POST" })
      : Promise.reject(new Error("bad token"))
    req
      .then((r) => setNotice({ ok: true, text: action === "confirm" ? t("confirmed", { email: r.email }) : t("unsubscribed", { email: r.email }) }))
      .catch(() => setNotice({ ok: false, text: t("badLink") }))
  }, [t])

  if (!notice) return null
  return <div className={cn("pt-box mt-4 border border-dashed px-4 py-2 text-sm", notice.ok ? "text-up" : "text-down")}>{notice.ok ? "✓" : "!"} {notice.text}</div>
}
