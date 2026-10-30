"use client"

import { useEffect, useState } from "react"
import QRCode from "qrcode"
import { toast } from "sonner"

import { CopyBlock } from "@/components/copy-block"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api, type Me } from "@/lib/api"

function errMsg(err: unknown) {
  return err instanceof Error ? err.message : "error"
}

function CodeInput({
  id,
  value,
  onChange,
  recovery,
}: {
  id: string
  value: string
  onChange: (v: string) => void
  recovery?: boolean
}) {
  return (
    <Input
      id={id}
      inputMode={recovery ? "text" : "numeric"}
      autoComplete="one-time-code"
      placeholder={recovery ? "123456 or xxxxx-xxxxx" : "123456"}
      required
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

function RecoveryCodes({
  codes,
  onDone,
}: {
  codes: string[]
  onDone: () => void
}) {
  return (
    <div className="grid gap-3">
      <p className="text-xs text-muted-foreground">
        save these recovery codes somewhere safe. each one works once, if you
        lose your phone. they are shown only now.
      </p>
      <CopyBlock code={codes.join("\n")} />
      <div>
        <Button variant="outline" onClick={onDone}>
          I saved them
        </Button>
      </div>
    </div>
  )
}

export function TwoFactor({ me, onChange }: { me: Me; onChange: () => void }) {
  const enabled = me.user.two_factor
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(
    null
  )
  const [qr, setQr] = useState("")
  const [code, setCode] = useState("")
  const [codes, setCodes] = useState<string[] | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!setup) return
    QRCode.toString(setup.uri, { type: "svg", margin: 1, width: 176 })
      .then(setQr)
      .catch(() => setQr(""))
  }, [setup])

  async function run<T>(fn: () => Promise<T>) {
    setBusy(true)
    try {
      return await fn()
    } catch (err) {
      toast.error(errMsg(err))
    } finally {
      setBusy(false)
    }
  }

  const start = () =>
    run(async () => {
      setSetup(await api("/api/auth/2fa/setup", { method: "POST" }))
      setCode("")
    })

  const confirm = (e: React.FormEvent) => {
    e.preventDefault()
    run(async () => {
      const r = await api<{ recovery_codes: string[] }>(
        "/api/auth/2fa/enable",
        { method: "POST", body: { code } }
      )
      setSetup(null)
      setCode("")
      setCodes(r.recovery_codes)
      toast.success("two-factor authentication is on")
    })
  }

  const regenerate = () =>
    run(async () => {
      const r = await api<{ recovery_codes: string[] }>(
        "/api/auth/2fa/recovery-codes",
        { method: "POST", body: { code } }
      )
      setCode("")
      setCodes(r.recovery_codes)
    })

  const disable = () =>
    run(async () => {
      await api("/api/auth/2fa/disable", { method: "POST", body: { code } })
      setCode("")
      toast.success("two-factor authentication is off")
      onChange()
    })

  if (codes) {
    return (
      <RecoveryCodes
        codes={codes}
        onDone={() => {
          setCodes(null)
          onChange()
        }}
      />
    )
  }

  if (setup) {
    return (
      <form onSubmit={confirm} className="grid gap-4">
        <p className="text-xs text-muted-foreground">
          scan the code with an authenticator app (1Password, Bitwarden, Aegis,
          Google Authenticator...), then type the 6 digit code it shows.
        </p>
        <div className="flex flex-wrap items-start gap-4">
          {qr ? (
            <div
              className="size-44 shrink-0 bg-white p-1"
              // SVG generated locally by the qrcode library from our own URI.
              dangerouslySetInnerHTML={{ __html: qr }}
            />
          ) : (
            <div className="size-44 shrink-0 border border-dashed" />
          )}
          <div className="grid min-w-0 flex-1 gap-3">
            <Field label="or enter this key">
              <CopyBlock code={setup.secret.match(/.{1,4}/g)!.join(" ")} />
            </Field>
            <Field label="code" htmlFor="totp-confirm">
              <CodeInput id="totp-confirm" value={code} onChange={setCode} />
            </Field>
            <div className="flex gap-2">
              <Button type="submit" disabled={busy}>
                turn on
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={() => setSetup(null)}
              >
                cancel
              </Button>
            </div>
          </div>
        </div>
      </form>
    )
  }

  if (!enabled) {
    return (
      <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
        <span className="text-muted-foreground">
          off · ask for a code from your phone after the password
        </span>
        <Button variant="outline" onClick={start} disabled={busy}>
          set up two-factor
        </Button>
      </div>
    )
  }

  return (
    <div className="grid gap-3">
      <div className="text-xs">
        <span className="text-up">on</span>{" "}
        <span className="text-muted-foreground">
          · {me.recovery_codes_left} recovery code
          {me.recovery_codes_left === 1 ? "" : "s"} left
        </span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label="code"
          htmlFor="totp-manage"
          hint="needed to turn it off or to get new recovery codes"
        >
          <CodeInput
            id="totp-manage"
            value={code}
            onChange={setCode}
            recovery
          />
        </Field>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" disabled={busy || !code} onClick={regenerate}>
          new recovery codes
        </Button>
        <Button variant="ghost" disabled={busy || !code} onClick={disable}>
          turn off
        </Button>
      </div>
    </div>
  )
}
