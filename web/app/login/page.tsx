"use client"

import { useEffect, useState } from "react"

import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api, type AuthOptions } from "@/lib/api"

export default function LoginPage() {
  const [setup, setSetup] = useState<boolean | null>(null)
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [options, setOptions] = useState<AuthOptions | null>(null)

  useEffect(() => {
    // Errors from a single sign-on round trip come back as ?error=
    const ssoError = new URLSearchParams(window.location.search).get("error")
    if (ssoError) window.history.replaceState(null, "", window.location.pathname)
    api<{ needs_setup: boolean }>("/api/setup")
      .then((r) => setSetup(r.needs_setup))
      .catch(() => setSetup(false))
    api<AuthOptions>("/api/auth/options")
      .catch(() => ({ password_login: true, providers: [] }))
      .then((o) => {
        setOptions(o)
        if (ssoError) setError(ssoError)
      })
  }, [])

  const showPassword = setup || options?.password_login !== false
  const providers = setup ? [] : (options?.providers ?? [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      await api(setup ? "/api/setup" : "/api/auth/login", {
        method: "POST",
        body: { email, password },
      })
      // eslint-disable-next-line @next/next/no-location-assign-relative-destination
      window.location.href = "/"
    } catch (err) {
      setError(err instanceof Error ? err.message : "error")
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-svh flex-col items-center justify-center px-4">
      <div className="absolute top-3 right-3">
        <ThemeToggle />
      </div>
      <form
        onSubmit={submit}
        className="w-full max-w-sm border border-dashed p-6"
      >
        <pre
          className="mb-6 text-[10px] leading-tight text-primary sm:text-xs"
          aria-hidden
        >
          {`       _             _       _
 _ __ (_)_ __   __ _| |___ _| |_
| '_ \\| | '_ \\ / _\` | __\\ \\/ / __|
| |_) | | | | | (_| | |_ >  <| |_
| .__/|_|_| |_|\\__, |\\__/_/\\_\\\\__|
|_|            |___/`}
        </pre>
        <div className="mb-5 text-xs text-muted-foreground">
          <span className="text-primary">$</span>{" "}
          {setup ? "pingtxt init --admin" : "pingtxt login"}
          <span className="cursor-blink">_</span>
        </div>
        {setup && (
          <p className="mb-4 text-xs text-muted-foreground">
            First run: create the admin account for this instance.
          </p>
        )}
        <div className="grid gap-4">
          {providers.map((p) => (
            <Button
              key={p.id}
              type="button"
              variant="outline"
              render={<a href={`/api/auth/oauth/${p.id}/start`} />}
              nativeButton={false}
            >
              continue with {p.label}
            </Button>
          ))}
          {providers.length > 0 && showPassword && (
            <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
              <span className="h-px flex-1 border-t border-dashed" />
              or
              <span className="h-px flex-1 border-t border-dashed" />
            </div>
          )}
          {showPassword && (
            <>
              <div className="grid gap-1.5">
                <Label htmlFor="email">email</Label>
                <Input
                  id="email"
                  type="email"
                  autoComplete="username"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="password">password</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete={setup ? "new-password" : "current-password"}
                  required
                  minLength={setup ? 8 : undefined}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
            </>
          )}
          {error && <div className="text-xs text-down">! {error}</div>}
          {showPassword && (
            <Button type="submit" disabled={busy || setup === null}>
              {busy ? "..." : setup ? "create account" : "login"}
            </Button>
          )}
        </div>
      </form>
    </div>
  )
}
