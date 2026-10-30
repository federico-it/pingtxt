"use client"

import { useState } from "react"
import { toast } from "sonner"

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
import { api, roles, useApi, type Role, type User } from "@/lib/api"

function RoleSelect({
  value,
  onChange,
  className,
}: {
  value: Role
  onChange: (r: Role) => void
  className?: string
}) {
  return (
    <Select
      items={roles}
      value={value}
      onValueChange={(v) => v && onChange(v as Role)}
    >
      <SelectTrigger size="sm" className={className}>
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

  async function remove(u: User) {
    try {
      await api(`/api/users/${u.id}`, { method: "DELETE" })
      toast.success(`${u.email} deleted`)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function resetTwoFactor(u: User) {
    try {
      await api(`/api/users/${u.id}/2fa`, { method: "DELETE" })
      toast.success(`two-factor turned off for ${u.email}`)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  async function setRole(u: User, role: Role) {
    try {
      await api(`/api/users/${u.id}`, { method: "PUT", body: { role } })
      toast.success(
        `${u.email} is now ${roles.find((r) => r.value === role)?.label}`
      )
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  return (
    <>
      <div className="divide-y divide-dashed border border-dashed">
        {data?.map((u) => {
          const methods = [
            u.has_password && "password",
            u.two_factor && "2fa",
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
                {u.id === meId && <span className="text-primary"> (you)</span>}
                <span className="block text-[11px] text-muted-foreground">
                  {methods || "single sign-on only"} · since{" "}
                  {new Date(u.created_at).toLocaleDateString()}
                </span>
              </div>
              {u.id === meId ? (
                <span className="text-muted-foreground">[admin]</span>
              ) : (
                <div className="flex flex-wrap items-center gap-1">
                  <RoleSelect
                    value={u.role}
                    onChange={(r) => setRole(u, r)}
                    className="w-32"
                  />
                  {u.two_factor && (
                    <Confirm
                      trigger={
                        <Button size="xs" variant="ghost">
                          reset 2fa
                        </Button>
                      }
                      title={`turn off two-factor for ${u.email}?`}
                      description="Use this when they lost their phone and recovery codes. They are logged out and can set it up again."
                      action="reset"
                      onConfirm={() => resetTwoFactor(u)}
                    />
                  )}
                  <Confirm
                    trigger={
                      <Button size="xs" variant="ghost">
                        delete
                      </Button>
                    }
                    title={`delete ${u.email}?`}
                    description="The user is logged out and can no longer log in."
                    onConfirm={() => remove(u)}
                  />
                </div>
              )}
            </div>
          )
        })}
      </div>
      <p className="mt-2 text-[11px] text-muted-foreground">
        read-only users see monitors, incidents and history but cannot change
        anything; passwords and tokens in monitors and notification channels are
        hidden from them.
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
          size="sm"
          className="mt-3"
          onClick={() => setAdding(true)}
        >
          add user
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

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api("/api/users", {
        method: "POST",
        body: { email, password, role },
      })
      toast.success(`${email} added`)
      onDone(true)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={submit}
      className="mt-3 grid gap-3 border border-dashed p-3 sm:grid-cols-3"
    >
      <Field label="email" htmlFor="new-user-email">
        <Input
          id="new-user-email"
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </Field>
      <Field
        label="password"
        htmlFor="new-user-password"
        hint="empty: log in with single sign-on (same email)"
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
      <Field label="role" hint={roles.find((r) => r.value === role)?.hint}>
        <RoleSelect value={role} onChange={setRole} className="w-full" />
      </Field>
      <div className="flex gap-2 sm:col-span-3">
        <Button type="submit" size="sm" disabled={busy}>
          add
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => onDone(false)}
        >
          cancel
        </Button>
      </div>
    </form>
  )
}
