"use client"

import { toast } from "sonner"

import { Confirm } from "@/components/confirm"
import { Button } from "@/components/ui/button"
import { api, useApi, type User } from "@/lib/api"

export function UsersList({ meId }: { meId: number }) {
  const { data, mutate } = useApi<User[]>("/api/users", { refreshInterval: 0 })

  async function remove(u: User) {
    try {
      await api(`/api/users/${u.id}`, { method: "DELETE" })
      toast.success(`${u.email} deleted`)
      mutate()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  return (
    <div className="divide-y divide-dashed border border-dashed">
      {data?.map((u) => {
        const methods = [
          u.has_password && "password",
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
                {methods || "no login method"} · since{" "}
                {new Date(u.created_at).toLocaleDateString()}
              </span>
            </div>
            {u.id !== meId && (
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
            )}
          </div>
        )
      })}
    </div>
  )
}
