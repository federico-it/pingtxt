import Link from "next/link"

import { ThemeToggle } from "@/components/theme"

export const REPO = "https://github.com/federico-it/pingtxt"

const nav = [
  { href: "/#features", label: "features" },
  { href: "/#install", label: "install" },
  { href: "/docs/", label: "docs" },
  { href: REPO, label: "github" },
]

export function SiteShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col px-4 sm:px-6">
      <header className="sticky top-0 z-30 -mx-4 flex flex-wrap items-center gap-x-6 gap-y-1 border-b border-dashed bg-background/90 px-4 py-3 backdrop-blur sm:-mx-6 sm:px-6">
        <Link href="/" className="font-semibold tracking-tight">
          <span className="text-up">&gt;</span> pingtxt<span className="cursor-blink text-up">_</span>
        </Link>
        <nav className="flex flex-wrap items-center gap-1 text-muted-foreground">
          {nav.map((n) => (
            <Link key={n.href} href={n.href} className="px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-foreground">
              {n.label}
            </Link>
          ))}
        </nav>
        <div className="ml-auto">
          <ThemeToggle />
        </div>
      </header>
      <main className="flex-1">{children}</main>
      <footer className="mt-16 flex flex-wrap items-center justify-between gap-2 border-t border-dashed py-4 text-xs text-muted-foreground">
        <span>
          pingtxt · open source ·{" "}
          <a href={REPO} className="underline-offset-4 hover:text-foreground hover:underline">
            github.com/federico-it/pingtxt
          </a>
        </span>
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
