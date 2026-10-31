import Link from "next/link"

import { CopyBlock } from "@/components/copy-block"
import { Shot } from "@/components/shot"
import { Prompt, REPO, SectionTitle } from "@/components/site-shell"

const features: { title: string; body: string }[] = [
  {
    title: "one container",
    body: "Go API, scheduler, SQLite and the web UI in a ~25 MB image. No Node runtime; PostgreSQL or MariaDB only if you want them.",
  },
  {
    title: "multi-location agents",
    body: "A ~8 MB static binary that only dials out over WebSocket. No open ports, works behind NAT. Install with one command.",
  },
  {
    title: "majority vote status",
    body: "Down when most online locations fail, degraded from 35% (your threshold). Response time per location.",
  },
  {
    title: "lots of check types",
    body: "HTTP(S), keyword, TCP, ICMP ping, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC health, SMTP and heartbeat.",
  },
  {
    title: "status pages",
    body: "Public pages with the monitors or groups you pick, 90-day uptime history and incidents. Custom domains.",
  },
  {
    title: "maintenance windows",
    body: "One-off or recurring, in any time zone, for monitors, groups or tags. No alerts, not counted in uptime.",
  },
  {
    title: "certificate + domain expiry",
    body: "TLS certificate and domain registration alerts (RDAP, WHOIS fallback) with thresholds you set.",
  },
  {
    title: "notifications",
    body: "Webhook, Discord, Slack, Telegram, ntfy and email (SMTP). Test any channel before saving.",
  },
  {
    title: "groups and tags",
    body: "Sections on the dashboard and on status pages, colored tags, filters. Select by group or tag anywhere.",
  },
  {
    title: "login your way",
    body: "Password + TOTP, GitHub, Google or any OpenID Connect provider. Personal API tokens, read or read+write.",
  },
  {
    title: "prometheus",
    body: "/metrics with status, latency per location, uptime, expiry and agent connectivity.",
  },
  {
    title: "self-updating agents",
    body: "Upgrade the hub, agents update themselves on reconnect, checksum-verified, with rollback on failure.",
  },
]

const compose = `services:
  pingtxt:
    image: ghcr.io/federico-it/pingtxt:latest
    restart: unless-stopped
    ports:
      - "8090:8090"
    volumes:
      - ./data:/data`

const agent = `# in the UI: agents → add agent, then on the remote machine:
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \\
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \\
  ghcr.io/federico-it/pingtxt-agent:latest

# or Linux + systemd, binary downloaded from your hub
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- \\
  --hub https://status.example.com --token pt_xxx`

export default function Home() {
  return (
    <div className="space-y-20 py-12">
      <section className="space-y-6">
        <Prompt>pingtxt --help</Prompt>
        <h1 className="max-w-3xl text-2xl font-semibold leading-tight tracking-tight sm:text-4xl">
          minimal, self-hosted uptime monitoring<span className="cursor-blink text-up">_</span>
        </h1>
        <p className="max-w-2xl text-muted-foreground sm:text-[15px]">
          Plain text, monospace, light and dark. One container for the hub, tiny agents for checks from every location
          you care about. Open source, scales to thousands of monitors on a small VPS.
        </p>
        <div className="flex flex-wrap gap-2">
          <a
            href="#install"
            className="bg-primary px-3 py-1.5 font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            $ install
          </a>
          <Link href="/docs/" className="border border-dashed px-3 py-1.5 transition-colors hover:bg-accent">
            read the docs
          </Link>
          <a href={REPO} className="border border-dashed px-3 py-1.5 transition-colors hover:bg-accent">
            github ↗
          </a>
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
          <span>
            <span className="text-up">●</span> up
          </span>
          <span>
            <span className="text-warn">●</span> degraded
          </span>
          <span>
            <span className="text-down">●</span> down
          </span>
          <span>
            <span className="text-info">●</span> maintenance
          </span>
        </div>
        <div className="pt-4">
          <Shot
            light="/screenshots/dashboard-light.png"
            dark="/screenshots/dashboard-dark.png"
            alt="pingtxt monitors ls"
          />
        </div>
      </section>

      <section id="features" className="space-y-6">
        <SectionTitle>features</SectionTitle>
        <div className="grid gap-px border border-dashed bg-border sm:grid-cols-2 lg:grid-cols-3">
          {features.map((f) => (
            <div key={f.title} className="space-y-1.5 bg-background p-4">
              <h3 className="font-semibold">
                <span className="text-up">+</span> {f.title}
              </h3>
              <p className="text-muted-foreground">{f.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="space-y-6">
        <SectionTitle>screenshots</SectionTitle>
        <div className="grid gap-8 lg:grid-cols-2 [&>*]:min-w-0">
          <Shot light="/screenshots/monitor.png" alt="pingtxt monitors show" caption="Response time and log per location." />
          <Shot
            light="/screenshots/status-page-light.png"
            dark="/screenshots/status-page-dark.png"
            alt="status.example.com"
            caption="A public status page, on its own domain if you like."
          />
          <Shot light="/screenshots/check-types.png" alt="pingtxt monitors new" caption="Pick a check type." />
          <Shot light="/screenshots/agent-install.png" alt="pingtxt agents add" caption="Add a location in one command." />
        </div>
      </section>

      <section id="install" className="space-y-6">
        <SectionTitle>install</SectionTitle>
        <div className="grid gap-8 lg:grid-cols-2 [&>*]:min-w-0">
          <div className="min-w-0 space-y-3">
            <h3 className="font-semibold">1. run the hub</h3>
            <p className="text-muted-foreground">
              Save this as <code className="text-foreground">docker-compose.yml</code> and run{" "}
              <code className="text-foreground">docker compose up -d</code>. Open{" "}
              <code className="text-foreground">http://localhost:8090</code> and create the admin account.
            </p>
            <CopyBlock label="docker-compose.yml" code={compose} />
            <CopyBlock
              label="or plain docker"
              code={`docker run -d --name pingtxt --restart unless-stopped \\
  -p 8090:8090 -v ./data:/data \\
  ghcr.io/federico-it/pingtxt:latest`}
            />
          </div>
          <div className="min-w-0 space-y-3">
            <h3 className="font-semibold">2. add locations (optional)</h3>
            <p className="text-muted-foreground">
              The hub checks from where it runs. For more locations, start an agent anywhere: it dials out to the hub, so
              nothing needs to be exposed.
            </p>
            <CopyBlock label="agent" code={agent} />
            <p className="text-muted-foreground">
              Everything else (public URL, login providers, notifications, retention) is set in the web UI.{" "}
              <Link href="/docs/" className="text-foreground underline underline-offset-4">
                Full docs →
              </Link>
            </p>
          </div>
        </div>
      </section>

      <section className="space-y-6">
        <SectionTitle>how it works</SectionTitle>
        <pre className="overflow-x-auto border border-dashed bg-muted/50 p-4 text-[12px] leading-snug">{`            ┌──────────────── hub container ────────────────┐
 browser ──>│  Go http server ─ /api/*  ─ SQLite/PG/MariaDB │
            │        │          /        ─ web UI            │
            │   scheduler ──> local probe ("hub" location)  │
            │        │                                       │
            └────────┼───────────────────────────────────────┘
                     │ jobs / results over WebSocket (agent dials out)
          ┌──────────┴──────────┐
     agent (Frankfurt)     agent (home lab) ...`}</pre>
        <p className="max-w-3xl text-muted-foreground">
          The hub schedules every check and sends it to the agents; agents are stateless and reconnect with backoff. Hub and
          agents share the same Go probe code, so a check behaves the same from every location.
        </p>
      </section>
    </div>
  )
}
