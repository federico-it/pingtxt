# Monitor

Un monitor controlla una cosa a intervalli regolari, dalle [località](locations.md) che scegli. Si crea con
**nuovo monitor** nella dashboard; **prova** esegue il controllo una volta dalle località scelte, senza salvare.

## Impostazioni comuni

| impostazione | |
| --- | --- |
| intervallo | secondi tra un controllo e l'altro (i valori predefiniti dei nuovi monitor sono in **impostazioni → monitor**) |
| timeout | ms prima che un controllo fallisca |
| tentativi | errori consecutivi prima che una località conti come in errore (da 1 a 10) |
| località | l'hub e gli agent che eseguono il controllo |
| gruppo, tag | vedi [gruppi e tag](#gruppi-e-tag) |
| notifiche | canali collegati a questo monitor uno per uno, vedi [avvisi](alerts.md#quali-monitor-ed-eventi-riceve-ogni-canale) |
| soglia di degradato | quota di località in errore che lo rende degradato, vedi [sotto](#degradato-e-soglia-di-lentezza) |
| soglia di lentezza | spenta, automatica o un valore in ms |
| versione ip | come trattare gli host con IPv4 e IPv6, vedi [IPv6](#ipv6) |
| diagnostica | predefinito dell'istanza, attiva o spenta, vedi [diagnostica](#diagnostica-in-caso-di-errore) |
| avvisi di scadenza | certificato TLS e registrazione del dominio, vedi [avvisi](alerts.md#avvisi-di-scadenza) |

Incolla un comando `curl` nel campo URL di un monitor HTTP per compilare metodo, header e body.

## Tipi di controllo

| tipo | destinazione | cosa viene controllato |
| --- | --- | --- |
| http(s) | `https://example.com/health` | codice di stato (predefinito 200-399), header, body, [regole JSON](#controlli-sulla-risposta-json), scadenza TLS |
| parola chiave | URL + testo | il body della risposta contiene (o non contiene) il testo |
| porta tcp | `host:porta` | la connessione si apre |
| ping | host o IP | echo ICMP, o ICMPv6 |
| dns | nome + tipo di record | il nome si risolve, eventualmente in un valore atteso, con il resolver di sistema o uno indicato |
| postgresql | `postgres://user:pass@host:5432/db?sslmode=disable` | connessione ed esecuzione di una query (predefinita `SELECT 1`); valore atteso facoltativo della prima colonna |
| mysql / mariadb | `mysql://user:pass@host:3306/db` (`?tls=true\|false\|skip-verify`) | come postgresql |
| redis | `redis://[user]:pass@host:6379/0`, `rediss://` per TLS | `AUTH` + `PING` |
| mongodb | `mongodb://user:pass@host:27017/?authSource=admin`, `mongodb+srv://` | `hello`, autenticazione SCRAM-SHA-256/SHA-1, `ping`; mostra primary/secondary |
| container docker | nome o id del container | down se non è in esecuzione, si sta riavviando, è in pausa o `unhealthy` |
| grpc health | `grpc://host:porta[/servizio]`, `grpcs://` per TLS | `grpc.health.v1.Health/Check` risponde `SERVING` |
| smtp | `smtp://host:25`, `smtp://host:587`, `smtps://host:465` | saluto, `EHLO`, `STARTTLS` quando disponibile (certificato verificato) |
| server mcp | `https://mcp.example.com/mcp` | l'handshake MCP e i suoi tool, vedi [server MCP](#server-mcp) |
| heartbeat (push) | nessuna: pingtxt ti dà un URL | il tuo job chiama l'URL; down quando le chiamate smettono, vedi [heartbeat](#monitor-heartbeat-cron-job) |

I monitor HTTP accettano anche metodo, header e body della richiesta, i codici di stato accettati e se ignorare gli
errori del certificato TLS. Gli agent più vecchi di un tipo di controllo segnalano `unknown monitor type`:
aggiornali.

I monitor ping inviano di default **3 richieste echo** per controllo, a 300 ms l'una dall'altra; imposta
**pacchetti per controllo** da 1 a 20 (API `ping_count`). Il controllo è up se torna almeno una risposta, e il
risultato dice quante, ad esempio `9/10 packets, 10% loss`. Il timeout copre tutto l'invio, quindi deve essere almeno
(pacchetti - 1) x 300 + 500 ms. Gli agent più vecchi di questa impostazione inviano 3 pacchetti: aggiornali.

## Controlli sulla risposta (JSON)

I monitor HTTP e con parola chiave possono controllare il body JSON della risposta, per esempio un endpoint di health
che restituisce `{"status": "ready", "checks": {"redis": {"status": "ok", "latencyMs": 3}}}`. Aggiungi le regole in
**controlli sulla risposta**: devono passare tutte, altrimenti il controllo fallisce e il risultato, l'incidente e
l'avviso dicono quale regola non è rispettata, per esempio `checks.redis.status: expected "ok", got "error"`.

| percorso | operatore | valore |
| --- | --- | --- |
| `status` | uguale | `ready` |
| `checks.redis.status` | uguale | `ok` |
| `checks.worker.ageMs` | < | `60000` |
| `items[0].id` | esiste | |

- I percorsi usano punti e parentesi quadre: `a.b`, `items[0]` (o `items.0`, `items[-1]` per l'ultimo),
  `["key.with.dots"]`. È ammesso un `$.` iniziale.
- Operatori: uguale, diverso, contiene (sottostringa, elemento di un array o chiave di un oggetto), non contiene,
  esiste, non esiste, `>`, `>=`, `<`, `<=` (numeri) e corrisponde (espressione regolare).
- I valori si confrontano per tipo: i numeri come numeri (`3` è uguale a `3.0`), `true` / `false` / `null` per nome,
  oggetti e array come JSON.
- Le regole funzionano anche sugli agent. Gli agent più vecchi di questa funzione fanno fallire il controllo
  chiedendo di aggiornarli, invece di farlo passare senza verificarlo.

## Tempo di risposta per fase

I controlli HTTP e con parola chiave misurano dove va il tempo: risoluzione **DNS**, **connessione**, handshake
**TLS**, **attesa** del primo byte (il server al lavoro) e **trasferimento** del body. Nella pagina del monitor passa
il grafico dei tempi di risposta a **per fase**, per località o tutte insieme; passa sopra una latenza nel log dei
controlli per vedere il dettaglio di quel controllo. Con i redirect le fasi di ogni passaggio vengono sommate.

La pagina del monitor mostra anche uptime e percentili del tempo di risposta (p50, p95, p99) per qualsiasi periodo,
a confronto con il periodo precedente. Trascina sul grafico per ingrandire.

## Degradato e soglia di lentezza

Un monitor è **degradato** quando alcune delle sue località falliscono, ma non più della metà. La quota che conta si
imposta in **impostazioni → monitor** (35% di default) e si può cambiare per monitor, fino al 50%. Degradato non apre
incidenti e conta come up per l'uptime; i canali possono riceverlo come evento (spento di default).

Un monitor può diventare degradato anche quando risponde **troppo lentamente**. Imposta la **soglia di lentezza**:

- **spenta** (predefinito);
- **fissa**, in ms (sotto il timeout): una località è lenta quando la mediana dei suoi ultimi 3 controlli riusciti
  la supera, quindi un singolo picco non cambia nulla;
- **automatica**: ogni località ha la sua soglia, 3 volte il suo tempo di risposta abituale e almeno 100 ms in più.
  L'abituale è la mediana delle sue medie orarie degli ultimi 7 giorni, così un agent lontano ha il suo riferimento.
  Parte quando la località ha 24 ore di storico.

Le località lente contano come quelle in errore per la quota di degradato, mai per il down. Con una sola località
basta che sia lenta. Il grafico dei tempi di risposta disegna la soglia come linea tratteggiata, e l'avviso dice quale
località era lenta e di quanto. Per i monitor heartbeat la soglia vale sulla durata `ping=`.

## IPv6

Ogni tipo di controllo funziona su IPv6, e gli host con soli record AAAA funzionano senza fare nulla. Scrivi gli
indirizzi IPv6 tra parentesi quadre quando segue una porta: `[2001:db8::1]:443` (tcp), `https://[2001:db8::1]/`,
`redis://[2001:db8::1]:6379`, `grpc://[2001:db8::1]:50051`. Ping e dns vogliono l'indirizzo senza parentesi
(`2001:db8::1`); il ping usa ICMPv6.

**versione ip** stabilisce come un controllo tratta un host con IPv4 e IPv6:

| opzione | API `ip_family` | il controllo |
| --- | --- | --- |
| predefinito dell'istanza | `0` | usa il valore impostato in **impostazioni → monitor** |
| IPv4 o IPv6 | `1` | una connessione, con la famiglia che risponde per prima: un IPv6 rotto passa inosservato |
| solo IPv4 / solo IPv6 | `4` / `6` | solo quella famiglia |
| IPv4 e IPv6 | `46` | entrambe, in parallelo: fallisce se una delle due fallisce (il messaggio dice quale), la latenza è la più lenta |

L'hub e ogni agent verificano se raggiungono Internet via IPv6; la pagina degli agent e il modulo del monitor
mostrano il risultato. Una località senza IPv6 non si può scegliere per *solo IPv6* o *IPv4 e IPv6*; lì *IPv4 o IPv6*
e il predefinito dell'istanza controllano via IPv4. La scadenza del certificato TLS si legge con l'una o l'altra
famiglia. Per Docker, vedi [IPv6 in Docker](install.md#ipv6).

## Database, container, gRPC e SMTP

Le password negli URL di connessione sono nascoste nell'elenco dei monitor, agli utenti in sola lettura e negli
avvisi; usa un utente del database in sola lettura. PostgreSQL, MySQL e MongoDB usano TLS quando il server lo offre.

I controlli Docker parlano con il Docker Engine della località che li esegue: l'hub o l'agent ha bisogno di
`/var/run/docker.sock` (o `DOCKER_HOST`). Con Docker, aggiungi `-v /var/run/docker.sock:/var/run/docker.sock:ro` al
container dell'hub o dell'agent (l'entrypoint lo rende usabile all'utente `pingtxt`). Per un agent installato con
systemd, permettilo con `sudo systemctl edit pingtxt-agent`:

```ini
[Service]
SupplementaryGroups=docker
```

L'accesso al socket Docker equivale a root su quella macchina (`:ro` non limita l'API). pingtxt legge solo
`GET /containers/<nome>/json`, ma attivalo solo dove ti servono i controlli sui container.

## Server MCP

Un monitor di tipo **server mcp** controlla un server [Model Context Protocol](https://modelcontextprotocol.io)
remoto. (Per far usare pingtxt stesso agli assistenti AI, vedi il [server MCP dell'hub](integrations.md#server-mcp).)

| impostazione | |
| --- | --- |
| destinazione | URL dell'endpoint, per esempio `https://mcp.example.com/mcp` |
| trasporto | `auto` (predefinito): streamable HTTP, con ripiego sul vecchio trasporto HTTP+SSE (2024-11-05) quando la POST viene rifiutata con 400/404/405/406/415; oppure forza `streamable` / `sse` |
| header | inviati con ogni richiesta, per esempio `Authorization: Bearer <token>`; un 401 o 403 manda il monitor down. Nascosti agli utenti in sola lettura |
| tool richiesti | facoltativi, separati da virgola: down se uno di loro non è in `tools/list` |
| tool minimi | facoltativo: down se `tools/list` ne restituisce meno |

Ogni controllo esegue `initialize`, invia `notifications/initialized`, poi `tools/list` (tutte le pagine) quando il
server dichiara dei tool o è impostata una condizione sui tool, e infine chiude la sessione. I tool non vengono mai
chiamati. Il risultato mostra nome e versione del server, versione del protocollo, trasporto e numero di tool, per
esempio `my-server 1.4.0 · mcp 2025-06-18 · streamable · 12 tools`. Gli endpoint https ricevono gli avvisi di
scadenza del certificato.

## Monitor heartbeat (cron job)

Per i job che girano a orari fissi (backup, cron, code), crea un monitor di tipo **heartbeat (push)**. Riceve un URL
segreto; il job lo chiama alla fine di ogni esecuzione, con qualsiasi metodo HTTP:

```sh
0 3 * * * /usr/local/bin/backup.sh && curl -fsS -m 10 --retry 3 https://status.example.com/api/push/<token> >/dev/null
```

Il monitor va down quando non arriva nessuna chiamata entro il **periodo atteso** più la **tolleranza**, e torna up
alla chiamata successiva. Parametri facoltativi nella query: `status=down` segnala un'esecuzione fallita, `msg=` salva
una breve nota, `ping=` la durata dell'esecuzione in ms. L'URL usa l'URL pubblico delle impostazioni e si può
rigenerare dalla pagina del monitor.

## Gruppi e tag

Un monitor sta in un **gruppo** (o in nessuno) e ha quanti **tag** colorati vuoi. Si gestiscono nella pagina
**gruppi**. Poi puoi:

- filtrare la dashboard per gruppo o tag, e mostrare i gruppi come sezioni;
- aggiungere interi gruppi a una [status page](status-pages.md): anche i monitor aggiunti al gruppo in seguito
  compaiono;
- scegliere i monitor per gruppo o tag nelle [finestre di manutenzione](alerts.md#finestre-di-manutenzione) e nei
  [canali di notifica](alerts.md#quali-monitor-ed-eventi-riceve-ogni-canale);
- filtrare l'API: `GET /api/monitors?group=<id>` (o `group=none`) e `?tag=<id>`.

## Diagnostica in caso di errore

Al primo errore di un controllo, la località che l'ha eseguito raccoglie subito una diagnostica e la invia insieme al
risultato fallito, così è pronta quando parte l'avviso:

- **DNS**: le risposte A/AAAA (e CNAME) del resolver di sistema; per i monitor DNS anche la risposta del loro server e
  i record NS del nome;
- **TCP**: una connessione a ogni indirizzo risolto (fino a 4) sulla porta del monitor;
- **TLS**: versione, soggetto del certificato, emittente, nomi, scadenza e l'errore di verifica;
- **HTTP**: stato, redirect (senza query string) e header della risposta. Cookie, `Authorization` e qualsiasi header
  il cui nome contiene token, key, secret, session, csrf o auth vengono nascosti. Il body e gli header della richiesta
  non vengono mai conservati;
- **traceroute**: fino a 20 hop con i nomi inversi, come SYN TCP sulla porta del monitor (UDP per ping e DNS). Non
  serve root: su Linux legge gli errori ICMP come `tracepath`, quindi funziona nei container senza root. Gli altri
  sistemi per ora lo saltano.

Richiede al massimo 8 secondi e 16 KB, e parte al massimo una volta ogni 10 minuti per monitor e località. Il primo
avviso di un incidente elenca una riga per ogni località in errore e rimanda alla diagnostica completa nella pagina
incidenti; i webhook ricevono `diagnostics` e `diagnostics_url`, i promemoria solo il link. La diagnostica non compare
mai nelle status page. È anche nell'API (`GET /api/incidents/{id}/diagnostics`) e nel tool MCP
`get_incident_diagnostics`.

Si disattiva in **impostazioni → monitor**, o per singolo monitor. La diagnostica degli incidenti si conserva 30
giorni (configurabile); quella degli errori che non sono diventati incidenti, un giorno.

## Importare da Uptime Kuma

**impostazioni → dati → importa da uptime kuma** legge un file di backup di Uptime Kuma 1.x (lì: Settings → Backup →
Export) e mostra cosa verrà creato prima di cambiare qualsiasi cosa. Gruppi e tag vengono importati e i monitor in
pausa restano in pausa; i canali di notifica non vengono importati.
