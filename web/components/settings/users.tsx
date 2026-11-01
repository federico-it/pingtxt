"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api, roles as roleValues, useApi, type Role, type User } from "@/lib/api"

/** Roles with their translated label and hint. */
export function useRoles() {
  const t = useTranslations("users.roles")
  return roleValues.map((value) => ({ value, label: t(`${value}.label`), hint: t(`${value}.hint`) }))
}

function RoleSelect({
  value,
  onChange,
  className,
  size = "default",
}: {
  value: Role
  onChange: (r: Role) => void
  className?: string
  size?: "xs" | "default"
}) {
  const roles = useRoles()
  return (
    <Select
      items={roles}
      value={value}
      onValueChange={(v) => v && onChange(v as Role)}
    >
      <SelectTrigger size={size} className={className}>
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
  )
}

export function UsersList({ meId }: { meId: number }) {
  const { data, mutate } = useApi<User[]>("/api/users", { refreshInterval: 0 })
  const [adding, setAdding] = useState(false)
  const t = useTranslations("users")
  const roleList = useRoles()

  async function remove(u: User) {
    try {
      await api(`/api/users/${u.id}`, { method: "DELETE" })
      toast.success(t("deleted", { email: u.email }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function resetTwoFactor(u: User) {
    try {
      await api(`/api/users/${u.id}/2fa`, { method: "DELETE" })
      toast.success(t("twoFactorReset", { email: u.email }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  async function setRole(u: User, role: Role) {
    try {
      await api(`/api/users/${u.id}`, { method: "PUT", body: { role } })
      toast.success(t("roleChanged", { email: u.email, role: roleList.find((r) => r.value === role)?.label ?? role }))
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    }
  }

  return (
    <>
      <div className="divide-y divide-dashed border border-dashed">
        {data?.map((u) => {
          const methods = [
            u.has_password && t("methods.password"),
            u.two_factor && t("methods.twoFactor"),
            ...u.identities.map((i) => i.provider),
          ]
            .filter(Boolean)
            .join(", ")
          return (
            <div
              key={u.id}
              className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-xs"
            >
              <div className="min-w-0">
                <span className="text-foreground">{u.email}</span>
                {u.id === meId && <span className="text-primary"> ({t("you")})</span>}
                <span className="block text-[11px] text-muted-foreground">
                  {methods || t("ssoOnly")} ·{" "}
                  {t("since", { date: new Date(u.created_at).toLocaleDateString() })}
                </span>
              </div>
              {u.id === meId ? (
                <span className="text-muted-foreground">[{roleList[0].label}]</span>
              ) : (
                <div className="flex flex-wrap items-center gap-1">
                  <RoleSelect
                    value={u.role}
                    onChange={(r) => setRole(u, r)}
                    size="xs"
                    className="w-32"
                  />
                  {u.two_factor && (
                    <Confirm
                      trigger={
                        <Button size="xs" variant="ghost">
                          {t("reset2fa")}
                        </Button>
                      }
                      title={t("reset2faTitle", { email: u.email })}
                      description={t("reset2faText")}
                      action={t("reset")}
                      onConfirm={() => resetTwoFactor(u)}
                    />
                  )}
                  <Confirm
                    trigger={
                      <Button size="xs" variant="ghost">
                        {t("delete")}
                      </Button>
                    }
                    title={t("deleteTitle", { email: u.email })}
                    description={t("deleteText")}
                    onConfirm={() => remove(u)}
                  />
                </div>
              )}
            </div>
          )
        })}
      </div>
      <p className="mt-2 text-[11px] text-muted-foreground">
        {t("viewerHint")}
      </p>
      {adding ? (
        <AddUser
          onDone={(added) => {
            setAdding(false)
            if (added) mutate()
          }}
        />
      ) : (
        <Button
          variant="outline"
          className="mt-3"
          onClick={() => setAdding(true)}
        >
          {t("add")}
        </Button>
      )}
    </>
  )
}

function AddUser({ onDone }: { onDone: (added: boolean) => void }) {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState<Role>("viewer")
  const [busy, setBusy] = useState(false)
  const t = useTranslations("users")
  const roles = useRoles()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api("/api/users", {
        method: "POST",
        body: { email, password, role },
      })
      toast.success(t("added", { email }))
      onDone(true)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("error"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={submit}
      className="mt-3 grid gap-3 border border-dashed p-3 sm:grid-cols-3"
    >
      <Field label={t("email")} htmlFor="new-user-email">
        <Input
          id="new-user-email"
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </Field>
      <Field
        label={t("password")}
        htmlFor="new-user-password"
        hint={t("passwordHint")}
      >
        <Input
          id="new-user-password"
          type="password"
          autoComplete="new-password"
          minLength={8}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </Field>
      <Field label={t("role")} hint={roles.find((r) => r.value === role)?.hint}>
        <RoleSelect value={role} onChange={setRole} className="w-full" />
      </Field>
      <div className="flex gap-2 sm:col-span-3">
        <Button type="submit" disabled={busy}>
          {t("addShort")}
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={() => onDone(false)}
        >
          {t("cancel")}
        </Button>
      </div>
    </form>
  )
}
