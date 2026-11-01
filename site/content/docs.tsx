import type { Metadata } from "next"

import { CopyBlock } from "@/components/copy-block"
import { Prompt, REPO, SectionTitle } from "@/components/site-shell"
import { localePath, tx, type Lang } from "@/lib/i18n"
import { pageMeta } from "@/lib/seo"

// Every text is t(english, italiano). When you change one, change the other.

export function docsMeta(lang: Lang): Metadata {
  const t = tx(lang)
  return pageMeta({
    lang,
    title: t("docs: install and configure", "documentazione: installazione e configurazione"),
    description: t(
      "Install pingtxt with Docker, add monitoring locations, set up check types, status pages, alerts, single sign-on, the API, MCP and Prometheus.",
      "Installa pingtxt con Docker, aggiungi località di monitoraggio, configura tipi di controllo, status page, avvisi, single sign-on, API, MCP e Prometheus.",
    ),
    path: "/docs/",
  })
}

const RAW = "https://raw.githubusercontent.com/federico-it/pingtxt/main"

const toc: [string, string, string][] = [
  ["hub", "run the hub", "avviare l'hub"],
  ["database", "postgres / mariadb", "postgres / mariadb"],
  ["reverse-proxy", "reverse proxy", "reverse proxy"],
  ["agents", "add a location", "aggiungere una località"],
  ["agent-updates", "agent updates", "aggiornamento agent"],
  ["checks", "check types", "tipi di controllo"],
  ["phases", "response time per phase", "tempo di risposta per fase"],
  ["ipv6", "ipv6", "ipv6"],
  ["response-checks", "response checks (JSON)", "controlli sulla risposta (JSON)"],
  ["mcp-checks", "mcp servers", "server mcp"],
  ["heartbeat", "heartbeat monitors", "monitor heartbeat"],
  ["status-pages", "status pages", "status page"],
  ["maintenance", "maintenance", "manutenzione"],
  ["expiry", "expiry alerts", "avvisi di scadenza"],
  ["notifications", "notifications", "notifiche"],
  ["sso", "single sign-on", "single sign-on"],
  ["2fa", "two-factor auth", "due fattori"],
  ["api", "api tokens", "token api"],
  ["mcp", "mcp server", "server mcp dell'hub"],
  ["apps", "native apps", "app native"],
  ["prometheus", "prometheus", "prometheus"],
  ["upgrade", "upgrade + backup", "aggiornamento + backup"],
  ["usage-stats", "usage stats", "statistiche d'uso"],
  ["verify", "verify a release", "verificare una release"],
  ["build", "build from source", "compilare dai sorgenti"],
  ["contributing", "contributing", "contribuire"],
]

function C({ children }: { children: React.ReactNode }) {
  return <code className="bg-muted px-1 text-foreground">{children}</code>
}

function B({ children }: { children: React.ReactNode }) {
  return <b className="text-foreground">{children}</b>
}

function A({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} className="text-foreground underline underline-offset-4">
      {children}
    </a>
  )
}

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3 [&_li]:ml-4 [&_li]:list-['-_'] [&_p]:text-muted-foreground [&_ul]:space-y-1 [&_ul]:text-muted-foreground">
      <SectionTitle id={id}>{title}</SectionTitle>
      {children}
    </section>
  )
}

function Table({ head, rows }: { head: string[]; rows: React.ReactNode[][] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-[12px]">
        <thead>
          <tr className="border-b border-dashed text-[11px] uppercase tracking-wider text-muted-foreground">
            {head.map((h, i) => (
              <th key={i} className="px-2 py-1.5 font-normal">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-b border-dashed align-top">
              {r.map((c, j) => (
                <td key={j} className={j === 0 ? "px-2 py-1.5 whitespace-nowrap" : "px-2 py-1.5 text-muted-foreground"}>
                  {c}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function DocsPage({ lang }: { lang: Lang }) {
  const t = tx(lang)
  return (
    <div className="grid gap-10 py-12 lg:grid-cols-[180px_1fr]">
      <aside className="lg:sticky lg:top-20 lg:self-start">
        <div className="mb-2 text-[11px] uppercase tracking-wider text-muted-foreground">{t("contents", "indice")}</div>
        <nav className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
          {toc.map(([id, en, it]) => (
            <a key={id} href={`#${id}`} className="text-muted-foreground transition-colors hover:text-foreground">
              {t(en, it)}
            </a>
          ))}
        </nav>
      </aside>

      <div className="min-w-0 space-y-12">
        <div className="space-y-2">
          <Prompt>man pingtxt</Prompt>
          <h1 className="text-xl font-semibold">{t("pingtxt docs", "documentazione di pingtxt")}</h1>
          <p className="max-w-2xl text-muted-foreground">
            {t(
              <>
                Everything except two env vars is configured from the web UI and stored in the database. The{" "}
                <A href={`${REPO}#readme`}>README</A> on GitHub has the same content.
              </>,
              <>
                Tutto, tranne due variabili d&apos;ambiente, si configura dall&apos;interfaccia web e viene salvato nel
                database. Il <A href={`${REPO}#readme`}>README</A> su GitHub ha lo stesso contenuto, in inglese.
              </>,
            )}
          </p>
        </div>

        <Section id="hub" title={t("run the hub", "avviare l'hub")}>
          <CopyBlock
            label="docker"
            code={`docker run -d --name pingtxt --restart unless-stopped \\
  -p 8090:8090 -v ./data:/data \\
  ghcr.io/federico-it/pingtxt:latest`}
          />
          <p>
            {t(
              <>
                Or <C>docker compose up -d</C> with the{" "}
                <A href={`${REPO}/blob/main/docker-compose.yml`}>docker-compose.yml</A> in the repo. Open{" "}
                <C>http://localhost:8090</C>, create the admin account, add monitors. Binaries for Linux and macOS are on the{" "}
                <A href={`${REPO}/releases`}>releases page</A>.
              </>,
              <>
                Oppure <C>docker compose up -d</C> con il{" "}
                <A href={`${REPO}/blob/main/docker-compose.yml`}>docker-compose.yml</A> del repository. Apri{" "}
                <C>http://localhost:8090</C>, crea l&apos;account admin, aggiungi i monitor. I binari per Linux e macOS sono
                nella <A href={`${REPO}/releases`}>pagina delle release</A>.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                The images run as the unprivileged user <C>pingtxt</C> (uid 10001). At start the entrypoint gives{" "}
                <C>/data</C> to that user (volumes of older versions were written by root), lets it use a mounted Docker
                socket and keeps only <C>CAP_NET_RAW</C> for ping. Run CLI commands with{" "}
                <C>docker exec -u pingtxt pingtxt pingtxt ...</C>; to skip the root step, set{" "}
                <C>user: &quot;10001:10001&quot;</C> and <C>chown -R 10001:10001 ./data</C>.
              </>,
              <>
                Le immagini girano con l&apos;utente senza privilegi <C>pingtxt</C> (uid 10001). All&apos;avvio
                l&apos;entrypoint assegna <C>/data</C> a quell&apos;utente (i volumi delle versioni precedenti erano scritti da
                root), gli permette di usare un socket Docker montato e tiene solo <C>CAP_NET_RAW</C> per il ping. Esegui i
                comandi della CLI con <C>docker exec -u pingtxt pingtxt pingtxt ...</C>; per saltare il passaggio come root,
                imposta <C>user: &quot;10001:10001&quot;</C> e <C>chown -R 10001:10001 ./data</C>.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Public URL, built-in location, retention, defaults for new monitors, users and login live under{" "}
                <B>settings</B>. Only these are read at startup:
              </>,
              <>
                URL pubblico, località integrata, conservazione dei dati, valori predefiniti dei nuovi monitor, utenti e
                accesso stanno in <B>impostazioni</B>. All&apos;avvio vengono lette solo queste:
              </>,
            )}
          </p>
          <Table
            head={["env", t("default", "predefinito"), ""]}
            rows={[
              [<C key="a">PINGTXT_ADDR</C>, ":8090", t("listen address", "indirizzo di ascolto")],
              [<C key="d">PINGTXT_DATA</C>, "/data", t("SQLite database directory", "cartella del database SQLite")],
              [
                <C key="u">PINGTXT_DB_URL</C>,
                "",
                t("PostgreSQL or MariaDB/MySQL instead of SQLite", "PostgreSQL o MariaDB/MySQL al posto di SQLite"),
              ],
            ]}
          />
        </Section>

        <Section id="database" title={t("postgresql or mariadb", "postgresql o mariadb")}>
          <p>
            {t(
              <>
                SQLite is the default and needs nothing else. To keep the data in PostgreSQL (13+) or MariaDB (10.6+) / MySQL
                (8.0.13+), start the hub with <C>PINGTXT_DB_URL</C>. Tables are created and migrated at start. The compose
                files below start the hub and the database with one command; set the passwords in <C>.env</C> before the
                first start.
              </>,
              <>
                SQLite è il predefinito e non richiede altro. Per tenere i dati in PostgreSQL (13+) o MariaDB (10.6+) / MySQL
                (8.0.13+), avvia l&apos;hub con <C>PINGTXT_DB_URL</C>. Le tabelle vengono create e migrate all&apos;avvio. I
                file compose qui sotto avviano hub e database con un solo comando; imposta le password in <C>.env</C> prima
                del primo avvio.
              </>,
            )}
          </p>
          <CopyBlock
            label="postgresql"
            code={`curl -O ${RAW}/docker-compose.postgres.yml
echo "DB_PASSWORD=$(openssl rand -hex 16)" > .env
docker compose -f docker-compose.postgres.yml up -d`}
          />
          <CopyBlock
            label="mariadb"
            code={`curl -O ${RAW}/docker-compose.mariadb.yml
printf "DB_PASSWORD=%s\\nDB_ROOT_PASSWORD=%s\\n" $(openssl rand -hex 16) $(openssl rand -hex 16) > .env
docker compose -f docker-compose.mariadb.yml up -d`}
          />
          <p>
            {t(
              "With your own database server, create an empty database and a user, then:",
              "Con un tuo server di database, crea un database vuoto e un utente, poi:",
            )}
          </p>
          <CopyBlock
            label="env"
            code={`PINGTXT_DB_URL=postgres://pingtxt:secret@db.example.com:5432/pingtxt?sslmode=require
PINGTXT_DB_URL=mariadb://pingtxt:secret@db.example.com:3306/pingtxt   # ${t("mysql:// works too", "va bene anche mysql://")}`}
          />
          <p>
            {t(
              "Special characters in the password must be URL-encoded. History rollups, Prometheus metrics and the configuration export work the same on every database. The built-in backups in settings are SQLite files: with PostgreSQL or MariaDB back up the server with its own tools.",
              "I caratteri speciali nella password vanno codificati per URL. Aggregazione dello storico, metriche Prometheus ed export della configurazione funzionano allo stesso modo con ogni database. I backup integrati nelle impostazioni sono file SQLite: con PostgreSQL o MariaDB fai il backup del server con i suoi strumenti.",
            )}
          </p>
          <CopyBlock
            label={t("postgresql backup / restore", "postgresql backup / ripristino")}
            code={`docker exec pingtxt-postgres pg_dump -U pingtxt -Fc pingtxt > pingtxt.dump
docker stop pingtxt
docker exec -i pingtxt-postgres pg_restore -U pingtxt -d pingtxt --clean --if-exists < pingtxt.dump
docker start pingtxt`}
          />
          <CopyBlock
            label={t("mariadb backup / restore", "mariadb backup / ripristino")}
            code={`docker exec pingtxt-mariadb sh -c 'mariadb-dump -u pingtxt -p"$MARIADB_PASSWORD" --single-transaction pingtxt' > pingtxt.sql
docker stop pingtxt
docker exec -i pingtxt-mariadb sh -c 'mariadb -u pingtxt -p"$MARIADB_PASSWORD" pingtxt' < pingtxt.sql
docker start pingtxt`}
          />
          <p>
            {t(
              "To move an existing SQLite hub, export the configuration with secrets (settings → configuration export) and import it in the new hub. History stays behind.",
              "Per spostare un hub SQLite esistente, esporta la configurazione con i segreti (impostazioni → export della configurazione) e importala nel nuovo hub. Lo storico non viene spostato.",
            )}
          </p>
        </Section>

        <Section id="reverse-proxy" title="reverse proxy">
          <p>
            {t(
              <>
                Put the hub behind a reverse proxy with TLS (Caddy, Traefik, nginx) and set the <B>public URL</B> in
                settings, so login callbacks and agent commands use it. WebSocket upgrade must be allowed on{" "}
                <C>/api/agent/ws</C>.
              </>,
              <>
                Metti l&apos;hub dietro un reverse proxy con TLS (Caddy, Traefik, nginx) e imposta l&apos;<B>url pubblico</B>{" "}
                nelle impostazioni, così lo usano i callback di accesso e i comandi degli agent. L&apos;upgrade WebSocket deve
                essere permesso su <C>/api/agent/ws</C>.
              </>,
            )}
          </p>
          <CopyBlock label="Caddyfile" code={`status.example.com {\n  reverse_proxy 127.0.0.1:8090\n}`} />
        </Section>

        <Section id="agents" title={t("add a location (agent)", "aggiungere una località (agent)")}>
          <p>
            {t(
              <>
                In the UI go to <B>agents → add agent</B>: you get a token and ready-to-paste commands.
              </>,
              <>
                Nell&apos;interfaccia vai in <B>agent → aggiungi agent</B>: ricevi un token e i comandi pronti da incollare.
              </>,
            )}
          </p>
          <CopyBlock
            code={`# Docker
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \\
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \\
  -v pingtxt-agent:/data ghcr.io/federico-it/pingtxt-agent:latest

${t("# Linux + systemd (binary downloaded from your hub)", "# Linux + systemd (binario scaricato dal tuo hub)")}
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --hub https://status.example.com --token pt_xxx

${t("# uninstall", "# disinstallazione")}
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --uninstall`}
          />
          <p>
            {t(
              "Then tick the new location in any monitor. The hub schedules every check and sends it to the agents; agents are stateless, open no ports and reconnect with backoff.",
              "Poi spunta la nuova località nei monitor che vuoi. L'hub pianifica ogni controllo e lo manda agli agent; gli agent non hanno stato, non aprono porte e si ricollegano con backoff.",
            )}
          </p>
          <p>
            {t(
              "With several locations the monitor is down when more than half of them fail, and degraded past the threshold set in settings. Uptime follows that status, not single checks: one failing location among several does not lower it.",
              "Con più località il monitor è down quando ne fallisce più della metà, e degradato oltre la soglia impostata nelle impostazioni. L'uptime segue questo stato, non i singoli controlli: una sola località che fallisce tra tante non lo abbassa.",
            )}
          </p>
        </Section>

        <Section id="agent-updates" title={t("agent updates", "aggiornamento degli agent")}>
          <p>
            {t(
              "The hub bundles agent binaries built with its own version. Agents (installed with the script or in Docker) update themselves when they reconnect after a hub upgrade: they download the binary from the hub, verify its SHA-256 and version, swap it and restart. A failed update keeps the old binary running.",
              "L'hub include i binari degli agent compilati con la sua stessa versione. Gli agent (installati con lo script o in Docker) si aggiornano da soli quando si ricollegano dopo un aggiornamento dell'hub: scaricano il binario dall'hub, ne verificano SHA-256 e versione, lo sostituiscono e si riavviano. Se l'aggiornamento fallisce, resta in funzione il binario precedente.",
            )}
          </p>
          <ul>
            <li>
              {t(
                <>
                  <B>agents → update agents automatically</B> turns it on or off; outdated agents also get an update button.
                </>,
                <>
                  <B>agent → aggiorna gli agent automaticamente</B> lo attiva o disattiva; gli agent non aggiornati hanno
                  anche un pulsante per aggiornarli.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Keep an agent on its version with <C>-no-auto-update</C> (or <C>PINGTXT_NO_AUTO_UPDATE=1</C> in{" "}
                  <C>/etc/pingtxt-agent.env</C>).
                </>,
                <>
                  Per tenere un agent alla sua versione usa <C>-no-auto-update</C> (oppure <C>PINGTXT_NO_AUTO_UPDATE=1</C>{" "}
                  in <C>/etc/pingtxt-agent.env</C>).
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Agents update themselves only from an https hub (or plain http on a loopback or private IP address). To
                  allow it over plain http anyway, use <C>-insecure-update</C> (<C>PINGTXT_INSECURE_UPDATE=1</C>).
                </>,
                <>
                  Gli agent si aggiornano da soli solo da un hub in https (o in http semplice su un indirizzo IP di loopback o
                  privato). Per permetterlo comunque in http semplice, usa <C>-insecure-update</C> (
                  <C>PINGTXT_INSECURE_UPDATE=1</C>).
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Agents also check the hub&apos;s checksum against the <C>SHA256SUMS</C> of the GitHub release of that
                  version, so a tampered hub cannot push its own binary. <C>-verify-update</C> (
                  <C>PINGTXT_VERIFY_UPDATE</C>) is <C>auto</C> by default (skipped with a warning if GitHub is unreachable),{" "}
                  <C>strict</C> to install only matching published releases, or <C>off</C> for forks and private builds.
                </>,
                <>
                  Gli agent confrontano anche il checksum dell&apos;hub con il file <C>SHA256SUMS</C> della release GitHub di
                  quella versione, così un hub manomesso non può distribuire un suo binario. <C>-verify-update</C> (
                  <C>PINGTXT_VERIFY_UPDATE</C>) è <C>auto</C> per impostazione predefinita (saltato con un avviso se GitHub
                  non è raggiungibile), <C>strict</C> per installare solo release pubblicate che corrispondono, oppure{" "}
                  <C>off</C> per fork e build private.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Docker agents keep the new binary in the <C>/data</C> volume and run it from there after a restart, with no
                  Docker socket. Mount a named volume (<C>-v pingtxt-agent:/data</C>) to keep updates when the container is
                  recreated. Pulling a new image starts its own binary again and clears the volume.
                </>,
                <>
                  Gli agent in Docker tengono il nuovo binario nel volume <C>/data</C> e lo eseguono da lì dopo un riavvio,
                  senza socket Docker. Monta un volume con nome (<C>-v pingtxt-agent:/data</C>) per non perdere gli
                  aggiornamenti quando il container viene ricreato. Scaricando una nuova immagine riparte il suo binario e il
                  volume viene svuotato.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  To keep containers immutable instead, set <C>PINGTXT_NO_AUTO_UPDATE=1</C> and let an image updater pull{" "}
                  <C>:latest</C> (What&apos;s Up Docker or the maintained Watchtower fork). It needs the Docker socket, and the
                  agent then follows the latest release rather than the hub&apos;s version.
                </>,
                <>
                  Se invece vuoi container immutabili, imposta <C>PINGTXT_NO_AUTO_UPDATE=1</C> e lascia che un aggiornatore di
                  immagini scarichi <C>:latest</C> (What&apos;s Up Docker o il fork mantenuto di Watchtower). Serve il socket
                  Docker, e l&apos;agent segue così l&apos;ultima release invece della versione dell&apos;hub.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Docker tags: <C>:latest</C> is the newest release, <C>:1.2.3</C> a given release, <C>:main</C> the latest
                  build of the main branch.
                </>,
                <>
                  Tag Docker: <C>:latest</C> è la release più recente, <C>:1.2.3</C> una release precisa, <C>:main</C>{" "}
                  l&apos;ultima build del branch main.
                </>,
              )}
            </li>
          </ul>
        </Section>

        <Section id="checks" title={t("check types", "tipi di controllo")}>
          <Table
            head={[t("type", "tipo"), t("target", "destinazione"), t("what is checked", "cosa viene controllato")]}
            rows={[
              [
                "http(s)",
                <C key="h">https://example.com</C>,
                t(
                  "status codes, headers, body, JSON rules, TLS expiry",
                  "codici di stato, header, body, regole JSON, scadenza TLS",
                ),
              ],
              [
                t("keyword", "parola chiave"),
                t("url + text", "url + testo"),
                t(
                  "the response body contains (or not) a keyword",
                  "il body della risposta contiene (o non contiene) una parola chiave",
                ),
              ],
              [
                t("tcp port", "porta tcp"),
                <C key="t">host:port</C>,
                t("connection opens (IPv6: [2001:db8::1]:443)", "la connessione si apre (IPv6: [2001:db8::1]:443)"),
              ],
              [
                "ping",
                t("host or IP", "host o IP"),
                t("ICMP echo or ICMPv6 (agents need NET_RAW)", "echo ICMP o ICMPv6 (gli agent richiedono NET_RAW)"),
              ],
              [
                "dns",
                t("name + record type", "nome + tipo di record"),
                t("resolves, optional expected value", "si risolve, valore atteso facoltativo"),
              ],
              [
                "postgresql",
                <C key="p">postgres://user:pass@host:5432/db</C>,
                t(
                  "connect + query (default SELECT 1), optional expected value",
                  "connessione + query (predefinita SELECT 1), valore atteso facoltativo",
                ),
              ],
              ["mysql / mariadb", <C key="m">mysql://user:pass@host:3306/db</C>, t("same as postgresql", "come postgresql")],
              ["redis", <C key="r">redis://:pass@host:6379/0</C>, t("AUTH + PING, rediss:// for TLS", "AUTH + PING, rediss:// per TLS")],
              [
                "mongodb",
                <C key="mo">mongodb://user:pass@host:27017/</C>,
                t("hello, auth, ping; shows primary/secondary", "hello, auth, ping; mostra primary/secondary"),
              ],
              [
                t("docker container", "container docker"),
                t("name or id", "nome o id"),
                t(
                  "down when not running, restarting, paused or unhealthy",
                  "down se non è in esecuzione, si sta riavviando, è in pausa o unhealthy",
                ),
              ],
              [
                "grpc health",
                <C key="g">grpc://host:port[/service]</C>,
                t("grpc.health.v1 Check returns SERVING", "grpc.health.v1 Check risponde SERVING"),
              ],
              [
                "smtp",
                <C key="s">smtp://host:587</C>,
                t("greeting, EHLO, STARTTLS when offered", "saluto, EHLO, STARTTLS quando disponibile"),
              ],
              [
                t("mcp server", "server mcp"),
                <C key="mc">https://host/mcp</C>,
                t(
                  "initialize handshake, optional tools/list checks",
                  "handshake initialize, controlli facoltativi su tools/list",
                ),
              ],
              [
                "heartbeat",
                t("push URL", "URL di push"),
                t("your job calls the URL; down when it stops", "il tuo job chiama l'URL; down quando smette"),
              ],
            ]}
          />
          <p>
            {t(
              <>
                Passwords in connection URLs are hidden in the list and in notifications; use a read-only user. Docker checks
                need <C>/var/run/docker.sock</C> on the hub or agent that runs them (add{" "}
                <C>-v /var/run/docker.sock:/var/run/docker.sock:ro</C>).
              </>,
              <>
                Le password negli URL di connessione sono nascoste nell&apos;elenco e nelle notifiche; usa un utente in sola
                lettura. I controlli Docker richiedono <C>/var/run/docker.sock</C> sull&apos;hub o sull&apos;agent che li
                esegue (aggiungi <C>-v /var/run/docker.sock:/var/run/docker.sock:ro</C>).
              </>,
            )}
          </p>
        </Section>

        <Section id="phases" title={t("response time per phase", "tempo di risposta per fase")}>
          <p>
            {t(
              <>
                HTTP and keyword checks measure where the time goes: <B>DNS</B> lookup, <B>connect</B>, <B>TLS</B>{" "}
                handshake, <B>wait</B> for the first byte (TTFB, the server at work) and <B>transfer</B> of the body. On the
                monitor page switch the response time chart to <B>by phase</B>, per location or all together; hover a latency
                in the log for the breakdown of that check. With redirects the phases of every hop are added up. Agents
                measure phases from this version on: update older ones to see their data.
              </>,
              <>
                I controlli HTTP e con parola chiave misurano dove va il tempo: risoluzione <B>DNS</B>, <B>connessione</B>,
                handshake <B>TLS</B>, <B>attesa</B> del primo byte (TTFB, il server al lavoro) e <B>trasferimento</B> del
                body. Nella pagina del monitor passa il grafico dei tempi di risposta a <B>per fase</B>, per località o tutte
                insieme; passa sopra una latenza nel log per vedere il dettaglio di quel controllo. Con i redirect le fasi di
                ogni passaggio vengono sommate. Gli agent misurano le fasi da questa versione in poi: aggiorna quelli più
                vecchi per vederne i dati.
              </>,
            )}
          </p>
        </Section>

        <Section id="ipv6" title="ipv6">
          <p>
            {t(
              <>
                Every check type works over IPv6. Hosts with only AAAA records just work; write IPv6 addresses in brackets
                when a port follows (<C>[2001:db8::1]:443</C>, <C>https://[2001:db8::1]/</C>,{" "}
                <C>redis://[2001:db8::1]:6379</C>). Ping and dns take the bare address.
              </>,
              <>
                Ogni tipo di controllo funziona su IPv6. Gli host con soli record AAAA funzionano senza fare nulla; scrivi gli
                indirizzi IPv6 tra parentesi quadre quando segue una porta (<C>[2001:db8::1]:443</C>,{" "}
                <C>https://[2001:db8::1]/</C>, <C>redis://[2001:db8::1]:6379</C>). Ping e dns vogliono l&apos;indirizzo
                senza parentesi.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                <B>ip version</B> on a dual-stack host: <i>IPv4 or IPv6</i> opens one connection with whichever family
                answers first, so a broken IPv6 goes unnoticed; <i>IPv4 only</i> or <i>IPv6 only</i> use one family;{" "}
                <i>IPv4 and IPv6</i> checks both and fails when either fails. The default is set in settings and each monitor
                can override it (API <C>ip_family</C>: 0 default, 1, 4, 6, 46). Agents older than this version cannot do it
                and report the check as failed: update them.
              </>,
              <>
                <B>versione ip</B> su un host dual-stack: <i>IPv4 o IPv6</i> apre una connessione con la famiglia che
                risponde per prima, quindi un IPv6 rotto passa inosservato; <i>solo IPv4</i> o <i>solo IPv6</i> usano una
                sola famiglia; <i>IPv4 e IPv6</i> le controlla entrambe e fallisce se una delle due fallisce. Il valore
                predefinito si imposta nelle impostazioni e ogni monitor può cambiarlo (API <C>ip_family</C>: 0
                predefinito, 1, 4, 6, 46). Gli agent più vecchi di questa versione non lo supportano e segnalano il controllo
                come fallito: aggiornali.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                The hub and agents check whether they reach IPv6 at start and every 10 minutes (agents page and monitor form
                show it). A location without IPv6 cannot be chosen for <i>IPv6 only</i> or <i>IPv4 and IPv6</i>;{" "}
                <i>IPv4 or IPv6</i> and the instance default check over IPv4 there. <C>PINGTXT_IPV6=on</C> or <C>off</C>{" "}
                skips the probe.
              </>,
              <>
                Hub e agent verificano se raggiungono IPv6 all&apos;avvio e ogni 10 minuti (lo mostrano la pagina degli agent
                e il modulo del monitor). Una località senza IPv6 non si può scegliere per <i>solo IPv6</i> o{" "}
                <i>IPv4 e IPv6</i>; lì <i>IPv4 o IPv6</i> e il predefinito dell&apos;istanza controllano su IPv4.{" "}
                <C>PINGTXT_IPV6=on</C> o <C>off</C> salta la verifica.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                The hub listens on IPv4 and IPv6 (<C>:8090</C>). Docker containers get no IPv6 by default, so checks to IPv6
                targets fail from a container until IPv6 is enabled on its network (Docker 27 or later, and IPv6 on the
                server):
              </>,
              <>
                L&apos;hub ascolta su IPv4 e IPv6 (<C>:8090</C>). I container Docker non hanno IPv6 per impostazione
                predefinita, quindi i controlli verso destinazioni IPv6 falliscono da un container finché IPv6 non viene
                attivato sulla sua rete (Docker 27 o successivo, e IPv6 sul server):
              </>,
            )}
          </p>
          <CopyBlock label="docker-compose.yml" code={`networks:\n  default:\n    enable_ipv6: true`} />
          <p>
            {t(
              <>
                With <C>docker run</C>: <C>docker network create --ipv6 pingtxt</C> and <C>--network pingtxt</C>, or{" "}
                <C>--network host</C>.
              </>,
              <>
                Con <C>docker run</C>: <C>docker network create --ipv6 pingtxt</C> e <C>--network pingtxt</C>, oppure{" "}
                <C>--network host</C>.
              </>,
            )}
          </p>
        </Section>

        <Section id="response-checks" title={t("response checks (JSON)", "controlli sulla risposta (JSON)")}>
          <p>
            {t(
              <>
                HTTP and keyword monitors can check the JSON body of the response, e.g. a health endpoint that returns{" "}
                <C>{`{"status": "ready", "checks": {"redis": {"status": "ok", "latencyMs": 3}}}`}</C>. Add rules under{" "}
                <B>response checks</B> in the monitor form: all must pass, otherwise the check fails and the result, the
                incident and the notification say which rule broke, e.g.{" "}
                <C>checks.redis.status: expected &quot;ok&quot;, got &quot;error&quot;</C>.
              </>,
              <>
                I monitor HTTP e con parola chiave possono controllare il body JSON della risposta, per esempio un endpoint di
                health che restituisce{" "}
                <C>{`{"status": "ready", "checks": {"redis": {"status": "ok", "latencyMs": 3}}}`}</C>. Aggiungi le regole in{" "}
                <B>controlli sulla risposta</B> nel modulo del monitor: devono passare tutte, altrimenti il controllo
                fallisce e il risultato, l&apos;incidente e la notifica dicono quale regola non è rispettata, per esempio{" "}
                <C>checks.redis.status: expected &quot;ok&quot;, got &quot;error&quot;</C>.
              </>,
            )}
          </p>
          <Table
            head={[t("path", "percorso"), t("operator", "operatore"), t("value", "valore")]}
            rows={[
              [<C key="1">status</C>, "equals", <C key="1v">ready</C>],
              [<C key="2">checks.redis.status</C>, "equals", <C key="2v">ok</C>],
              [<C key="3">checks.worker.ageMs</C>, "<", <C key="3v">60000</C>],
              [<C key="4">items[0].id</C>, "exists", ""],
            ]}
          />
          <ul>
            <li>
              {t(
                <>
                  Paths use dots and brackets: <C>a.b</C>, <C>items[0]</C> (or <C>items.0</C>, <C>items[-1]</C> for the last
                  one), <C>{`["key.with.dots"]`}</C>. A leading <C>$.</C> is allowed.
                </>,
                <>
                  I percorsi usano punti e parentesi quadre: <C>a.b</C>, <C>items[0]</C> (o <C>items.0</C>,{" "}
                  <C>items[-1]</C> per l&apos;ultimo), <C>{`["key.with.dots"]`}</C>. È ammesso un <C>$.</C> iniziale.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Operators: equals, not equals, contains (substring, array element or object key), not contains, exists, not
                  exists, &gt;, &gt;=, &lt;, &lt;= (numbers) and matches (regular expression).
                </>,
                <>
                  Operatori: uguale, diverso, contiene (sottostringa, elemento di un array o chiave di un oggetto), non
                  contiene, esiste, non esiste, &gt;, &gt;=, &lt;, &lt;= (numeri) e corrisponde (espressione regolare).
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Values are compared by type: numbers numerically (<C>3</C> equals <C>3.0</C>), <C>true</C>/<C>false</C>/
                  <C>null</C> by name, objects and arrays as JSON.
                </>,
                <>
                  I valori si confrontano per tipo: i numeri come numeri (<C>3</C> è uguale a <C>3.0</C>),{" "}
                  <C>true</C>/<C>false</C>/<C>null</C> per nome, oggetti e array come JSON.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  Rules run on agents too. Agents older than this feature report the check as failed with &quot;update
                  it&quot; instead of passing it unchecked.
                </>,
                <>
                  Le regole funzionano anche sugli agent. Gli agent più vecchi di questa funzione segnalano il controllo come
                  fallito chiedendo di aggiornarli, invece di farlo passare senza verificarlo.
                </>,
              )}
            </li>
          </ul>
        </Section>

        <Section id="mcp-checks" title={t("mcp servers", "server mcp")}>
          <p>
            {t(
              <>
                An <B>mcp server</B> monitor checks a remote Model Context Protocol server: it runs the <C>initialize</C>{" "}
                handshake and, when the server has tools, lists them with <C>tools/list</C>. The result shows the server name
                and version, the negotiated protocol version, the transport and the number of tools.
              </>,
              <>
                Un monitor <B>server mcp</B> controlla un server Model Context Protocol remoto: esegue l&apos;handshake{" "}
                <C>initialize</C> e, se il server ha dei tool, li elenca con <C>tools/list</C>. Il risultato mostra nome e
                versione del server, la versione del protocollo negoziata, il trasporto e il numero di tool.
              </>,
            )}
          </p>
          <ul>
            <li>
              {t(
                <>
                  <B>target</B>: the URL of the endpoint, e.g. <C>https://mcp.example.com/mcp</C>.
                </>,
                <>
                  <B>destinazione</B>: l&apos;URL dell&apos;endpoint, per esempio <C>https://mcp.example.com/mcp</C>.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <B>transport</B>: <C>auto</C> tries streamable HTTP and falls back to the legacy HTTP+SSE transport
                  (2024-11-05) when the POST is rejected; pick <C>streamable</C> or <C>sse</C> to force one.
                </>,
                <>
                  <B>trasporto</B>: <C>auto</C> prova lo streamable HTTP e ripiega sul vecchio trasporto HTTP+SSE
                  (2024-11-05) quando la POST viene rifiutata; scegli <C>streamable</C> o <C>sse</C> per forzarne uno.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <B>headers</B>: sent with every request, e.g. <C>Authorization: Bearer &lt;token&gt;</C>. A 401 or 403
                  makes the monitor go down. Read-only users don&apos;t see the values.
                </>,
                <>
                  <B>header</B>: inviati con ogni richiesta, per esempio <C>Authorization: Bearer &lt;token&gt;</C>. Un 401 o
                  un 403 manda il monitor down. Gli utenti in sola lettura non vedono i valori.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <B>required tools</B> (comma-separated) and <B>minimum tools</B>: the monitor goes down when a tool
                  disappears or the server lists fewer tools.
                </>,
                <>
                  <B>tool richiesti</B> (separati da virgola) e <B>tool minimi</B>: il monitor va down quando un tool sparisce
                  o il server ne elenca meno.
                </>,
              )}
            </li>
          </ul>
          <p>
            {t(
              <>
                Tools are never called. https endpoints get TLS certificate expiry alerts like http monitors. Agents older
                than this version report <C>unknown monitor type: mcp</C>: update them.
              </>,
              <>
                I tool non vengono mai chiamati. Gli endpoint https ricevono gli avvisi di scadenza del certificato TLS come
                i monitor http. Gli agent più vecchi di questa versione segnalano <C>unknown monitor type: mcp</C>:
                aggiornali.
              </>,
            )}
          </p>
        </Section>

        <Section id="heartbeat" title={t("heartbeat monitors (cron jobs)", "monitor heartbeat (cron job)")}>
          <p>
            {t(
              <>
                A <B>heartbeat (push)</B> monitor gets a secret URL. Call it at the end of every run, with any HTTP method:
              </>,
              <>
                Un monitor <B>heartbeat (push)</B> riceve un URL segreto. Chiamalo alla fine di ogni esecuzione, con
                qualsiasi metodo HTTP:
              </>,
            )}
          </p>
          <CopyBlock
            code={`0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 https://status.example.com/api/push/<token> >/dev/null`}
          />
          <p>
            {t(
              <>
                It goes down when no call arrives within the expected period plus the grace time. Optional query parameters:{" "}
                <C>status=down</C>, <C>msg=</C>, <C>ping=</C> (run time in ms).
              </>,
              <>
                Va down quando non arriva nessuna chiamata entro il periodo atteso più la tolleranza. Parametri facoltativi
                nella query: <C>status=down</C>, <C>msg=</C>, <C>ping=</C> (durata dell&apos;esecuzione in ms).
              </>,
            )}
          </p>
        </Section>

        <Section id="status-pages" title={t("status pages", "status page")}>
          <p>
            {t(
              <>
                Create them under <B>status</B>: pick monitors or whole groups, their order, and whether past incidents are
                shown. A page is public at <C>https://&lt;hub&gt;/status/&lt;slug&gt;/</C>. Visitors see names, status, uptime
                and incident times, never targets, locations or error messages.
              </>,
              <>
                Si creano in <B>status</B>: scegli monitor o interi gruppi, il loro ordine e se mostrare gli incidenti
                passati. Una pagina è pubblica su <C>https://&lt;hub&gt;/status/&lt;slug&gt;/</C>. I visitatori vedono nomi,
                stato, uptime e orari degli incidenti, mai destinazioni, località o messaggi di errore.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                For a custom domain (e.g. <C>status.example.com</C>), set it on the page, point its DNS at the hub and route
                the host to pingtxt in your reverse proxy. On that host only the status page is served.
              </>,
              <>
                Per un dominio personalizzato (per esempio <C>status.example.com</C>), impostalo sulla pagina, punta il suo
                DNS all&apos;hub e instrada l&apos;host verso pingtxt nel reverse proxy. Su quell&apos;host viene servita solo
                la status page.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                To keep a page private, turn on <i>protect with a password</i>. Visitors then see a password form; the right
                password opens the page for 30 days in that browser, together with its badges and subscription form. The
                password is stored hashed, wrong attempts are rate limited, and changing it logs every visitor out. Logged-in
                users never need it.
              </>,
              <>
                Per tenere privata una pagina, attiva <i>proteggi con una password</i>. I visitatori vedono allora un modulo
                per la password; quella giusta apre la pagina per 30 giorni in quel browser, insieme ai suoi badge e al modulo
                di iscrizione. La password è salvata come hash, i tentativi sbagliati sono limitati e cambiarla disconnette
                tutti i visitatori. Agli utenti con accesso non serve mai.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Post <B>incidents</B> on a page with a timeline of written updates (investigating, identified, monitoring,
                resolved). With an email channel picked under <i>email subscriptions</i>, visitors can subscribe: they confirm
                through a link, then get every update by email, each with an unsubscribe link.
              </>,
              <>
                Pubblica <B>incidenti</B> su una pagina con una cronologia di aggiornamenti scritti (stiamo indagando, causa
                individuata, sotto osservazione, risolto). Scegliendo un canale email in <i>iscrizioni via email</i>, i
                visitatori possono iscriversi: confermano con un link, poi ricevono ogni aggiornamento via email, ognuno con
                un link per disiscriversi.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                <B>README badges</B>: every published page has SVG badges for its overall state and for each monitor it shows
                (state, uptime, response time). Copy them from the monitor page or the status page editor:
              </>,
              <>
                <B>Badge per il README</B>: ogni pagina pubblicata ha badge SVG per il suo stato generale e per ogni monitor
                che mostra (stato, uptime, tempo di risposta). Copiali dalla pagina del monitor o dall&apos;editor della status
                page:
              </>,
            )}
          </p>
          <CopyBlock
            code={`[![uptime](https://status.example.com/api/badge/acme/12/uptime.svg?period=7d)](https://status.example.com/status/acme/)`}
          />
          <p>
            {t(
              <>
                Kinds: <C>status</C>, <C>uptime</C>, <C>response</C>; <C>period=</C> <C>24h</C>, <C>7d</C>, <C>30d</C> or{" "}
                <C>90d</C>; optional <C>label=</C> and <C>style=flat-square</C>. Drafts and monitors not on the page have no
                badge.
              </>,
              <>
                Tipi: <C>status</C>, <C>uptime</C>, <C>response</C>; <C>period=</C> <C>24h</C>, <C>7d</C>, <C>30d</C> o{" "}
                <C>90d</C>; facoltativi <C>label=</C> e <C>style=flat-square</C>. Le bozze e i monitor che non sono nella
                pagina non hanno badge.
              </>,
            )}
          </p>
        </Section>

        <Section id="maintenance" title={t("maintenance windows", "finestre di manutenzione")}>
          <p>
            {t(
              "One-off or recurring (daily, weekly, monthly, in any time zone), for some or all monitors, groups or tags. Checks keep running but no incidents or alerts are raised, and those checks don't count toward uptime.",
              "Singole o ricorrenti (ogni giorno, settimana o mese, in qualsiasi fuso orario), per alcuni o tutti i monitor, gruppi o tag. I controlli continuano, ma non si aprono incidenti né partono avvisi, e quei controlli non contano per l'uptime.",
            )}
          </p>
        </Section>

        <Section id="expiry" title={t("expiry alerts", "avvisi di scadenza")}>
          <p>
            {t(
              "TLS certificates of https monitors and domain registrations (RDAP, WHOIS fallback for registries such as .it). Thresholds in days are set in settings (default 14/7/3/1 for certificates, 30/14/7/1 for domains): one alert per threshold crossed, one on expiry, one on renewal.",
              "Certificati TLS dei monitor https e registrazioni dei domini (RDAP, con WHOIS come riserva per registri come il .it). Le soglie in giorni si impostano nelle impostazioni (predefinite 14/7/3/1 per i certificati, 30/14/7/1 per i domini): un avviso per ogni soglia superata, uno alla scadenza, uno al rinnovo.",
            )}
          </p>
        </Section>

        <Section id="notifications" title={t("notifications", "notifiche")}>
          <p>
            {t(
              <>
                Webhook (JSON), Discord, Slack, Telegram, ntfy and email (SMTP with STARTTLS/TLS, or the Resend API with an
                API key from a verified domain). Add them under{" "}
                <B>notify</B>, test them before saving, and attach them to monitors.
              </>,
              <>
                Webhook (JSON), Discord, Slack, Telegram, ntfy ed email (SMTP con STARTTLS/TLS, oppure l&apos;API di Resend
                con una API key di un dominio verificato). Aggiungili in{" "}
                <B>notifiche</B>, provali prima di salvare e collegali ai monitor.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                <B>Periodic reports</B>, on the same page, email the uptime, the incidents and the downtime of the last week
                (every Monday) or month (on the 1st) per monitor group, compared with the period before. Pick the email
                channels, optionally some groups, the hour and the time zone; preview a report or send it now before turning
                it on.
              </>,
              <>
                I <B>report periodici</B>, nella stessa pagina, mandano via email uptime, incidenti e tempi di inattività
                dell&apos;ultima settimana (ogni lunedì) o dell&apos;ultimo mese (il giorno 1) per gruppo di monitor, a
                confronto con il periodo precedente. Scegli i canali email, eventualmente alcuni gruppi, l&apos;ora e il fuso
                orario; guarda l&apos;anteprima di un report o invialo subito prima di attivarlo.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Each channel has a <B>scope</B>: all monitors (also future ones), or the monitors of some groups and tags,
                or only the monitors linked one by one; and the <B>events</B> it gets: down, up, degraded (off by default),
                certificate and domain expiry. Link a monitor one by one, from the channel or from the monitor form, to add
                it, change its events or leave it out of an all-monitors channel. Existing channels keep getting everything
                they got before.
              </>,
              <>
                Ogni canale ha un <B>ambito</B>: tutti i monitor (anche quelli futuri), i monitor di alcuni gruppi e tag,
                oppure solo i monitor collegati uno per uno; e gli <B>eventi</B> che riceve: down, up, degradato (spento di
                default), scadenza del certificato e del dominio. Collega un monitor uno per uno, dal canale o dal form del
                monitor, per aggiungerlo, cambiarne gli eventi o escluderlo da un canale su tutti i monitor. I canali già
                esistenti continuano a ricevere tutto quello che ricevevano prima.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Escalation and reminders are set per channel. <B>send after</B> N minutes alerts the channel only if the
                monitor is still down by then (an on-call phone, a manager), so short outages never reach it.{" "}
                <B>repeat every</B> N minutes sends a STILL DOWN reminder while the monitor stays down (at least 5). Webhooks
                carry <C>reminder</C> and <C>down_since</C>. Nothing is sent during maintenance windows; due alerts go out
                when the window ends.
              </>,
              <>
                Escalation e promemoria si impostano per canale. <B>invia dopo</B> N minuti avvisa il canale solo se a quel
                punto il monitor è ancora down (un telefono di reperibilità, un responsabile), così i disservizi brevi non lo
                raggiungono mai. <B>ripeti ogni</B> N minuti manda un promemoria STILL DOWN finché il monitor resta down
                (almeno 5). I webhook includono <C>reminder</C> e <C>down_since</C>. Durante le finestre di manutenzione non
                viene inviato nulla; gli avvisi in sospeso partono quando la finestra finisce.
              </>,
            )}
          </p>
        </Section>

        <Section id="sso" title="single sign-on">
          <p>
            {t(
              <>
                In <B>settings → authentication</B> turn on GitHub, Google or generic OpenID Connect (Authentik, Keycloak,
                Authelia, Pocket ID, Zitadel, Okta, Entra ID...), paste the client id and secret and register the callback
                URL shown there.
              </>,
              <>
                In <B>impostazioni → autenticazione</B> attiva GitHub, Google o un OpenID Connect generico (Authentik,
                Keycloak, Authelia, Pocket ID, Zitadel, Okta, Entra ID...), incolla client id e secret e registra
                l&apos;URL di callback mostrato lì.
              </>,
            )}
          </p>
          <ul>
            <li>
              {t(
                "Logins are matched to existing users by verified email. New accounts can be allowed, optionally by email domain.",
                "Gli accessi vengono associati agli utenti esistenti tramite l'email verificata. Si possono consentire nuovi account, anche solo per alcuni domini email.",
              )}
            </li>
            <li>
              {t(
                <>
                  Locked out after turning off password login? Run{" "}
                  <C>docker exec -u pingtxt pingtxt pingtxt -enable-password-login</C> and restart.
                </>,
                <>
                  Rimasto fuori dopo aver disattivato l&apos;accesso con password? Esegui{" "}
                  <C>docker exec -u pingtxt pingtxt pingtxt -enable-password-login</C> e riavvia.
                </>,
              )}
            </li>
          </ul>
        </Section>

        <Section id="2fa" title={t("two-factor authentication", "autenticazione a due fattori")}>
          <p>
            {t(
              <>
                In <B>settings → two-factor authentication</B> scan the QR code with any authenticator app. You get 10
                single-use recovery codes. To reset a user: <B>settings → users → reset 2fa</B>, or{" "}
                <C>docker exec -u pingtxt pingtxt pingtxt -reset-2fa you@example.com</C>.
              </>,
              <>
                In <B>impostazioni → autenticazione a due fattori</B> inquadra il codice QR con una qualsiasi app di
                autenticazione. Ricevi 10 codici di recupero monouso. Per azzerarla a un utente:{" "}
                <B>impostazioni → utenti → azzera 2fa</B>, oppure{" "}
                <C>docker exec -u pingtxt pingtxt pingtxt -reset-2fa you@example.com</C>.
              </>,
            )}
          </p>
        </Section>

        <Section id="api" title={t("api tokens", "token api")}>
          <p>
            {t(
              <>
                Create personal tokens in <B>settings → api tokens</B>, read only or read + write, with an optional expiry.
                They are shown once and stored hashed.
              </>,
              <>
                Crea token personali in <B>impostazioni → token api</B>, in sola lettura o lettura + scrittura, con una
                scadenza facoltativa. Vengono mostrati una sola volta e salvati come hash.
              </>,
            )}
          </p>
          <CopyBlock code={`curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors`} />
          <p>
            {t(
              <>
                <C>GET /api/monitors</C> accepts <C>?group=&lt;id&gt;</C> and <C>?tag=&lt;id&gt;</C>. Tokens never reach
                account endpoints (password, 2FA, tokens, users, instance settings).
              </>,
              <>
                <C>GET /api/monitors</C> accetta <C>?group=&lt;id&gt;</C> e <C>?tag=&lt;id&gt;</C>. I token non arrivano mai
                agli endpoint dell&apos;account (password, 2FA, token, utenti, impostazioni dell&apos;istanza).
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Every endpoint, with parameters and examples, is in the <A href={localePath(lang, "/docs/api/")}>API reference</A>
                . Each hub also serves the OpenAPI 3.1 document at <C>/api/openapi.json</C> and an interactive Swagger UI at{" "}
                <C>/api/docs</C> (linked from <B>settings → api tokens</B>).
              </>,
              <>
                Tutti gli endpoint, con parametri ed esempi, sono nel <A href={localePath(lang, "/docs/api/")}>riferimento API</A>
                . Ogni hub serve anche il documento OpenAPI 3.1 su <C>/api/openapi.json</C> e una Swagger UI interattiva su{" "}
                <C>/api/docs</C> (con un link da <B>impostazioni → token api</B>).
              </>,
            )}
          </p>
        </Section>

        <Section id="mcp" title={t("mcp server", "server mcp dell'hub")}>
          <p>
            {t(
              <>
                The hub is an <A href="https://modelcontextprotocol.io">MCP</A> server at <C>/mcp</C> (Streamable HTTP): Claude
                and other AI assistants can tell you what is down, read incidents and response times, and create monitors or
                maintenance windows. Roles and hidden secrets are the same as the API.
              </>,
              <>
                L&apos;hub è un server <A href="https://modelcontextprotocol.io">MCP</A> su <C>/mcp</C> (Streamable HTTP):
                Claude e altri assistenti AI possono dirti cosa è down, leggere incidenti e tempi di risposta, e creare
                monitor o finestre di manutenzione. Ruoli e segreti nascosti sono gli stessi dell&apos;API.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                <B>OAuth 2.1</B>, nothing to copy: add <C>https://status.example.com/mcp</C> as a custom connector in
                claude.ai or Claude Desktop (or <C>claude mcp add --transport http pingtxt https://status.example.com/mcp</C>{" "}
                then <C>/mcp</C> in Claude Code). You log in to pingtxt in the browser and choose read only or read + write
                (admins only). Discovery, dynamic client registration, PKCE and rotating refresh tokens are supported; access
                tokens last an hour and work on <C>/mcp</C> and the API. Connected apps can be disconnected in{" "}
                <B>settings → api tokens</B>. Behind a proxy, set the public URL in settings.
              </>,
              <>
                <B>OAuth 2.1</B>, niente da copiare: aggiungi <C>https://status.example.com/mcp</C> come connettore
                personalizzato in claude.ai o Claude Desktop (oppure{" "}
                <C>claude mcp add --transport http pingtxt https://status.example.com/mcp</C> e poi <C>/mcp</C> in Claude
                Code). Accedi a pingtxt nel browser e scegli sola lettura o lettura + scrittura (solo admin). Sono supportati
                discovery, registrazione dinamica dei client, PKCE e refresh token a rotazione; gli access token durano
                un&apos;ora e valgono su <C>/mcp</C> e sull&apos;API. Le app collegate si possono scollegare in{" "}
                <B>impostazioni → token api</B>. Dietro un proxy, imposta l&apos;URL pubblico nelle impostazioni.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                <B>API token</B>, for clients without OAuth: read tokens get the read tools, a read + write token of an admin
                also gets the tools that change data.
              </>,
              <>
                <B>Token API</B>, per i client senza OAuth: i token di lettura hanno i tool di lettura, un token lettura +
                scrittura di un admin ha anche i tool che modificano i dati.
              </>,
            )}
          </p>
          <CopyBlock
            label="claude code"
            code={`claude mcp add --transport http pingtxt https://status.example.com/mcp \\
  --header "Authorization: Bearer ptk_..."`}
          />
          <CopyBlock
            label="claude desktop · claude_desktop_config.json"
            code={`{
  "mcpServers": {
    "pingtxt": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://status.example.com/mcp",
               "--header", "Authorization:\${PINGTXT_AUTH}"],
      "env": { "PINGTXT_AUTH": "Bearer ptk_..." }
    }
  }
}`}
          />
          <CopyBlock
            label={t("cursor, windsurf and other url + headers clients", "cursor, windsurf e altri client con url + header")}
            code={`{
  "mcpServers": {
    "pingtxt": {
      "url": "https://status.example.com/mcp",
      "headers": { "Authorization": "Bearer ptk_..." }
    }
  }
}`}
          />
          <Table
            head={["tool", t("does", "cosa fa")]}
            rows={[
              [
                <C key="1">get_overview</C>,
                t("monitors by status, agents online, open incidents", "monitor per stato, agent online, incidenti aperti"),
              ],
              [
                <C key="2">list_monitors</C>,
                t(
                  "status, uptime, response time; filter by status, group, tag, name",
                  "stato, uptime, tempo di risposta; filtri per stato, gruppo, tag, nome",
                ),
              ],
              [
                <C key="3">get_monitor</C>,
                t(
                  "settings, uptime 24h/7d/30d, status per location, expiry",
                  "impostazioni, uptime 24h/7g/30g, stato per località, scadenze",
                ),
              ],
              [<C key="4">get_monitor_results</C>, t("latest check results", "ultimi risultati dei controlli")],
              [<C key="5">list_incidents</C>, t("incidents, open only or per monitor", "incidenti, solo aperti o per monitor")],
              [
                <C key="6">list_maintenances</C>,
                t("maintenance windows and next occurrence", "finestre di manutenzione e prossima occorrenza"),
              ],
              [
                <C key="7">list_groups · list_tags · list_agents · list_status_pages</C>,
                t("ids for everything else", "gli id per tutto il resto"),
              ],
              [<C key="8">create / update / pause / delete_monitor</C>, t("admin write token", "token di scrittura di un admin")],
              [
                <C key="9">create / delete_maintenance</C>,
                t("admin write token, ISO 8601 times", "token di scrittura di un admin, orari ISO 8601"),
              ],
            ]}
          />
        </Section>

        <Section id="apps" title={t("native apps", "app native")}>
          <p>
            {t(
              <>
                The iOS app signs in with OAuth 2.1 (authorization code + PKCE) on the hub&apos;s own login page, so passwords,
                2FA, GitHub, Google and OpenID Connect all work. It is the built-in public client <C>pingtxt-ios</C> with
                redirect URI <C>pingtxt://oauth</C>: no secret and no registration. Admins choose read only or read + write on
                the consent page, read-only users always get read only.
              </>,
              <>
                L&apos;app iOS accede con OAuth 2.1 (authorization code + PKCE) sulla pagina di accesso dell&apos;hub, quindi
                funzionano password, 2FA, GitHub, Google e OpenID Connect. È il client pubblico integrato{" "}
                <C>pingtxt-ios</C> con redirect URI <C>pingtxt://oauth</C>: nessun secret e nessuna registrazione. Gli admin
                scelgono sola lettura o lettura + scrittura nella pagina di consenso, gli utenti in sola lettura ricevono
                sempre la sola lettura.
              </>,
            )}
          </p>
          <Table
            head={[t("step", "passo"), t("request", "richiesta")]}
            rows={[
              [
                t("sign in", "accesso"),
                <span key="1" className="break-all">
                  <C>
                    GET
                    /oauth/authorize?response_type=code&amp;client_id=pingtxt-ios&amp;redirect_uri=pingtxt://oauth&amp;scope=read%20write&amp;state=…&amp;code_challenge=…&amp;code_challenge_method=S256
                  </C>
                </span>,
              ],
              [
                t("get tokens", "ottenere i token"),
                <span key="2" className="break-all">
                  <C>
                    POST /oauth/token
                    grant_type=authorization_code&amp;code=…&amp;client_id=pingtxt-ios&amp;redirect_uri=pingtxt://oauth&amp;code_verifier=…
                  </C>
                </span>,
              ],
              [
                t("call the api", "chiamare l'api"),
                <span key="3" className="break-all">
                  <C>Authorization: Bearer pma_…</C>
                </span>,
              ],
              [
                t("refresh", "rinnovo"),
                <span key="4" className="break-all">
                  <C>POST /oauth/token grant_type=refresh_token&amp;refresh_token=pmr_…&amp;client_id=pingtxt-ios</C>
                </span>,
              ],
              [
                t("sign out", "uscita"),
                <span key="5" className="break-all">
                  <C>POST /oauth/revoke token=pmr_…&amp;client_id=pingtxt-ios</C>
                </span>,
              ],
            ]}
          />
          <p>
            {t(
              <>
                Access tokens last an hour and work on the whole API like an API token of the same scope (never on account
                endpoints). Refresh tokens last 30 days from their last use and rotate: every refresh returns a new one, and
                using an old one again ends the sign-in. Each sign-in is listed in{" "}
                <B>settings → api tokens → connected apps</B>, where it can be disconnected.
              </>,
              <>
                Gli access token durano un&apos;ora e valgono su tutta l&apos;API come un token API con lo stesso scope (mai
                sugli endpoint dell&apos;account). I refresh token durano 30 giorni dall&apos;ultimo uso e ruotano: ogni rinnovo
                ne restituisce uno nuovo, e riusarne uno vecchio chiude l&apos;accesso. Ogni accesso è elencato in{" "}
                <B>impostazioni → token api → app collegate</B>, da dove si può scollegare.
              </>,
            )}
          </p>
        </Section>

        <Section id="prometheus" title="prometheus">
          <p>
            {t(
              <>
                <C>GET /metrics</C> needs a read token, unless <B>settings → prometheus metrics → public</B> is on. Output is
                cached for 10 seconds.
              </>,
              <>
                <C>GET /metrics</C> richiede un token di lettura, a meno che non sia attivo{" "}
                <B>impostazioni → metriche prometheus → pubblico</B>. L&apos;output resta in cache per 10 secondi.
              </>,
            )}
          </p>
          <CopyBlock
            label="prometheus.yml"
            code={`scrape_configs:
  - job_name: pingtxt
    scheme: https
    authorization:
      credentials: ptk_...
    static_configs:
      - targets: ["status.example.com"]`}
          />
          <Table
            head={[t("metric", "metrica"), t("meaning", "significato")]}
            rows={[
              [<C key="1">pingtxt_monitor_up</C>, t("1 up or degraded, 0 down", "1 up o degradato, 0 down")],
              [
                <C key="2">pingtxt_monitor_uptime_ratio</C>,
                t("uptime per window (24h, 7d, 30d)", "uptime per intervallo (24h, 7d, 30d)"),
              ],
              [<C key="3">pingtxt_check_latency_seconds</C>, t("last check per location", "ultimo controllo per località")],
              [
                <C key="3p">pingtxt_check_phase_seconds</C>,
                t(
                  "DNS, connect, TLS and first byte of the last HTTP check",
                  "DNS, connessione, TLS e primo byte dell'ultimo controllo HTTP",
                ),
              ],
              [
                <C key="4">pingtxt_cert_expiry_days</C>,
                t("days before the certificate expires", "giorni alla scadenza del certificato"),
              ],
              [<C key="5">pingtxt_domain_expiry_days</C>, t("days before the domain expires", "giorni alla scadenza del dominio")],
              [<C key="6">pingtxt_agent_connected</C>, t("agent connectivity", "connessione degli agent")],
            ]}
          />
        </Section>

        <Section id="upgrade" title={t("upgrade + backup", "aggiornamento + backup")}>
          <CopyBlock code={`docker compose pull && docker compose up -d`} />
          <p>
            {t(
              <>
                Database migrations run on start. With SQLite all state is one file, <C>/data/pingtxt.db</C>: settings →
                database backups takes copies while the hub runs, on demand or on a schedule, and restores them. With
                PostgreSQL or MariaDB use <A href="#database">pg_dump or mariadb-dump</A>. Agents installed with the script
                follow the hub version on their own.
              </>,
              <>
                Le migrazioni del database partono all&apos;avvio. Con SQLite tutto lo stato è un solo file,{" "}
                <C>/data/pingtxt.db</C>: impostazioni → backup del database ne fa copie mentre l&apos;hub è in funzione, su
                richiesta o a orari pianificati, e le ripristina. Con PostgreSQL o MariaDB usa{" "}
                <A href="#database">pg_dump o mariadb-dump</A>. Gli agent installati con lo script seguono da soli la
                versione dell&apos;hub.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                While the hub restarts or updates, browsers show a <B>pingtxt is updating</B> page that follows the migrations
                and reloads by itself when the hub is ready, and <C>/api/health</C> answers 503. Open tabs wait on the same
                screen instead of failing. When the container is fully down (pulling the new image), browsers that opened
                pingtxt before over https still show it from their cache; the first visit ever gets the reverse proxy&apos;s
                error until the hub listens again.
              </>,
              <>
                Mentre l&apos;hub si riavvia o si aggiorna, i browser mostrano una pagina <B>pingtxt si sta aggiornando</B>{" "}
                che segue le migrazioni e si ricarica da sola quando l&apos;hub è pronto, e <C>/api/health</C> risponde 503.
                Le schede aperte aspettano sulla stessa schermata invece di dare errore. Quando il container è del tutto
                fermo (mentre scarica la nuova immagine), i browser che avevano già aperto pingtxt in https la mostrano dalla
                cache; la primissima visita riceve l&apos;errore del reverse proxy finché l&apos;hub non torna in ascolto.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Release builds ask GitHub for the latest release every 12 hours and show admins a notice when a newer one is
                out. Turn it off in <B>settings → updates</B>, or start the hub with{" "}
                <C>PINGTXT_DISABLE_UPDATE_CHECK=true</C> so it never contacts GitHub.
              </>,
              <>
                Le build di release chiedono a GitHub l&apos;ultima release ogni 12 ore e mostrano un avviso agli admin quando
                ne esce una più recente. Disattivalo in <B>impostazioni → aggiornamenti</B>, oppure avvia l&apos;hub con{" "}
                <C>PINGTXT_DISABLE_UPDATE_CHECK=true</C> perché non contatti mai GitHub.
              </>,
            )}
          </p>
        </Section>

        <Section id="usage-stats" title={t("usage stats", "statistiche d'uso")}>
          <p>
            {t(
              <>
                After installing, admins are asked once whether the hub may send anonymous usage stats. Nothing is sent until
                someone says yes, and the answer can be changed any time in <B>settings → usage stats</B>, which also shows
                the exact next report.
              </>,
              <>
                Dopo l&apos;installazione, agli admin viene chiesto una volta se l&apos;hub può inviare statistiche d&apos;uso
                anonime. Non viene inviato nulla finché qualcuno non dice sì, e la risposta si può cambiare in qualsiasi
                momento in <B>impostazioni → statistiche d&apos;uso</B>, che mostra anche il prossimo report esatto.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                Once an hour the hub sends to <C>telemetry.pingtxt.com</C> only this:
              </>,
              <>
                Una volta all&apos;ora l&apos;hub invia a <C>telemetry.pingtxt.com</C> solo questo:
              </>,
            )}
          </p>
          <ul>
            <li>
              <C>id</C>:{" "}
              {t(
                "a random id made the first time stats are turned on. It never changes and says nothing about the hub.",
                "un id casuale creato la prima volta che si attivano le statistiche. Non cambia mai e non dice nulla dell'hub.",
              )}
            </li>
            <li>
              <C>version</C>: {t("the pingtxt version.", "la versione di pingtxt.")}
            </li>
            <li>
              <C>monitors</C>: {t("how many monitors are not paused.", "quanti monitor non sono in pausa.")}
            </li>
            <li>
              <C>agents</C>: {t("how many agents are connected.", "quanti agent sono collegati.")}
            </li>
          </ul>
          <p>
            {t(
              <>
                No names, targets, addresses, results or emails, and the service does not store IP addresses. Start the hub
                with <C>PINGTXT_DISABLE_TELEMETRY=true</C> to turn the stats off for good and hide the question. Development
                builds never send.
              </>,
              <>
                Nessun nome, destinazione, indirizzo, risultato o email, e il servizio non salva indirizzi IP. Avvia l&apos;hub
                con <C>PINGTXT_DISABLE_TELEMETRY=true</C> per disattivare le statistiche per sempre e nascondere la domanda.
                Le build di sviluppo non inviano mai nulla.
              </>,
            )}
          </p>
          <p>
            {t(
              <>
                The totals are public at <A href="https://telemetry.pingtxt.com/">telemetry.pingtxt.com</A> (JSON at{" "}
                <C>/v1/stats</C>): live hubs, monitors and agents, versions in use and 90 days of history. Never a single
                hub&apos;s report.
              </>,
              <>
                I totali sono pubblici su <A href="https://telemetry.pingtxt.com/">telemetry.pingtxt.com</A> (JSON su{" "}
                <C>/v1/stats</C>): hub, monitor e agent attivi, versioni in uso e 90 giorni di storico. Mai il report di un
                singolo hub.
              </>,
            )}
          </p>
        </Section>

        <Section id="verify" title={t("verify a release", "verificare una release")}>
          <p>
            {t(
              <>
                Releases after v0.1.0 are signed with <A href="https://docs.sigstore.dev/">cosign</A> in keyless mode, tied to
                the GitHub workflow that built them. Each release ships <C>SHA256SUMS</C> and its signature{" "}
                <C>SHA256SUMS.sigstore.json</C>; images on ghcr.io are signed by digest.
              </>,
              <>
                Le release successive alla v0.1.0 sono firmate con <A href="https://docs.sigstore.dev/">cosign</A> in
                modalità keyless, legate al workflow GitHub che le ha compilate. Ogni release include <C>SHA256SUMS</C> e la
                sua firma <C>SHA256SUMS.sigstore.json</C>; le immagini su ghcr.io sono firmate per digest.
              </>,
            )}
          </p>
          <CopyBlock
            code={`V=v0.2.0
base=https://github.com/federico-it/pingtxt/releases/download/$V
curl -fsSLO $base/SHA256SUMS -O $base/SHA256SUMS.sigstore.json -O $base/pingtxt-agent-linux-amd64
cosign verify-blob SHA256SUMS --bundle SHA256SUMS.sigstore.json \\
  --certificate-identity "https://github.com/federico-it/pingtxt/.github/workflows/release.yml@refs/tags/$V" \\
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --ignore-missing -c SHA256SUMS`}
          />
          <CopyBlock
            code={`cosign verify ghcr.io/federico-it/pingtxt:\${V#v} \\
  --certificate-identity "https://github.com/federico-it/pingtxt/.github/workflows/docker.yml@refs/tags/$V" \\
  --certificate-oidc-issuer https://token.actions.githubusercontent.com`}
          />
        </Section>

        <Section id="build" title={t("build from source", "compilare dai sorgenti")}>
          <p>
            {t(
              <>
                Needs <B>Go 1.26+</B>, <B>Node.js 22+</B> with npm, <C>make</C> and <C>git</C>. No C compiler (everything
                builds with <C>CGO_ENABLED=0</C>).
              </>,
              <>
                Servono <B>Go 1.26+</B>, <B>Node.js 22+</B> con npm, <C>make</C> e <C>git</C>. Nessun compilatore C (tutto si
                compila con <C>CGO_ENABLED=0</C>).
              </>,
            )}
          </p>
          <CopyBlock
            code={`git clone https://github.com/federico-it/pingtxt.git && cd pingtxt
git checkout v1.2.3        # ${t("optional: a release tag", "facoltativo: il tag di una release")}
make web hub agent agents`}
          />
          <Table
            head={["target", "output", ""]}
            rows={[
              [
                <C key="w">make web</C>,
                <C key="w2">internal/ui/dist</C>,
                t(
                  "the web UI, embedded in the hub: run it before make hub",
                  "l'interfaccia web, inclusa nell'hub: eseguilo prima di make hub",
                ),
              ],
              [
                <C key="h">make hub</C>,
                <C key="h2">dist/pingtxt</C>,
                t("the hub for this machine, UI included", "l'hub per questa macchina, interfaccia inclusa"),
              ],
              [<C key="a">make agent</C>, <C key="a2">dist/pingtxt-agent</C>, t("the agent for this machine", "l'agent per questa macchina")],
              [
                <C key="as">make agents</C>,
                <C key="as2">dist/agents/</C>,
                t(
                  "agents for linux amd64/arm64/arm and darwin, served by the hub to the install script and for updates",
                  "agent per linux amd64/arm64/arm e darwin, serviti dall'hub allo script di installazione e per gli aggiornamenti",
                ),
              ],
            ]}
          />
          <p>
            {t(
              <>
                The version comes from <C>git describe</C> (override with <C>VERSION=v1.2.3</C>). Build hub and agents from
                the same checkout: agents update themselves to the hub version.
              </>,
              <>
                La versione viene da <C>git describe</C> (si cambia con <C>VERSION=v1.2.3</C>). Compila hub e agent dallo
                stesso checkout: gli agent si aggiornano alla versione dell&apos;hub.
              </>,
            )}
          </p>
          <CopyBlock
            label={t("run", "avvio")}
            code={`./dist/pingtxt     # ${t("data in ./data, agents from ./dist/agents, :8090", "dati in ./data, agent da ./dist/agents, :8090")}
PINGTXT_DATA=/var/lib/pingtxt PINGTXT_AGENT_DIR=/usr/local/share/pingtxt/agents /usr/local/bin/pingtxt`}
          />
          <p>
            {t(
              <>
                Ping checks use a raw socket with <C>CAP_NET_RAW</C>, otherwise an unprivileged ICMP socket (allowed by{" "}
                <C>net.ipv4.ping_group_range</C>). Outside Docker: <C>sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt</C>{" "}
                (and the agent); <C>PINGTXT_UNPRIVILEGED_PING=1</C> or <C>0</C> forces one of the two. A systemd unit example
                is in the <A href={`${REPO}#build-from-source`}>README</A>.
              </>,
              <>
                I controlli ping usano un raw socket con <C>CAP_NET_RAW</C>, altrimenti un socket ICMP senza privilegi
                (permesso da <C>net.ipv4.ping_group_range</C>). Fuori da Docker:{" "}
                <C>sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt</C> (e per l&apos;agent);{" "}
                <C>PINGTXT_UNPRIVILEGED_PING=1</C> o <C>0</C> forza uno dei due. Un esempio di unit systemd è nel{" "}
                <A href={`${REPO}#build-from-source`}>README</A>.
              </>,
            )}
          </p>
          <CopyBlock
            label={t("cross-compile", "cross-compilazione")}
            code={`V=$(git describe --tags --always)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/pingtxt-linux-arm64 ./cmd/pingtxt
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/pingtxt-agent-linux-armv7 ./cmd/pingtxt-agent`}
          />
          <CopyBlock
            label={t("docker images (only docker needed)", "immagini docker (serve solo docker)")}
            code={`make docker     # pingtxt:dev ${t("and", "e")} pingtxt-agent:dev
docker build --build-arg VERSION=$(git describe --tags --always) -t pingtxt:dev .
docker build --build-arg VERSION=$(git describe --tags --always) -f Dockerfile.agent -t pingtxt-agent:dev .
docker buildx build --platform linux/amd64,linux/arm64 -t registry.example.com/pingtxt:dev --push .`}
          />
          <p>
            {t(
              <>
                To run a compose file with your own build, add <C>docker-compose.build.yml</C> after it. Same for SQLite,
                PostgreSQL and MariaDB (create the <C>.env</C> with the passwords first):
              </>,
              <>
                Per avviare un file compose con la tua build, aggiungi <C>docker-compose.build.yml</C> dopo di esso. Vale per
                SQLite, PostgreSQL e MariaDB (prima crea il <C>.env</C> con le password):
              </>,
            )}
          </p>
          <CopyBlock
            code={`docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.postgres.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.mariadb.yml -f docker-compose.build.yml up -d --build`}
          />
        </Section>

        <Section id="contributing" title={t("contributing", "contribuire")}>
          <p>
            {t(
              <>
                pingtxt is free software under the <A href={`${REPO}/blob/main/LICENSE`}>AGPL-3.0 license</A>: use it,
                self-host it and change it freely; if you distribute a modified version or run one as a service for others,
                publish its source under the same license. Bug reports, fixes and new check types or notifiers are welcome.
              </>,
              <>
                pingtxt è software libero con <A href={`${REPO}/blob/main/LICENSE`}>licenza AGPL-3.0</A>: usalo, ospitalo
                sui tuoi server e modificalo liberamente; se distribuisci una versione modificata o la offri come servizio ad
                altri, pubblicane i sorgenti con la stessa licenza. Segnalazioni di bug, correzioni e nuovi tipi di controllo
                o di notifica sono benvenuti.
              </>,
            )}
          </p>
          <ul>
            <li>
              {t(
                <>
                  <B>bugs</B>: search the <A href={`${REPO}/issues`}>issues</A> first, then open a{" "}
                  <A href={`${REPO}/issues/new?template=bug_report.yml`}>bug report</A> with the version (shown in settings),
                  how you run it, steps and logs.
                </>,
                <>
                  <B>bug</B>: cerca prima tra le <A href={`${REPO}/issues`}>issue</A>, poi apri un{" "}
                  <A href={`${REPO}/issues/new?template=bug_report.yml`}>bug report</A> con la versione (la trovi nelle
                  impostazioni), come lo esegui, i passi per riprodurlo e i log.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <B>ideas</B>: open a <A href={`${REPO}/issues/new?template=feature_request.yml`}>feature request</A>. For
                  bigger changes, agree on the approach in an issue before writing code.
                </>,
                <>
                  <B>idee</B>: apri una <A href={`${REPO}/issues/new?template=feature_request.yml`}>feature request</A>. Per
                  le modifiche più grandi, concorda l&apos;approccio in una issue prima di scrivere codice.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <B>security</B>: never in a public issue.{" "}
                  <A href={`${REPO}/security/advisories/new`}>Report it privately</A>, see{" "}
                  <A href={`${REPO}/blob/main/SECURITY.md`}>SECURITY.md</A>.
                </>,
                <>
                  <B>sicurezza</B>: mai in una issue pubblica.{" "}
                  <A href={`${REPO}/security/advisories/new`}>Segnalalo in privato</A>, vedi{" "}
                  <A href={`${REPO}/blob/main/SECURITY.md`}>SECURITY.md</A>.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <B>pull requests</B>: fork, branch from <C>main</C>, run <C>make test lint</C>, open the PR against{" "}
                  <C>main</C>. It is merged by a maintainer once the <C>go</C> and <C>web</C> checks pass and the review is
                  approved.
                </>,
                <>
                  <B>pull request</B>: fai un fork, crea un branch da <C>main</C>, esegui <C>make test lint</C>, apri la PR
                  verso <C>main</C>. La unisce un maintainer quando i controlli <C>go</C> e <C>web</C> passano e la review è
                  approvata.
                </>,
              )}
            </li>
          </ul>
          <CopyBlock
            label={t("dev setup", "ambiente di sviluppo")}
            code={`git clone https://github.com/<you>/pingtxt.git && cd pingtxt
(cd web && npm ci)
make dev-hub   # ${t("hub on :8090", "hub su :8090")}
make dev-web   # ${t("UI on :3000, proxies /api", "interfaccia su :3000, inoltra /api")}`}
          />
          <p>
            {t(
              <>
                Everything else, commit style and code style included, is in{" "}
                <A href={`${REPO}/blob/main/CONTRIBUTING.md`}>CONTRIBUTING.md</A>. Everyone taking part follows the{" "}
                <A href={`${REPO}/blob/main/CODE_OF_CONDUCT.md`}>code of conduct</A>.
              </>,
              <>
                Tutto il resto, stile dei commit e del codice compreso, è in{" "}
                <A href={`${REPO}/blob/main/CONTRIBUTING.md`}>CONTRIBUTING.md</A> (in inglese). Chi partecipa segue il{" "}
                <A href={`${REPO}/blob/main/CODE_OF_CONDUCT.md`}>codice di condotta</A>.
              </>,
            )}
          </p>
        </Section>
      </div>
    </div>
  )
}
