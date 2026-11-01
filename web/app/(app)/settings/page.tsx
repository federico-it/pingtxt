"use client"

import { useEffect, useState } from "react"
import { useTheme } from "next-themes"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { PageHeader, Section } from "@/components/page-header"
import { ApiTokens } from "@/components/settings/api-tokens"
import { AuthSettingsForm } from "@/components/settings/auth-settings"
import { ConfigTransfer, DatabaseBackups } from "@/components/settings/backups"
import { KumaImport } from "@/components/settings/kuma-import"
import { GeneralSettingsForm } from "@/components/settings/general-settings"
import { MobileAppSettings } from "@/components/settings/mobile-app"
import { TwoFactor } from "@/components/settings/two-factor"
import { UsersList } from "@/components/settings/users"
import { Confirm } from "@/components/confirm"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  api,
  useApi,
  type AuthOptions,
  type Me,
  type Settings,
} from "@/lib/api"
import { isLocale, locales, useLocale, type Locale } from "@/lib/i18n"
import { option } from "@/components/ui/control"

export default function SettingsPage() {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  // Instance settings and users are for admins only.
  const isAdmin = me.data?.user.role === "admin"
  const settings = useApi<Settings>(isAdmin ? "/api/settings" : null, {
    refreshInterval: 0,
  })
  const options = useApi<AuthOptions>("/api/auth/options", {
    refreshInterval: 0,
  })
  const { theme, setTheme } = useTheme()
  const { locale, setLocale } = useLocale()
  const t = useTranslations("settings")
  const tc = useTranslations("common")
  const tl = useTranslations("language")

  // Result of an OAuth "connect account" round trip.
  useEffect(() => {
    const q = new URLSearchParams(window.location.search)
    const err = q.get("error")
    const linked = q.get("linked")
    if (err) toast.error(err)
    if (linked) toast.success(t("linked", { provider: linked }))
    if (err || linked)
      window.history.replaceState(null, "", window.location.pathname)
  }, [t])

  return (
    <>
      <PageHeader cmd="pingtxt config" title={t("title")} />
      <div className="max-w-2xl">
        <Section title={t("appearance")} className="mt-0">
          <div className="flex gap-2">
            {(["system", "light", "dark"] as const).map((th) => (
              <button
                key={th}
                onClick={() => setTheme(th)}
                className={option({ active: theme === th })}
              >
                {tc(`theme.${th}`)}
              </button>
            ))}
          </div>
        </Section>

        <Section title={tl("title")}>
          <div className="flex flex-wrap gap-2">
            {(Object.keys(locales) as Locale[]).map((l) => (
              <button
                key={l}
                lang={l}
                onClick={() => isLocale(l) && setLocale(l)}
                className={option({ active: locale === l })}
              >
                {locales[l]}
              </button>
            ))}
          </div>
          <p className="mt-2 text-[11px] text-muted-foreground">
            {tl.rich("hint", {
              link: (c) => (
                <a href="https://crowdin.com/project/pingtxt" target="_blank" rel="noreferrer" className="underline underline-offset-4">
                  {c}
                </a>
              ),
            })}
          </p>
        </Section>

        <Section title={t("account")}>
          {me.data && (
            <Account
              me={me.data}
              providers={options.data?.providers ?? []}
              onChange={() => {
                me.mutate()
                options.mutate()
              }}
            />
          )}
        </Section>

        <Section title={t("twoFactor")}>
          {me.data && <TwoFactor me={me.data} onChange={() => me.mutate()} />}
        </Section>

        <Section title={t("apiTokens")}>
          <ApiTokens />
        </Section>

        {settings.data && (
          <>
            <GeneralSettingsForm
              key={JSON.stringify(settings.data.general)}
              settings={settings.data}
              onSaved={(s) => settings.mutate(s, { revalidate: false })}
            />
            <AuthSettingsForm
              key={JSON.stringify(settings.data.auth)}
              settings={settings.data}
              onSaved={(s) => {
                settings.mutate(s, { revalidate: false })
                options.mutate()
              }}
            />
          </>
        )}

        {isAdmin && <MobileAppSettings />}

        {isAdmin && me.data && (
          <Section title={t("users")}>
            <UsersList meId={me.data.user.id} />
          </Section>
        )}

        {isAdmin && (
          <>
            <ConfigTransfer
              onImported={() => {
                settings.mutate()
                options.mutate()
              }}
            />
            <KumaImport />
            <DatabaseBackups />
          </>
        )}

        <Section title={t("about")}>
          <pre className="text-xs text-muted-foreground">
            {`version    ${me.data?.version ?? "--"}
source     github.com/federico-it/pingtxt`}
          </pre>
        </Section>
      </div>
    </>
  )
}

function Account({
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
