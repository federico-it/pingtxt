"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { api, useApi, type PushSettings } from "@/lib/api"

const defaultRelay = "https://push.pingtxt.com"

/** Push notifications to the mobile app, through the push relay (admins). */
export function MobileAppSettings() {
  const { data, mutate } = useApi<PushSettings>("/api/settings/push", { refreshInterval: 0 })
  const t = useTranslations("mobileApp")
  const loaded = !!data
  // Links from the notification channels land here (/settings/integrations/#mobile-app).
  // The sections above fill in after the jump, so follow the layout for a
  // moment, until the user scrolls.
  useEffect(() => {
    const el = document.getElementById("mobile-app")
    if (!loaded || !el || window.location.hash !== "#mobile-app") return
    const go = () => el.scrollIntoView()
    go()
    const ro = new ResizeObserver(go)
    ro.observe(document.body)
    const stop = () => ro.disconnect()
    const timer = setTimeout(stop, 3000)
    window.addEventListener("wheel", stop, { once: true })
    window.addEventListener("touchstart", stop, { once: true })
    return () => {
      clearTimeout(timer)
      stop()
      window.removeEventListener("wheel", stop)
      window.removeEventListener("touchstart", stop)
    }
  }, [loaded])
  return (
    <Section id="mobile-app" title={t("title")}>
      {data && <MobileAppForm key={JSON.stringify(data)} settings={data} onSaved={(s) => mutate(s, { revalidate: false })} />}
    </Section>
  )
}

function MobileAppForm({ settings, onSaved }: { settings: PushSettings; onSaved: (s: PushSettings) => void }) {
  const t = useTranslations("mobileApp")
  const [enabled, setEnabled] = useState(settings.enabled)
  const [relay, setRelay] = useState(settings.relay_url || defaultRelay)
  const [busy, setBusy] = useState(false)
  const relayChanged = settings.registered && relay.replace(/\/+$/, "") !== settings.relay_url

  async function run(fn: () => Promise<void>) {
    setBusy(true)
    try {
      await fn()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  const save = (e?: React.FormEvent) => {
    e?.preventDefault()
    return run(async () => {
      const s = await api<PushSettings>("/api/settings/push", {
        method: "PUT",
        body: { enabled, relay_url: relay },
      })
      toast.success(t("saved"))
      onSaved(s)
    })
  }

  const test = () =>
    run(async () => {
      await api("/api/settings/push/test", { method: "POST" })
      toast.success(t("testOk"))
    })

  return (
    <form onSubmit={save} className="grid gap-4 text-xs">
      <p className="text-muted-foreground">{t("intro")}</p>
      <label className="flex items-center gap-2">
        <Switch checked={enabled} onCheckedChange={setEnabled} />
        <span>{t("enabled")}</span>
      </label>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t("relayUrl")} htmlFor="push-relay" hint={relayChanged ? <span className="text-warn">{t("relayChanged")}</span> : t("relayHint")}>
          <Input id="push-relay" type="url" value={relay} placeholder={defaultRelay} onChange={(e) => setRelay(e.target.value)} />
        </Field>
      </div>
      <div className="text-muted-foreground">
        {settings.registered ? t("status", { hub: settings.hub_id, count: settings.devices }) : t("notRegistered")}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="outline" disabled={busy}>
          {t("save")}
        </Button>
        {settings.registered && (
          <Button type="button" variant="ghost" disabled={busy} onClick={test}>
            {t("test")}
          </Button>
        )}
      </div>
      <p className="text-[11px] text-muted-foreground">{t("channelHint")}</p>
    </form>
  )
}
