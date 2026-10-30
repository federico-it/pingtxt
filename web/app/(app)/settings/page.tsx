"use client"

import { useState } from "react"
import { useTheme } from "next-themes"
import { toast } from "sonner"

import { PageHeader, Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, useApi } from "@/lib/api"
import { cn } from "@/lib/utils"

export default function SettingsPage() {
  const me = useApi<{ user: { email: string }; version: string }>("/api/auth/me", { refreshInterval: 0 })
  const { theme, setTheme } = useTheme()
  const [cur, setCur] = useState("")
  const [next, setNext] = useState("")

  async function changePassword(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api("/api/auth/password", { method: "POST", body: { current: cur, new: next } })
      toast.success("password changed. other sessions were logged out.")
      setCur("")
      setNext("")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "error")
    }
  }

  return (
    <>
      <PageHeader cmd="pingtxt config" title="settings" />
      <div className="max-w-xl">
        <Section title="appearance" className="mt-0">
          <div className="flex gap-2">
            {["system", "light", "dark"].map((t) => (
              <button
                key={t}
                onClick={() => setTheme(t)}
                className={cn("border border-dashed px-3 py-1.5 hover:bg-accent", theme === t && "border-solid border-primary text-primary")}
              >
                {t}
              </button>
            ))}
          </div>
        </Section>

        <Section title="account">
          <div className="mb-4 text-xs text-muted-foreground">
            logged in as <span className="text-foreground">{me.data?.user.email}</span>
          </div>
          <form onSubmit={changePassword} className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor="cur">current password</Label>
              <Input id="cur" type="password" autoComplete="current-password" required value={cur} onChange={(e) => setCur(e.target.value)} />
            </div>
            <div className="grid gap-1.5">
              <Label htmlFor="new">new password</Label>
              <Input id="new" type="password" autoComplete="new-password" minLength={8} required value={next} onChange={(e) => setNext(e.target.value)} />
            </div>
            <div>
              <Button type="submit" variant="outline">
                change password
              </Button>
            </div>
          </form>
        </Section>

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
