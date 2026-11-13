"use client"

import { useRef, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  ApiError,
  apiRaw,
  useApi,
  type CodeApplyResult,
  type CodeChange,
  type CodePlan,
  type CodeSyncStatus,
} from "@/lib/api"
import { ago, datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

const DOCS = "https://pingtxt.com/docs/administration#configuration-as-code"

const mark: Record<CodeChange["action"], { sign: string; cls: string }> = {
  create: { sign: "+", cls: "text-up" },
  update: { sign: "~", cls: "text-warn" },
  delete: { sign: "-", cls: "text-down" },
}

/** The plan of a configuration as code file: changes, errors, warnings. */
export function CodePlanView({ plan }: { plan: CodePlan }) {
  const t = useTranslations("configCode")
  const unchanged = Object.values(plan.unchanged ?? {}).reduce((a, b) => a + b, 0)
  return (
    <div className="grid gap-2 text-xs">
      {plan.errors.length > 0 && (
        <ul className="grid gap-1 text-down">
          {plan.errors.map((e, i) => (
            <li key={i}>✗ {e}</li>
          ))}
        </ul>
      )}
      {plan.changes.length === 0 && plan.errors.length === 0 ? (
        <div className="text-muted-foreground">{t("noChanges")}</div>
      ) : (
        plan.changes.length > 0 && (
          <ul className="grid gap-0.5">
            {plan.changes.map((c, i) => (
              <li key={i} className="flex min-w-0 gap-2">
                <span className={cn("w-3 shrink-0", mark[c.action].cls)}>{mark[c.action].sign}</span>
                <span className="shrink-0 text-muted-foreground">{t(`kinds.${c.kind}`)}</span>
                <span className="min-w-0 truncate">{c.name}</span>
                {c.fields && c.fields.length > 0 && (
                  <span className="min-w-0 truncate text-muted-foreground">({c.fields.join(", ")})</span>
                )}
              </li>
            ))}
          </ul>
        )
      )}
      {unchanged > 0 && <div className="text-muted-foreground">{t("unchanged", { count: unchanged })}</div>}
      {plan.warnings.length > 0 && (
        <ul className="grid gap-1 text-warn">
          {plan.warnings.map((w, i) => (
            <li key={i}>! {w}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** Settings → data: configuration as code (YAML) export, plan and apply. */
export function ConfigCode({ onApplied }: { onApplied?: () => void }) {
  const t = useTranslations("configCode")
  const sync = useApi<CodeSyncStatus>("/api/config/code/sync", { refreshInterval: 30_000 })
  const [text, setText] = useState("")
  const [prune, setPrune] = useState(false)
  const [withSettings, setWithSettings] = useState(false)
  const [plan, setPlan] = useState<CodePlan | null>(null)
  const [result, setResult] = useState<CodeApplyResult | null>(null)
  const [busy, setBusy] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const managed = !!sync.data?.managed

  const exportHref = `/api/config/code?download=1${withSettings ? "&settings=1" : ""}`

  async function loadCurrent() {
    setBusy(true)
    try {
      const res = await fetch(`/api/config/code${withSettings ? "?settings=1" : ""}`, { credentials: "same-origin" })
      if (!res.ok) throw new Error(res.statusText)
      edit(await res.text())
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function pick(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    e.target.value = ""
    if (f) edit(await f.text())
  }

  function edit(v: string) {
    setText(v)
    setPlan(null)
    setResult(null)
  }

  async function runPlan() {
    setBusy(true)
    setResult(null)
    try {
      setPlan(await apiRaw<CodePlan>(`/api/config/code/plan${prune ? "?prune=1" : ""}`, text, "application/yaml"))
    } catch (err) {
      setPlan(null)
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function runApply() {
    setBusy(true)
    try {
      const r = await apiRaw<CodeApplyResult>(`/api/config/code/apply${prune ? "?prune=1" : ""}`, text, "application/yaml")
      setResult(r)
      setPlan(null)
      toast.success(r.applied ? t("applied") : t("noChanges"))
      onApplied?.()
    } catch (err) {
      if (err instanceof ApiError && err.status === 422 && err.data) {
        setPlan((err.data as CodeApplyResult).plan)
      }
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function syncNow() {
    setBusy(true)
    try {
      const s = await apiRaw<CodeSyncStatus>("/api/config/code/sync", "", "application/json")
      await sync.mutate(s, { revalidate: false })
      if (s.error) toast.error(s.error)
      else toast.success(t("applied"))
      onApplied?.()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const deletes = plan?.changes.filter((c) => c.action === "delete").length ?? 0
  const canApply = !!plan && plan.errors.length === 0 && plan.changes.length > 0 && !managed
  const applyButton = (
    <Button variant={deletes > 0 ? "destructive" : "default"} disabled={busy || !canApply}>
      {t("apply", { count: plan?.changes.length ?? 0 })}
    </Button>
  )

  return (
    <Section title={t("title")}>
      <div className="grid gap-6">
        <p className="text-xs text-muted-foreground">
          {t.rich("intro", {
            docs: (c) => (
              <a href={DOCS} target="_blank" rel="noreferrer" className="underline underline-offset-2 hover:text-foreground">
                {c}
              </a>
            ),
          })}
        </p>

        {managed && sync.data && (
          <div className={cn("grid gap-2 border px-3 py-2 text-xs", sync.data.error ? "border-down/40 bg-down/5" : "border-info/40 bg-info/5")}>
            <div>
              {t.rich("managed", {
                file: sync.data.file ?? "",
                hl: (c) => <code className="text-foreground">{c}</code>,
              })}
              {sync.data.prune && <span className="text-muted-foreground"> · {t("managedPrune")}</span>}
            </div>
            <div className="text-muted-foreground">
              {sync.data.applied_at
                ? t("lastApplied", { when: ago(sync.data.applied_at) })
                : t("neverApplied")}
              {sync.data.checked_at ? ` · ${t("lastChecked", { when: datetime(sync.data.checked_at) })}` : ""}
            </div>
            {sync.data.error && <div className="text-down">✗ {sync.data.error}</div>}
            <div>
              <Button size="sm" variant="outline" disabled={busy} onClick={syncNow}>
                {t("syncNow")}
              </Button>
            </div>
          </div>
        )}

        <div className="grid gap-3">
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={withSettings} onCheckedChange={setWithSettings} />
            {t("withSettings")}
          </label>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" render={<a href={exportHref} download="pingtxt.yaml" />} nativeButton={false}>
              {t("download")}
            </Button>
            <Button variant="outline" disabled={busy} onClick={loadCurrent}>
              {t("loadCurrent")}
            </Button>
            <Button variant="outline" onClick={() => input.current?.click()}>
              {t("openFile")}
            </Button>
            <input
              ref={input}
              type="file"
              accept=".yaml,.yml,.json,application/yaml,application/json,text/yaml"
              className="hidden"
              onChange={pick}
            />
          </div>
        </div>

        <div className="grid gap-3">
          <Textarea
            value={text}
            onChange={(e) => edit(e.target.value)}
            placeholder={"version: 1\nmonitors:\n  - name: website\n    type: http\n    target: https://example.com"}
            spellCheck={false}
            aria-label={t("editor")}
            className="max-h-96 min-h-40 font-mono text-xs md:text-xs"
          />
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={prune} onCheckedChange={(v) => { setPrune(v); setPlan(null) }} />
            {t("prune")}
          </label>
          <p className="-mt-1 text-[11px] text-muted-foreground">{prune ? t("pruneOnHint") : t("pruneOffHint")}</p>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" disabled={busy || !text.trim()} onClick={runPlan}>
              {t("plan")}
            </Button>
            {plan &&
              (deletes > 0 ? (
                <Confirm
                  trigger={applyButton}
                  title={t("deleteTitle", { count: deletes })}
                  description={t("deleteText")}
                  action={t("apply", { count: plan.changes.length })}
                  onConfirm={runApply}
                />
              ) : (
                <Button disabled={busy || !canApply} onClick={runApply}>
                  {t("apply", { count: plan.changes.length })}
                </Button>
              ))}
          </div>
          {managed && plan && <p className="text-[11px] text-muted-foreground">{t("managedNoApply")}</p>}
          {plan && (
            <div className="border border-dashed p-3">
              <CodePlanView plan={plan} />
            </div>
          )}
          {result && (
            <div className="grid gap-1 border border-dashed p-3 text-xs">
              <div>
                <span className="text-primary">✓</span> {result.applied ? t("applied") : t("noChanges")}
              </div>
              {result.import?.backup && (
                <div className="text-muted-foreground">{t("backup", { name: result.import.backup })}</div>
              )}
              <CodePlanView plan={result.plan} />
              {result.import?.warnings && result.import.warnings.length > 0 && (
                <ul className="mt-1 grid gap-1 text-warn">
                  {result.import.warnings.map((w, i) => (
                    <li key={i}>! {w}</li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      </div>
    </Section>
  )
}
