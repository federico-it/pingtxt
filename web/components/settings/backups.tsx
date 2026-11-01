"use client"

import { useRef, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

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
import { tr } from "@/lib/i18n"
import { option } from "@/components/ui/control"

function bytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function errText(err: unknown) {
  return err instanceof Error ? err.message : tr("backups.error")
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
    .map((k) => tr(`backups.kinds.${k}`, { count: c![k] }))
  return parts.length ? parts.join(", ") : tr("backups.none")
}

export function ConfigTransfer({ onImported }: { onImported?: () => void }) {
  const [secrets, setSecrets] = useState(false)
  const [file, setFile] = useState<{ name: string; text: string; doc: ConfigFile } | null>(null)
  const [mode, setMode] = useState<"merge" | "replace">("merge")
  const [applySettings, setApplySettings] = useState(true)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<ImportResult | null>(null)
  const input = useRef<HTMLInputElement>(null)
  const { data: backups } = useApi<Backups>("/api/backups")
  const t = useTranslations("backups")

  async function pick(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    e.target.value = ""
    if (!f) return
    setResult(null)
    try {
      const text = await f.text()
      const doc = JSON.parse(text) as ConfigFile
      if (doc.format !== "pingtxt-config") throw new Error(t("notExport"))
      setFile({ name: f.name, text, doc })
    } catch (err) {
      setFile(null)
      toast.error(err instanceof SyntaxError ? t("notJson") : errText(err))
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
      toast.success(t("imported"))
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
      {busy ? t("importing") : t("import")}
    </Button>
  )

  return (
    <Section title={t("exportTitle")}>
      <div className="grid gap-6">
        <p className="text-xs text-muted-foreground">
          {t("exportIntro")}
        </p>

        <div className="grid gap-3">
          <label className="flex items-center gap-2 text-xs">
            <Switch checked={secrets} onCheckedChange={setSecrets} />
            {t("includeSecrets")}
          </label>
          <p className="text-[11px] text-muted-foreground">
            {secrets ? t("withSecretsHint") : t("withoutSecretsHint")}
          </p>
          <div>
            <Button
              variant="outline"
              render={<a href={`/api/config/export${secrets ? "?secrets=1" : ""}`} download />}
              nativeButton={false}
            >
              {t("exportJson")}
            </Button>
          </div>
        </div>

        <div className="grid gap-3">
          <div className="text-xs text-muted-foreground">{t("importTitle")}</div>
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
                {t("chooseFile")}
              </Button>
            </div>
          ) : (
            <div className="grid gap-4 border border-dashed p-3">
              <div className="grid gap-1 text-xs">
                <div className="truncate text-foreground">{file.name}</div>
                <div className="text-muted-foreground">
                  pingtxt {doc!.hub_version || "?"} ·{" "}
                  {t("exportedAt", { date: doc!.exported_at ? datetime(Date.parse(doc!.exported_at)) : "?" })} ·{" "}
                  {doc!.secrets ? t("withSecrets") : t("withoutSecrets")}
                </div>
                <div className="text-muted-foreground">{counts(fileCounts)}</div>
              </div>
              <Field
                label={t("mode")}
                hint={mode === "merge" ? t("mergeHint") : t("replaceHint")}
              >
                <div className="flex gap-2">
                  {(["merge", "replace"] as const).map((m) => (
                    <button
                      key={m}
                      type="button"
                      onClick={() => setMode(m)}
                      className={option({ active: mode === m })}
                    >
                      {t(`modes.${m}`)}
                    </button>
                  ))}
                </div>
              </Field>
              <label className="flex items-center gap-2 text-xs">
                <Switch checked={applySettings} onCheckedChange={setApplySettings} />
                {t("applySettings")}
              </label>
              <p className="text-[11px] text-muted-foreground">
                {backups && !backups.file_backups ? t("noAutoBackup") : t("autoBackup")}
              </p>
              <div className="flex gap-2">
                {mode === "replace" ? (
                  <Confirm
                    trigger={importButton}
                    title={t("replaceTitle")}
                    description={backups && !backups.file_backups ? t("replaceText") : `${t("replaceText")} ${t("replaceBackup")}`}
                    action={t("modes.replace")}
                    onConfirm={runImport}
                  />
                ) : (
                  <Button disabled={busy} onClick={runImport}>
                    {busy ? t("importing") : t("import")}
                  </Button>
                )}
                <Button variant="ghost" onClick={() => setFile(null)}>
                  {t("cancel")}
                </Button>
              </div>
            </div>
          )}

          {result && (
            <div className="grid gap-1 border border-dashed p-3 text-xs">
              <div>
                <span className="text-primary">✓</span> {t("importedMode", { mode: result.mode === "replace" || result.mode === "merge" ? t(`modes.${result.mode}`) : result.mode })}
              </div>
              {Object.keys(result.deleted).length > 0 && (
                <div className="text-muted-foreground">{t("deleted", { list: counts(result.deleted) })}</div>
              )}
              <div className="text-muted-foreground">{t("created", { list: counts(result.created) })}</div>
              <div className="text-muted-foreground">{t("updated", { list: counts(result.updated) })}</div>
              {Object.keys(result.skipped).length > 0 && (
                <div className="text-muted-foreground">{t("skipped", { list: counts(result.skipped) })}</div>
              )}
              {result.backup && (
                <div className="text-muted-foreground">
                  {t.rich("backupBefore", { name: result.backup, hl: (c) => <span className="text-foreground">{c}</span> })}
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
  const t = useTranslations("backups")

  async function backupNow() {
    setBusy(true)
    try {
      const b = await api<BackupFile>("/api/backups", { method: "POST" })
      toast.success(t("backupCreated", { name: b.name }))
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
      toast.success(t("uploaded", { name: f.name }))
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
      toast.success(t("restartToRestore"))
      mutate()
    } catch (err) {
      toast.error(errText(err))
    }
  }

  async function cancelRestore() {
    try {
      await api("/api/backups/restore", { method: "DELETE" })
      toast.success(t("restoreCancelled"))
      mutate()
    } catch (err) {
      toast.error(errText(err))
    }
  }

  async function remove(b: BackupFile) {
    try {
      await api(`/api/backups/${b.name}`, { method: "DELETE" })
      toast.success(t("fileDeleted", { name: b.name }))
      mutate()
    } catch (err) {
      toast.error(errText(err))
    }
  }

  if (data && !data.file_backups) return <ServerBackups driver={data.driver} />

  return (
    <Section title={t("dbTitle")}>
      <div className="grid gap-6">
        <p className="text-xs text-muted-foreground">
          {t("dbIntro")}
          {data && (
            <>
              {" "}
              {t.rich("dbDir", { dir: data.dir, hl: (c) => <span className="text-foreground">{c}</span> })}
            </>
          )}
        </p>

        {data?.pending_restore && (
          <div className="grid gap-2 border border-primary p-3 text-xs">
            <div>
              <span className="text-primary">!</span>{" "}
              {t.rich("pending", {
                name: data.pending_restore.name || t("uploadedBackup"),
                hl: (c) => <span className="text-foreground">{c}</span>,
              })}
            </div>
            <code className="text-muted-foreground">docker restart pingtxt</code>
            <div>
              <Button size="xs" variant="outline" onClick={cancelRestore}>
                {t("cancelRestore")}
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
            {t("backupNow")}
          </Button>
          <input
            ref={upload}
            type="file"
            accept=".db,.sqlite,.sqlite3,application/vnd.sqlite3,application/octet-stream"
            className="hidden"
            onChange={uploadFile}
          />
          <Button variant="outline" disabled={busy} onClick={() => upload.current?.click()}>
            {t("uploadBackup")}
          </Button>
        </div>

        {data && data.backups.length === 0 && (
          <div className="border border-dashed p-6 text-center text-xs text-muted-foreground">
            {t("noBackups")}
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
                    {t(`fileKinds.${b.kind}`)} · {bytes(b.size)}
                  </div>
                </div>
                <div className="flex gap-1">
                  <Button
                    size="xs"
                    variant="ghost"
                    render={<a href={`/api/backups/${b.name}`} download={b.name} />}
                    nativeButton={false}
                  >
                    {t("download")}
                  </Button>
                  <Confirm
                    trigger={
                      <Button size="xs" variant="ghost">
                        {t("restore")}
                      </Button>
                    }
                    title={t("restoreTitle")}
                    description={t("restoreText", { name: b.name, date: datetime(b.created_at) })}
                    action={t("restore")}
                    onConfirm={() => restore(b)}
                  />
                  <Confirm
                    trigger={
                      <Button size="xs" variant="ghost">
                        {t("delete")}
                      </Button>
                    }
                    title={t("deleteTitle")}
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

const dumpCommands = {
  postgres: {
    name: "PostgreSQL",
    backup: "docker exec pingtxt-postgres pg_dump -U pingtxt -Fc pingtxt > pingtxt.dump",
    restore:
      "docker stop pingtxt\ndocker exec -i pingtxt-postgres pg_restore -U pingtxt -d pingtxt --clean --if-exists < pingtxt.dump\ndocker start pingtxt",
  },
  mysql: {
    name: "MariaDB/MySQL",
    backup:
      'docker exec pingtxt-mariadb sh -c \'mariadb-dump -u pingtxt -p"$MARIADB_PASSWORD" --single-transaction pingtxt\' > pingtxt.sql',
    restore:
      'docker stop pingtxt\ndocker exec -i pingtxt-mariadb sh -c \'mariadb -u pingtxt -p"$MARIADB_PASSWORD" pingtxt\' < pingtxt.sql\ndocker start pingtxt',
  },
}

/** With PostgreSQL or MariaDB/MySQL the database server is backed up with its own tools. */
function ServerBackups({ driver }: { driver: "postgres" | "mysql" | "sqlite" }) {
  const c = dumpCommands[driver === "postgres" ? "postgres" : "mysql"]
  const t = useTranslations("backups")
  return (
    <Section title={t("dbTitle")}>
      <div className="grid gap-4 text-xs">
        <p className="text-muted-foreground">
          {t("serverIntro", { db: c.name })}
        </p>
        <div className="grid gap-1">
          <div className="text-muted-foreground">{t("backup")}</div>
          <pre className="overflow-x-auto border border-dashed p-3 text-[11px]">{c.backup}</pre>
        </div>
        <div className="grid gap-1">
          <div className="text-muted-foreground">{t("restore")}</div>
          <pre className="overflow-x-auto border border-dashed p-3 text-[11px]">{c.restore}</pre>
        </div>
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
  const t = useTranslations("backups")

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api("/api/settings/backups", { method: "PUT", body: s })
      toast.success(t("scheduleSaved"))
      onSaved()
    } catch (err) {
      toast.error(errText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="grid gap-4 sm:grid-cols-3">
      <Field label={t("scheduled")}>
        <label className="flex h-8 items-center gap-2 text-xs">
          <Switch
            checked={s.enabled}
            onCheckedChange={(v) => setS((p) => ({ ...p, enabled: v }))}
          />
          {s.enabled ? t("enabled") : t("disabled")}
        </label>
      </Field>
      <Field label={t("everyHours")} htmlFor="backup_interval">
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
      <Field label={t("keep")} htmlFor="backup_keep" hint={t("keepHint")}>
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
            {t("saveSchedule")}
          </Button>
        </div>
      )}
    </form>
  )
}
