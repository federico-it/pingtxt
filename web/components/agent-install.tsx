"use client"

import { useState } from "react"

import { CopyBlock } from "@/components/copy-block"
import { cn } from "@/lib/utils"

const AGENT_IMAGE = "ghcr.io/federico-it/pingtxt-agent:latest"

export function AgentInstall({ hub, token }: { hub: string; token: string }) {
  const tabs = {
    docker: `docker run -d --name pingtxt-agent --restart unless-stopped \\
  --cap-add NET_RAW \\
  -e PINGTXT_HUB=${hub} \\
  -e PINGTXT_TOKEN=${token} \\
  ${AGENT_IMAGE}`,
    compose: `services:
  pingtxt-agent:
    image: ${AGENT_IMAGE}
    restart: unless-stopped
    cap_add: [NET_RAW]
    environment:
      PINGTXT_HUB: ${hub}
      PINGTXT_TOKEN: ${token}`,
    linux: `curl -fsSL ${hub}/install-agent.sh | sudo sh -s -- --hub ${hub} --token ${token}`,
    binary: `curl -fsSLo pingtxt-agent ${hub}/download/pingtxt-agent-linux-amd64 && chmod +x pingtxt-agent
PINGTXT_HUB=${hub} PINGTXT_TOKEN=${token} ./pingtxt-agent`,
  }
  const [tab, setTab] = useState<keyof typeof tabs>("docker")
  return (
    <div className="grid min-w-0 gap-2">
      <div className="flex gap-1 text-xs">
        {(Object.keys(tabs) as (keyof typeof tabs)[]).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={cn("px-2 py-0.5 text-muted-foreground hover:text-foreground", tab === t && "bg-accent text-foreground")}
          >
            {t}
          </button>
        ))}
      </div>
      <CopyBlock code={tabs[tab]} />
      <p className="text-[11px] text-muted-foreground">
        The agent only dials out to the hub over HTTPS/WebSocket: no ports to open. The token is shown only now; rotate it to get a new one.
      </p>
    </div>
  )
}
