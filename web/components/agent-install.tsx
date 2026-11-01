"use client"

import { useState } from "react"
import { useTranslations } from "use-intl"

import { CopyBlock } from "@/components/copy-block"
import { segment } from "@/components/ui/control"

const AGENT_IMAGE = "ghcr.io/federico-it/pingtxt-agent:latest"

export function AgentInstall({ hub, token }: { hub: string; token: string }) {
  const tabs = {
    docker: `docker run -d --name pingtxt-agent --restart unless-stopped \\
  --cap-add NET_RAW \\
  -e PINGTXT_HUB=${hub} \\
  -e PINGTXT_TOKEN=${token} \\
  -v pingtxt-agent:/data \\
  ${AGENT_IMAGE}`,
    compose: `services:
  pingtxt-agent:
    image: ${AGENT_IMAGE}
    restart: unless-stopped
    cap_add: [NET_RAW]
    environment:
      PINGTXT_HUB: ${hub}
      PINGTXT_TOKEN: ${token}
    volumes:
      - pingtxt-agent:/data # keeps self-updates
volumes:
  pingtxt-agent:`,
    linux: `curl -fsSL ${hub}/install-agent.sh | sudo sh -s -- --hub ${hub} --token ${token}`,
    binary: `curl -fsSLo pingtxt-agent ${hub}/download/pingtxt-agent-linux-amd64 && chmod +x pingtxt-agent
PINGTXT_HUB=${hub} PINGTXT_TOKEN=${token} ./pingtxt-agent`,
  }
  const [tab, setTab] = useState<keyof typeof tabs>("docker")
  const t = useTranslations("agentInstall")
  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex gap-1 text-xs">
        {(Object.keys(tabs) as (keyof typeof tabs)[]).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setTab(k)}
            className={segment({ active: tab === k })}
          >
            {k}
          </button>
        ))}
      </div>
      <CopyBlock code={tabs[tab]} />
      <p className="text-[11px] text-muted-foreground">
        {t("hint")}
      </p>
    </div>
  )
}
