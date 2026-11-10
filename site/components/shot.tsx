/* eslint-disable @next/next/no-img-element */

// Screenshot in a terminal-like frame. `label` is the fake command in the title
// bar, `alt` describes the image for screen readers and search engines. With
// `dark`, the image follows the theme. `priority` loads it right away (use it
// for the first screenshot on a page, the largest thing above the fold).
export function Shot({
  light,
  dark,
  label,
  alt,
  width,
  height,
  caption,
  priority,
}: {
  light: string
  dark?: string
  label: string
  alt: string
  width: number
  height: number
  caption?: string
  priority?: boolean
}) {
  const img = {
    alt,
    width,
    height,
    loading: priority ? ("eager" as const) : ("lazy" as const),
    fetchPriority: priority ? ("high" as const) : undefined,
    decoding: "async" as const,
  }
  return (
    <figure className="space-y-3">
      <div className="border border-edge bg-background shadow-[6px_6px_0_0_var(--color-hard)] dark:shadow-[6px_6px_0_0_var(--color-up)]">
        <div
          aria-hidden
          className="flex items-center gap-1.5 border-b border-edge px-3 py-2 text-[11px] text-muted-foreground"
        >
          <span className="size-2 bg-down/80" />
          <span className="size-2 bg-warn/80" />
          <span className="size-2 bg-up/80" />
          <span className="ml-2 truncate">{label}</span>
        </div>
        <img src={light} {...img} className={dark ? "block h-auto w-full dark:hidden" : "block h-auto w-full"} />
        {dark && <img src={dark} {...img} className="hidden h-auto w-full dark:block" />}
      </div>
      {caption && <figcaption className="text-xs font-semibold text-muted-foreground">{caption}</figcaption>}
    </figure>
  )
}
