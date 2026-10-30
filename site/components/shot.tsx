/* eslint-disable @next/next/no-img-element */

// Screenshot in a terminal-like frame. With `dark`, the image follows the theme.
export function Shot({ light, dark, alt, caption }: { light: string; dark?: string; alt: string; caption?: string }) {
  return (
    <figure className="space-y-2">
      <div className="border border-dashed bg-muted/40 p-1">
        <div className="flex items-center gap-1.5 border-b border-dashed px-2 pb-1.5 pt-0.5 text-[11px] text-muted-foreground">
          <span className="size-2 rounded-full bg-down/70" />
          <span className="size-2 rounded-full bg-warn/70" />
          <span className="size-2 rounded-full bg-up/70" />
          <span className="ml-2 truncate">{alt}</span>
        </div>
        <img src={light} alt={alt} loading="lazy" className={dark ? "block w-full dark:hidden" : "block w-full"} />
        {dark && <img src={dark} alt={alt} loading="lazy" className="hidden w-full dark:block" />}
      </div>
      {caption && <figcaption className="text-xs text-muted-foreground">{caption}</figcaption>}
    </figure>
  )
}
