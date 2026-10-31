"use client"

import { useSearchParams } from "next/navigation"
import { Suspense } from "react"
import { useTranslations } from "use-intl"

import { AdminOnly } from "@/components/admin-only"
import { MonitorForm } from "@/components/monitor-form"
import { PageHeader } from "@/components/page-header"
import { useApi, type MonitorDetail } from "@/lib/api"

function Edit() {
  const t = useTranslations("monitorForm")
  const tc = useTranslations("common")
  const id = Number(useSearchParams().get("id"))
  const { data, error } = useApi<MonitorDetail>(id ? `/api/monitors/${id}` : null, { refreshInterval: 0, revalidateOnFocus: false })
  if (error) return <div className="text-down">! {error.message}</div>
  if (!data) return <div className="text-muted-foreground">{tc("loading")}<span className="cursor-blink">_</span></div>
  return (
    <>
      <PageHeader cmd={`pingtxt monitors edit ${id}`} title={t("editTitle", { name: data.name })} />
      <MonitorForm initial={data} id={id} />
    </>
  )
}

export default function EditMonitorPage() {
  return (
    <AdminOnly>
      <Suspense>
        <Edit />
      </Suspense>
    </AdminOnly>
  )
}
