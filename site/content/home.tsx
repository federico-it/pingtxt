import type { Metadata } from "next"
import Link from "next/link"

import { CopyBlock } from "@/components/copy-block"
import { Shot } from "@/components/shot"
import { Prompt, REPO, SectionTitle } from "@/components/site-shell"
import { UsageStats } from "@/components/usage-stats"
import { localePath, tx, type Lang } from "@/lib/i18n"
import { jsonLd, NAME, pageMeta, softwareJsonLd } from "@/lib/seo"

export function homeMeta(lang: Lang): Metadata {
  const t = tx(lang)
  return pageMeta({
    lang,
    title: t(
      `${NAME} · open source, self-hosted uptime monitoring`,
      `${NAME} · monitoraggio uptime open source e self-hosted`,
    ),
    absoluteTitle: true,
    description: t(
      "Open source, self-hosted uptime monitoring. HTTP, TCP, ping, DNS, database and more checks from multiple locations, status pages and alerts, in one Docker container.",
      "Monitoraggio dell'uptime open source e self-hosted. Controlli HTTP, TCP, ping, DNS, database e altro da più località, status page e avvisi, in un solo container Docker.",
    ),
    path: "/",
  })
}

type Feature = { title: string; body: string }

const featuresEn: Feature[] = [
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
    body: "HTTP(S), keyword, TCP, ICMP ping, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC health, SMTP, MCP servers and heartbeat.",
  },
  {
    title: "status pages",
    body: "Public pages with the monitors or groups you pick, 90-day uptime history and incidents with written updates. Email subscribers, custom domains.",
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
    body: "Webhook, Discord, Slack, Telegram, ntfy and email (SMTP). Test any channel before saving. Weekly or monthly email reports per group.",
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
    title: "prometheus + mcp",
    body: "/metrics for Prometheus, and an MCP server so Claude and other AI assistants can check status and manage monitors.",
  },
  {
    title: "self-updating agents",
    body: "Upgrade the hub, agents update themselves on reconnect, checksum-verified, with rollback on failure.",
  },
]

const featuresIt: Feature[] = [
  {
    title: "un solo container",
    body: "API in Go, scheduler, SQLite e interfaccia web in un'immagine di ~25 MB. Nessun runtime Node; PostgreSQL o MariaDB solo se li vuoi.",
  },
  {
    title: "agent in più località",
    body: "Un binario statico di ~8 MB che apre solo connessioni in uscita via WebSocket. Nessuna porta aperta, funziona dietro NAT. Si installa con un comando.",
  },
  {
    title: "stato a maggioranza",
    body: "Down quando fallisce la maggior parte delle località online, degradato dal 35% (soglia a tua scelta). Tempo di risposta per località.",
  },
  {
    title: "tanti tipi di controllo",
    body: "HTTP(S), parola chiave, TCP, ping ICMP, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC health, SMTP, server MCP e heartbeat.",
  },
  {
    title: "status page",
    body: "Pagine pubbliche con i monitor o i gruppi che scegli, 90 giorni di storico dell'uptime e incidenti con aggiornamenti scritti. Iscritti via email, domini personalizzati.",
  },
  {
    title: "finestre di manutenzione",
    body: "Singole o ricorrenti, in qualsiasi fuso orario, per monitor, gruppi o tag. Nessun avviso, non contano nell'uptime.",
  },
  {
    title: "scadenza certificati + domini",
    body: "Avvisi sui certificati TLS e sulla registrazione dei domini (RDAP, con WHOIS come riserva) con le soglie che decidi tu.",
  },
  {
    title: "notifiche",
    body: "Webhook, Discord, Slack, Telegram, ntfy ed email (SMTP). Prova ogni canale prima di salvarlo. Report via email settimanali o mensili per gruppo.",
  },
  {
    title: "gruppi e tag",
    body: "Sezioni nella dashboard e nelle status page, tag colorati, filtri. Selezioni per gruppo o tag ovunque.",
  },
  {
    title: "accesso come preferisci",
    body: "Password + TOTP, GitHub, Google o qualsiasi provider OpenID Connect. Token API personali, in sola lettura o lettura+scrittura.",
  },
  {
    title: "prometheus + mcp",
    body: "/metrics per Prometheus e un server MCP, così Claude e altri assistenti AI possono controllare lo stato e gestire i monitor.",
  },
  {
    title: "agent che si aggiornano da soli",
    body: "Aggiorni l'hub e gli agent si aggiornano da soli quando si ricollegano, con checksum verificato e ritorno alla versione precedente se qualcosa va storto.",
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

const agent = (t: ReturnType<typeof tx>) => `${t("# in the UI: agents → add agent, then on the remote machine:", "# nell'interfaccia: agent → aggiungi agent, poi sulla macchina remota:")}
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \\
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \\
  -v pingtxt-agent:/data ghcr.io/federico-it/pingtxt-agent:latest

${t("# or Linux + systemd, binary downloaded from your hub", "# oppure Linux + systemd, binario scaricato dal tuo hub")}
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- \\
  --hub https://status.example.com --token pt_xxx`

export function HomePage({ lang }: { lang: Lang }) {
  const t = tx(lang)
  const features = t(featuresEn, featuresIt)
  return (
    <div className="space-y-20 py-12">
      <script type="application/ld+json" dangerouslySetInnerHTML={jsonLd(softwareJsonLd(lang))} />
      <section className="space-y-6">
        <Prompt>pingtxt --help</Prompt>
        <h1 className="max-w-3xl text-2xl font-semibold leading-tight tracking-tight sm:text-4xl">
          {t("minimal, self-hosted uptime monitoring", "monitoraggio dell'uptime minimale e self-hosted")}<span className="cursor-blink text-up">_</span>
        </h1>
        <p className="max-w-2xl text-muted-foreground sm:text-[15px]">
          {t(
            "Plain text, monospace, light and dark. One container for the hub, tiny agents for checks from every location you care about. Open source, scales to thousands of monitors on a small VPS.",
            "Testo semplice, monospace, chiaro e scuro. Un container per l'hub, agent leggeri per controllare da ogni località che ti interessa. Open source, regge migliaia di monitor su una piccola VPS.",
          )}
        </p>
        <div className="flex flex-wrap gap-2">
          <a
            href="#install"
            className="bg-primary px-3 py-1.5 font-medium text-primary-foreground transition-opacity hover:opacity-90"
          >
            $ {t("install", "installa")}
          </a>
          <Link href={localePath(lang, "/docs/")} className="border border-dashed px-3 py-1.5 transition-colors hover:bg-accent">
            {t("read the docs", "leggi la documentazione")}
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
            <span className="text-warn">●</span> {t("degraded", "degradato")}
          </span>
          <span>
            <span className="text-down">●</span> down
          </span>
          <span>
            <span className="text-info">●</span> {t("maintenance", "manutenzione")}
          </span>
        </div>
        <div className="pt-4">
          <Shot
            light="/screenshots/dashboard-light.png"
            dark="/screenshots/dashboard-dark.png"
            label="pingtxt monitors ls"
            alt={t(
              "pingtxt dashboard: list of monitors with status, uptime bars and response time per location",
              "dashboard di pingtxt: elenco dei monitor con stato, barre di uptime e tempo di risposta per località",
            )}
            width={1280}
            height={860}
            priority
          />
        </div>
      </section>

      <section id="features" className="space-y-6">
        <SectionTitle>{t("features", "funzioni")}</SectionTitle>
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
          <Shot
            light="/screenshots/monitor.png"
            label="pingtxt monitors show"
            alt={t(
              "Monitor detail page with a response time chart and the check log per location",
              "Pagina di dettaglio di un monitor con il grafico dei tempi di risposta e il log dei controlli per località",
            )}
            width={1280}
            height={1388}
            caption={t("Response time and log per location.", "Tempo di risposta e log per località.")}
          />
          <Shot
            light="/screenshots/status-page-light.png"
            dark="/screenshots/status-page-dark.png"
            label="status.example.com"
            alt={t(
              "Public status page with 90-day uptime history per service",
              "Status page pubblica con 90 giorni di storico dell'uptime per servizio",
            )}
            width={1200}
            height={900}
            caption={t("A public status page, on its own domain if you like.", "Una status page pubblica, anche su un dominio tutto suo.")}
          />
          <Shot
            light="/screenshots/check-types.png"
            label="pingtxt monitors new"
            alt={t(
              "New monitor form with the list of check types: HTTP, keyword, TCP, ping, DNS, databases and more",
              "Modulo per un nuovo monitor con l'elenco dei tipi di controllo: HTTP, parola chiave, TCP, ping, DNS, database e altro",
            )}
            width={1280}
            height={900}
            caption={t("Pick a check type.", "Scegli un tipo di controllo.")}
          />
          <Shot
            light="/screenshots/agent-install.png"
            label="pingtxt agents add"
            alt={t(
              "Adding a monitoring location: the one-line agent install command",
              "Aggiunta di una località di monitoraggio: il comando di una riga per installare l'agent",
            )}
            width={1280}
            height={900}
            caption={t("Add a location in one command.", "Aggiungi una località con un comando.")}
          />
        </div>
      </section>

      <UsageStats />

      <section id="install" className="space-y-6">
        <SectionTitle>{t("install", "installazione")}</SectionTitle>
        <div className="grid gap-8 lg:grid-cols-2 [&>*]:min-w-0">
          <div className="min-w-0 space-y-3">
            <h3 className="font-semibold">{t("1. run the hub", "1. avvia l'hub")}</h3>
            <p className="text-muted-foreground">
              {t(
                <>
                  Save this as <code className="text-foreground">docker-compose.yml</code> and run{" "}
                  <code className="text-foreground">docker compose up -d</code>. Open{" "}
                  <code className="text-foreground">http://localhost:8090</code> and create the admin account.
                </>,
                <>
                  Salvalo come <code className="text-foreground">docker-compose.yml</code> ed esegui{" "}
                  <code className="text-foreground">docker compose up -d</code>. Apri{" "}
                  <code className="text-foreground">http://localhost:8090</code> e crea l&apos;account admin.
                </>,
              )}
            </p>
            <CopyBlock label="docker-compose.yml" code={compose} />
            <CopyBlock
              label={t("or plain docker", "oppure solo docker")}
              code={`docker run -d --name pingtxt --restart unless-stopped \\
  -p 8090:8090 -v ./data:/data \\
  ghcr.io/federico-it/pingtxt:latest`}
            />
          </div>
          <div className="min-w-0 space-y-3">
            <h3 className="font-semibold">{t("2. add locations (optional)", "2. aggiungi località (facoltativo)")}</h3>
            <p className="text-muted-foreground">
              {t(
                "The hub checks from where it runs. For more locations, start an agent anywhere: it dials out to the hub, so nothing needs to be exposed.",
                "L'hub controlla dal posto in cui gira. Per altre località avvia un agent dove vuoi: è lui a collegarsi all'hub, quindi non c'è niente da esporre.",
              )}
            </p>
            <CopyBlock label="agent" code={agent(t)} />
            <p className="text-muted-foreground">
              {t(
                "Everything else (public URL, login providers, notifications, retention) is set in the web UI.",
                "Tutto il resto (URL pubblico, provider di accesso, notifiche, conservazione dei dati) si imposta nell'interfaccia web.",
              )}{" "}
              <Link href={localePath(lang, "/docs/")} className="text-foreground underline underline-offset-4">
                {t("Full docs →", "Documentazione completa →")}
              </Link>
            </p>
          </div>
        </div>
      </section>

      <section className="space-y-6">
        <SectionTitle>{t("how it works", "come funziona")}</SectionTitle>
        <pre className="overflow-x-auto border border-dashed bg-muted/50 p-4 text-[12px] leading-snug">{`            ┌──────────────── hub container ────────────────┐
 browser ──>│  Go http server ─ /api/*  ─ SQLite/PG/MariaDB │
            │        │          /        ─ web UI            │
            │   scheduler ──> local probe ("hub" location)  │
            │        │                                       │
            └────────┼───────────────────────────────────────┘
                     │ ${t("jobs / results over WebSocket (agent dials out)", "lavori / risultati via WebSocket (si collega l'agent)")}
          ┌──────────┴──────────┐
     ${t("agent (Frankfurt)     agent (home lab) ...", "agent (Francoforte)   agent (home lab) ...")}`}</pre>
        <p className="max-w-3xl text-muted-foreground">
          {t(
            "The hub schedules every check and sends it to the agents; agents are stateless and reconnect with backoff. Hub and agents share the same Go probe code, so a check behaves the same from every location.",
            "L'hub pianifica ogni controllo e lo manda agli agent; gli agent non hanno stato e si ricollegano con backoff. Hub e agent usano lo stesso codice Go per i controlli, quindi un controllo si comporta allo stesso modo da ogni località.",
          )}
        </p>
      </section>
    </div>
  )
}
