"use client"

import { PlusIcon, XIcon } from "lucide-react"
import { useTranslations } from "use-intl"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { Assertion, AssertionOp } from "@/lib/api"

const assertionOps: AssertionOp[] = [
  "eq",
  "ne",
  "contains",
  "not_contains",
  "exists",
  "not_exists",
  "gt",
  "gte",
  "lt",
  "lte",
  "matches",
]

const noValue: AssertionOp[] = ["exists", "not_exists"]
const numeric: AssertionOp[] = ["gt", "gte", "lt", "lte"]

/** Max rules per monitor, as enforced by the hub. */
const maxRules = 20

export function ResponseRules({
  value,
  onChange,
}: {
  value: Assertion[]
  onChange: (v: Assertion[]) => void
}) {
  const t = useTranslations("monitorForm.responseRules")
  const opItems = assertionOps.map((o) => ({ value: o, label: t(`ops.${o}`) }))
  const update = (i: number, patch: Partial<Assertion>) =>
    onChange(value.map((a, j) => (j === i ? { ...a, ...patch } : a)))

  return (
    <div className="grid gap-2">
      <p className="text-xs text-muted-foreground">
        {t.rich("hint", {
          code: (c) => <code className="font-mono">{c}</code>,
        })}
      </p>
      {value.map((a, i) => (
        <div
          key={i}
          className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-2 border-b border-dashed pb-2 sm:grid-cols-[minmax(0,2fr)_9rem_minmax(0,1.5fr)_auto] sm:border-0 sm:pb-0"
        >
          <Input
            aria-label={t("path")}
            className="col-span-2 font-mono sm:col-span-1"
            required
            value={a.path}
            onChange={(e) => update(i, { path: e.target.value })}
            placeholder="checks.redis.status"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={t("remove")}
            className="sm:order-last"
            onClick={() => onChange(value.filter((_, j) => j !== i))}
          >
            <XIcon />
          </Button>
          <Select
            items={opItems}
            value={a.op}
            onValueChange={(v) =>
              v &&
              update(i, {
                op: v as AssertionOp,
                value: noValue.includes(v as AssertionOp) ? "" : a.value,
              })
            }
          >
            <SelectTrigger className="w-full" aria-label={t("operator")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {opItems.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {noValue.includes(a.op) ? (
            <span />
          ) : (
            <Input
              aria-label={t("value")}
              className="font-mono"
              inputMode={numeric.includes(a.op) ? "decimal" : undefined}
              required={numeric.includes(a.op)}
              value={a.value ?? ""}
              onChange={(e) => update(i, { value: e.target.value })}
              placeholder={
                numeric.includes(a.op)
                  ? "1000"
                  : a.op === "matches"
                    ? "^ok$"
                    : "ok"
              }
            />
          )}
        </div>
      ))}
      {value.length < maxRules && (
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              onChange([...value, { path: "", op: "eq", value: "" }])
            }
          >
            <PlusIcon />
            {t("add")}
          </Button>
        </div>
      )}
    </div>
  )
}
