import type { Metadata } from "next"
import Link from "next/link"

import { Cmd, CodeCard } from "@/components/code-card"
import { Shot } from "@/components/shot"
import { BTN, BTN_PRIMARY, Eyebrow, REPO, WRAP } from "@/components/ui"
import { UsageStats } from "@/components/usage-stats"
import { docHref } from "@/lib/docs"
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
    body: "HTTP(S) with multi-step requests, keyword, TCP, UDP, WebSocket, ICMP ping, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC health, SMTP, MCP servers and heartbeat.",
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
    body: "Email, Slack, Discord, Teams, Telegram, Matrix, ntfy, Gotify, Pushover, PagerDuty, Opsgenie, SMS and webhooks. Test any channel before saving. Weekly or monthly email reports per group.",
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
    body: "HTTP(S) anche a più passaggi, parola chiave, TCP, UDP, WebSocket, ping ICMP, DNS, PostgreSQL, MySQL, Redis, MongoDB, Docker, gRPC health, SMTP, server MCP e heartbeat.",
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
    body: "Email, Slack, Discord, Teams, Telegram, Matrix, ntfy, Gotify, Pushover, PagerDuty, Opsgenie, SMS e webhook. Prova ogni canale prima di salvarlo. Report via email settimanali o mensili per gruppo.",
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

const agentCmd = "curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --token pt_xxx"

// The hero picture: a monitor list drawn in HTML, uptime bars as pixels.
// u up, w degraded, d down, m maintenance.
const demo: { name: string; type: string; bars: string; uptime: string; ms: string }[] = [
  { name: "api.example.com", type: "https", bars: "uuuuuuuuuuuuuuuuuuuuuuuuuuuuuu", uptime: "100%", ms: "142 ms" },
  { name: "shop.example.com", type: "keyword", bars: "uuuuuuuuuuuuuuuuuwuuuuuuuuuuuu", uptime: "99.98%", ms: "88 ms" },
  { name: "db-primary", type: "postgres", bars: "uuuuuuuuuduuuuuuuuuuuuuuuuuuuu", uptime: "99.91%", ms: "4 ms" },
  { name: "mail.example.com", type: "smtp", bars: "uuuuuuuuuuuuuuuuuuuuummuuuuuuu", uptime: "100%", ms: "31 ms" },
  { name: "vpn-gateway", type: "ping", bars: "uuuuuuuuuuuuuuuuuuuuuuuuuuuwwu", uptime: "99.72%", ms: "19 ms" },
]
const cell: Record<string, string> = { u: "bg-up", w: "bg-warn", d: "bg-down", m: "bg-info" }

function DemoCard({ lang }: { lang: Lang }) {
  const t = tx(lang)
  return (
    <div
      aria-hidden
      className="border border-edge bg-background shadow-[8px_8px_0_0_var(--color-hard)] dark:shadow-[8px_8px_0_0_var(--color-up)]"
    >
      <div className="flex items-center gap-1.5 border-b border-edge px-3 py-2 text-[11px] text-muted-foreground">
        <span className="size-2 bg-down/80" />
        <span className="size-2 bg-warn/80" />
        <span className="size-2 bg-up/80" />
        <span className="ml-2 truncate">pingtxt monitors ls</span>
        <span className="ml-auto shrink-0 text-up">● live</span>
      </div>
      <ul className="divide-y divide-dashed">
        {demo.map((m) => (
          <li key={m.name} className="grid grid-cols-[minmax(0,1fr)_4rem] items-center gap-x-4 gap-y-1.5 px-3 py-2.5">
            <div className="min-w-0 truncate">
              <span className={m.bars.endsWith("u") ? "text-up" : "text-warn"}>●</span>{" "}
              <span className="font-semibold">{m.name}</span>{" "}
              <span className="text-[11px] text-muted-foreground">{m.type}</span>
            </div>
            <div className="text-right text-[11px] text-muted-foreground tabular-nums">{m.ms}</div>
            <div className="flex gap-[2px]">
              {[...m.bars].map((c, i) => (
                <span key={i} className={`h-3.5 flex-1 ${cell[c]}`} />
              ))}
            </div>
            <div className="text-right text-[11px] font-semibold tabular-nums">{m.uptime}</div>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap justify-between gap-x-4 gap-y-1 border-t border-edge px-3 py-2 text-[11px] text-muted-foreground">
        <span>{t("4 locations", "4 località")}: frankfurt · milan · new york · home lab</span>
        <span>{t("30 days", "30 giorni")}</span>
      </div>
    </div>
  )
}

function H2({ children }: { children: React.ReactNode }) {
  return <h2 className="mt-3 text-3xl font-bold leading-[1.08] tracking-tight text-balance sm:text-5xl">{children}</h2>
}

function Lede({ children }: { children: React.ReactNode }) {
  return <p className="mt-4 max-w-2xl text-[14px] leading-relaxed text-muted-foreground sm:text-[15px]">{children}</p>
}

const section = "py-20 sm:py-24"
const band = "border-y border-edge bg-paper"
const card = "border border-edge bg-background"

export function HomePage({ lang }: { lang: Lang }) {
  const t = tx(lang)
  const features = t(featuresEn, featuresIt)
  const docs = localePath(lang, "/docs/")
  const steps: { title: string; body: React.ReactNode; cmd?: string }[] = [
    {
      title: t("Run the hub", "Avvia l'hub"),
      body: t(
        "One container with the API, the scheduler, the database and the web UI. Save the file on the right and start it.",
        "Un container con l'API, lo scheduler, il database e l'interfaccia web. Salva il file qui accanto e avvialo.",
      ),
      cmd: "docker compose up -d",
    },
    {
      title: t("Create the admin account", "Crea l'account admin"),
      body: t(
        <>
          Open <code className="text-foreground">http://localhost:8090</code>. Password + TOTP, or sign in with GitHub,
          Google or any OpenID Connect provider.
        </>,
        <>
          Apri <code className="text-foreground">http://localhost:8090</code>. Password + TOTP, oppure accesso con GitHub,
          Google o qualsiasi provider OpenID Connect.
        </>,
      ),
    },
    {
      title: t("Add locations", "Aggiungi località"),
      body: t(
        "Optional. Start an agent anywhere: it dials out to the hub over WebSocket, so nothing needs to be exposed.",
        "Facoltativo. Avvia un agent dove vuoi: è lui a collegarsi all'hub via WebSocket, quindi non c'è niente da esporre.",
      ),
      cmd: agentCmd,
    },
    {
      title: t("Add monitors and channels", "Aggiungi monitor e canali"),
      body: t(
        "Everything else is in the web UI: checks, status pages, notification channels you can test before saving.",
        "Tutto il resto è nell'interfaccia web: controlli, status page, canali di notifica da provare prima di salvarli.",
      ),
    },
  ]
  const places: { title: string; body: string; bullets: string[]; note: string; href: string; cta: string; tag?: string }[] = [
    {
      title: t("A server you have", "Un server che hai già"),
      body: t(
        "A small VPS, a home server, a Raspberry Pi. Docker Compose and the file above.",
        "Una piccola VPS, un server di casa, un Raspberry Pi. Docker Compose e il file qui sopra.",
      ),
      bullets: t(
        ["~25 MB image, SQLite by default", "Thousands of monitors on a small VPS", "Backups and config export in the UI"],
        ["Immagine di ~25 MB, SQLite di default", "Migliaia di monitor su una piccola VPS", "Backup ed esportazione della configurazione dall'interfaccia"],
      ),
      note: t("Free. It's your server.", "Gratis. Il server è tuo."),
      href: docHref(lang, "install"),
      cta: t("Read the install guide", "Leggi la guida"),
      tag: t("recommended", "consigliato"),
    },
    {
      title: t("Coolify or another PaaS", "Coolify o un'altra PaaS"),
      body: t(
        "Paste one of the ready compose files into Coolify and deploy. The platform takes care of the rest.",
        "Incolla in Coolify uno dei file compose pronti e pubblica. Al resto pensa la piattaforma.",
      ),
      bullets: t(
        ["Compose files for SQLite or PostgreSQL", "Domain and TLS set up for you", "Nothing to write by hand"],
        ["File compose per SQLite o PostgreSQL", "Dominio e TLS configurati da Coolify", "Niente da scrivere a mano"],
      ),
      note: t("Whatever your host charges.", "Quello che costa il tuo hosting."),
      href: docHref(lang, "install", "coolify"),
      cta: t("Coolify setup", "Configura Coolify"),
    },
    {
      title: t("Without Docker", "Senza Docker"),
      body: t(
        "One static Go binary and a systemd unit, for machines where you would rather not run containers.",
        "Un binario Go statico e un'unità systemd, per le macchine dove preferisci non usare container.",
      ),
      bullets: t(
        ["Linux amd64 and arm64", "Binaries on the releases page", "Same code as the container"],
        ["Linux amd64 e arm64", "Binari nella pagina delle release", "Lo stesso codice del container"],
      ),
      note: t("Free, and nothing else to install.", "Gratis, e nient'altro da installare."),
      href: docHref(lang, "install", "without-docker"),
      cta: t("Run the binary", "Usa il binario"),
    },
  ]
  const parts: { title: string; body: string }[] = [
    {
      title: t("The hub", "L'hub"),
      body: t(
        "A Go server with the REST API, the scheduler and the web UI. It plans every check and stores the results in SQLite, PostgreSQL or MariaDB.",
        "Un server Go con la REST API, lo scheduler e l'interfaccia web. Pianifica ogni controllo e salva i risultati in SQLite, PostgreSQL o MariaDB.",
      ),
    },
    {
      title: t("The agents", "Gli agent"),
      body: t(
        "Stateless ~8 MB binaries that dial out over WebSocket and reconnect with backoff. They update themselves when the hub does.",
        "Binari di ~8 MB senza stato che si collegano in uscita via WebSocket e si ricollegano con backoff. Si aggiornano da soli insieme all'hub.",
      ),
    },
    {
      title: t("The vote", "Il voto"),
      body: t(
        "Each location runs the same probe code. A monitor is down when most online locations fail, so one bad network path doesn't page you.",
        "Ogni località usa lo stesso codice di controllo. Un monitor è down quando fallisce la maggior parte delle località online, così un solo percorso di rete difettoso non ti sveglia.",
      ),
    },
    {
      title: t("Your data", "I tuoi dati"),
      body: t(
        "Everything lives in one data folder or your own database. Export the config, back it up, read it with the API or the MCP server.",
        "Tutto sta in una cartella di dati o nel tuo database. Esporti la configurazione, fai backup, la leggi con l'API o il server MCP.",
      ),
    },
  ]
  return (
    <div className="site-bleed">
      <script type="application/ld+json" dangerouslySetInnerHTML={jsonLd(softwareJsonLd(lang))} />

      {/* hero */}
      <section className="bg-band bg-grid">
        <div className={`${WRAP} grid items-center gap-12 pt-14 pb-16 sm:pt-20 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.05fr)] lg:gap-16 lg:pb-24`}>
          <div className="min-w-0 lg:order-2">
            <h1 className="text-[2.6rem] font-bold leading-[1.02] tracking-tight sm:text-6xl">
              {t("Uptime,", "L'uptime,")}
              <br />
              {t("in plain text", "in testo semplice")}
              <span className="cursor-blink text-up">_</span>
            </h1>
            <p className="mt-6 max-w-xl text-[14px] leading-relaxed sm:text-[15px]">
              {t(
                "Pingtxt watches your sites, APIs and servers from every location you care about, and tells you when something breaks. One container for the hub, tiny agents for the checks, on hardware you control.",
                "Pingtxt controlla siti, API e server da ogni località che ti interessa e ti avvisa quando qualcosa si rompe. Un container per l'hub, agent leggeri per i controlli, su macchine che controlli tu.",
              )}
            </p>
            <div className="mt-8 flex flex-wrap gap-3">
              <a href="#install" className={BTN_PRIMARY}>
                $ {t("install", "installa")}
              </a>
              <Link href={docs} className={BTN}>
                {t("Read the docs", "Leggi la documentazione")}
              </Link>
            </div>
            <p className="mt-6 max-w-md text-[12px] font-semibold text-muted-foreground">
              {t(
                "Runs on a small VPS, a Raspberry Pi or your home lab. Open source, AGPL-3.0.",
                "Gira su una piccola VPS, un Raspberry Pi o il tuo home lab. Open source, AGPL-3.0.",
              )}
            </p>
          </div>
          <div className="min-w-0 lg:order-1">
            <DemoCard lang={lang} />
          </div>
        </div>
        <div aria-hidden className="pixel-edge bg-background" />
      </section>

      {/* why */}
      <section className={`${WRAP} pt-16 pb-6 sm:pt-20`}>
        <Eyebrow>{t("why pingtxt", "perché pingtxt")}</Eyebrow>
        <div className="mt-4 max-w-4xl space-y-4 text-[15px] leading-relaxed sm:text-[17px]">
          <p>
            {t(
              "Most uptime monitors are someone else's cloud, priced per check, and they see everything you watch. Pingtxt is the one you run yourself.",
              "La maggior parte dei monitor di uptime è il cloud di qualcun altro, a pagamento per controllo, e vede tutto quello che controlli. Pingtxt è quello che gestisci tu.",
            )}
          </p>
          <p className="text-muted-foreground">
            {t(
              "It's small on purpose: plain text, monospace, light and dark, one container. Agents check from where your users are and vote, so you hear about real outages and not about one flaky route.",
              "È piccolo apposta: testo semplice, monospace, chiaro e scuro, un solo container. Gli agent controllano da dove sono i tuoi utenti e votano, così ti arrivano i disservizi veri e non quelli di una sola rotta instabile.",
            )}
          </p>
        </div>
      </section>

      {/* features */}
      <section id="features" className={`${WRAP} ${section} scroll-mt-12`}>
        <Eyebrow>{t("what it does", "cosa fa")}</Eyebrow>
        <H2>{t("Everything you want from a monitor.", "Tutto quello che chiedi a un monitor.")}</H2>
        <Lede>
          {t(
            "Checks from many places, status pages, alerts where you already are. Nothing to pay per check, nothing that phones home unless you ask.",
            "Controlli da tanti posti, status page, avvisi dove sei già. Niente da pagare per controllo, niente che chiami casa se non lo chiedi tu.",
          )}
        </Lede>
        <div className="mt-8">
          <Link href={docs} className={BTN}>
            {t("All of it in the docs →", "Tutto nella documentazione →")}
          </Link>
        </div>
        <div className="mt-10 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {features.map((f) => (
            <div key={f.title} className={`${card} p-4`}>
              <h3 className="font-bold">{f.title}</h3>
              <p className="mt-1.5 text-muted-foreground">{f.body}</p>
            </div>
          ))}
        </div>
      </section>

      {/* install */}
      <section id="install" className={`${WRAP} ${section} scroll-mt-12 border-t border-dashed`}>
        <div className="grid gap-12 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] lg:gap-16">
          <div className="min-w-0">
            <Eyebrow>{t("get started", "per iniziare")}</Eyebrow>
            <H2>{t("Up and running in two minutes.", "Operativo in due minuti.")}</H2>
            <Lede>
              {t(
                "The hub checks from wherever it runs. Add agents when you want more locations, and that's the whole setup.",
                "L'hub controlla dal posto in cui gira. Aggiungi agent quando vuoi altre località, e la configurazione è tutta qui.",
              )}
            </Lede>
            <ol className="mt-10 space-y-7">
              {steps.map((s, i) => (
                <li key={s.title} className="grid grid-cols-[2.25rem_minmax(0,1fr)]">
                  <span className="pt-px font-bold text-muted-foreground tabular-nums">{String(i + 1).padStart(2, "0")}</span>
                  <div className="min-w-0">
                    <h3 className="text-[14px] font-bold">{s.title}</h3>
                    <p className="mt-1 text-muted-foreground">{s.body}</p>
                    {s.cmd && (
                      <div className="mt-3">
                        <Cmd code={s.cmd} />
                      </div>
                    )}
                  </div>
                </li>
              ))}
            </ol>
          </div>
          <div className="min-w-0 lg:pt-2">
            <CodeCard
              title={t("docker-compose.yml = a hub", "docker-compose.yml = un hub")}
              code={compose}
              footer={
                <Link href={docHref(lang, "install")} className="font-semibold text-[#5fd38a] underline underline-offset-4">
                  {t("Configuration, databases, reverse proxy →", "Configurazione, database, reverse proxy →")}
                </Link>
              }
            />
          </div>
        </div>
      </section>

      {/* where */}
      <section className={band}>
        <div className={`${WRAP} ${section}`}>
          <Eyebrow>{t("where it runs", "dove gira")}</Eyebrow>
          <H2>{t("Pick where it lives.", "Scegli dove vive.")}</H2>
          <Lede>
            {t(
              "The software is the same everywhere. Choose the machine and how much setup you want.",
              "Il software è lo stesso ovunque. Scegli la macchina e quanta configurazione vuoi fare.",
            )}
          </Lede>
          <div className="mt-12 grid gap-8 lg:grid-cols-3 lg:gap-6">
            {places.map((p, i) => (
              <div
                key={p.title}
                className={`flex flex-col border border-edge p-6 shadow-[6px_6px_0_0_var(--color-hard)] ${p.tag ? "bg-band" : "bg-background"}`}
              >
                <div className="flex items-center justify-between gap-3 text-[12px] font-bold text-up">
                  <span>{String(i + 1).padStart(2, "0")}</span>
                  {p.tag && (
                    <span className="border border-up px-1.5 py-px text-[10px] uppercase tracking-widest">{p.tag}</span>
                  )}
                </div>
                <h3 className="mt-2 text-lg font-bold leading-snug">{p.title}</h3>
                <p className="mt-3 text-muted-foreground">{p.body}</p>
                <ul className="mt-4 space-y-2">
                  {p.bullets.map((b) => (
                    <li key={b} className="grid grid-cols-[1rem_minmax(0,1fr)]">
                      <span aria-hidden className="mt-[0.5em] size-1.5 bg-up" />
                      <span>{b}</span>
                    </li>
                  ))}
                </ul>
                <p className="mt-6 border-t border-dashed pt-4 font-bold">{p.note}</p>
                <div className="mt-auto pt-6">
                  <Link href={p.href} className={BTN}>
                    {p.cta} →
                  </Link>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* screenshots */}
      <section className={`${WRAP} ${section}`}>
        <Eyebrow>screenshots</Eyebrow>
        <H2>{t("See it before you run it.", "Guardalo prima di avviarlo.")}</H2>
        <div className="mt-12 grid gap-10 lg:grid-cols-2 [&>*]:min-w-0">
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
            caption={t("The dashboard, light or dark.", "La dashboard, chiara o scura.")}
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
          <div className="space-y-10">
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
        </div>
      </section>

      <UsageStats docsHref={docHref(lang, "administration", "usage-stats")} />

      {/* how it works */}
      <section className={band}>
        <div className={`${WRAP} ${section}`}>
          <Eyebrow>{t("how it works", "come funziona")}</Eyebrow>
          <H2>{t("A small stack, all readable.", "Uno stack piccolo, tutto leggibile.")}</H2>
          <div className="mt-12 grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
            {parts.map((p) => (
              <div key={p.title}>
                <h3 className="text-[14px] font-bold">{p.title}</h3>
                <p className="mt-2 text-muted-foreground">{p.body}</p>
              </div>
            ))}
          </div>
          <pre className={`${card} mt-12 overflow-x-auto p-4 text-[12px] leading-snug`}>{`            ┌──────────────── hub container ────────────────┐
 browser ──>│  Go http server ─ /api/*  ─ SQLite/PG/MariaDB │
            │        │          /        ─ web UI            │
            │   scheduler ──> local probe ("hub" location)  │
            │        │                                       │
            └────────┼───────────────────────────────────────┘
                     │ ${t("jobs / results over WebSocket (agent dials out)", "lavori / risultati via WebSocket (si collega l'agent)")}
          ┌──────────┴──────────┐
     ${t("agent (Frankfurt)     agent (home lab) ...", "agent (Francoforte)   agent (home lab) ...")}`}</pre>
        </div>
      </section>

      {/* project */}
      <section className={`${WRAP} ${section}`}>
        <div className={`${card} p-6 shadow-[8px_8px_0_0_var(--color-hard)] sm:p-10`}>
          <Eyebrow>{t("the project", "il progetto")}</Eyebrow>
          <H2>{t("Small, honest and open.", "Piccolo, onesto e aperto.")}</H2>
          <Lede>
            {t(
              "Pingtxt is in active development and licensed under AGPL-3.0. Issues and pull requests are welcome, and the docs live in the repository next to the code.",
              "Pingtxt è in sviluppo attivo e ha licenza AGPL-3.0. Issue e pull request sono benvenute, e la documentazione vive nel repository accanto al codice.",
            )}
          </Lede>
          <div className="mt-8 flex flex-wrap gap-3">
            <a href={REPO} className={BTN_PRIMARY}>
              {t("Read the source", "Leggi il sorgente")} ↗
            </a>
            <a href={`${REPO}/issues`} className={BTN}>
              {t("Open an issue", "Apri una issue")}
            </a>
            <a href={`${REPO}/blob/main/CONTRIBUTING.md`} className={BTN}>
              {t("Contributing", "Contribuire")}
            </a>
            <Link href={docs} className={BTN}>
              docs
            </Link>
          </div>
        </div>
      </section>
    </div>
  )
}
