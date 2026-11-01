"use client"

import { useTranslations } from "use-intl"

import { Switch } from "@/components/ui/switch"
import { notify, setBrowserNotify, useBrowserNotify } from "@/lib/browser-notify"

/** Turns desktop notifications on or off for this browser. */
export function BrowserNotifyCard() {
  const t = useTranslations("browserNotify")
  const state = useBrowserNotify()

  async function toggle(on: boolean) {
    const s = await setBrowserNotify(on)
    if (on && s === "on") {
      void notify(t("testTitle"), { body: t("testBody"), tag: "pingtxt-test" })
    }
  }

  return (
    <div className="mb-6 flex flex-wrap items-center gap-3 border border-dashed px-3 py-2.5">
      <Switch
        checked={state === "on"}
        onCheckedChange={(v) => void toggle(v)}
        disabled={state === "unsupported" || state === "denied"}
        aria-label={t("title")}
      />
      <div className="min-w-0 flex-1">
        <div className="font-medium">{t("title")}</div>
        <div className="text-xs text-muted-foreground">
          {state === "unsupported" ? t("unsupported") : state === "denied" ? t("denied") : t("hint")}
        </div>
      </div>
    </div>
  )
}
