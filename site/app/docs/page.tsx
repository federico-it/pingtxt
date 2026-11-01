import type { Metadata } from "next"

import { CopyBlock } from "@/components/copy-block"
import { Prompt, REPO, SectionTitle } from "@/components/site-shell"
import { pageMeta } from "@/lib/seo"

export const metadata: Metadata = pageMeta({
  title: "docs: install and configure",
  description:
    "Install pingtxt with Docker, add monitoring locations, set up check types, status pages, alerts, single sign-on, the API, MCP and Prometheus.",
  path: "/docs/",
})

const RAW = "https://raw.githubusercontent.com/federico-it/pingtxt/main"

const toc = [
  ["hub", "run the hub"],
  ["database", "postgres / mariadb"],
  ["reverse-proxy", "reverse proxy"],
  ["agents", "add a location"],
  ["agent-updates", "agent updates"],
  ["checks", "check types"],
  ["phases", "response time per phase"],
  ["ipv6", "ipv6"],
  ["response-checks", "response checks (JSON)"],
  ["mcp-checks", "mcp servers"],
  ["heartbeat", "heartbeat monitors"],
  ["status-pages", "status pages"],
  ["maintenance", "maintenance"],
  ["expiry", "expiry alerts"],
  ["notifications", "notifications"],
  ["sso", "single sign-on"],
  ["2fa", "two-factor auth"],
  ["api", "api tokens"],
  ["mcp", "mcp server"],
  ["apps", "native apps"],
  ["prometheus", "prometheus"],
  ["upgrade", "upgrade + backup"],
  ["usage-stats", "usage stats"],
  ["verify", "verify a release"],
  ["build", "build from source"],
  ["contributing", "contributing"],
] as const

function C({ children }: { children: React.ReactNode }) {
  return <code className="bg-muted px-1 text-foreground">{children}</code>
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
            {head.map((h) => (
              <th key={h} className="px-2 py-1.5 font-normal">
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

export default function Docs() {
  return (
    <div className="grid gap-10 py-12 lg:grid-cols-[180px_1fr]">
      <aside className="lg:sticky lg:top-20 lg:self-start">
        <div className="mb-2 text-[11px] uppercase tracking-wider text-muted-foreground">contents</div>
        <nav className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
          {toc.map(([id, label]) => (
            <a key={id} href={`#${id}`} className="text-muted-foreground transition-colors hover:text-foreground">
              {label}
            </a>
          ))}
        </nav>
      </aside>

      <div className="min-w-0 space-y-12">
        <div className="space-y-2">
          <Prompt>man pingtxt</Prompt>
          <h1 className="text-xl font-semibold">pingtxt docs</h1>
          <p className="max-w-2xl text-muted-foreground">
            Everything except two env vars is configured from the web UI and stored in the database. The{" "}
            <a href={`${REPO}#readme`} className="text-foreground underline underline-offset-4">
              README
            </a>{" "}
            on GitHub has the same content.
          </p>
        </div>

        <Section id="hub" title="run the hub">
          <CopyBlock
            label="docker"
            code={`docker run -d --name pingtxt --restart unless-stopped \\
  -p 8090:8090 -v ./data:/data \\
  ghcr.io/federico-it/pingtxt:latest`}
          />
          <p>
            Or <C>docker compose up -d</C> with the{" "}
            <a href={`${REPO}/blob/main/docker-compose.yml`} className="text-foreground underline underline-offset-4">
              docker-compose.yml
            </a>{" "}
            in the repo. Open <C>http://localhost:8090</C>, create the admin account, add monitors. Binaries for Linux and
            macOS are on the{" "}
            <a href={`${REPO}/releases`} className="text-foreground underline underline-offset-4">
              releases page
            </a>
            .
          </p>
          <p>
            The images run as the unprivileged user <C>pingtxt</C> (uid 10001). At start the entrypoint gives{" "}
            <C>/data</C> to that user (volumes of older versions were written by root), lets it use a mounted Docker
            socket and keeps only <C>CAP_NET_RAW</C> for ping. Run CLI commands with{" "}
            <C>docker exec -u pingtxt pingtxt pingtxt ...</C>; to skip the root step, set{" "}
            <C>user: &quot;10001:10001&quot;</C> and <C>chown -R 10001:10001 ./data</C>.
          </p>
          <p>
            Public URL, built-in location, retention, defaults for new monitors, users and login live under{" "}
            <b className="text-foreground">settings</b>. Only these are read at startup:
          </p>
          <Table
            head={["env", "default", ""]}
            rows={[
              [<C key="a">PINGTXT_ADDR</C>, ":8090", "listen address"],
              [<C key="d">PINGTXT_DATA</C>, "/data", "SQLite database directory"],
              [<C key="u">PINGTXT_DB_URL</C>, "", "PostgreSQL or MariaDB/MySQL instead of SQLite"],
            ]}
          />
        </Section>

        <Section id="database" title="postgresql or mariadb">
          <p>
            SQLite is the default and needs nothing else. To keep the data in PostgreSQL (13+) or MariaDB (10.6+) / MySQL
            (8.0.13+), start the hub with <C>PINGTXT_DB_URL</C>. Tables are created and migrated at start. The compose files
            below start the hub and the database with one command; set the passwords in <C>.env</C> before the first start.
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
          <p>With your own database server, create an empty database and a user, then:</p>
          <CopyBlock
            label="env"
            code={`PINGTXT_DB_URL=postgres://pingtxt:secret@db.example.com:5432/pingtxt?sslmode=require
PINGTXT_DB_URL=mariadb://pingtxt:secret@db.example.com:3306/pingtxt   # mysql:// works too`}
          />
          <p>
            Special characters in the password must be URL-encoded. History rollups, Prometheus metrics and the
            configuration export work the same on every database. The built-in backups in settings are SQLite files: with
            PostgreSQL or MariaDB back up the server with its own tools.
          </p>
          <CopyBlock
            label="postgresql backup / restore"
            code={`docker exec pingtxt-postgres pg_dump -U pingtxt -Fc pingtxt > pingtxt.dump
docker stop pingtxt
docker exec -i pingtxt-postgres pg_restore -U pingtxt -d pingtxt --clean --if-exists < pingtxt.dump
docker start pingtxt`}
          />
          <CopyBlock
            label="mariadb backup / restore"
            code={`docker exec pingtxt-mariadb sh -c 'mariadb-dump -u pingtxt -p"$MARIADB_PASSWORD" --single-transaction pingtxt' > pingtxt.sql
docker stop pingtxt
docker exec -i pingtxt-mariadb sh -c 'mariadb -u pingtxt -p"$MARIADB_PASSWORD" pingtxt' < pingtxt.sql
docker start pingtxt`}
          />
          <p>
            To move an existing SQLite hub, export the configuration with secrets (settings → configuration export) and
            import it in the new hub. History stays behind.
          </p>
        </Section>

        <Section id="reverse-proxy" title="reverse proxy">
          <p>
            Put the hub behind a reverse proxy with TLS (Caddy, Traefik, nginx) and set the <b className="text-foreground">public URL</b>{" "}
            in settings, so login callbacks and agent commands use it. WebSocket upgrade must be allowed on{" "}
            <C>/api/agent/ws</C>.
          </p>
          <CopyBlock label="Caddyfile" code={`status.example.com {\n  reverse_proxy 127.0.0.1:8090\n}`} />
        </Section>

        <Section id="agents" title="add a location (agent)">
          <p>
            In the UI go to <b className="text-foreground">agents → add agent</b>: you get a token and ready-to-paste commands.
          </p>
          <CopyBlock
            code={`# Docker
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \\
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \\
  -v pingtxt-agent:/data ghcr.io/federico-it/pingtxt-agent:latest

# Linux + systemd (binary downloaded from your hub)
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --hub https://status.example.com --token pt_xxx

# uninstall
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --uninstall`}
          />
          <p>
            Then tick the new location in any monitor. The hub schedules every check and sends it to the agents; agents are
            stateless, open no ports and reconnect with backoff.
          </p>
          <p>
            With several locations the monitor is down when more than half of them fail, and degraded past the threshold set
            in settings. Uptime follows that status, not single checks: one failing location among several does not lower it.
          </p>
        </Section>

        <Section id="agent-updates" title="agent updates">
          <p>
            The hub bundles agent binaries built with its own version. Agents (installed with the script or in Docker) update
            themselves when they reconnect after a hub upgrade: they download the binary from the hub, verify its SHA-256 and version, swap it
            and restart. A failed update keeps the old binary running.
          </p>
          <ul>
            <li>
              <b className="text-foreground">agents → update agents automatically</b> turns it on or off; outdated agents also get
              an update button.
            </li>
            <li>
              Keep an agent on its version with <C>-no-auto-update</C> (or <C>PINGTXT_NO_AUTO_UPDATE=1</C> in{" "}
              <C>/etc/pingtxt-agent.env</C>).
            </li>
            <li>
              Agents update themselves only from an https hub (or plain http on a loopback or private IP address).
              To allow it over plain http anyway, use <C>-insecure-update</C> (<C>PINGTXT_INSECURE_UPDATE=1</C>).
            </li>
            <li>
              Agents also check the hub&apos;s checksum against the <C>SHA256SUMS</C> of the GitHub release of that version, so a
              tampered hub cannot push its own binary. <C>-verify-update</C> (<C>PINGTXT_VERIFY_UPDATE</C>) is{" "}
              <C>auto</C> by default (skipped with a warning if GitHub is unreachable), <C>strict</C> to install only matching
              published releases, or <C>off</C> for forks and private builds.
            </li>
            <li>
              Docker agents keep the new binary in the <C>/data</C> volume and run it from there after a restart, with no Docker
              socket. Mount a named volume (<C>-v pingtxt-agent:/data</C>) to keep updates when the container is recreated.
              Pulling a new image starts its own binary again and clears the volume.
            </li>
            <li>
              To keep containers immutable instead, set <C>PINGTXT_NO_AUTO_UPDATE=1</C> and let an image updater pull{" "}
              <C>:latest</C> (What&apos;s Up Docker or the maintained Watchtower fork). It needs the Docker socket, and the
              agent then follows the latest release rather than the hub&apos;s version.
            </li>
            <li>
              Docker tags: <C>:latest</C> is the newest release, <C>:1.2.3</C> a given release, <C>:main</C> the latest build
              of the main branch.
            </li>
          </ul>
        </Section>

        <Section id="checks" title="check types">
          <Table
            head={["type", "target", "what is checked"]}
            rows={[
              ["http(s)", <C key="h">https://example.com</C>, "status codes, headers, body, JSON rules, TLS expiry"],
              ["keyword", "url + text", "the response body contains (or not) a keyword"],
              ["tcp port", <C key="t">host:port</C>, "connection opens (IPv6: [2001:db8::1]:443)"],
              ["ping", "host or IP", "ICMP echo or ICMPv6 (agents need NET_RAW)"],
              ["dns", "name + record type", "resolves, optional expected value"],
              ["postgresql", <C key="p">postgres://user:pass@host:5432/db</C>, "connect + query (default SELECT 1), optional expected value"],
              ["mysql / mariadb", <C key="m">mysql://user:pass@host:3306/db</C>, "same as postgresql"],
              ["redis", <C key="r">redis://:pass@host:6379/0</C>, "AUTH + PING, rediss:// for TLS"],
              ["mongodb", <C key="mo">mongodb://user:pass@host:27017/</C>, "hello, auth, ping; shows primary/secondary"],
              ["docker container", "name or id", "down when not running, restarting, paused or unhealthy"],
              ["grpc health", <C key="g">grpc://host:port[/service]</C>, "grpc.health.v1 Check returns SERVING"],
              ["smtp", <C key="s">smtp://host:587</C>, "greeting, EHLO, STARTTLS when offered"],
              ["mcp server", <C key="mc">https://host/mcp</C>, "initialize handshake, optional tools/list checks"],
              ["heartbeat", "push URL", "your job calls the URL; down when it stops"],
            ]}
          />
          <p>
            Passwords in connection URLs are hidden in the list and in notifications; use a read-only user. Docker checks need{" "}
            <C>/var/run/docker.sock</C> on the hub or agent that runs them (add{" "}
            <C>-v /var/run/docker.sock:/var/run/docker.sock:ro</C>).
          </p>
        </Section>

        <Section id="phases" title="response time per phase">
          <p>
            HTTP and keyword checks measure where the time goes: <b className="text-foreground">DNS</b> lookup,{" "}
            <b className="text-foreground">connect</b>, <b className="text-foreground">TLS</b> handshake,{" "}
            <b className="text-foreground">wait</b> for the first byte (TTFB, the server at work) and{" "}
            <b className="text-foreground">transfer</b> of the body. On the monitor page switch the response time chart to{" "}
            <b className="text-foreground">by phase</b>, per location or all together; hover a latency in the log for the
            breakdown of that check. With redirects the phases of every hop are added up. Agents measure phases from this
            version on: update older ones to see their data.
          </p>
        </Section>

        <Section id="ipv6" title="ipv6">
          <p>
            Every check type works over IPv6. Hosts with only AAAA records just work; write IPv6 addresses in brackets
            when a port follows (<C>[2001:db8::1]:443</C>, <C>https://[2001:db8::1]/</C>,{" "}
            <C>redis://[2001:db8::1]:6379</C>). Ping and dns take the bare address.
          </p>
          <p>
            <b className="text-foreground">ip version</b> on a dual-stack host: <i>IPv4 or IPv6</i> opens one connection
            with whichever family answers first, so a broken IPv6 goes unnoticed; <i>IPv4 only</i> or <i>IPv6 only</i> use
            one family; <i>IPv4 and IPv6</i> checks both and fails when either fails. The default is set in settings and
            each monitor can override it (API <C>ip_family</C>: 0 default, 1, 4, 6, 46). Agents older than this version
            cannot do it and report the check as failed: update them.
          </p>
          <p>
            The hub and agents check whether they reach IPv6 at start and every 10 minutes (agents page and monitor form
            show it). A location without IPv6 cannot be chosen for <i>IPv6 only</i> or <i>IPv4 and IPv6</i>; <i>IPv4 or
            IPv6</i> and the instance default check over IPv4 there. <C>PINGTXT_IPV6=on</C> or <C>off</C> skips the probe.
          </p>
          <p>
            The hub listens on IPv4 and IPv6 (<C>:8090</C>). Docker containers get no IPv6 by default, so checks to IPv6
            targets fail from a container until IPv6 is enabled on its network (Docker 27 or later, and IPv6 on the server):
          </p>
          <CopyBlock label="docker-compose.yml" code={`networks:\n  default:\n    enable_ipv6: true`} />
          <p>
            With <C>docker run</C>: <C>docker network create --ipv6 pingtxt</C> and <C>--network pingtxt</C>, or{" "}
            <C>--network host</C>.
          </p>
        </Section>

        <Section id="response-checks" title="response checks (JSON)">
          <p>
            HTTP and keyword monitors can check the JSON body of the response, e.g. a health endpoint that returns{" "}
            <C>{`{"status": "ready", "checks": {"redis": {"status": "ok", "latencyMs": 3}}}`}</C>. Add rules under{" "}
            <b className="text-foreground">response checks</b> in the monitor form: all must pass, otherwise the check fails and
            the result, the incident and the notification say which rule broke, e.g.{" "}
            <C>checks.redis.status: expected &quot;ok&quot;, got &quot;error&quot;</C>.
          </p>
          <Table
            head={["path", "operator", "value"]}
            rows={[
              [<C key="1">status</C>, "equals", <C key="1v">ready</C>],
              [<C key="2">checks.redis.status</C>, "equals", <C key="2v">ok</C>],
              [<C key="3">checks.worker.ageMs</C>, "<", <C key="3v">60000</C>],
              [<C key="4">items[0].id</C>, "exists", ""],
            ]}
          />
          <ul>
            <li>
              Paths use dots and brackets: <C>a.b</C>, <C>items[0]</C> (or <C>items.0</C>, <C>items[-1]</C> for the last one),{" "}
              <C>{`["key.with.dots"]`}</C>. A leading <C>$.</C> is allowed.
            </li>
            <li>
              Operators: equals, not equals, contains (substring, array element or object key), not contains, exists, not
              exists, &gt;, &gt;=, &lt;, &lt;= (numbers) and matches (regular expression).
            </li>
            <li>
              Values are compared by type: numbers numerically (<C>3</C> equals <C>3.0</C>), <C>true</C>/<C>false</C>/
              <C>null</C> by name, objects and arrays as JSON.
            </li>
            <li>
              Rules run on agents too. Agents older than this feature report the check as failed with &quot;update it&quot;
              instead of passing it unchecked.
            </li>
          </ul>
        </Section>

        <Section id="mcp-checks" title="mcp servers">
          <p>
            An <b className="text-foreground">mcp server</b> monitor checks a remote Model Context Protocol server: it runs the{" "}
            <C>initialize</C> handshake and, when the server has tools, lists them with <C>tools/list</C>. The result shows the
            server name and version, the negotiated protocol version, the transport and the number of tools.
          </p>
          <ul>
            <li>
              <b className="text-foreground">target</b>: the URL of the endpoint, e.g. <C>https://mcp.example.com/mcp</C>.
            </li>
            <li>
              <b className="text-foreground">transport</b>: <C>auto</C> tries streamable HTTP and falls back to the legacy
              HTTP+SSE transport (2024-11-05) when the POST is rejected; pick <C>streamable</C> or <C>sse</C> to force one.
            </li>
            <li>
              <b className="text-foreground">headers</b>: sent with every request, e.g. <C>Authorization: Bearer &lt;token&gt;</C>.
              A 401 or 403 makes the monitor go down. Read-only users don&apos;t see the values.
            </li>
            <li>
              <b className="text-foreground">required tools</b> (comma-separated) and{" "}
              <b className="text-foreground">minimum tools</b>: the monitor goes down when a tool disappears or the server
              lists fewer tools.
            </li>
          </ul>
          <p>
            Tools are never called. https endpoints get TLS certificate expiry alerts like http monitors. Agents older than
            this version report <C>unknown monitor type: mcp</C>: update them.
          </p>
        </Section>

        <Section id="heartbeat" title="heartbeat monitors (cron jobs)">
          <p>
            A <b className="text-foreground">heartbeat (push)</b> monitor gets a secret URL. Call it at the end of every run, with
            any HTTP method:
          </p>
          <CopyBlock
            code={`0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 https://status.example.com/api/push/<token> >/dev/null`}
          />
          <p>
            It goes down when no call arrives within the expected period plus the grace time. Optional query parameters:{" "}
            <C>status=down</C>, <C>msg=</C>, <C>ping=</C> (run time in ms).
          </p>
        </Section>

        <Section id="status-pages" title="status pages">
          <p>
            Create them under <b className="text-foreground">status</b>: pick monitors or whole groups, their order, and whether
            past incidents are shown. A page is public at <C>https://&lt;hub&gt;/status/&lt;slug&gt;/</C>. Visitors see names,
            status, uptime and incident times, never targets, locations or error messages.
          </p>
          <p>
            For a custom domain (e.g. <C>status.example.com</C>), set it on the page, point its DNS at the hub and route the host
            to pingtxt in your reverse proxy. On that host only the status page is served.
          </p>
          <p>
            To keep a page private, turn on <i>protect with a password</i>. Visitors then see a password form; the right password
            opens the page for 30 days in that browser, together with its badges and subscription form. The password is stored
            hashed, wrong attempts are rate limited, and changing it logs every visitor out. Logged-in users never need it.
          </p>
          <p>
            Post <b className="text-foreground">incidents</b> on a page with a timeline of written updates (investigating,
            identified, monitoring, resolved). With an email channel picked under <i>email subscriptions</i>, visitors can
            subscribe: they confirm through a link, then get every update by email, each with an unsubscribe link.
          </p>
          <p>
            <b className="text-foreground">README badges</b>: every published page has SVG badges for its overall state and for
            each monitor it shows (state, uptime, response time). Copy them from the monitor page or the status page editor:
          </p>
          <CopyBlock
            code={`[![uptime](https://status.example.com/api/badge/acme/12/uptime.svg?period=7d)](https://status.example.com/status/acme/)`}
          />
          <p>
            Kinds: <C>status</C>, <C>uptime</C>, <C>response</C>; <C>period=</C> <C>24h</C>, <C>7d</C>, <C>30d</C> or{" "}
            <C>90d</C>; optional <C>label=</C> and <C>style=flat-square</C>. Drafts and monitors not on the page have no badge.
          </p>
        </Section>

        <Section id="maintenance" title="maintenance windows">
          <p>
            One-off or recurring (daily, weekly, monthly, in any time zone), for some or all monitors, groups or tags. Checks keep
            running but no incidents or alerts are raised, and those checks don&apos;t count toward uptime.
          </p>
        </Section>

        <Section id="expiry" title="expiry alerts">
          <p>
            TLS certificates of https monitors and domain registrations (RDAP, WHOIS fallback for registries such as .it).
            Thresholds in days are set in settings (default 14/7/3/1 for certificates, 30/14/7/1 for domains): one alert per
            threshold crossed, one on expiry, one on renewal.
          </p>
        </Section>

        <Section id="notifications" title="notifications">
          <p>
            Webhook (JSON), Discord, Slack, Telegram, ntfy and email (SMTP with STARTTLS/TLS, or the Resend API with an API key from a verified domain). Add them under{" "}
            <b className="text-foreground">notify</b>, test them before saving, and attach them to monitors.
          </p>
          <p>
            <b className="text-foreground">Periodic reports</b>, on the same page, email the uptime, the incidents and the
            downtime of the last week (every Monday) or month (on the 1st) per monitor group, compared with the period before.
            Pick the email channels, optionally some groups, the hour and the time zone; preview a report or send it now
            before turning it on.
          </p>
          <p>
            Escalation and reminders are set per channel. <b className="text-foreground">send after</b> N minutes alerts the
            channel only if the monitor is still down by then (an on-call phone, a manager), so short outages never reach it.{" "}
            <b className="text-foreground">repeat every</b> N minutes sends a STILL DOWN reminder while the monitor stays down
            (at least 5). Webhooks carry <C>reminder</C> and <C>down_since</C>. Nothing is sent during maintenance
            windows; due alerts go out when the window ends.
          </p>
        </Section>

        <Section id="sso" title="single sign-on">
          <p>
            In <b className="text-foreground">settings → authentication</b> turn on GitHub, Google or generic OpenID Connect
            (Authentik, Keycloak, Authelia, Pocket ID, Zitadel, Okta, Entra ID...), paste the client id and secret and register
            the callback URL shown there.
          </p>
          <ul>
            <li>Logins are matched to existing users by verified email. New accounts can be allowed, optionally by email domain.</li>
            <li>
              Locked out after turning off password login? Run <C>docker exec -u pingtxt pingtxt pingtxt -enable-password-login</C> and
              restart.
            </li>
          </ul>
        </Section>

        <Section id="2fa" title="two-factor authentication">
          <p>
            In <b className="text-foreground">settings → two-factor authentication</b> scan the QR code with any authenticator app.
            You get 10 single-use recovery codes. To reset a user: <b className="text-foreground">settings → users → reset 2fa</b>, or{" "}
            <C>docker exec -u pingtxt pingtxt pingtxt -reset-2fa you@example.com</C>.
          </p>
        </Section>

        <Section id="api" title="api tokens">
          <p>
            Create personal tokens in <b className="text-foreground">settings → api tokens</b>, read only or read + write, with an
            optional expiry. They are shown once and stored hashed.
          </p>
          <CopyBlock code={`curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors`} />
          <p>
            <C>GET /api/monitors</C> accepts <C>?group=&lt;id&gt;</C> and <C>?tag=&lt;id&gt;</C>. Tokens never reach account
            endpoints (password, 2FA, tokens, users, instance settings).
          </p>
          <p>
            Every endpoint, with parameters and examples, is in the{" "}
            <a href="/docs/api/" className="text-foreground underline underline-offset-4">
              API reference
            </a>
            . Each hub also serves the OpenAPI 3.1 document at <C>/api/openapi.json</C> and an interactive Swagger UI at{" "}
            <C>/api/docs</C> (linked from <b className="text-foreground">settings → api tokens</b>).
          </p>
        </Section>

        <Section id="mcp" title="mcp server">
          <p>
            The hub is an <a href="https://modelcontextprotocol.io" className="text-foreground underline underline-offset-4">MCP</a>{" "}
            server at <C>/mcp</C> (Streamable HTTP): Claude and other AI assistants can tell you what is down, read incidents and
            response times, and create monitors or maintenance windows. Roles and hidden secrets are the same as the API.
          </p>
          <p>
            <b className="text-foreground">OAuth 2.1</b>, nothing to copy: add <C>https://status.example.com/mcp</C> as a custom
            connector in claude.ai or Claude Desktop (or <C>claude mcp add --transport http pingtxt https://status.example.com/mcp</C>{" "}
            then <C>/mcp</C> in Claude Code). You log in to pingtxt in the browser and choose read only or read + write (admins
            only). Discovery, dynamic client registration, PKCE and rotating refresh tokens are supported; access tokens last an
            hour and work on <C>/mcp</C> and the API. Connected apps can be disconnected in{" "}
            <b className="text-foreground">settings → api tokens</b>. Behind a proxy, set the public URL in settings.
          </p>
          <p>
            <b className="text-foreground">API token</b>, for clients without OAuth: read tokens get the read tools, a read + write
            token of an admin also gets the tools that change data.
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
            label="cursor, windsurf and other url + headers clients"
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
            head={["tool", "does"]}
            rows={[
              [<C key="1">get_overview</C>, "monitors by status, agents online, open incidents"],
              [<C key="2">list_monitors</C>, "status, uptime, response time; filter by status, group, tag, name"],
              [<C key="3">get_monitor</C>, "settings, uptime 24h/7d/30d, status per location, expiry"],
              [<C key="4">get_monitor_results</C>, "latest check results"],
              [<C key="5">list_incidents</C>, "incidents, open only or per monitor"],
              [<C key="6">list_maintenances</C>, "maintenance windows and next occurrence"],
              [<C key="7">list_groups · list_tags · list_agents · list_status_pages</C>, "ids for everything else"],
              [<C key="8">create / update / pause / delete_monitor</C>, "admin write token"],
              [<C key="9">create / delete_maintenance</C>, "admin write token, ISO 8601 times"],
            ]}
          />
        </Section>

        <Section id="apps" title="native apps">
          <p>
            The iOS app signs in with OAuth 2.1 (authorization code + PKCE) on the hub&apos;s own login page, so passwords, 2FA,
            GitHub, Google and OpenID Connect all work. It is the built-in public client <C>pingtxt-ios</C> with redirect URI{" "}
            <C>pingtxt://oauth</C>: no secret and no registration. Admins choose read only or read + write on the consent page,
            read-only users always get read only.
          </p>
          <Table
            head={["step", "request"]}
            rows={[
              [
                "sign in",
                <span key="1" className="break-all"><C>GET /oauth/authorize?response_type=code&amp;client_id=pingtxt-ios&amp;redirect_uri=pingtxt://oauth&amp;scope=read%20write&amp;state=…&amp;code_challenge=…&amp;code_challenge_method=S256</C></span>,
              ],
              [
                "get tokens",
                <span key="2" className="break-all"><C>POST /oauth/token grant_type=authorization_code&amp;code=…&amp;client_id=pingtxt-ios&amp;redirect_uri=pingtxt://oauth&amp;code_verifier=…</C></span>,
              ],
              ["call the api", <span key="3" className="break-all"><C>Authorization: Bearer pma_…</C></span>],
              [
                "refresh",
                <span key="4" className="break-all"><C>POST /oauth/token grant_type=refresh_token&amp;refresh_token=pmr_…&amp;client_id=pingtxt-ios</C></span>,
              ],
              ["sign out", <span key="5" className="break-all"><C>POST /oauth/revoke token=pmr_…&amp;client_id=pingtxt-ios</C></span>],
            ]}
          />
          <p>
            Access tokens last an hour and work on the whole API like an API token of the same scope (never on account
            endpoints). Refresh tokens last 30 days from their last use and rotate: every refresh returns a new one, and using
            an old one again ends the sign-in. Each sign-in is listed in{" "}
            <b className="text-foreground">settings → api tokens → connected apps</b>, where it can be disconnected.
          </p>
        </Section>

        <Section id="prometheus" title="prometheus">
          <p>
            <C>GET /metrics</C> needs a read token, unless <b className="text-foreground">settings → prometheus metrics → public</b>{" "}
            is on. Output is cached for 10 seconds.
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
            head={["metric", "meaning"]}
            rows={[
              [<C key="1">pingtxt_monitor_up</C>, "1 up or degraded, 0 down"],
              [<C key="2">pingtxt_monitor_uptime_ratio</C>, "uptime per window (24h, 7d, 30d)"],
              [<C key="3">pingtxt_check_latency_seconds</C>, "last check per location"],
              [<C key="3p">pingtxt_check_phase_seconds</C>, "DNS, connect, TLS and first byte of the last HTTP check"],
              [<C key="4">pingtxt_cert_expiry_days</C>, "days before the certificate expires"],
              [<C key="5">pingtxt_domain_expiry_days</C>, "days before the domain expires"],
              [<C key="6">pingtxt_agent_connected</C>, "agent connectivity"],
            ]}
          />
        </Section>

        <Section id="upgrade" title="upgrade + backup">
          <CopyBlock code={`docker compose pull && docker compose up -d`} />
          <p>
            Database migrations run on start. With SQLite all state is one file, <C>/data/pingtxt.db</C>: settings → database
            backups takes copies while the hub runs, on demand or on a schedule, and restores them. With PostgreSQL or MariaDB
            use <a href="#database" className="text-foreground underline underline-offset-4">pg_dump or mariadb-dump</a>.
            Agents installed with the script follow the hub version on their own.
          </p>
          <p>
            While the hub restarts or updates, browsers show a <b className="text-foreground">pingtxt is updating</b> page
            that follows the migrations and reloads by itself when the hub is ready, and <C>/api/health</C> answers 503. Open
            tabs wait on the same screen instead of failing. When the container is fully down (pulling the new image), browsers
            that opened pingtxt before over https still show it from their cache; the first visit ever gets the reverse proxy&apos;s
            error until the hub listens again.
          </p>
          <p>
            Release builds ask GitHub for the latest release every 12 hours and show admins a notice when a newer one is out.
            Turn it off in <b className="text-foreground">settings → updates</b>, or start the hub with{" "}
            <C>PINGTXT_DISABLE_UPDATE_CHECK=true</C> so it never contacts GitHub.
          </p>
        </Section>

        <Section id="usage-stats" title="usage stats">
          <p>
            After installing, admins are asked once whether the hub may send anonymous usage stats. Nothing is sent until
            someone says yes, and the answer can be changed any time in <b className="text-foreground">settings → usage stats</b>,
            which also shows the exact next report.
          </p>
          <p>Once an hour the hub sends to <C>telemetry.pingtxt.com</C> only this:</p>
          <ul>
            <li>
              <C>id</C>: a random id made the first time stats are turned on. It never changes and says nothing about the hub.
            </li>
            <li>
              <C>version</C>: the pingtxt version.
            </li>
            <li>
              <C>monitors</C>: how many monitors are not paused.
            </li>
            <li>
              <C>agents</C>: how many agents are connected.
            </li>
          </ul>
          <p>
            No names, targets, addresses, results or emails, and the service does not store IP addresses. Start the hub with{" "}
            <C>PINGTXT_DISABLE_TELEMETRY=true</C> to turn the stats off for good and hide the question. Development builds never send.
          </p>
          <p>
            The totals are public at <a href="https://telemetry.pingtxt.com/" className="text-foreground underline underline-offset-4">telemetry.pingtxt.com</a>{" "}
            (JSON at <C>/v1/stats</C>): live hubs, monitors and agents, versions in use and 90 days of history. Never a single
            hub&apos;s report.
          </p>
        </Section>

        <Section id="verify" title="verify a release">
          <p>
            Releases after v0.1.0 are signed with <a href="https://docs.sigstore.dev/" className="text-foreground underline underline-offset-4">cosign</a>{" "}
            in keyless mode, tied to the GitHub workflow that built them. Each release ships <C>SHA256SUMS</C> and its signature{" "}
            <C>SHA256SUMS.sigstore.json</C>; images on ghcr.io are signed by digest.
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

        <Section id="build" title="build from source">
          <p>
            Needs <b className="text-foreground">Go 1.26+</b>, <b className="text-foreground">Node.js 22+</b> with npm,{" "}
            <C>make</C> and <C>git</C>. No C compiler (everything builds with <C>CGO_ENABLED=0</C>).
          </p>
          <CopyBlock
            code={`git clone https://github.com/federico-it/pingtxt.git && cd pingtxt
git checkout v1.2.3        # optional: a release tag
make web hub agent agents`}
          />
          <Table
            head={["target", "output", ""]}
            rows={[
              [<C key="w">make web</C>, <C key="w2">internal/ui/dist</C>, "the web UI, embedded in the hub: run it before make hub"],
              [<C key="h">make hub</C>, <C key="h2">dist/pingtxt</C>, "the hub for this machine, UI included"],
              [<C key="a">make agent</C>, <C key="a2">dist/pingtxt-agent</C>, "the agent for this machine"],
              [
                <C key="as">make agents</C>,
                <C key="as2">dist/agents/</C>,
                "agents for linux amd64/arm64/arm and darwin, served by the hub to the install script and for updates",
              ],
            ]}
          />
          <p>
            The version comes from <C>git describe</C> (override with <C>VERSION=v1.2.3</C>). Build hub and agents from the
            same checkout: agents update themselves to the hub version.
          </p>
          <CopyBlock
            label="run"
            code={`./dist/pingtxt     # data in ./data, agents from ./dist/agents, :8090
PINGTXT_DATA=/var/lib/pingtxt PINGTXT_AGENT_DIR=/usr/local/share/pingtxt/agents /usr/local/bin/pingtxt`}
          />
          <p>
            Ping checks use a raw socket with <C>CAP_NET_RAW</C>, otherwise an unprivileged ICMP socket (allowed by{" "}
            <C>net.ipv4.ping_group_range</C>). Outside Docker: <C>sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt</C>{" "}
            (and the agent); <C>PINGTXT_UNPRIVILEGED_PING=1</C> or <C>0</C> forces one of the two. A systemd unit example is in
            the{" "}
            <a href={`${REPO}#build-from-source`} className="text-foreground underline underline-offset-4">
              README
            </a>
            .
          </p>
          <CopyBlock
            label="cross-compile"
            code={`V=$(git describe --tags --always)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/pingtxt-linux-arm64 ./cmd/pingtxt
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w -X main.version=$V" -o dist/pingtxt-agent-linux-armv7 ./cmd/pingtxt-agent`}
          />
          <CopyBlock
            label="docker images (only docker needed)"
            code={`make docker     # pingtxt:dev and pingtxt-agent:dev
docker build --build-arg VERSION=$(git describe --tags --always) -t pingtxt:dev .
docker build --build-arg VERSION=$(git describe --tags --always) -f Dockerfile.agent -t pingtxt-agent:dev .
docker buildx build --platform linux/amd64,linux/arm64 -t registry.example.com/pingtxt:dev --push .`}
          />
          <p>
            To run a compose file with your own build, add <C>docker-compose.build.yml</C> after it. Same for SQLite,
            PostgreSQL and MariaDB (create the <C>.env</C> with the passwords first):
          </p>
          <CopyBlock
            code={`docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.postgres.yml -f docker-compose.build.yml up -d --build
docker compose -f docker-compose.mariadb.yml -f docker-compose.build.yml up -d --build`}
          />
        </Section>

        <Section id="contributing" title="contributing">
          <p>
            pingtxt is free software under the{" "}
            <a href={`${REPO}/blob/main/LICENSE`} className="text-foreground underline underline-offset-4">
              AGPL-3.0 license
            </a>
            : use it, self-host it and change it freely; if you distribute a modified version or run one as a service for
            others, publish its source under the same license. Bug reports, fixes and new check types or notifiers are
            welcome.
          </p>
          <ul>
            <li>
              <b className="text-foreground">bugs</b>: search the{" "}
              <a href={`${REPO}/issues`} className="text-foreground underline underline-offset-4">issues</a> first, then open a{" "}
              <a href={`${REPO}/issues/new?template=bug_report.yml`} className="text-foreground underline underline-offset-4">bug report</a>{" "}
              with the version (shown in settings), how you run it, steps and logs.
            </li>
            <li>
              <b className="text-foreground">ideas</b>: open a{" "}
              <a href={`${REPO}/issues/new?template=feature_request.yml`} className="text-foreground underline underline-offset-4">feature request</a>.
              For bigger changes, agree on the approach in an issue before writing code.
            </li>
            <li>
              <b className="text-foreground">security</b>: never in a public issue.{" "}
              <a href={`${REPO}/security/advisories/new`} className="text-foreground underline underline-offset-4">Report it privately</a>,
              see <a href={`${REPO}/blob/main/SECURITY.md`} className="text-foreground underline underline-offset-4">SECURITY.md</a>.
            </li>
            <li>
              <b className="text-foreground">pull requests</b>: fork, branch from <C>main</C>, run <C>make test lint</C>, open the PR
              against <C>main</C>. It is merged by a maintainer once the <C>go</C> and <C>web</C> checks pass and the review is approved.
            </li>
          </ul>
          <CopyBlock
            label="dev setup"
            code={`git clone https://github.com/<you>/pingtxt.git && cd pingtxt
(cd web && npm ci)
make dev-hub   # hub on :8090
make dev-web   # UI on :3000, proxies /api`}
          />
          <p>
            Everything else, commit style and code style included, is in{" "}
            <a href={`${REPO}/blob/main/CONTRIBUTING.md`} className="text-foreground underline underline-offset-4">CONTRIBUTING.md</a>.
            Everyone taking part follows the{" "}
            <a href={`${REPO}/blob/main/CODE_OF_CONDUCT.md`} className="text-foreground underline underline-offset-4">code of conduct</a>.
          </p>
        </Section>
      </div>
    </div>
  )
}
