import Link from "next/link"

import { SiteHeader } from "@/components/site-header"
import { StatusLine } from "@/components/status-dot"

export const REPO = "https://github.com/federico-it/pingtxt"

const nav = [
  { href: "/#features", label: "features" },
  { href: "/#install", label: "install" },
  { href: "/docs/", label: "docs" },
  { href: "/docs/api/", label: "api" },
  { href: REPO, label: "github" },
]

export function SiteShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col px-4 sm:px-6">
      <SiteHeader nav={nav} />
      <main className="flex-1">{children}</main>
      <footer className="mt-16 flex flex-wrap items-center justify-between gap-2 border-t border-dashed py-4 text-xs text-muted-foreground">
        <span>
          pingtxt ·{" "}
          <a href={`${REPO}/blob/main/LICENSE`} className="underline-offset-4 hover:text-foreground hover:underline">
            AGPL-3.0 license
          </a>{" "}
          ·{" "}
          <a href={REPO} className="underline-offset-4 hover:text-foreground hover:underline">
            github.com/federico-it/pingtxt
          </a>{" "}
          ·{" "}
          <Link href="/docs/#contributing" className="underline-offset-4 hover:text-foreground hover:underline">
            contributing
          </Link>{" "}
          ·{" "}
          <a href={`${REPO}/blob/main/SECURITY.md`} className="underline-offset-4 hover:text-foreground hover:underline">
            security
          </a>{" "}
          ·{" "}
          <Link href="/privacy/" className="underline-offset-4 hover:text-foreground hover:underline">
            privacy
          </Link>
        </span>
        <StatusLine />
        <span>
          press <kbd className="border px-1">d</kbd> to toggle theme
        </span>
      </footer>
    </div>
  )
}

export function Prompt({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-xs text-muted-foreground">
      <span className="text-up">$</span> {children}
    </div>
  )
}

export function SectionTitle({ id, children }: { id?: string; children: React.ReactNode }) {
  return (
    <h2 id={id} className="flex items-center gap-3 text-[11px] uppercase tracking-wider text-muted-foreground">
      <span>──</span>
      <span>{children}</span>
      <span className="h-px flex-1 border-t border-dashed" />
    </h2>
  )
}
