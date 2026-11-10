// Small pieces shared by server and client components. No server-only
// imports here: client components import this file.

export const REPO = "https://github.com/federico-it/pingtxt"

// The page width. Docs pages (.docs-frame) get a wider one, the home page
// (.site-bleed) draws its own full-width bands and puts WRAP inside them.
export const WRAP = "mx-auto w-full max-w-6xl px-4 sm:px-6 group-has-[.docs-frame]/site:max-w-[88rem]"

// Buttons with an offset shadow, like the docs' previous / next cards.
const btnBase =
  "inline-flex items-center gap-1.5 border border-edge px-4 py-2 text-[12px] font-semibold shadow-[3px_3px_0_0_var(--sh)] transition-all hover:-translate-x-px hover:-translate-y-px hover:shadow-[4px_4px_0_0_var(--sh)] active:translate-x-0.5 active:translate-y-0.5 active:shadow-none"
export const BTN = `${btnBase} bg-background [--sh:var(--color-up)]`
export const BTN_PRIMARY = `${btnBase} bg-primary text-primary-foreground [--sh:var(--color-up)] dark:border-primary dark:[--sh:var(--color-hard)]`

export function Prompt({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-xs text-muted-foreground">
      <span className="text-up">$</span> {children}
    </div>
  )
}

// "// something" above a heading, as on the home page and in the docs.
export function Eyebrow({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={`text-[11px] font-semibold uppercase tracking-widest text-up ${className}`}>
      {"// "}
      {children}
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
