"use client"

import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { ms } from "@/lib/format"
import { cn } from "@/lib/utils"

export interface TestResult {
  location: string
  name: string
  ok: boolean
  skipped?: boolean
  latency_ms: number
  status_code?: number
  message?: string
}

/** Results of a one-off check, one line per location. Nothing was stored. */
export function TestResults({
  results,
  running,
  onClose,
  className,
}: {
  results: TestResult[] | null
  running: boolean
  onClose?: () => void
  className?: string
}) {
  const t = useTranslations("testCheck")
  if (!running && !results) return null
  return (
    <div
      className={cn("border border-dashed px-3 py-2 text-xs", className)}
      aria-live="polite"
    >
      <div className="mb-1 flex items-center gap-2 text-muted-foreground">
        <span>
          <span className="text-primary">$</span> {t("title")}
        </span>
        {onClose && !running && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="ml-auto px-1"
            onClick={onClose}
          >
            [x]
          </Button>
        )}
      </div>
      {running ? (
        <div className="text-muted-foreground">
          {t("running")}
          <span className="cursor-blink">_</span>
        </div>
      ) : (
        <ul className="grid gap-1">
          {results!.map((r) => (
            <li key={r.location} className="flex flex-wrap items-baseline gap-x-3">
              <span
                className={cn(
                  "w-10 shrink-0 font-medium",
                  r.skipped ? "text-muted-foreground" : r.ok ? "text-up" : "text-down"
                )}
              >
                {r.skipped ? t("skipped") : r.ok ? t("ok") : t("fail")}
              </span>
              <span className="min-w-24 text-muted-foreground">{r.name}</span>
              {!r.skipped && <span>{ms(r.latency_ms)}</span>}
              {!!r.status_code && (
                <span className="text-muted-foreground">HTTP {r.status_code}</span>
              )}
              {r.message && (
                <span className="min-w-0 break-words text-muted-foreground">
                  {r.message}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
      {!running && <p className="mt-1.5 text-[11px] text-muted-foreground">{t("notStored")}</p>}
    </div>
  )
}
