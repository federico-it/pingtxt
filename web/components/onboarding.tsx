"use client"

import Link from "next/link"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Switch } from "@/components/ui/switch"
import { api, useApi, type Onboarding, type OnboardingStepId } from "@/lib/api"
import { cn } from "@/lib/utils"

/** Where each step is done. Ids the UI does not know yet are not shown. */
const stepHref: Record<OnboardingStepId, string> = {
  monitor: "/monitors/new/",
  channel: "/notifications/",
  public_url: "/settings/general/",
  agent: "/agents/",
  status_page: "/status-pages/",
  two_factor: "/settings/account/",
  backups: "/settings/data/",
}

/** First-run checklist on the dashboard, for admins, until every required
 * step is done or it is hidden. */
export function OnboardingChecklist({ enabled }: { enabled: boolean }) {
  const t = useTranslations("onboarding")
  const { data, mutate } = useApi<Onboarding>(enabled ? "/api/onboarding" : null, { refreshInterval: 30_000 })
  if (!data || data.dismissed || data.complete) return null
  const steps = data.steps.filter((s) => s.id in stepHref)

  async function setDismissed(dismissed: boolean) {
    try {
      await mutate(api<Onboarding>("/api/onboarding", { method: "PUT", body: { dismissed } }), { revalidate: false })
      if (dismissed)
        toast(t("hidden"), { action: { label: t("undo"), onClick: () => void setDismissed(false) } })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }

  const pct = Math.round((data.done / Math.max(1, data.total)) * 100)
  return (
    <section aria-labelledby="onboarding-title" className="mb-4 border border-dashed px-3 py-3 text-sm">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id="onboarding-title" className="font-semibold">
          <span className="text-primary">$</span> {t("title")}
        </h2>
        <span className="text-xs text-muted-foreground">{t("progress", { done: data.done, total: data.total })}</span>
        <button
          type="button"
          onClick={() => void setDismissed(true)}
          className="ml-auto text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
        >
          {t("hide")}
        </button>
      </div>
      <div className="mt-2 h-1 w-full bg-muted" aria-hidden>
        <div className="h-full bg-primary transition-[width]" style={{ width: `${pct}%` }} />
      </div>
      <ol className="mt-3 grid gap-x-6 gap-y-1.5 sm:grid-cols-2">
        {steps.map((s) => (
          <li key={s.id} className="flex min-w-0 items-baseline gap-2">
            <span aria-hidden className={cn("shrink-0", s.done ? "text-up" : "text-muted-foreground")}>
              {s.done ? "[x]" : "[ ]"}
            </span>
            <span className="sr-only">{s.done ? t("doneLabel") : t("todoLabel")}</span>
            <div className="min-w-0">
              {s.done ? (
                <span className="text-muted-foreground line-through decoration-muted-foreground/50">{t(`steps.${s.id}.title`)}</span>
              ) : (
                <Link href={stepHref[s.id]} className="underline underline-offset-2 hover:text-primary">
                  {t(`steps.${s.id}.title`)}
                </Link>
              )}
              {s.optional && <span className="ml-1.5 text-xs text-muted-foreground">{t("optional")}</span>}
              {!s.done && <div className="text-xs text-muted-foreground">{t(`steps.${s.id}.hint`)}</div>}
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}

/** Settings → general: show or hide the checklist on the dashboard. */
export function OnboardingSetting() {
  const t = useTranslations("onboarding")
  const tg = useTranslations("generalSettings")
  const { data, mutate } = useApi<Onboarding>("/api/onboarding", { refreshInterval: 0 })
  if (!data) return null
  async function toggle(on: boolean) {
    try {
      await mutate(api<Onboarding>("/api/onboarding", { method: "PUT", body: { dismissed: !on } }), { revalidate: false })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <Section title={t("title")}>
      <Field label={t("show")} hint={t("showHint")}>
        <label className="flex h-8 items-center gap-2 text-xs">
          <Switch checked={!data.dismissed} onCheckedChange={toggle} />
          {!data.dismissed ? tg("enabled") : tg("disabled")}
          <span className="text-muted-foreground">· {t("progress", { done: data.done, total: data.total })}</span>
        </label>
      </Field>
    </Section>
  )
}
