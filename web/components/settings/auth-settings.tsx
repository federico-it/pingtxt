"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { CopyBlock } from "@/components/copy-block"
import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { SaveBar } from "@/components/settings/save-bar"
import { useRoles } from "@/components/settings/users"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import {
  api,
  type AuthSettings,
  type Role,
  type ProviderId,
  type ProviderSettings,
  type Settings,
} from "@/lib/api"
import { cn } from "@/lib/utils"

// Setup help per provider (authSettings.help.<id>); <link> opens the console.
const helpLinks: Record<ProviderId, string> = {
  github: "https://github.com/settings/developers",
  google: "https://console.cloud.google.com/apis/credentials",
  oidc: "",
}

function ProviderHelp({ id }: { id: ProviderId }) {
  const t = useTranslations("authSettings.help")
  return t.rich(id, {
    link: (c) => (
      <a className="underline" href={helpLinks[id]} target="_blank" rel="noreferrer">
        {c}
      </a>
    ),
  })
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: React.ReactNode
}) {
  return (
    <label className="flex items-start gap-2 text-xs">
      <Switch className="mt-0.5" checked={checked} onCheckedChange={onChange} />
      <span>{label}</span>
    </label>
  )
}

export function AuthSettingsForm({
  settings,
  onSaved,
}: {
  settings: Settings
  onSaved: (s: Settings) => void
}) {
  const [a, setA] = useState<AuthSettings>(settings.auth)
  const [domains, setDomains] = useState(
    settings.auth.allowed_domains.join(", ")
  )
  const [busy, setBusy] = useState(false)
  const t = useTranslations("authSettings")
  const roles = useRoles()
  const setP = (id: ProviderId, patch: Partial<ProviderSettings>) =>
    setA((p) => ({
      ...p,
      providers: p.providers.map((x) => (x.id === id ? { ...x, ...patch } : x)),
    }))
  const dirty =
    JSON.stringify(a) !== JSON.stringify(settings.auth) ||
    domains !== settings.auth.allowed_domains.join(", ")
  const anyProvider = a.providers.some((p) => p.enabled)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const body = {
        password_login: a.password_login,
        allow_signup: a.allow_signup,
        allowed_domains: domains.split(/[\s,]+/).filter(Boolean),
        default_role: a.default_role,
        providers: Object.fromEntries(
          a.providers.map((p) => [
            p.id,
            {
              enabled: p.enabled,
              client_id: p.client_id,
              client_secret: p.client_secret,
              name: p.name,
              issuer: p.issuer,
              scopes: p.scopes,
            },
          ])
        ),
      }
      const s = await api<Settings>("/api/settings/auth", {
        method: "PUT",
        body,
      })
      toast.success(t("saved"))
      onSaved(s)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save}>
      <Section title={t("title")} className="mt-0">
        <div className="grid gap-3">
          <Toggle
            checked={a.password_login}
            onChange={(v) => setA((p) => ({ ...p, password_login: v }))}
            label={
              <>
                {t("passwordLogin")}
                <span className="block text-[11px] text-muted-foreground">
                  {t.rich("passwordLoginHint", { code: (c) => <code>{c}</code> })}
                </span>
              </>
            }
          />
          <Toggle
            checked={a.allow_signup}
            onChange={(v) => setA((p) => ({ ...p, allow_signup: v }))}
            label={
              <>
                {t("signup")}
                <span className="block text-[11px] text-muted-foreground">
                  {t("signupHint")}
                </span>
              </>
            }
          />
          {a.allow_signup && (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field
                label={t("domains")}
                htmlFor="domains"
                hint={t("domainsHint")}
              >
                <Input
                  id="domains"
                  placeholder="example.com, corp.io"
                  value={domains}
                  onChange={(e) => setDomains(e.target.value)}
                />
              </Field>
              <Field
                label={t("defaultRole")}
                hint={roles.find((r) => r.value === a.default_role)?.hint}
              >
                <Select
                  items={roles}
                  value={a.default_role}
                  onValueChange={(v) =>
                    v && setA((p) => ({ ...p, default_role: v as Role }))
                  }
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {roles.map((r) => (
                      <SelectItem key={r.value} value={r.value}>
                        {r.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
          )}
        </div>

        <div className="mt-6 grid gap-3">
          {a.providers.map((p) => (
            <div
              key={p.id}
              className={cn(
                "border border-dashed",
                p.enabled && "border-solid"
              )}
            >
              <label className="flex cursor-pointer items-center justify-between gap-2 px-3 py-2 text-xs">
                <span>
                  <span className={cn(p.enabled && "text-primary")}>
                    {p.id === "oidc" ? "OpenID Connect" : p.label}
                  </span>
                  <span className="text-muted-foreground">
                    {p.enabled ? ` · ${t("enabled")}` : ` · ${t("off")}`}
                  </span>
                </span>
                <Switch
                  checked={p.enabled}
                  onCheckedChange={(v) => setP(p.id, { enabled: v })}
                />
              </label>
              {p.enabled && (
                <div className="grid gap-4 border-t border-dashed p-3 sm:grid-cols-2">
                  <p className="text-[11px] text-muted-foreground sm:col-span-2">
                    <ProviderHelp id={p.id} />
                  </p>
                  <Field label={t("callbackUrl")} className="sm:col-span-2">
                    <CopyBlock code={p.callback_url} />
                  </Field>
                  {p.id === "oidc" && (
                    <>
                      <Field label={t("buttonLabel")} htmlFor={`${p.id}-name`}>
                        <Input
                          id={`${p.id}-name`}
                          placeholder="SSO"
                          value={p.name ?? ""}
                          onChange={(e) => setP(p.id, { name: e.target.value })}
                        />
                      </Field>
                      <Field
                        label={t("issuer")}
                        htmlFor={`${p.id}-issuer`}
                        hint={t("issuerHint")}
                      >
                        <Input
                          id={`${p.id}-issuer`}
                          type="url"
                          placeholder="https://auth.example.com/application/o/pingtxt"
                          required
                          value={p.issuer ?? ""}
                          onChange={(e) =>
                            setP(p.id, { issuer: e.target.value })
                          }
                        />
                      </Field>
                    </>
                  )}
                  <Field label={t("clientId")} htmlFor={`${p.id}-cid`}>
                    <Input
                      id={`${p.id}-cid`}
                      required
                      autoComplete="off"
                      value={p.client_id}
                      onChange={(e) =>
                        setP(p.id, { client_id: e.target.value })
                      }
                    />
                  </Field>
                  <Field
                    label={t("clientSecret")}
                    htmlFor={`${p.id}-secret`}
                    hint={
                      p.client_secret_set
                        ? t("secretSaved")
                        : undefined
                    }
                  >
                    <Input
                      id={`${p.id}-secret`}
                      type="password"
                      autoComplete="new-password"
                      required={!p.client_secret_set}
                      placeholder={p.client_secret_set ? "••••••••" : ""}
                      value={p.client_secret}
                      onChange={(e) =>
                        setP(p.id, { client_secret: e.target.value })
                      }
                    />
                  </Field>
                  {p.id === "oidc" && (
                    <Field
                      label={t("scopes")}
                      htmlFor={`${p.id}-scopes`}
                      hint={t("scopesHint")}
                      className="sm:col-span-2"
                    >
                      <Input
                        id={`${p.id}-scopes`}
                        placeholder="openid email profile"
                        value={p.scopes ?? ""}
                        onChange={(e) => setP(p.id, { scopes: e.target.value })}
                      />
                    </Field>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>

        {!a.password_login && !anyProvider && (
          <div className="mt-3 text-xs text-down">
            ! {t("needProvider")}
          </div>
        )}
      </Section>
      <SaveBar
        dirty={dirty}
        busy={busy}
        onDiscard={() => {
          setA(settings.auth)
          setDomains(settings.auth.allowed_domains.join(", "))
        }}
      />
    </form>
  )
}
