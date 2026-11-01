"use client"

import Link from "next/link"
import { useRef, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { apiRaw } from "@/lib/api"
import { cn } from "@/lib/utils"

interface KumaItem {
  name: string
  kuma_type: string
  type?: string
  target?: string
  group?: string
  paused?: boolean
  action: "create" | "exists" | "skip"
  reason?: string
  note?: string
}

interface KumaPlan {
  version: string
  monitors: KumaItem[]
  groups: string[]
  tags: string[]
  warnings: string[]
  created: number
}

/** Import of the monitors of an Uptime Kuma backup, with a preview first. */
export function KumaImport() {
  const t = useTranslations("kumaImport")
  const input = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<{ name: string; text: string } | null>(null)
  const [plan, setPlan] = useState<KumaPlan | null>(null)
  const [done, setDone] = useState<KumaPlan | null>(null)
  const [busy, setBusy] = useState(false)

  async function pick(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    e.target.value = ""
    if (!f) return
    setDone(null)
    setBusy(true)
    try {
      const text = await f.text()
      const p = await apiRaw<KumaPlan>("/api/import/uptime-kuma?dry_run=1", text, "application/json")
      setFile({ name: f.name, text })
      setPlan(p)
    } catch (err) {
      setFile(null)
      setPlan(null)
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  async function run() {
    if (!file) return
    setBusy(true)
    try {
      const p = await apiRaw<KumaPlan>("/api/import/uptime-kuma", file.text, "application/json")
      setDone(p)
      setPlan(null)
      setFile(null)
      toast.success(t("done", { count: p.created }))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  const toCreate = plan?.monitors.filter((m) => m.action === "create").length ?? 0

  return (
    <Section title={t("title")}>
      <div className="grid gap-3">
        <p className="text-xs text-muted-foreground">{t("intro")}</p>
        <input
          ref={input}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={pick}
        />
        {!plan && (
          <div>
            <Button variant="outline" disabled={busy} onClick={() => input.current?.click()}>
              {busy ? t("reading") : t("chooseFile")}
            </Button>
          </div>
        )}
        {done && (
          <p className="text-xs">
            <span className="text-up">{t("done", { count: done.created })}</span>{" "}
            <Link href="/" className="text-primary underline underline-offset-4">
              {t("openDashboard")}
            </Link>
          </p>
        )}
        {plan && file && (
          <div className="grid gap-3 border border-dashed p-3 text-xs">
            <div className="grid gap-1">
              <div className="truncate">{file.name}</div>
              <div className="text-muted-foreground">
                {t("summary", {
                  version: plan.version || "?",
                  create: toCreate,
                  total: plan.monitors.length,
                  groups: plan.groups.length,
                  tags: plan.tags.length,
                })}
              </div>
              {plan.warnings.map((w) => (
                <div key={w} className="text-warn">
                  ! {w}
                </div>
              ))}
            </div>
            <div className="max-h-80 overflow-y-auto border-t border-dashed pt-2">
              <table className="w-full">
                <tbody>
                  {plan.monitors.map((m, i) => (
                    <tr key={i} className="align-top">
                      <td
                        className={cn(
                          "w-16 py-0.5 pr-2",
                          m.action === "create" ? "text-up" : "text-muted-foreground"
                        )}
                      >
                        {t(`action.${m.action}`)}
                      </td>
                      <td className="py-0.5 pr-2">
                        {m.name}
                        {m.paused && <span className="text-muted-foreground"> ({t("paused")})</span>}
                      </td>
                      <td className="py-0.5 pr-2 text-muted-foreground">
                        {m.action === "create"
                          ? [m.type, m.target, m.group && `[${m.group}]`].filter(Boolean).join(" · ")
                          : m.kuma_type}
                      </td>
                      <td className="py-0.5 text-muted-foreground">{m.reason || m.note}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex gap-2">
              <Button disabled={busy || toCreate === 0} onClick={run}>
                {busy ? t("importing") : t("import", { count: toCreate })}
              </Button>
              <Button
                variant="ghost"
                disabled={busy}
                onClick={() => {
                  setPlan(null)
                  setFile(null)
                }}
              >
                {t("cancel")}
              </Button>
            </div>
          </div>
        )}
      </div>
    </Section>
  )
}
