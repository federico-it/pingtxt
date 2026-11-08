"use client"

import { ArrowDownIcon, ArrowUpIcon, PlusIcon, XIcon } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import type { Extract, ExtractFrom, Step } from "@/lib/api"
import { headersToText, textToHeaders } from "@/lib/headers"

const methods = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
const sources: ExtractFrom[] = ["json", "header", "regex"]
const placeholders: Record<ExtractFrom, string> = {
  json: "data.token",
  header: "X-Csrf-Token",
  regex: 'name="csrf" value="([^"]+)"',
}

/** Max steps per monitor and values per step, as enforced by the hub. */
const maxSteps = 10
const maxExtracts = 10

/**
 * Requests an http/keyword monitor makes before its own one, e.g. a login.
 * Values they extract are used as {{name}} in later requests.
 */
export function RequestSteps({
  value,
  onChange,
}: {
  value: Step[]
  onChange: (v: Step[]) => void
}) {
  const t = useTranslations("monitorForm.steps")
  // Stable keys, so each step keeps its own headers text when others move.
  const next = useRef(value.length)
  const [keys, setKeys] = useState(() => value.map((_, i) => i))

  const update = (i: number, s: Step) =>
    onChange(value.map((x, j) => (j === i ? s : x)))
  function remove(i: number) {
    onChange(value.filter((_, j) => j !== i))
    setKeys(keys.filter((_, j) => j !== i))
  }
  function move(i: number, d: -1 | 1) {
    const v = [...value]
    const k = [...keys]
    ;[v[i], v[i + d]] = [v[i + d], v[i]]
    ;[k[i], k[i + d]] = [k[i + d], k[i]]
    onChange(v)
    setKeys(k)
  }
  function add() {
    onChange([
      ...value,
      { name: "", method: "POST", url: "", headers: {}, body: "", expected_status: "", extract: [] },
    ])
    setKeys([...keys, next.current++])
  }

  // Variables set by the steps before each one.
  const before = (i: number) =>
    value
      .slice(0, i)
      .flatMap((s) => (s.extract ?? []).map((e) => e.var))
      .filter(Boolean)

  return (
    <div className="grid gap-3">
      <p className="text-xs text-muted-foreground">
        {t.rich("hint", {
          example: "{{token}}",
          code: (c) => <code className="font-mono">{c}</code>,
        })}
      </p>
      {value.map((s, i) => (
        <StepCard
          key={keys[i] ?? i}
          index={i}
          step={s}
          vars={before(i)}
          last={i === value.length - 1}
          onChange={(x) => update(i, x)}
          onRemove={() => remove(i)}
          onMove={(d) => move(i, d)}
        />
      ))}
      {value.length > 0 && (
        <p className="text-xs text-muted-foreground">
          {t("final", { vars: before(value.length).map((v) => `{{${v}}}`).join(" ") || "-" })}
        </p>
      )}
      {value.length < maxSteps && (
        <div>
          <Button type="button" variant="outline" onClick={add}>
            <PlusIcon />
            {t("add")}
          </Button>
        </div>
      )}
    </div>
  )
}

function StepCard({
  index,
  step,
  vars,
  last,
  onChange,
  onRemove,
  onMove,
}: {
  index: number
  step: Step
  vars: string[]
  last: boolean
  onChange: (s: Step) => void
  onRemove: () => void
  onMove: (d: -1 | 1) => void
}) {
  const t = useTranslations("monitorForm.steps")
  const tr = useTranslations("monitorForm")
  const [headers, setHeaders] = useState(headersToText(step.headers))
  const set = <K extends keyof Step>(k: K, v: Step[K]) =>
    onChange({ ...step, [k]: v })
  const extract = step.extract ?? []
  const setExtract = (i: number, patch: Partial<Extract>) =>
    set(
      "extract",
      extract.map((e, j) => (j === i ? { ...e, ...patch } : e))
    )
  const sourceItems = sources.map((x) => ({ value: x, label: t(`from.${x}`) }))

  return (
    <div className="grid gap-3 rounded-md border p-3">
      <div className="flex items-center gap-2">
        <span className="font-mono text-xs text-muted-foreground">
          {t("number", { n: index + 1 })}
        </span>
        <Input
          aria-label={t("name")}
          className="flex-1"
          value={step.name ?? ""}
          onChange={(e) => set("name", e.target.value)}
          placeholder={t("namePlaceholder")}
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t("moveUp")}
          disabled={index === 0}
          onClick={() => onMove(-1)}
        >
          <ArrowUpIcon />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t("moveDown")}
          disabled={last}
          onClick={() => onMove(1)}
        >
          <ArrowDownIcon />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label={t("remove")}
          onClick={onRemove}
        >
          <XIcon />
        </Button>
      </div>
      <div className="grid gap-2 sm:grid-cols-[7rem_minmax(0,1fr)_8rem]">
        <Select
          items={methods.map((x) => ({ value: x, label: x }))}
          value={step.method || "GET"}
          onValueChange={(v) => v && set("method", v)}
        >
          <SelectTrigger className="w-full" aria-label={tr("method")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {methods.map((x) => (
              <SelectItem key={x} value={x}>
                {x}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          aria-label={t("url")}
          className="font-mono"
          required
          value={step.url}
          onChange={(e) => set("url", e.target.value)}
          placeholder="https://example.com/api/login"
        />
        <Input
          aria-label={tr("acceptedStatus")}
          value={step.expected_status ?? ""}
          onChange={(e) => set("expected_status", e.target.value)}
          placeholder="200-399"
        />
      </div>
      <div className="grid gap-1.5">
        <Label>{tr("headers")}</Label>
        <Textarea
          rows={2}
          value={headers}
          onChange={(e) => {
            setHeaders(e.target.value)
            set("headers", textToHeaders(e.target.value))
          }}
          placeholder="Content-Type: application/json"
        />
      </div>
      {!["GET", "HEAD"].includes(step.method) && (
        <div className="grid gap-1.5">
          <Label>{tr("body")}</Label>
          <Textarea
            rows={2}
            className="font-mono"
            value={step.body ?? ""}
            onChange={(e) => set("body", e.target.value)}
            placeholder='{"email": "monitor@example.com", "password": "..."}'
          />
        </div>
      )}
      {vars.length > 0 && (
        <p className="text-[11px] text-muted-foreground">
          {t("available", { vars: vars.map((v) => `{{${v}}}`).join(" ") })}
        </p>
      )}
      <div className="grid gap-2">
        <Label>{t("extract")}</Label>
        {extract.map((e, i) => (
          <div
            key={i}
            className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 sm:grid-cols-[minmax(0,1fr)_8rem_minmax(0,2fr)_auto]"
          >
            <Input
              aria-label={t("var")}
              className="font-mono"
              required
              value={e.var}
              onChange={(x) => setExtract(i, { var: x.target.value })}
              placeholder="token"
            />
            <Select
              items={sourceItems}
              value={e.from}
              onValueChange={(v) => v && setExtract(i, { from: v as ExtractFrom })}
            >
              <SelectTrigger className="w-full" aria-label={t("source")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sourceItems.map((x) => (
                  <SelectItem key={x.value} value={x.value}>
                    {x.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              aria-label={t("path")}
              className="col-span-2 font-mono sm:col-span-1"
              required
              value={e.path}
              onChange={(x) => setExtract(i, { path: x.target.value })}
              placeholder={placeholders[e.from]}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t("removeValue")}
              className="col-start-3 row-start-1 sm:col-start-auto"
              onClick={() =>
                set(
                  "extract",
                  extract.filter((_, j) => j !== i)
                )
              }
            >
              <XIcon />
            </Button>
          </div>
        ))}
        {extract.length < maxExtracts && (
          <div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() =>
                set("extract", [...extract, { var: "", from: "json", path: "" }])
              }
            >
              <PlusIcon />
              {t("addValue")}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
