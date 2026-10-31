"use client"

import { XIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import type { PageIncident, PageIncidentUpdate, ReportImpact, ReportStatus } from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

/** Color of an incident report status. */
export const reportStatusClass: Record<ReportStatus, string> = {
  investigating: "text-down",
  identified: "text-warn",
  monitoring: "text-primary",
  resolved: "text-up",
}

/** Border color of an open incident, by impact. */
export const reportImpactBorder: Record<ReportImpact, string> = {
  none: "border-border",
  minor: "border-warn/40",
  major: "border-down/40",
  critical: "border-down",
}

/** Text color of the impact label. */
export const reportImpactText: Record<ReportImpact, string> = {
  none: "text-muted-foreground",
  minor: "text-warn",
  major: "text-down",
  critical: "text-down font-medium",
}

export function ReportStatusText({ status, className }: { status: ReportStatus; className?: string }) {
  const t = useTranslations("pageIncidents.status")
  return <span className={cn(reportStatusClass[status] ?? "text-muted-foreground", className)}>{t(status)}</span>
}

/** The updates of an incident, newest first. */
export function ReportTimeline({
  incident,
  onDelete,
}: {
  incident: PageIncident
  onDelete?: (u: PageIncidentUpdate) => void
}) {
  const t = useTranslations("pageIncidents")
  return (
    <ol className="grid gap-3">
      {incident.updates.map((u) => (
        <li key={u.id} className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3">
          <span className="mt-1.5 size-1.5 rounded-full bg-current text-muted-foreground" aria-hidden />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-x-2 text-xs">
              <ReportStatusText status={u.status} className="font-medium" />
              <span className="text-muted-foreground">{datetime(u.created_at)}</span>
              {onDelete && incident.updates.length > 1 && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="ml-auto"
                  aria-label={t("deleteUpdate")}
                  onClick={() => onDelete(u)}
                >
                  <XIcon />
                </Button>
              )}
            </div>
            <p className="mt-0.5 break-words whitespace-pre-line">{u.message}</p>
          </div>
        </li>
      ))}
    </ol>
  )
}
