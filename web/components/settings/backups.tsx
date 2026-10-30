"use client"

import { useRef, useState } from "react"
import { toast } from "sonner"

import { Confirm } from "@/components/confirm"
import { Section } from "@/components/page-header"
import { Field } from "@/components/settings/field"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
  api,
  apiRaw,
  useApi,
  type BackupFile,
  type Backups,
  type BackupSettings,
  type ConfigFile,
  type ImportResult,
} from "@/lib/api"
import { datetime } from "@/lib/format"
import { cn } from "@/lib/utils"

function bytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function errText(err: unknown) {
  return err instanceof Error ? err.message : "error"
}

const kinds = [
  "monitors",
  "groups",
  "tags",
  "notifiers",
  "maintenances",
  "status_pages",
  "agents",
] as const

function counts(c: Record<string, number> | undefined) {
  const parts = kinds
    .filter((k) => c?.[k])
    .map((k) => `${c![k]} ${k.replace("_", " ")}`)
  return parts.length ? parts.join(", ") : "none"
}

export function ConfigTransfer({ onImported }: { onImported?: () => void }) {
  const [secrets, setSecrets] = useState(false)
  const [file, setFile] = useState<{ name: string; text: string; doc: ConfigFile } | null>(null)
  const [mode, setMode] = useState<"merge" | "replace">("merge")
  const [applySettings, setApplySettings] = useState(true)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ImportResult | null>(null)
  const input = useRef<HTMLInputElement>(null)

  async function pick(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    e.target.value = ""
    if (!f) return
    setResult(null)
    try {
      const text = await f.text()
      const doc = JSON.parse(text) as ConfigFile
      if (doc.format !== "pingtxt-config") throw new Error("not a pingtxt configuration export")
      setFile({ name: f.name, text, doc })
    } catch (err) {
      setFile(null)
      toast.error(err instanceof SyntaxError ? "not a JSON file" : errText(err))
    }
  }

  async function runImport() {
    if (!file) return
    setBusy(true)
    try {
      const q = new URLSearchParams({ mode, settings: applySettings ? "1" : "0" })
      const r = await apiRaw<ImportResult>(`/api/config/import?${q}`, file.text, "application/json")
      setResult(r)
      setFile(null)
      toast.success("configuration imported")
      onImported?.()
    } catch (err) {
      toast.error(errText(err))
    } finally {
      setBusy(false)
    }
  }

  const doc = file?.doc
  const fileCounts = doc
    ? Object.fromEntries(kinds.map((k) => [k, (doc[k] as unknown[] | undefined)?.length ?? 0]))
    : undefined

  const importButton = (
    <Button variant={mode === "replace" ? "destructive" : "default"} disabled={busy}>
      {busy ? "importing..." : "import"}
    </Button>
  )

  return (
    <Section title="configuration export">
      <div className="grid gap-6">
        <p className="text-xs text-muted-foreground">
          monitors, groups, tags, notification channels, maintenance windows, status
          pages, agents and instance settings as a JSON file. no history and no user
          accounts: use a database backup for that.
        </p>

        <div className="grid gap-3">
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={secrets} onCheckedChange={setSecrets} />
            include secrets
          </label>
          <p className="text-[11px] text-muted-foreground">
            {secrets
              ? "the file will hold passwords, bot tokens, secret webhook urls, login client secrets and agent tokens. keep it safe."
              : "passwords, tokens and secret webhook urls are replaced by [redacted]. importing here keeps the stored values; on another hub those channels are skipped and those monitors paused until you fill them in."}
          </p>
          <div>
            <Button
              variant="outline"
              render={<a href={`/api/config/export${secrets ? "?secrets=1" : ""}`} download />}
              nativeButton={false}
            >
              export json
            </Button>
          </div>
        </div>

        <div className="grid gap-3">
          <div className="text-xs text-muted-foreground">import</div>
          <input
            ref={input}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={pick}
          />
          {!file ? (
            <div>
              <Button variant="outline" onClick={() => input.current?.click()}>
                choose file
              </Button>
            </div>
          ) : (
            <div className="grid gap-4 border border-dashed p-3">
              <div className="grid gap-1 text-xs">
                <div className="truncate text-foreground">{file.name}</div>
                <div className="text-muted-foreground">
                  pingtxt {doc!.hub_version || "?"} · exported{" "}
                  {doc!.exported_at ? datetime(Date.parse(doc!.exported_at)) : "?"} ·{" "}
                  {doc!.secrets ? "with secrets" : "without secrets"}
                </div>
                <div className="text-muted-foreground">{counts(fileCounts)}</div>
              </div>
              <Field
                label="mode"
                hint={
                  mode === "merge"
                    ? "objects with the same name are updated, the others are added. nothing is deleted."
                    : "deletes every monitor (with its history), group, tag, channel, maintenance window and status page first."
                }
              >
                <div className="flex gap-2">
                  {(["merge", "replace"] as const).map((m) => (
                    <button
                      key={m}
                      type="button"
                      onClick={() => setMode(m)}
                      className={cn(
                        "border border-dashed px-3 py-1.5 text-xs hover:bg-accent",
                        mode === m && "border-solid border-primary text-primary"
                      )}
                    >
                      {m}
                    </button>
                  ))}
                </div>
              </Field>
              <label className="flex items-center gap-2 text-xs">
                <Switch checked={applySettings} onCheckedChange={setApplySettings} />
                also apply instance settings (public url, login, retention...)
              </label>
              <p className="text-[11px] text-muted-foreground">
                a database backup is taken before importing.
              </p>
              <div className="flex gap-2">
                {mode === "replace" ? (
                  <Confirm
                    trigger={importButton}
                    title="replace the configuration?"
                    description="Every current monitor, with its history, and every group, tag, channel, maintenance window and status page will be deleted, then the file is imported. A backup is taken first."
                    action="replace"
                    onConfirm={runImport}
                  />
                ) : (
                  <Button disabled={busy} onClick={runImport}>
                    {busy ? "importing..." : "import"}
                  </Button>
                )}
                <Button variant="ghost" onClick={() => setFile(null)}>
                  cancel
                </Button>
              </div>
            </div>
          )}

          {result && (
            <div className="grid gap-1 border border-dashed p-3 text-xs">
              <div>
                <span className="text-primary">✓</span> imported ({result.mode})
              </div>
              {Object.keys(result.deleted).length > 0 && (
                <div className="text-muted-foreground">deleted: {counts(result.deleted)}</div>
              )}
              <div className="text-muted-foreground">created: {counts(result.created)}</div>
              <div className="text-muted-foreground">updated: {counts(result.updated)}</div>
              {Object.keys(result.skipped).length > 0 && (
                <div className="text-muted-foreground">skipped: {counts(result.skipped)}</div>
              )}
              {result.backup && (
                <div className="text-muted-foreground">
                  backup before import: <span className="text-foreground">{result.backup}</span>
                </div>
              )}
              {result.warnings.length > 0 && (
                <ul className="mt-2 grid gap-1 text-amber-600 dark:text-amber-400">
                  {result.warnings.map((w, i) => (
                    <li key={i}>! {w}</li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      </div>
    </Section>
  )
}

export function DatabaseBackups() {
  const { data, mutate } = useApi<Backups>("/api/backups", { refreshInterval: 30_000 })
  const [busy, setBusy] = useState(false)
  const upload = useRef<HTMLInputElement>(null)

  async function backupNow() {
    setBusy(true)
    try {
      const b = await api<BackupFile>("/api/backups", { method: "POST" })
      toast.success(`backup ${b.name} created`)
      mutate()
    } catch (err) {
      toast.error(errText(err))
    } finally {
      setBusy(false)
    }
  }

  async function uploadFile(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    e.target.value = ""
    if (!f) return
    setBusy(true)
    try {
      await apiRaw("/api/backups/upload", f, "application/octet-stream")
      toast.success(`${f.name} uploaded`)
      mutate()
    } catch (err) {
      toast.error(errText(err))
    } finally {
      setBusy(false)
    }
  }

  async function restore(b: BackupFile) {
    try {
      await api(`/api/backups/${b.name}/restore`, { method: "POST" })
      toast.success("restart the hub to finish the restore")
      mutate()
    } catch (err) {
      toast.error(errText(err))
    }
  }

  async function cancelRestore() {
    try {
      await api("/api/backups/restore", { method: "DELETE" })
      toast.success("restore cancelled")
      mutate()
    } catch (err) {
      toast.error(errText(err))
    }
  }

  async function remove(b: BackupFile) {
    try {
      await api(`/api/backups/${b.name}`, { method: "DELETE" })
      toast.success(`${b.name} deleted`)
      mutate()
    } catch (err) {
      toast.error(errText(err))
    }
  }

  return (
    <Section title="database backups">
      <div className="grid gap-6">
        <p className="text-xs text-muted-foreground">
          a backup is a full copy of the database: settings, users, secrets and
          history. it is taken while the hub keeps running.
          {data && (
            <>
              {" "}
              files are kept in <span className="text-foreground">{data.dir}</span>.
            </>
          )}
        </p>

        {data?.pending_restore && (
          <div className="grid gap-2 border border-primary p-3 text-xs">
            <div>
              <span className="text-primary">!</span> restore pending:{" "}
              <span className="text-foreground">
                {data.pending_restore.name || "uploaded backup"}
              </span>{" "}
              replaces the database when the hub restarts. the current database is
              kept as a pre-restore backup.
            </div>
            <code className="text-muted-foreground">docker restart pingtxt</code>
            <div>
              <Button size="xs" variant="outline" onClick={cancelRestore}>
                cancel restore
              </Button>
            </div>
          </div>
        )}

        {data && (
          <ScheduleForm
            key={JSON.stringify(data.settings)}
            settings={data.settings}
            onSaved={() => mutate()}
          />
        )}

        <div className="flex flex-wrap gap-2">
          <Button disabled={busy} onClick={backupNow}>
            backup now
          </Button>
          <input
            ref={upload}
            type="file"
            accept=".db,.sqlite,.sqlite3,application/vnd.sqlite3,application/octet-stream"
            className="hidden"
            onChange={uploadFile}
          />
          <Button variant="outline" disabled={busy} onClick={() => upload.current?.click()}>
            upload backup
          </Button>
        </div>

        {data && data.backups.length === 0 && (
          <div className="border border-dashed p-6 text-center text-xs text-muted-foreground">
            no backups yet
          </div>
        )}
        {data && data.backups.length > 0 && (
          <div className="divide-y divide-dashed border border-dashed">
            {data.backups.map((b) => (
              <div
                key={b.name}
                className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-xs"
              >
                <div className="min-w-0">
                  <div className="truncate text-foreground">{datetime(b.created_at)}</div>
                  <div className="text-muted-foreground">
                    {b.kind} · {bytes(b.size)}
                  </div>
                </div>
                <div className="flex gap-1">
                  <Button
                    size="xs"
                    variant="ghost"
                    render={<a href={`/api/backups/${b.name}`} download={b.name} />}
                    nativeButton={false}
                  >
                    download
                  </Button>
                  <Confirm
                    trigger={
                      <Button size="xs" variant="ghost">
                        restore
                      </Button>
                    }
                    title="restore this backup?"
                    description={`The database will be replaced by ${b.name} when the hub restarts. Everything after ${datetime(b.created_at)} is lost; the current database is kept as a pre-restore backup.`}
                    action="restore"
                    onConfirm={() => restore(b)}
                  />
                  <Confirm
                    trigger={
                      <Button size="xs" variant="ghost">
                        delete
                      </Button>
                    }
                    title="delete this backup?"
                    description={b.name}
                    onConfirm={() => remove(b)}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </Section>
  )
}

function ScheduleForm({
  settings,
  onSaved,
}: {
  settings: BackupSettings
  onSaved: () => void
}) {
  const [s, setS] = useState(settings)
  const [busy, setBusy] = useState(false)
  const dirty = JSON.stringify(s) !== JSON.stringify(settings)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api("/api/settings/backups", { method: "PUT", body: s })
      toast.success("backup schedule saved")
      onSaved()
    } catch (err) {
      toast.error(errText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="grid gap-4 sm:grid-cols-3">
      <Field label="scheduled backups">
        <label className="flex h-8 items-center gap-2 text-xs">
          <Switch
            checked={s.enabled}
            onCheckedChange={(v) => setS((p) => ({ ...p, enabled: v }))}
          />
          {s.enabled ? "enabled" : "disabled"}
        </label>
      </Field>
      <Field label="every (hours)" htmlFor="backup_interval">
        <Input
          id="backup_interval"
          type="number"
          min={1}
          max={720}
          disabled={!s.enabled}
          value={s.interval_hours}
          onChange={(e) => setS((p) => ({ ...p, interval_hours: Number(e.target.value) }))}
        />
      </Field>
      <Field label="keep" htmlFor="backup_keep" hint="older scheduled backups are deleted">
        <Input
          id="backup_keep"
          type="number"
          min={1}
          max={365}
          disabled={!s.enabled}
          value={s.keep}
          onChange={(e) => setS((p) => ({ ...p, keep: Number(e.target.value) }))}
        />
      </Field>
      {dirty && (
        <div className="sm:col-span-3">
          <Button type="submit" variant="outline" disabled={busy}>
            save schedule
          </Button>
        </div>
      )}
    </form>
  )
}
