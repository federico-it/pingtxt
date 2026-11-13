"use client"

import { useState } from "react"
import Link from "next/link"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, type Me } from "@/lib/api"

/** The phone number of the current user, for on-call SMS and calls. */
export function PhoneNumber({ me, onChange }: { me: Me; onChange: () => void }) {
  const t = useTranslations("oncall")
  const [phone, setPhone] = useState(me.user.phone ?? "")
  const [busy, setBusy] = useState(false)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api("/api/account/phone", { method: "PUT", body: { phone } })
      toast.success(phone ? t("phoneSaved") : t("phoneRemoved"))
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="grid max-w-md gap-2">
      <div className="flex gap-2">
        <Input
          type="tel"
          autoComplete="tel"
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          placeholder="+39 333 123 4567"
          aria-label={t("phone")}
        />
        <Button type="submit" variant="outline" disabled={busy || phone === (me.user.phone ?? "")}>
          {t("save")}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t.rich("phoneHint", {
          link: (c) => (
            <Link href="/oncall/" className="underline underline-offset-2">
              {c}
            </Link>
          ),
        })}
      </p>
    </form>
  )
}
