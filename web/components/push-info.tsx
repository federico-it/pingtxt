"use client"

import { useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"

import { Confirm } from "@/components/confirm"
import { CopyBlock } from "@/components/copy-block"
import { Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { segment } from "@/components/ui/control"

const examples = (url: string, c: { cron: string; failure: string }) => ({
  cron: `# ${c.cron}
0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 ${url} >/dev/null`,
  failure: `# ${c.failure}
start=$(date +%s%3N)
if /usr/local/bin/backup.sh; then status=up; else status=down; fi
curl -fsS -m 10 --retry 3 "${url}?status=$status&ping=$(( $(date +%s%3N) - start ))&msg=backup" >/dev/null`,
  url,
})

/** Heartbeat URL and usage examples for a push monitor. */
export function PushInfo({
  id,
  url,
  onRotated,
}: {
  id: number
  url: string
  onRotated: () => void
}) {
  const [tab, setTab] = useState<"url" | "cron" | "failure">("url")
  const t = useTranslations("pushInfo")
  const ex = examples(url, { cron: t("cronComment"), failure: t("failureComment") })

  async function rotate() {
    await api(`/api/monitors/${id}/push-token`, { method: "POST" })
    toast.success(t("rotated"))
    onRotated()
  }

  return (
    <Section
      title={t("title")}
      actions={
        <div className="flex items-center gap-1 normal-case">
          {(["url", "cron", "failure"] as const).map((k) => (
            <button
              key={k}
              onClick={() => setTab(k)}
              className={segment({ active: tab === k })}
            >
              {t(`tabs.${k}`)}
            </button>
          ))}
        </div>
      }
    >
      <p className="mb-2 text-xs text-muted-foreground">
        {t.rich("hint", { code: (c) => <code>{c}</code> })}
      </p>
      <CopyBlock code={ex[tab]} />
      <div className="mt-2">
        <Confirm
          title={t("confirmTitle")}
          description={t("confirmText")}
          action={t("regenerate")}
          onConfirm={rotate}
          trigger={
            <Button variant="outline" size="xs">
              {t("regenerateUrl")}
            </Button>
          }
        />
      </div>
    </Section>
  )
}
