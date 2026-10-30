"use client"

import { useEffect, useState } from "react"
import { useTheme } from "next-themes"
import { toast } from "sonner"

import { PageHeader, Section } from "@/components/page-header"
import { ApiTokens } from "@/components/settings/api-tokens"
import { AuthSettingsForm } from "@/components/settings/auth-settings"
import { ConfigTransfer, DatabaseBackups } from "@/components/settings/backups"
import { GeneralSettingsForm } from "@/components/settings/general-settings"
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
import { cn } from "@/lib/utils"

export default function SettingsPage() {
  const me = useApi<Me>("/api/auth/me", { refreshInterval: 0 })
  const settings = useApi<Settings>("/api/settings", { refreshInterval: 0 })
  const options = useApi<AuthOptions>("/api/auth/options", {
    refreshInterval: 0,
  })
  const { theme, setTheme } = useTheme()

  // Result of an OAuth "connect account" round trip.
  useEffect(() => {
    const q = new URLSearchParams(window.location.search)
    const err = q.get("error")
    const linked = q.get("linked")
    if (err) toast.error(err)
    if (linked) toast.success(`${linked} account connected`)
    if (err || linked)
      window.history.replaceState(null, "", window.location.pathname)
  }, [])

  return (
    <>
      <PageHeader cmd="pingtxt config" title="settings" />
      <div className="max-w-2xl">
        <Section title="appearance" className="mt-0">
          <div className="flex gap-2">
            {["system", "light", "dark"].map((t) => (
              <button
                key={t}
                onClick={() => setTheme(t)}
                className={cn(
                  "border border-dashed px-3 py-1.5 hover:bg-accent",
                  theme === t && "border-solid border-primary text-primary"
                )}
              >
                {t}
              </button>
            ))}
          </div>
        </Section>

        <Section title="account">
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

        <Section title="two-factor authentication">
          {me.data && <TwoFactor me={me.data} onChange={() => me.mutate()} />}
        </Section>

        <Section title="api tokens">
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

        <Section title="users">
          {me.data && <UsersList meId={me.data.user.id} />}
        </Section>

        <ConfigTransfer
          onImported={() => {
            settings.mutate()
            options.mutate()
          }}
        />
        <DatabaseBackups />

        <Section title="about">
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

  async function changePassword(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api("/api/auth/password", {
        method: "POST",
        body: { current: cur, new: next },
      })
      toast.success(
        u.has_password
          ? "password changed. other sessions were logged out."
          : "password set"
      )
      setCur("")
      setNext("")
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function unlink(provider: string) {
    try {
      await api(`/api/auth/identities/${provider}`, { method: "DELETE" })
      toast.success(`${provider} disconnected`)
      onChange()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  return (
    <>
      <div className="mb-4 text-xs text-muted-foreground">
        logged in as <span className="text-foreground">{u.email}</span>
      </div>
      <form onSubmit={changePassword} className="grid gap-3 sm:grid-cols-2">
        {u.has_password && (
          <div className="grid gap-1.5">
            <Label htmlFor="cur">current password</Label>
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
            {u.has_password ? "new password" : "set a password"}
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
            {u.has_password ? "change password" : "set password"}
          </Button>
        </div>
      </form>

      {(providers.length > 0 || u.identities.length > 0) && (
        <div className="mt-6">
          <div className="mb-2 text-xs text-muted-foreground">
            connected logins
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
                      {id ? `· ${id.email || "connected"}` : "· not connected"}
                    </span>
                  </div>
                  {id ? (
                    <Confirm
                      trigger={
                        <Button size="xs" variant="ghost">
                          disconnect
                        </Button>
                      }
                      title={`disconnect ${p.label}?`}
                      description="You will no longer be able to log in with this account."
                      action="disconnect"
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
                      connect
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
