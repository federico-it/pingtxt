# Amministrazione

Tenere in funzione un hub: aggiornamenti, backup e ripristini, spostare la configurazione tra hub, statistiche
d'uso, verifica delle firme delle release e opzioni da riga di comando.

## Aggiornamento

```sh
docker compose pull && docker compose up -d
```

Le migrazioni del database partono all'avvio. Gli agent installati con lo script o in Docker seguono da soli la
versione dell'hub, vedi [aggiornamento degli agent](locations.md#aggiornamento-degli-agent). Fai prima un
[backup](#backup) se vuoi poter tornare indietro: un backup di una versione più recente non si può ripristinare su un
hub più vecchio.

Mentre l'hub si riavvia o si aggiorna, i browser mostrano una pagina **pingtxt si sta aggiornando** che segue le
migrazioni e si ricarica da sola quando l'hub è pronto, e `/api/health` risponde 503. Le schede aperte aspettano sulla
stessa schermata invece di dare errore. Quando il container è del tutto fermo (mentre scarica la nuova immagine), i
browser che avevano già aperto pingtxt in https mostrano quella pagina dalla cache; una prima visita riceve l'errore
del reverse proxy finché l'hub non torna in ascolto.

## Controllo degli aggiornamenti

Le build di release chiedono a GitHub l'ultima release di pingtxt ogni 12 ore. Quando ne esce una più recente, gli
admin vedono un avviso con le note di rilascio e come aggiornare. Si disattiva in **impostazioni → generali**; per le
installazioni offline, `PINGTXT_DISABLE_UPDATE_CHECK=true` lo spegne dal primo avvio, prima di qualsiasi richiesta.
Le build di sviluppo e di `main` non controllano mai.

## Backup

Con SQLite, **impostazioni → dati → backup del database** fa una copia coerente di tutto il database (impostazioni,
utenti, segreti, storico, registro attività) mentre l'hub continua a funzionare. Attiva i **backup pianificati** per
farne uno ogni N ore e conservare gli ultimi N. I file stanno in `<data>/backups` (`/data/backups` in Docker) e si
possono scaricare o caricare dalla stessa pagina. Un backup viene fatto in automatico anche prima di ogni importazione
della configurazione e di ogni ripristino.

Il **ripristino** sostituisce l'intero database. Nell'interfaccia scegli un backup → *ripristina*: viene messo in
attesa e applicato al riavvio dell'hub (`docker restart pingtxt`). Da una shell, con l'hub fermo:

```sh
pingtxt -restore /data/backups/pingtxt-auto-20261030-020000.db
# Docker: docker compose stop pingtxt
#         docker compose run --rm pingtxt -restore /data/backups/<file>.db
#         docker compose start pingtxt
```

Il database sostituito viene conservato come `pingtxt-pre-restore-*.db`. I backup delle versioni precedenti vengono
migrati all'avvio; quelli di una versione più recente vengono rifiutati. Per fare un backup da cron:
`pingtxt -backup /percorso/file.db` (sicuro mentre l'hub gira; in Docker
`docker exec -u pingtxt pingtxt pingtxt -backup /data/backups/x.db`).

### Backup di PostgreSQL e MariaDB

I backup integrati sono file SQLite. Con PostgreSQL o MariaDB, fai il backup del server del database con i suoi
strumenti:

```sh
# PostgreSQL
docker exec pingtxt-postgres pg_dump -U pingtxt -Fc pingtxt > pingtxt.dump
docker stop pingtxt
docker exec -i pingtxt-postgres pg_restore -U pingtxt -d pingtxt --clean --if-exists < pingtxt.dump
docker start pingtxt
```

```sh
# MariaDB
docker exec pingtxt-mariadb sh -c 'mariadb-dump -u pingtxt -p"$MARIADB_PASSWORD" --single-transaction pingtxt' > pingtxt.sql
docker stop pingtxt
docker exec -i pingtxt-mariadb sh -c 'mariadb -u pingtxt -p"$MARIADB_PASSWORD" pingtxt' < pingtxt.sql
docker start pingtxt
```

## Export e import della configurazione

**impostazioni → dati → export della configurazione** scarica un file JSON con monitor, gruppi, tag, canali di
notifica, finestre di manutenzione, status page, agent e impostazioni dell'istanza: niente storico e niente account
utente. Senza **includi i segreti**, password, token dei bot, URL segreti dei webhook (Discord, Slack), client secret
di accesso, header con credenziali e password negli URL di destinazione vengono sostituiti da `[redacted]`, e i token
di agent e heartbeat vengono omessi.

Importando:

- **merge** aggiorna gli oggetti con lo stesso nome (le status page per slug) e aggiunge gli altri; i valori
  `[redacted]` mantengono quelli salvati. **replace** elimina prima monitor attuali (con il loro storico), gruppi, tag,
  canali, finestre di manutenzione e status page. Gli agent non vengono mai eliminati.
- Su un altro hub, i canali con segreti `[redacted]` vengono saltati e i monitor con credenziali `[redacted]` messi in
  pausa; il risultato dell'importazione elenca cosa compilare. Esporta *con* i segreti per spostare un hub per intero:
  gli agent mantengono i loro token e gli URL heartbeat restano uguali. È anche il modo per passare da SQLite a
  PostgreSQL o MariaDB.

Lo stesso è disponibile dall'API e da riga di comando:

```sh
curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/config/export > pingtxt.json   # con un token, senza segreti
pingtxt -export-config pingtxt.json [-export-secrets]
pingtxt -import-config pingtxt.json [-import-mode replace]   # poi riavvia l'hub
```

Arrivi da Uptime Kuma? Vedi [importare da Uptime Kuma](monitors.md#importare-da-uptime-kuma).

## Configurazione come codice

Tieni monitor, gruppi, tag, canali di notifica, finestre di manutenzione e status page in un file YAML, in git, e
lascia che l'hub lo segua. A differenza dell'[export](#export-e-import-della-configurazione), il file è pensato per
essere scritto a mano: gli oggetti si richiamano per nome, i valori predefiniti sono omessi, e applicarlo rende l'hub
uguale al file. **impostazioni → dati → configurazione come codice** scarica la configurazione attuale come
`pingtxt.yaml`, un buon punto di partenza, e ha un editor per pianificare e applicare un file.

```yaml
version: 1
groups:
  - name: production
  - name: api
    parent: production
tags:
  - {name: critical, color: red}
channels:
  - name: ops
    type: slack
    config: {url: "${SLACK_WEBHOOK}"}   # vedi sotto: dall'ambiente dell'hub
    groups: [production]
monitors:
  - name: website
    type: http
    target: https://example.com
    group: production
    tags: [critical]
    locations: [local, frankfurt]       # "local" è l'hub, gli altri nomi di agent
  - name: api health
    type: keyword
    target: https://api.example.com/health
    keyword: ok
    interval_sec: 30
    group: api
    depends_on: [website]
    channels:
      - {channel: ops, events: [down, up]}
  - name: nightly backup
    type: push
    interval_sec: 86400
maintenances:
  - name: sunday window
    kind: weekly
    weekdays: [0]
    start_time: "03:00"
    duration_min: 60
    groups: [production]
status_pages:
  - slug: status
    title: Example status
    groups: [production]
```

Ogni oggetto accetta i campi del suo oggetto della [REST API](integrations.md#rest-api) (`Monitor`, `Notifier`,
`Maintenance`, `StatusPage`) senza id; un campo omesso prende il valore predefinito, e per i monitor questo include i
**valori predefiniti dei nuovi monitor** dell'hub. I riferimenti sono nomi: i monitor hanno `group`, `tags`,
`depends_on`, `locations` e `channels` (un nome, oppure `channel` con `events`), i canali `groups` e `tags`, le finestre
di manutenzione `monitors`, `groups` e `tags`, le status page `monitors`, `groups` e `subscribe_channel`, i gruppi
`parent`. Un errore di battitura in un campo o un nome sconosciuto è un errore, e un file con errori non cambia niente.

Come viene applicato un file:

- Gli oggetti vengono abbinati per nome: monitor e canali per tipo e nome, status page per slug. Rinominarne uno crea
  un nuovo oggetto (e, con prune, elimina il vecchio con il suo storico).
- Vengono toccate solo le sezioni presenti nel file: un file con solo `monitors:` lascia stare canali e status page.
- Con **prune** gli oggetti di quelle sezioni che il file non elenca vengono eliminati. Senza, restano.
- I segreti omessi da un canale (URL di Slack e Discord, token, password) mantengono il valore salvato nell'hub, quindi
  non serve metterli in git. Un canale nuovo senza il suo segreto è un errore.
- URL degli heartbeat, token degli agent, password delle status page e storico non sono mai nel file e non cambiano.
- Gli agent non sono nel file: hanno dei token. Aggiungili dall'interfaccia e richiamali per nome in `locations`.
- La sezione facoltativa `settings:` accetta `general`, `agent_updates`, `backups` e `auth` (impostazioni di accesso);
  le chiavi omesse mantengono il loro valore. Le impostazioni di accesso non si possono cambiare con un token API.
- Con SQLite, prima di ogni applicazione che cambia qualcosa viene fatto un backup del database.

### Dalla CI

Pianifica e applica tramite l'API con un [token API](integrations.md#token-api) di un admin. Il piano non cambia
niente, quindi per controllare una pull request basta un token in sola lettura; per applicare serve un token di
scrittura.

```sh
# cosa cambierebbe (gli errori sono in "errors")
curl -sf -H "Authorization: Bearer $PINGTXT_TOKEN" -H "Content-Type: application/yaml" --data-binary @pingtxt.yaml \
  "https://status.example.com/api/config/code/plan?prune=1"
# applicalo: 422 con il piano quando il file ha errori
curl -sf -H "Authorization: Bearer $PINGTXT_TOKEN" -H "Content-Type: application/yaml" --data-binary @pingtxt.yaml \
  "https://status.example.com/api/config/code/apply?prune=1"
# la configurazione attuale (senza segreti con un token)
curl -sf -H "Authorization: Bearer $PINGTXT_TOKEN" https://status.example.com/api/config/code > pingtxt.yaml
```

Metti i segreti nell'archivio segreti della CI e inseriscili prima dell'invio, per esempio con
`envsubst < pingtxt.yaml`. Il server MCP offre lo stesso con `get_configuration_code`, `plan_configuration_code` e
`apply_configuration_code`.

### Da un file accanto all'hub

Imposta `PINGTXT_CONFIG_FILE` su un file YAML (montato nel container, per esempio da un checkout git) e l'hub lo
applica all'avvio ed entro 30 secondi da ogni modifica. `${NOME}` nel file viene sostituito dalla variabile d'ambiente
`NOME` dell'hub, così i segreti possono restare fuori dal file; una variabile non impostata blocca l'applicazione. Con
`PINGTXT_CONFIG_PRUNE=true` quello che il file non elenca viene eliminato.

Finché un file gestisce l'hub, **impostazioni → dati** lo dice, con l'ora dell'ultima applicazione e l'ultimo errore,
e applicare un altro file dall'interfaccia o dall'API viene rifiutato. Le modifiche fatte dall'interfaccia restano fino
alla prossima modifica del file; **applica il file ora** riporta subito l'hub com'è nel file. Ogni applicazione che
cambia qualcosa è nel [registro attività](security.md#registro-attività).

## Statistiche d'uso

Dopo l'installazione, agli admin viene chiesto una volta se l'hub può inviare statistiche d'uso anonime. Non viene
inviato nulla finché qualcuno non dice sì, e la risposta si può cambiare in qualsiasi momento in
**impostazioni → generali**, che mostra anche il prossimo report esatto. Una volta all'ora l'hub invia a
`telemetry.pingtxt.com` solo:

- `id`: un id casuale creato la prima volta che si attivano le statistiche. Non cambia mai e non dice nulla dell'hub;
- `version`: la versione di pingtxt;
- `monitors`: quanti monitor non sono in pausa;
- `agents`: quanti agent sono collegati.

Nessun nome, destinazione, indirizzo, risultato o email, e il servizio non salva indirizzi IP. Avvia l'hub con
`PINGTXT_DISABLE_TELEMETRY=true` per disattivare le statistiche per sempre e nascondere la domanda. Le build di
sviluppo non inviano mai nulla. I totali sono pubblici su [telemetry.pingtxt.com](https://telemetry.pingtxt.com/)
(JSON su `/v1/stats`): hub, monitor e agent attivi, versioni in uso e 90 giorni di storico, mai il report di un
singolo hub. Il servizio è open source: [pingtxt-telemetry](https://github.com/federico-it/pingtxt-telemetry).

## Verificare una release

Le release successive alla v0.1.0 sono firmate con [cosign](https://docs.sigstore.dev/) in modalità keyless: la firma
è legata al workflow GitHub Actions che le ha compilate, senza chiavi che possano trapelare. Ogni release ha
`SHA256SUMS` e la sua firma `SHA256SUMS.sigstore.json`; le immagini su ghcr.io sono firmate per digest.

```sh
V=v0.2.0   # la release da verificare
base=https://github.com/federico-it/pingtxt/releases/download/$V
curl -fsSLO $base/SHA256SUMS -O $base/SHA256SUMS.sigstore.json -O $base/pingtxt-agent-linux-amd64

# 1. SHA256SUMS è stato firmato da release.yml per questo tag
cosign verify-blob SHA256SUMS --bundle SHA256SUMS.sigstore.json \
  --certificate-identity "https://github.com/federico-it/pingtxt/.github/workflows/release.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 2. i file scaricati corrispondono
sha256sum --ignore-missing -c SHA256SUMS
```

```sh
# Immagini Docker (tag senza la "v")
cosign verify ghcr.io/federico-it/pingtxt:${V#v} \
  --certificate-identity "https://github.com/federico-it/pingtxt/.github/workflows/docker.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

`:latest` è l'immagine dell'ultimo tag di release, firmata allo stesso modo. Le immagini `:main` sono firmate da
`docker.yml@refs/heads/main`.

## Riga di comando

Il binario dell'hub ha alcune opzioni per ciò che si fa fuori dall'interfaccia web. In Docker eseguile come
`docker exec -u pingtxt pingtxt pingtxt <opzione>`.

| opzione | cosa fa |
| --- | --- |
| `-version` | stampa la versione |
| `-backup <file>` | scrive una copia del database (sicuro mentre l'hub gira) |
| `-restore <file>` | sostituisce il database con un backup (prima ferma l'hub) |
| `-export-config <file>` | scrive la configurazione in JSON (`-` per lo stdout); aggiungi `-export-secrets` per includere i segreti |
| `-import-config <file>` | applica un file di configurazione (`-import-mode merge` o `replace`), poi riavvia l'hub |
| `-enable-password-login` | riattiva l'accesso con password |
| `-reset-2fa <email>` | disattiva l'autenticazione a due fattori di un utente |
| `-make-admin <email>` | dà a un utente il ruolo di admin |
