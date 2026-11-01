import { cn } from "@/lib/utils"

export function PageHeader({
  cmd,
  title,
  children,
  className,
}: {
  cmd: string
  title: React.ReactNode
  children?: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn("mb-6 flex flex-wrap items-end justify-between gap-3", className)}>
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">
          <span className="text-primary">$</span> {cmd}
        </div>
        <h1 className="mt-1 truncate text-lg font-semibold tracking-tight">{title}</h1>
      </div>
      {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  )
}

export function Section({ id, title, children, className, actions }: { id?: string; title: string; children: React.ReactNode; className?: string; actions?: React.ReactNode }) {
  return (
    <section id={id} className={cn("mt-8 scroll-mt-20", className)}>
      <div className="mb-2 flex items-center gap-3 text-xs text-muted-foreground uppercase">
        <span className="shrink-0">── {title}</span>
        <span className="h-px flex-1 border-t border-dashed" />
        {actions}
      </div>
      {children}
    </section>
  )
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="border border-dashed p-8 text-center text-muted-foreground">{children}</div>
}

export function KV({ k, v, className }: { k: string; v: React.ReactNode; className?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      <div className="text-[11px] text-muted-foreground uppercase">{k}</div>
      <div className="truncate text-base">{v}</div>
    </div>
  )
}
