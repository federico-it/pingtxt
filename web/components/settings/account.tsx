"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, type AuthOptions, type Me } from "@/lib/api"

/** Email, password and connected single sign-on logins of the current user. */
export function Account({
  me,
  providers,
  onChange,
}: {
  me: Me
  providers: AuthOptions["providers"]
  onChange: () => void
}) {
  const [cur, setCur] = useState("")
  const [next, setNext] = useState("")
  const u = me.user
  const t = useTranslations("settings")

  async function changePassword(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api("/api/auth/password", {
        method: "POST",
        body: { current: cur, new: next },
      })
      toast.success(
        u.has_password ? t("passwordChanged") : t("passwordSet")
      )
      setCur("")
      setNext("")
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function unlink(provider: string) {
    try {
      await api(`/api/auth/identities/${provider}`, { method: "DELETE" })
      toast.success(t("unlinked", { provider }))
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  return (
    <>
      <div className="mb-4 text-xs text-muted-foreground">
        {t.rich("loggedInAs", { email: u.email, hl: (c) => <span className="text-foreground">{c}</span> })}
        {u.role === "viewer" && (
          <span className="block text-[11px]">
            {t("viewerHint")}
          </span>
        )}
      </div>
      <form onSubmit={changePassword} className="grid gap-3 sm:grid-cols-2">
        {u.has_password && (
          <div className="grid gap-1.5">
            <Label htmlFor="cur">{t("currentPassword")}</Label>
            <Input
              id="cur"
              type="password"
              autoComplete="current-password"
              required
              value={cur}
              onChange={(e) => setCur(e.target.value)}
            />
          </div>
        )}
        <div className="grid gap-1.5">
          <Label htmlFor="new">
            {u.has_password ? t("newPassword") : t("setAPassword")}
          </Label>
          <Input
            id="new"
            type="password"
            autoComplete="new-password"
            minLength={8}
            required
            value={next}
            onChange={(e) => setNext(e.target.value)}
          />
        </div>
        <div className="sm:col-span-2">
          <Button type="submit" variant="outline">
            {u.has_password ? t("changePassword") : t("setPassword")}
          </Button>
        </div>
      </form>

      {(providers.length > 0 || u.identities.length > 0) && (
        <div className="mt-6">
          <div className="mb-2 text-xs text-muted-foreground">
            {t("connectedLogins")}
          </div>
          <div className="divide-y divide-dashed border border-dashed">
            {providers.map((p) => {
              const id = u.identities.find((i) => i.provider === p.id)
              return (
                <div
                  key={p.id}
                  className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-xs"
                >
                  <div className="min-w-0">
                    <span className="text-foreground">{p.label}</span>{" "}
                    <span className="text-muted-foreground">
                      {id ? `· ${id.email || t("connected")}` : `· ${t("notConnected")}`}
                    </span>
                  </div>
                  {id ? (
                    <Confirm
                      trigger={
                        <Button size="xs" variant="ghost">
                          {t("disconnect")}
                        </Button>
                      }
                      title={t("disconnectTitle", { provider: p.label })}
                      description={t("disconnectText")}
                      action={t("disconnect")}
                      onConfirm={() => unlink(p.id)}
                    />
                  ) : (
                    <Button
                      size="xs"
                      variant="outline"
                      render={
                        <a href={`/api/auth/oauth/${p.id}/start?link=1`} />
                      }
                      nativeButton={false}
                    >
                      {t("connect")}
                    </Button>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}
    </>
  )
}
