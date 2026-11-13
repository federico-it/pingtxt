# Installazione

L'hub è un container Docker con una cartella per i suoi dati. Gli agent sono facoltativi, vedi
[Località](locations.md).

## Avviare l'hub

```sh
docker run -d --name pingtxt --restart unless-stopped \
  -p 8090:8090 -v ./data:/data \
  ghcr.io/federico-it/pingtxt:latest
```

Oppure `docker compose up -d` con il [docker-compose.yml](../../docker-compose.yml) del repository. Apri
`http://localhost:8090`, crea l'account admin e aggiungi il primo [monitor](monitors.md).

La dashboard mostra poi agli admin una checklist **primi passi**: aggiungi un monitor, configura un canale di notifica,
imposta l'URL pubblico, aggiungi un [agent](locations.md) (facoltativo), pubblica una [status page](status-pages.md)
(facoltativo), attiva l'autenticazione a due fattori e, con SQLite, pianifica i [backup](administration.md#backup).
Ogni passo si spunta da solo in base alla configurazione, e la checklist sparisce quando i passi necessari sono fatti.
**nascondi** la toglie prima; **impostazioni → generali** la rimostra.

L'immagine gira su linux/amd64 e linux/arm64. Tag Docker: `:latest` è la release più recente, `:1.2.3` una release
precisa, `:main` l'ultima build del branch main. I binari dell'hub per Linux (e degli agent per Linux e macOS) sono
nella [pagina delle release](https://github.com/federico-it/pingtxt/releases), vedi [senza Docker](#senza-docker).

## Configurazione

Tutto si configura dall'interfaccia web in **impostazioni** e viene salvato nel database: URL pubblico, località
integrata, conservazione dello storico, valori predefiniti dei nuovi monitor, utenti e accesso. All'avvio vengono
lette solo poche variabili d'ambiente:

| env | predefinito | |
| --- | --- | --- |
| `PINGTXT_ADDR` | `:8090` | indirizzo di ascolto (IPv4 e IPv6) |
| `PINGTXT_DATA` | `/data` | cartella del database SQLite, dei backup e dei ripristini in attesa |
| `PINGTXT_DB_URL` | | PostgreSQL o MariaDB/MySQL al posto di SQLite, vedi [sotto](#postgresql-o-mariadb) |
| `PINGTXT_DISABLE_UPDATE_CHECK` | | `true`: non chiede mai a GitHub le nuove release, vedi [amministrazione](administration.md#controllo-degli-aggiornamenti) |
| `PINGTXT_DISABLE_TELEMETRY` | | `true`: non invia mai statistiche d'uso e nasconde la domanda, vedi [amministrazione](administration.md#statistiche-duso) |
| `PINGTXT_IPV6` | | `on` o `off` salta la verifica della raggiungibilità IPv6, vedi [IPv6](#ipv6) |
| `PINGTXT_UNPRIVILEGED_PING` | | `1` o `0` forza il socket ICMP senza privilegi o quello raw, vedi [senza Docker](#senza-docker) |
| `PINGTXT_AGENT_DIR` | | cartella dei binari degli agent, quando non sono accanto all'hub |

`PINGTXT_PUBLIC_URL`, `PINGTXT_RETENTION_DAYS`, `PINGTXT_LOCAL_NAME` e `PINGTXT_DISABLE_LOCAL` funzionano ancora, ma
compilano le impostazioni solo al primo avvio. Dopo, si cambiano dall'interfaccia.

Imposta l'**URL pubblico** in **impostazioni → generali** appena l'hub ha il suo indirizzo definitivo: lo usano i
link nelle email, i callback di accesso, gli URL heartbeat e i comandi di installazione degli agent.

## Container senza root

Le immagini dell'hub e degli agent eseguono pingtxt con l'utente `pingtxt` (uid/gid 10001), non come root. Il
container parte come root solo per l'entrypoint, che:

- assegna `/data` a `pingtxt` quando alcuni file appartengono a un altro utente (i volumi delle versioni precedenti
  erano scritti da root: il primo avvio dopo l'aggiornamento li sistema);
- aggiunge `pingtxt` al gruppo di un socket Docker montato (per i monitor [container docker](monitors.md#database-container-grpc-e-smtp));
- passa a `pingtxt` senza capability tranne `CAP_NET_RAW` (per il ping), con `no-new-privileges`.

Per saltare il passaggio come root, imposta tu l'utente (`--user 10001:10001`, oppure `user: "10001:10001"` nel
compose). La cartella dei dati deve allora appartenere a quell'utente (`sudo chown -R 10001:10001 ./data`), e un
socket Docker richiede `group_add: ["<gid di /var/run/docker.sock>"]`. Se l'entrypoint non riesce a cambiare il
proprietario di `/data` (per esempio una condivisione NFS), scrive un avviso nel log e gira come root.

Esegui le opzioni da riga di comando con lo stesso utente: `docker exec -u pingtxt pingtxt pingtxt -backup ...`.

## PostgreSQL o MariaDB

SQLite è il predefinito e non richiede altro. Per tenere i dati in **PostgreSQL 13+** o **MariaDB 10.6+ / MySQL
8.0.13+**, imposta `PINGTXT_DB_URL`. Il database deve esistere ed essere vuoto (o contenere uno schema di pingtxt);
le tabelle vengono create e migrate all'avvio.

```sh
PINGTXT_DB_URL=postgres://pingtxt:secret@db:5432/pingtxt?sslmode=disable
PINGTXT_DB_URL=mariadb://pingtxt:secret@db:3306/pingtxt     # va bene anche mysql://...
```

I caratteri speciali nella password vanno codificati per URL. I file compose pronti avviano hub e database con un
solo comando:

```sh
curl -O https://raw.githubusercontent.com/federico-it/pingtxt/main/docker-compose.postgres.yml
echo "DB_PASSWORD=$(openssl rand -hex 16)" > .env
docker compose -f docker-compose.postgres.yml up -d
```

```sh
curl -O https://raw.githubusercontent.com/federico-it/pingtxt/main/docker-compose.mariadb.yml
printf "DB_PASSWORD=%s\nDB_ROOT_PASSWORD=%s\n" $(openssl rand -hex 16) $(openssl rand -hex 16) > .env
docker compose -f docker-compose.mariadb.yml up -d
```

I file compose non partono senza le password in `.env`. Il container del database le usa solo quando crea il
database la prima volta, quindi impostale prima del primo avvio.

Tutto funziona allo stesso modo sui tre database, tranne i [backup del database](administration.md#backup)
integrati, che sono file SQLite: con PostgreSQL o MariaDB fai il backup del server con i suoi strumenti
([comandi](administration.md#backup-di-postgresql-e-mariadb)). Per spostare un hub SQLite esistente,
[esporta la configurazione](administration.md#export-e-import-della-configurazione) con i segreti e importala nel
nuovo hub; lo storico non viene spostato.

## Reverse proxy

Metti l'hub dietro un reverse proxy con TLS (Caddy, Traefik, nginx) e imposta l'URL pubblico in
**impostazioni → generali**. L'upgrade WebSocket deve essere permesso su `/api/agent/ws`, dove si collegano gli
agent.

```text
status.example.com {
  reverse_proxy 127.0.0.1:8090
}
```

Caddy inoltra i WebSocket senza impostazioni in più. Le status page su un
[dominio personalizzato](status-pages.md#dominio-personalizzato) passano dallo stesso hub.

## Coolify

Il repository ha due file compose pronti per [Coolify](https://coolify.io):
[docker-compose.coolify.yml](../../docker-compose.coolify.yml) (SQLite) e
[docker-compose.coolify.postgres.yml](../../docker-compose.coolify.postgres.yml) (PostgreSQL). In Coolify crea una
nuova risorsa di tipo **Docker Compose Empty**, incolla uno dei due file e fai il deploy.

Coolify riempie da solo le variabili: `SERVICE_URL_PINGTXT_8090` crea il dominio e lo inoltra alla porta 8090
con TLS, e utente e password di PostgreSQL arrivano da `SERVICE_USER_POSTGRES` e `SERVICE_PASSWORD_POSTGRES`.
Non c'è niente da scrivere a mano. Per usare un tuo dominio cambialo nelle impostazioni del servizio prima del
primo deploy: l'URL pubblico viene preso da lì solo al primo avvio, poi si cambia in **impostazioni → generali**.

I dati restano nel volume `pingtxt-data` (e `pingtxt-postgres` per il database). La rete Docker di Coolify non ha
IPv6 di default, quindi i controlli verso destinazioni solo IPv6 falliscono dall'hub; falli partire da un
[agent](locations.md) con IPv6.

## IPv6

L'hub ascolta su IPv4 e IPv6, e ogni tipo di controllo funziona su IPv6 (vedi [IPv6 nei monitor](monitors.md#ipv6)).
Docker non dà IPv6 ai container sulla rete predefinita, quindi i controlli verso destinazioni IPv6 falliscono dal
container anche se il server ha IPv6. Attivalo sulla rete (Docker 27 o successivo):

```yaml
# docker-compose.yml
networks:
  default:
    enable_ipv6: true
```

Con `docker run`, crea una rete con `docker network create --ipv6 pingtxt` e aggiungi `--network pingtxt`, oppure usa
`--network host`. Le porte pubblicate accettano client IPv6 in ogni caso.

L'hub e ogni agent verificano se raggiungono Internet via IPv6 all'avvio e ogni 10 minuti. Imposta
`PINGTXT_IPV6=on` o `off` per saltare la verifica nelle reti che bloccano gli indirizzi che usa.

## Senza Docker

Scarica l'hub (`pingtxt-linux-amd64` o `pingtxt-linux-arm64`) e i binari `pingtxt-agent-*` dalla
[pagina delle release](https://github.com/federico-it/pingtxt/releases), oppure
[compilali](../../CONTRIBUTING.md#build-from-source). L'hub cerca i dati in `./data` e i binari degli agent in
`./dist/agents`; imposta `PINGTXT_DATA` e `PINGTXT_AGENT_DIR` per metterli altrove.

I controlli **ping** usano un socket raw quando il processo ha `CAP_NET_RAW`, altrimenti un socket ICMP senza
privilegi, che richiede il gruppo del processo in `net.ipv4.ping_group_range` (la maggior parte delle distribuzioni
con systemd lo permette). Dai la capability al binario con `sudo setcap cap_net_raw+ep /usr/local/bin/pingtxt`,
oppure `AmbientCapabilities=CAP_NET_RAW` nella unit. `PINGTXT_UNPRIVILEGED_PING=1` usa sempre il socket senza
privilegi, `0` sempre quello raw.

Una unit systemd minima:

```ini
# /etc/systemd/system/pingtxt.service
[Unit]
Description=pingtxt hub
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/pingtxt
Environment=PINGTXT_DATA=/var/lib/pingtxt PINGTXT_AGENT_DIR=/usr/local/share/pingtxt/agents
StateDirectory=pingtxt
DynamicUser=yes
AmbientCapabilities=CAP_NET_RAW
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Gli agent si aggiornano alla versione dell'hub, quindi tieni in `PINGTXT_AGENT_DIR` i binari degli agent della
stessa release.
