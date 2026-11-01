"use client"

import Link from "next/link"
import { usePathname, useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { useTranslations } from "use-intl"

import { CommandMenu } from "@/components/command-menu"

import { LanguagePicker } from "@/components/language-picker"
import { TelemetryNotice } from "@/components/telemetry-notice"
import { ThemeToggle } from "@/components/theme-toggle"
import { UpdateNotice, useVersionStatus } from "@/components/update-notice"
import { Button } from "@/components/ui/button"
import { useHotkey } from "@/hooks/use-hotkey"
import { useIncidentNotifications } from "@/hooks/use-incident-notifications"
import { api, useApi, type Me, type Overview } from "@/lib/api"
import { settingsHref, useSettingsGroups } from "@/lib/settings-nav"
import { cn } from "@/lib/utils"
import type en from "@/messages/en.json"

const nav: {
  href: string
  key: keyof (typeof en)["nav"]["links"]
  match?: (p: string) => boolean
}[] = [
  {
    href: "/",
    key: "monitors",
    match: (p: string) => p === "/" || p.startsWith("/monitors"),
  },
  { href: "/incidents/", key: "incidents" },
  { href: "/maintenance/", key: "maintenance" },
  { href: "/groups/", key: "groups" },
  { href: "/agents/", key: "agents" },
  { href: "/notifications/", key: "notifications" },
  { href: "/status-pages/", key: "statusPages" },
  { href: "/settings/", key: "settings" },
]

export function Shell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() || "/"
  const t = useTranslations("nav")
  const tc = useTranslations("common")
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 60_000 })
  const ov = useApi<Overview>(me.data ? "/api/overview" : null)
  const [menuOpen, setMenuOpen] = useState(false)
  // Close the mobile menu whenever the page changes (back/forward included).
  const [lastPath, setLastPath] = useState(pathname)
  if (lastPath !== pathname) {
    setLastPath(pathname)
    setMenuOpen(false)
  }
  const ver = useVersionStatus(me.data?.user.role === "admin")
  const tu = useTranslations("update")
  const tk = useTranslations("commandMenu")
  const router = useRouter()
  const [cmdOpen, setCmdOpen] = useState(false)
  const isAdmin = me.data?.user.role === "admin"
  const tset = useTranslations("settings")
  const settingsGroups = useSettingsGroups()

  // Ctrl+K / ⌘K opens the jump menu, also from inside text fields.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setCmdOpen((o) => !o)
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])
  useHotkey("n", () => router.push("/monitors/new/"), isAdmin)

  useStatusInTab(ov.data, pathname)
  useIncidentNotifications(!!me.data)

  async function logout() {
    await api("/api/auth/logout", { method: "POST" }).catch(() => {})
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.href = "/login/"
  }

  if (!me.data) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        <span>
          {tc("loading")}
          <span className="cursor-blink">_</span>
        </span>
      </div>
    )
  }

  const down = ov.data?.status.down ?? 0
  const maint = ov.data?.status.maintenance ?? 0
  const statusText = ov.data
    ? [
        down > 0 ? t("down", { count: down }) : t("allGood"),
        maint > 0 ? t("inMaintenance", { count: maint }) : null,
        t("agents", { online: ov.data.agents_online, total: ov.data.agents }),
      ]
        .filter(Boolean)
        .join(" · ")
    : ""
  const links = nav.map((n) => ({
    ...n,
    label: t(`links.${n.key}`),
    active: n.match ? n.match(pathname) : pathname.startsWith(n.href),
  }))
  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col px-4 sm:px-6">
      <header className="sticky top-0 z-30 -mx-4 border-b border-dashed bg-background/90 px-4 backdrop-blur sm:-mx-6 sm:px-6">
        <div className="flex h-12 items-center gap-x-4 whitespace-nowrap">
          <Link href="/" className="shrink-0 font-semibold tracking-tight">
            <span className="text-primary">&gt;</span> pingtxt
            <span className="cursor-blink text-primary">_</span>
          </Link>
          <nav className="hidden min-w-0 items-center text-muted-foreground lg:flex">
            {links.map((n) => (
              <Link
                key={n.href}
                href={n.href}
                aria-current={n.active ? "page" : undefined}
                className={cn(
                  "px-1 py-0.5 transition-colors hover:text-foreground xl:px-1.5",
                  n.active && "bg-accent text-foreground"
                )}
              >
                {n.active ? `[${n.label}]` : n.label}
              </Link>
            ))}
          </nav>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {ov.data && (
              <span
                className="mr-1 inline-flex items-center gap-1.5 text-xs text-muted-foreground"
                title={statusText}
              >
                <span
                  className={cn(
                    "size-2 rounded-full",
                    down > 0 ? "bg-down" : maint > 0 ? "bg-info" : "bg-up"
                  )}
                  aria-hidden
                />
                <span aria-hidden>
                  {down > 0 && <span className="text-down">{down}↓ · </span>}
                  {t("agents", {
                    online: ov.data.agents_online,
                    total: ov.data.agents,
                  })}
                </span>
                <span className="sr-only">{statusText}</span>
              </span>
            )}
            {me.data.user.role === "viewer" && (
              <span
                className="mr-1 hidden border border-dashed px-1.5 text-xs text-muted-foreground sm:inline lg:hidden xl:inline"
                title={t("readOnlyHint")}
              >
                {t("readOnly")}
              </span>
            )}
            <div className="hidden items-center gap-1 lg:flex">
              <LanguagePicker />
              <ThemeToggle />
              <Button
                variant="ghost"
                size="sm"
                onClick={logout}
                title={me.data.user.email}
              >
                {t("logout")}
              </Button>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="lg:hidden"
              onClick={() => setMenuOpen((o) => !o)}
              aria-expanded={menuOpen}
              aria-controls="mobile-nav"
            >
              {menuOpen ? "[x]" : "[≡]"}
              <span className="sr-only">{t("menu")}</span>
            </Button>
          </div>
        </div>
        {menuOpen && (
          <div
            id="mobile-nav"
            className="-mx-4 border-t border-dashed px-4 pt-2 pb-3 sm:-mx-6 sm:px-6 lg:hidden"
          >
            <div className="mb-1 text-xs text-muted-foreground">
              <span className="text-primary">$</span> ls ~/
            </div>
            <nav className="grid grid-cols-2 gap-x-4 sm:grid-cols-4">
              {links.map((n) => (
                <Link
                  key={n.href}
                  href={n.href}
                  onClick={() => setMenuOpen(false)}
                  aria-current={n.active ? "page" : undefined}
                  className={cn(
                    "px-1.5 py-2 text-muted-foreground transition-colors hover:text-foreground",
                    n.active && "bg-accent text-foreground"
                  )}
                >
                  <span
                    className={cn("text-primary", !n.active && "invisible")}
                  >
                    &gt;{" "}
                  </span>
                  {n.label}/
                </Link>
              ))}
            </nav>
            {ov.data && (
              <div className="mt-2 px-1.5 text-xs text-muted-foreground">
                {statusText}
              </div>
            )}
            <div className="mt-2 flex items-center gap-1 border-t border-dashed pt-2">
              <span className="min-w-0 flex-1 truncate px-1.5 text-xs text-muted-foreground">
                {me.data.user.email}
              </span>
              <LanguagePicker />
              <ThemeToggle />
              <Button variant="ghost" size="sm" onClick={logout}>
                {t("logout")}
              </Button>
            </div>
          </div>
        )}
      </header>
      <CommandMenu
        open={cmdOpen}
        onOpenChange={setCmdOpen}
        pages={[
          ...links.map((n) => ({ href: n.href, label: n.label })),
          ...settingsGroups.flatMap((g) =>
            g.cats.map((slug) => ({
              href: settingsHref(slug),
              label: `${t("links.settings")} › ${tset(`cats.${slug}`)}`,
              hint: tset(`catHints.${slug}`),
              searchOnly: true,
            }))
          ),
        ]}
        actions={
          isAdmin
            ? [
                { href: "/monitors/new/", label: tk("newMonitor") },
                { href: "/status-pages/edit/", label: tk("newStatusPage") },
              ]
            : []
        }
      />
      <UpdateNotice status={ver.data} />
      <TelemetryNotice isAdmin={me.data.user.role === "admin"} />
      <main className="flex-1 py-8">{children}</main>
      <footer className="flex justify-between border-t border-dashed py-3 text-xs text-muted-foreground">
        <span>
          pingtxt {me.data.version}
          {ver.data?.update_available && ver.data.latest && (
            <>
              {" · "}
              <a
                href={ver.data.latest.url}
                target="_blank"
                rel="noreferrer"
                className="text-info underline underline-offset-4"
              >
                {tu("footer", { latest: ver.data.latest.version })}
              </a>
            </>
          )}
        </span>
        <span className="hidden sm:inline">
          <button
            type="button"
            onClick={() => setCmdOpen(true)}
            className="hover:text-foreground"
          >
            {t.rich("jumpHint", {
              kbd: (c) => <kbd className="border px-1">{c}</kbd>,
            })}
          </button>
          {" · "}
          {t.rich("themeHint", {
            kbd: (c) => <kbd className="border px-1">{c}</kbd>,
          })}
        </span>
      </footer>
    </div>
  )
}

// Original tab title and icon, restored when nothing is down.
const baseTitle = "pingtxt"
const baseIcon = "/icon.svg"

/** The prompt icon with a status dot in the corner, as a data URL. */
function iconWithDot(color: string) {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" fill="#111"/><path d="M6 10l6 6-6 6" stroke="#4ade80" stroke-width="3" fill="none" stroke-linecap="square"/><rect x="15" y="20" width="11" height="3" fill="#4ade80"/><circle cx="25" cy="7" r="6.5" fill="${color}" stroke="#111" stroke-width="2"/></svg>`
  return "data:image/svg+xml," + encodeURIComponent(svg)
}

/**
 * Shows how many monitors are down in the browser tab: "(2) pingtxt" and a
 * red dot on the icon (amber when only degraded), so it is visible from
 * another tab.
 */
function useStatusInTab(ov: Overview | undefined, pathname: string) {
  const down = ov?.status.down ?? 0
  const degraded = ov?.status.degraded ?? 0
  useEffect(() => {
    if (!ov) return
    const title = document.title.replace(/^\(\d+\)\s*/, "") || baseTitle
    document.title = down > 0 ? `(${down}) ${title}` : title
    const icon = down > 0 ? iconWithDot("#ef4444") : degraded > 0 ? iconWithDot("#f59e0b") : baseIcon
    let links = document.querySelectorAll<HTMLLinkElement>("link[rel~='icon']")
    if (links.length === 0) {
      const l = document.createElement("link")
      l.rel = "icon"
      document.head.appendChild(l)
      links = document.querySelectorAll<HTMLLinkElement>("link[rel~='icon']")
    }
    links.forEach((l) => {
      l.type = "image/svg+xml"
      l.href = icon
    })
  }, [ov, down, degraded, pathname])
}
