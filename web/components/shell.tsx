"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { useTranslations } from "use-intl"

import { LanguagePicker } from "@/components/language-picker"
import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import { api, useApi, type Me, type Overview } from "@/lib/api"
import { cn } from "@/lib/utils"
import type en from "@/messages/en.json"

const nav: { href: string; key: keyof (typeof en)["nav"]["links"]; match?: (p: string) => boolean }[] = [
  { href: "/", key: "monitors", match: (p: string) => p === "/" || p.startsWith("/monitors") },
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

  async function logout() {
    await api("/api/auth/logout", { method: "POST" }).catch(() => {})
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.href = "/login/"
  }

  if (!me.data) {
    return (
      <div className="flex min-h-svh items-center justify-center text-muted-foreground">
        <span>
          {tc("loading")}<span className="cursor-blink">_</span>
        </span>
      </div>
    )
  }

  const down = ov.data?.status.down ?? 0
  const maint = ov.data?.status.maintenance ?? 0
  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col px-4 sm:px-6">
      <header className="sticky top-0 z-30 -mx-4 flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-dashed bg-background/90 px-4 py-3 backdrop-blur sm:-mx-6 sm:px-6">
        <Link href="/" className="font-semibold tracking-tight">
          <span className="text-primary">&gt;</span> pingtxt<span className="cursor-blink text-primary">_</span>
        </Link>
        <nav className="flex flex-wrap items-center gap-1 text-muted-foreground">
          {nav.map((n) => {
            const active = n.match ? n.match(pathname) : pathname.startsWith(n.href)
            return (
              <Link
                key={n.href}
                href={n.href}
                className={cn(
                  "px-1.5 py-0.5 transition-colors hover:text-foreground",
                  active && "bg-accent text-foreground"
                )}
              >
                {active ? `[${t(`links.${n.key}`)}]` : ` ${t(`links.${n.key}`)} `}
              </Link>
            )
          })}
        </nav>
        <div className="ml-auto flex items-center gap-1">
          {ov.data && (
            <span className="mr-2 hidden text-xs text-muted-foreground md:inline">
              {down > 0 ? <span className="text-down">{t("down", { count: down })}</span> : <span className="text-up">{t("allGood")}</span>} ·{" "}
              {maint > 0 && <span className="text-info">{t("inMaintenance", { count: maint })} · </span>}
              {t("agents", { online: ov.data.agents_online, total: ov.data.agents })}
            </span>
          )}
          {me.data.user.role === "viewer" && (
            <span className="mr-1 border border-dashed px-1.5 text-xs text-muted-foreground" title={t("readOnlyHint")}>
              {t("readOnly")}
            </span>
          )}
          <LanguagePicker />
          <ThemeToggle />
          <Button variant="ghost" size="sm" onClick={logout} title={me.data.user.email}>
            {t("logout")}
          </Button>
        </div>
      </header>
      <main className="flex-1 py-8">{children}</main>
      <footer className="flex justify-between border-t border-dashed py-3 text-xs text-muted-foreground">
        <span>pingtxt {me.data.version}</span>
        <span>{t.rich("themeHint", { kbd: (c) => <kbd className="border px-1">{c}</kbd> })}</span>
      </footer>
    </div>
  )
}
