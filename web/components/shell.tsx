"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"

import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import { api, useApi, type Overview } from "@/lib/api"
import { cn } from "@/lib/utils"

const nav = [
  { href: "/", label: "monitors", match: (p: string) => p === "/" || p.startsWith("/monitors") },
  { href: "/incidents/", label: "incidents" },
  { href: "/agents/", label: "agents" },
  { href: "/notifications/", label: "notify" },
  { href: "/status-pages/", label: "status" },
  { href: "/settings/", label: "settings" },
]

export function Shell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() || "/"
  const me = useApi<{ user: { email: string }; version: string }>("/api/auth/me", { refreshInterval: 60_000 })
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
          loading<span className="cursor-blink">_</span>
        </span>
      </div>
    )
  }

  const down = ov.data?.status.down ?? 0
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
                {active ? `[${n.label}]` : ` ${n.label} `}
              </Link>
            )
          })}
        </nav>
        <div className="ml-auto flex items-center gap-1">
          {ov.data && (
            <span className="mr-2 hidden text-xs text-muted-foreground md:inline">
              {down > 0 ? <span className="text-down">{down} down</span> : <span className="text-up">all systems go</span>} ·{" "}
              {ov.data.agents_online}/{ov.data.agents} agents
            </span>
          )}
          <ThemeToggle />
          <Button variant="ghost" size="sm" onClick={logout} title={me.data.user.email}>
            logout
          </Button>
        </div>
      </header>
      <main className="flex-1 py-8">{children}</main>
      <footer className="flex justify-between border-t border-dashed py-3 text-xs text-muted-foreground">
        <span>pingtxt {me.data.version}</span>
        <span>
          press <kbd className="border px-1">d</kbd> to toggle theme
        </span>
      </footer>
    </div>
  )
}
