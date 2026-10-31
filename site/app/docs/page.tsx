import type { Metadata } from "next"

import { CopyBlock } from "@/components/copy-block"
import { Prompt, REPO, SectionTitle } from "@/components/site-shell"

export const metadata: Metadata = {
  title: "docs",
  description: "Install and configure pingtxt: hub, agents, login, notifications, status pages, API and Prometheus.",
}

const RAW = "https://raw.githubusercontent.com/federico-it/pingtxt/main"

const toc = [
  ["hub", "run the hub"],
  ["database", "postgres / mariadb"],
  ["reverse-proxy", "reverse proxy"],
  ["agents", "add a location"],
  ["agent-updates", "agent updates"],
  ["checks", "check types"],
  ["response-checks", "response checks (JSON)"],
  ["heartbeat", "heartbeat monitors"],
  ["status-pages", "status pages"],
  ["maintenance", "maintenance"],
  ["expiry", "expiry alerts"],
  ["notifications", "notifications"],
  ["sso", "single sign-on"],
  ["2fa", "two-factor auth"],
  ["api", "api tokens"],
  ["prometheus", "prometheus"],
  ["upgrade", "upgrade + backup"],
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
          <h1 className="text-xl font-semibold">docs</h1>
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
  ghcr.io/federico-it/pingtxt-agent:latest

# Linux + systemd (binary downloaded from your hub)
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --hub https://status.example.com --token pt_xxx

# uninstall
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --uninstall`}
          />
          <p>
            Then tick the new location in any monitor. The hub schedules every check and sends it to the agents; agents are
            stateless, open no ports and reconnect with backoff.
          </p>
        </Section>

        <Section id="agent-updates" title="agent updates">
          <p>
            The hub bundles agent binaries built with its own version. Agents installed with the script update themselves when
            they reconnect after a hub upgrade: they download the binary from the hub, verify its SHA-256 and version, swap it
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
            <li>Docker agents never replace their binary: pull the new image or use Watchtower.</li>
          </ul>
        </Section>

        <Section id="checks" title="check types">
          <Table
            head={["type", "target", "what is checked"]}
            rows={[
              ["http(s)", <C key="h">https://example.com</C>, "status codes, headers, body, JSON rules, TLS expiry"],
              ["keyword", "url + text", "the response body contains (or not) a keyword"],
              ["tcp port", <C key="t">host:port</C>, "connection opens"],
              ["ping", "host", "ICMP echo (agents need NET_RAW)"],
              ["dns", "name + record type", "resolves, optional expected value"],
              ["postgresql", <C key="p">postgres://user:pass@host:5432/db</C>, "connect + query (default SELECT 1), optional expected value"],
              ["mysql / mariadb", <C key="m">mysql://user:pass@host:3306/db</C>, "same as postgresql"],
              ["redis", <C key="r">redis://:pass@host:6379/0</C>, "AUTH + PING, rediss:// for TLS"],
              ["mongodb", <C key="mo">mongodb://user:pass@host:27017/</C>, "hello, auth, ping; shows primary/secondary"],
              ["docker container", "name or id", "down when not running, restarting, paused or unhealthy"],
              ["grpc health", <C key="g">grpc://host:port[/service]</C>, "grpc.health.v1 Check returns SERVING"],
              ["smtp", <C key="s">smtp://host:587</C>, "greeting, EHLO, STARTTLS when offered"],
              ["heartbeat", "push URL", "your job calls the URL; down when it stops"],
            ]}
          />
          <p>
            Passwords in connection URLs are hidden in the list and in notifications; use a read-only user. Docker checks need{" "}
            <C>/var/run/docker.sock</C> on the hub or agent that runs them (add{" "}
            <C>-v /var/run/docker.sock:/var/run/docker.sock:ro</C>).
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
            Webhook (JSON), Discord, Slack, Telegram, ntfy and email (SMTP with STARTTLS/TLS). Add them under{" "}
            <b className="text-foreground">notify</b>, test them before saving, and attach them to monitors.
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
              Locked out after turning off password login? Run <C>docker exec pingtxt pingtxt -enable-password-login</C> and
              restart.
            </li>
          </ul>
        </Section>

        <Section id="2fa" title="two-factor authentication">
          <p>
            In <b className="text-foreground">settings → two-factor authentication</b> scan the QR code with any authenticator app.
            You get 10 single-use recovery codes. To reset a user: <b className="text-foreground">settings → users → reset 2fa</b>, or{" "}
            <C>docker exec pingtxt pingtxt -reset-2fa you@example.com</C>.
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
            Outside Docker, ping checks need raw sockets: <C>sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt</C> (and the
            agent), or <C>PINGTXT_UNPRIVILEGED_PING=1</C> with <C>net.ipv4.ping_group_range</C>. A systemd unit example is in
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
