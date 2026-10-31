"use client"

import Link from "next/link"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Empty, PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { api, statusPageURL, useApi, useIsAdmin, type StatusPage } from "@/lib/api"

export default function StatusPagesPage() {
  const { data, mutate } = useApi<{ pages: StatusPage[]; hub_url: string }>("/api/status-pages", { refreshInterval: 0 })
  const isAdmin = useIsAdmin()
  const t = useTranslations("statusPages")

  async function remove(p: StatusPage) {
    try {
      await api(`/api/status-pages/${p.id}`, { method: "DELETE" })
      toast.success(t("deleted"))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  return (
    <>
      <PageHeader cmd="pingtxt status-pages ls" title={t("title")}>
        {isAdmin && (
          <Button render={<Link href="/status-pages/edit/" />} nativeButton={false}>
            <PlusIcon /> {t("new")}
          </Button>
        )}
      </PageHeader>
      <p className="-mt-3 mb-6 max-w-2xl text-xs text-muted-foreground">
        {t("intro")}
      </p>

      {data && data.pages.length === 0 ? (
        <Empty>
          {t("empty")}{" "}
          {isAdmin && (
            <Link href="/status-pages/edit/" className="text-primary underline underline-offset-4">
              {t("createOne")}
            </Link>
          )}
        </Empty>
      ) : (
        <div className="border-t border-dashed">
          {(data?.pages ?? []).map((p) => {
            const url = statusPageURL(p, data?.hub_url ?? "")
            return (
              <div key={p.id} className="flex flex-wrap items-center gap-3 border-b border-dashed px-2 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 font-medium">
                    {isAdmin ? (
                      <Link href={`/status-pages/edit/?id=${p.id}`} className="truncate underline-offset-4 hover:underline">
                        {p.title}
                      </Link>
                    ) : (
                      <span className="truncate">{p.title}</span>
                    )}
                    {!p.published && <span className="text-xs text-muted-foreground">[{t("draft")}]</span>}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {url} · {t("monitorCount", { count: p.monitor_ids.length })}
                  </div>
                </div>
                <Button variant="ghost" size="xs" render={<a href={`/status/${p.slug}/`} target="_blank" rel="noreferrer" />} nativeButton={false}>
                  {t("open")}
                </Button>
                <Button variant="ghost" size="xs" render={<Link href={`/status-pages/incidents/?id=${p.id}`} />} nativeButton={false}>
                  {t("incidents")}
                </Button>
                {isAdmin && (
                  <>
                    <Button variant="ghost" size="xs" render={<Link href={`/status-pages/edit/?id=${p.id}`} />} nativeButton={false}>
                      {t("edit")}
                    </Button>
                    <Confirm
                      title={t("deleteTitle", { name: p.title })}
                      description={t("deleteText")}
                      onConfirm={() => remove(p)}
                      trigger={
                        <Button variant="ghost" size="xs" className="text-down">
                          {t("rm")}
                        </Button>
                      }
                    />
                  </>
                )}
              </div>
            )
          })}
        </div>
      )}
    </>
  )
}
