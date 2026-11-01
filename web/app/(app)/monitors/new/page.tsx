"use client"

import { useSearchParams } from "next/navigation"
import { Suspense } from "react"
import { useTranslations } from "use-intl"

import { AdminOnly } from "@/components/admin-only"
import { emptyMonitor, MonitorForm, type MonitorInput } from "@/components/monitor-form"
import { PageHeader } from "@/components/page-header"
import { useApi, type MonitorDetail, type Settings } from "@/lib/api"

export default function NewMonitorPage() {
  return (
    <AdminOnly>
      <Suspense>
        <NewMonitor />
      </Suspense>
    </AdminOnly>
  )
}

/** The fields of a monitor that a copy keeps (not its id, status or push URL). */
function copyOf(m: MonitorDetail, name: string): MonitorInput {
  const out = { ...emptyMonitor }
  for (const k of Object.keys(emptyMonitor) as (keyof MonitorInput)[]) {
    if (m[k] !== undefined && m[k] !== null) (out as Record<string, unknown>)[k] = m[k]
  }
  return { ...out, name, paused: false }
}

function NewMonitor() {
  const from = Number(useSearchParams().get("from")) || 0
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  const source = useApi<MonitorDetail>(from ? `/api/monitors/${from}` : null, {
    refreshInterval: 0,
    revalidateOnFocus: false,
  })
  const defaults = settings.data?.general.monitor_defaults
  const t = useTranslations("monitorForm")
  if (from && source.error) return <div className="text-down">! {source.error.message}</div>
  const ready = from ? !!source.data : !!(settings.data || settings.error)
  return (
    <>
      <PageHeader
        cmd={from ? `pingtxt monitors cp ${from}` : "pingtxt monitors add"}
        title={source.data ? t("copyTitle", { name: source.data.name }) : t("newTitle")}
      />
      {ready && (
        <MonitorForm
          key={from}
          initial={
            source.data
              ? copyOf(source.data, t("copyName", { name: source.data.name }))
              : { ...emptyMonitor, ...defaults }
          }
        />
      )}
    </>
  )
}
