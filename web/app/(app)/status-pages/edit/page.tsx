"use client"

import { useSearchParams } from "next/navigation"
import { Suspense } from "react"

import { AdminOnly } from "@/components/admin-only"
import { PageHeader } from "@/components/page-header"
import { StatusPageForm } from "@/components/status-page-form"
import { useApi, type StatusPage } from "@/lib/api"

function Edit() {
  const id = Number(useSearchParams().get("id")) || 0
  const { data, error } = useApi<StatusPage>(id ? `/api/status-pages/${id}` : null, { refreshInterval: 0, revalidateOnFocus: false })
  if (!id) {
    return (
      <>
        <PageHeader cmd="pingtxt status-pages new" title="new status page" />
        <StatusPageForm />
      </>
    )
  }
  if (error) return <div className="text-down">! {error.message}</div>
  if (!data) return <div className="text-muted-foreground">loading<span className="cursor-blink">_</span></div>
  return (
    <>
      <PageHeader cmd={`pingtxt status-pages edit ${id}`} title={`edit: ${data.title}`} />
      <StatusPageForm initial={data} key={data.id} />
    </>
  )
}

export default function EditStatusPage() {
  return (
    <AdminOnly>
      <Suspense>
        <Edit />
      </Suspense>
    </AdminOnly>
  )
}
