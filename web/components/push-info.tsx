"use client"

import { useState } from "react"
import { toast } from "sonner"

import { Confirm } from "@/components/confirm"
import { CopyBlock } from "@/components/copy-block"
import { Section } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"

const examples = (url: string) => ({
  cron: `# crontab: ping only when the job succeeds
0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 ${url} >/dev/null`,
  failure: `# report the outcome either way, with the run time in ms
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
  const ex = examples(url)

  async function rotate() {
    await api(`/api/monitors/${id}/push-token`, { method: "POST" })
    toast.success("new push URL created")
    onRotated()
  }

  return (
    <Section
      title="push url"
      actions={
        <div className="flex items-center gap-1 normal-case">
          {(["url", "cron", "failure"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={cn(
                "px-1.5 hover:text-foreground",
                tab === t && "bg-accent text-foreground"
              )}
            >
              {t}
            </button>
          ))}
        </div>
      }
    >
      <p className="mb-2 text-xs text-muted-foreground">
        Call this URL (any method) at the end of every run. Optional parameters:{" "}
        <code>status=down</code> to report a failed run, <code>msg=</code> for a
        short note, <code>ping=</code> for the run time in ms. Keep it secret:
        anyone with the URL can send heartbeats.
      </p>
      <CopyBlock code={ex[tab]} />
      <div className="mt-2">
        <Confirm
          title="create a new push URL?"
          description="The current URL stops working immediately. Update your jobs with the new one."
          action="regenerate"
          onConfirm={rotate}
          trigger={
            <Button variant="outline" size="xs">
              regenerate url
            </Button>
          }
        />
      </div>
    </Section>
  )
}
