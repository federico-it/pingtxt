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
| http(s) | `https://example.com/health` | codice di stato (predefinito 200-399), header, body, [regole JSON](#controlli-sulla-risposta-json), scadenza TLS; [passi prima](#controlli-http-a-passi) facoltativi come un login |
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
| udp | `host:porta` | un datagramma riceve risposta, eventualmente con un testo o dei byte, vedi [UDP](#udp) |
| websocket | `wss://example.com/socket` | la connessione si apre; se vuoi un messaggio riceve la risposta attesa, vedi [WebSocket](#websocket) |
| status page (atlassian) | `https://status.example.com` | lo stato di una pagina Atlassian Statuspage o di alcuni suoi componenti; i problemi minori sono degradato, vedi [Status page di altri servizi](#status-page-di-altri-servizi) |
| ssh | `host` o `host:porta` | il banner SSH e la chiave host, eventualmente fissata, vedi [SSH](#ssh) |
| imap, pop3 | `imaps://host`, `pop3s://host`, `imap://utente:pass@host` | saluto, `STARTTLS`/`STLS` quando disponibile, login facoltativo, vedi [Server di posta](#server-di-posta-imap-pop3) |
| ntp | `host` o `host:porta` | il server risponde, è sincronizzato e il suo orologio è vicino a quello della posizione, vedi [NTP](#ntp) |
| blacklist (rbl) | indirizzo IP o dominio di posta | non è nelle blacklist DNS, vedi [Blacklist](#blacklist-rbl) |
| heartbeat (push) | nessuna: pingtxt ti dà un URL | il tuo job chiama l'URL; down quando le chiamate smettono, vedi [heartbeat](#monitor-heartbeat-cron-job) |

I monitor HTTP accettano anche metodo, header e body della richiesta, i codici di stato accettati e se ignorare gli
errori del certificato TLS. Gli agent più vecchi di un tipo di controllo ne falliscono i controlli con "this agent
cannot run ... checks: update it".

I monitor ping inviano di default **3 richieste echo** per controllo, a 300 ms l'una dall'altra; imposta
**pacchetti per controllo** da 1 a 20 (API `ping_count`). Il controllo è up se torna almeno una risposta, e il
risultato dice quante, ad esempio `9/10 packets, 10% loss`. Il timeout copre tutto l'invio, quindi deve essere almeno
(pacchetti - 1) x 300 + 500 ms. Gli agent più vecchi di questa impostazione inviano 3 pacchetti: aggiornali.

Per essere più severi imposta il **limite perdita pacchetti** (API `ping_loss_pct`): il controllo fallisce se si perde
almeno quella quota di pacchetti, ad esempio con 50 fallisce `5/10 packets, 50% loss`. 0 (default) fallisce solo se
si perdono tutti. Gli agent più vecchi di questa impostazione falliscono il controllo con "update it" invece di
ignorare il limite.

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

## Controlli HTTP a passi

Un monitor http(s) o parola chiave può fare prima altre richieste, in **passi prima della richiesta** (API `steps`):
fare il login, prendere il token dalla risposta, poi controllare un endpoint protetto. I passi vengono eseguiti in
ordine, al massimo 10, e la richiesta del monitor viene fatta per ultima.

Ogni passo ha metodo, URL, header, body e codici di stato accettati (predefinito 200-399), e può **salvare valori**
dalla risposta come variabili:

| da | percorso | esempio |
| --- | --- | --- |
| percorso json | un percorso nel body JSON, come nei [controlli sulla risposta](#controlli-sulla-risposta-json) | `data.token` |
| header | un header della risposta | `X-Csrf-Token` |
| regex | un'espressione regolare sul body: il primo gruppo, o tutta la corrispondenza | `name="csrf" value="([^"]+)"` |

Scrivi `{{token}}` per usare una variabile nell'URL, negli header o nel body dei passi successivi e della richiesta
del monitor, per esempio `Authorization: Bearer {{token}}`. I cookie impostati da una risposta vengono inviati con le
richieste successive, quindi i login a sessione funzionano senza variabili.

Il controllo fallisce al primo passo che fallisce, e il messaggio lo indica: `step 1 (login): unexpected status 401`
o `step 2: {{token}}: data.token not found`. La latenza comprende tutte le richieste. Header e body dei passi
(password) sono nascosti agli utenti in sola lettura. Gli agent più vecchi dei passi falliscono il controllo con
"update it" invece di saltare il login.

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

Finché un monitor è degradato o down, la sua pagina dice perché in alto, e la riga nella dashboard mostra il motivo al
posto del target. Le località che riportano lo stesso problema sono elencate insieme, su una riga: vale anche per la
causa di incidenti e avvisi (`agent-1, agent-2: HTTP 503`). Nell'API i motivi sono in `reasons` di ogni monitor.

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

## UDP

Un monitor **udp** invia un **payload** a `host:porta` e attende la risposta. UDP non ha una connessione da aprire,
quindi un servizio che tace non si distingue da uno fermo: il controllo passa solo se arriva una risposta entro il
timeout. Invia qualcosa a cui il servizio risponde, per esempio una query DNS, una richiesta NTP o la query di stato
del tuo server di gioco.

- **scritto come** (API `method`): `text` (predefinito) o `hex`. In esadecimale i byte possono essere separati da
  spazi o due punti: `1b 00 00 00`.
- **risposta attesa** (API `keyword`): testo o byte che la risposta deve contenere, nella stessa codifica. Vuota:
  va bene qualsiasi risposta.

Il risultato è `reply 48 bytes`, `no reply (timeout)` oppure `port unreachable` quando l'host risponde che sulla
porta non c'è nessuno in ascolto. La diagnostica in caso di errore risolve il nome e traccia il percorso (non c'è una
connessione TCP da provare).

## WebSocket

Un monitor **websocket** apre una connessione `ws://` o `wss://`; un upgrade rifiutato fallisce con il suo stato
HTTP, per esempio `unexpected status 401 (no WebSocket upgrade)`.

- **messaggio da inviare** (API `body`): messaggio di testo facoltativo inviato dopo la connessione.
- **risposta attesa** (API `keyword`): testo che il primo messaggio del server deve contenere. Con un messaggio o una
  risposta attesa impostati, il controllo attende quel primo messaggio.
- **header**: inviati con l'handshake, per esempio `Authorization: Bearer <token>`. `Sec-WebSocket-Protocol: chat, v2`
  chiede dei sottoprotocolli; quello scelto dal server compare nel risultato, per esempio `connected (chat)`.

Gli endpoint `wss://` ricevono gli avvisi di scadenza del certificato.

## SSH

Un monitor **ssh** si collega a `host` (porta 22) o `host:porta`, legge il banner di versione del server e fa lo
scambio di chiavi fino alla chiave host del server. Non fa mai login, quindi non serve un account. Il risultato
mostra il banner e la chiave host, per esempio `SSH-2.0-OpenSSH_9.6p1, ssh-ed25519 SHA256:…`.

**banner o chiave host attesi** (API `keyword`) è facoltativo:

- un testo, per esempio `OpenSSH`: il banner deve contenerlo;
- l'impronta di una chiave host `SHA256:…`, come la stampa `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` o la
  mostra un controllo riuscito: il controllo fallisce con `host key changed` quando nessuna chiave del server ce
  l'ha. Fissare la chiave ti avvisa quando un server è stato reinstallato, o quando al suo indirizzo risponde
  qualcos'altro.

## Server di posta (IMAP, POP3)

I monitor **imap** e **pop3** controllano un server di caselle di posta come **smtp** controlla un server di invio:

- `imaps://host` (993) e `pop3s://host` (995) usano TLS da subito; `imap://host` (143) e `pop3://host` (110)
  leggono il saluto e passano a TLS con `STARTTLS` (`STLS` per POP3) quando il server lo offre. Un semplice
  `host[:porta]` usa lo schema senza TLS. Il certificato viene verificato, a meno che **ignora gli errori del
  certificato TLS** sia attivo.
- Con utente e password nell'URL, per esempio `imaps://monitor%40example.com:segreta@mail.example.com`, il controllo
  fa anche login (`LOGIN`, oppure `USER`/`PASS`) e fallisce con una password sbagliata o un account bloccato. Non
  invia mai la password senza TLS. Usa una casella creata per il monitoraggio: gli admin vedono la password, gli
  utenti in sola lettura no.

Il risultato è per esempio `tls, logged in, * OK Dovecot ready.`.

## NTP

Un monitor **ntp** chiede l'ora a un server NTP (SNTP) su `host` (porta 123) o `host:porta`. Fallisce quando il
server non risponde, risponde di non essere sincronizzato (stratum 16 o indicatore di allarme), rifiuta la richiesta
(un "kiss-o'-death", per esempio `RATE`), oppure quando il suo orologio si discosta da quello della posizione che fa
il controllo più dello **sfasamento massimo** (API `expected_status`, in ms, predefinito 1000). Il risultato è per
esempio `stratum 2, offset +1.3 ms`, e il tempo di risposta è il viaggio di andata e ritorno in rete.

Lo sfasamento si misura rispetto all'orologio della posizione: tieni sincronizzati gli orologi dell'hub e degli
agent, oppure alza il limite.

## Blacklist (RBL)

Un monitor **blacklist (rbl)** cerca una destinazione nelle blacklist DNS, le liste che i server di posta usano per
rifiutare lo spam. È giù finché la destinazione è in lista, e il messaggio nomina le liste, per esempio
`listed: 203.0.113.7 on zen.spamhaus.org (127.0.0.2)`.

- Un **indirizzo IP** (IPv4 o IPv6) viene controllato nelle liste di IP.
- Un **dominio di posta** viene controllato nelle liste di domini, e gli indirizzi dei suoi server MX (o del dominio
  stesso se non ha record MX) nelle liste di IP. Con una famiglia IP impostata si controllano solo gli indirizzi di
  quella famiglia.
- **blacklist** (API `keyword`): le zone da interrogare, separate da virgole. Vuoto usa quelle predefinite:
  Spamhaus ZEN, SpamCop, Barracuda, Mailspike, PSBL, Manitu, DroneBL e s5h per gli indirizzi, Spamhaus DBL, SURBL e
  URIBL per i domini. Le zone note per elencare domini vanno ai controlli dei domini, tutte le altre a quelli degli
  indirizzi.
- **resolver** (API `dns_server`): il server DNS da interrogare. Le blacklist rifiutano le richieste dai grandi
  resolver pubblici come 8.8.8.8 o 1.1.1.1; quando nessuna lista risponde il controllo fallisce con
  `no blacklist answered`. Fai il controllo da una posizione con un resolver suo, o imposta quello del provider. Le
  risposte che significano "richiesta rifiutata" (127.0.0.1, 127.255.255.x) non contano mai come presenza in lista.

Controlla ogni ora o meno spesso: le liste cambiano lentamente e alcune limitano le richieste gratuite.

## Certificati client (mutual TLS)

I monitor **http**, **parola chiave**, **websocket**, **grpc** e **mcp** possono presentare un certificato client ai
server che lo chiedono (mutual TLS). Clicca **aggiungi certificato client (mTLS)** e incolla il certificato (PEM, può
seguire la catena) e la sua chiave privata (PEM, non cifrata); l'hub controlla che corrispondano prima di salvare (API
`client_cert` e `client_key`). Un certificato scaduto fa fallire il controllo con la sua data. La chiave è nascosta
agli utenti in sola lettura; gli altri tipi ignorano il certificato. Gli agent più vecchi di questa impostazione
falliscono il controllo con "this agent cannot send client certificates: update it".

## Status page di altri servizi

Molti servizi pubblicano il loro stato su una pagina **Atlassian Statuspage**, per esempio `status.claude.com`,
`www.githubstatus.com` o `www.cloudflarestatus.com`. Un monitor **status page (atlassian)** legge la sua API pubblica
(`/api/v2/summary.json`) e segue quello che riporta il fornitore:

| stato della pagina o del componente | monitor |
| --- | --- |
| tutto operativo | su |
| problema minore: prestazioni ridotte o interruzione parziale | degradato |
| problema grave o critico: interruzione totale | giù |
| manutenzione in corso | su, con la manutenzione nel messaggio |

Il messaggio del controllo riporta gli incidenti aperti, per esempio `Minor Service Outage · Incident: Elevated
errors on Claude Opus (identified)`, e lo stesso fanno gli avvisi. Degradato invia l'evento **degradato** dei canali,
non un avviso di monitor giù.

- **destinazione**: l'indirizzo della pagina. Va bene qualsiasi URL del sito:
  `https://status.claude.com/api/v2/status.json` viene salvato come `https://status.claude.com`.
- **componenti da seguire** (API `keyword`, id dei componenti separati da virgole): il form elenca i componenti della
  pagina da spuntare. Se non ne scegli nessuno conta lo stato generale della pagina; se ne scegli alcuni contano solo
  il loro stato e i loro incidenti, così un problema di claude.ai non tocca un monitor che segue la Claude API.
- **località**: la pagina dà la stessa risposta ovunque, quindi un nuovo monitor di questo tipo la controlla solo dal
  hub, e quando aggiungi un agent non viene proposto. Altre località ripetono solo la stessa richiesta.

Quando scrivi l'URL di una pagina di questo tipo in un monitor **http(s)** o **parola chiave**, il form propone di
passare a questo tipo con un clic. Prima si usava una regola JSON come `status.indicator` uguale a `none`, che manda
il monitor giù al primo incidente minore.

Per questo tipo non ci sono diagnostica né avvisi di scadenza del certificato o del dominio: la pagina è del
fornitore.

### Servizi popolari

**monitor → servizi** elenca circa 70 servizi popolari che pubblicano una pagina di questo tipo, per categoria: AI
(Claude, OpenAI, Mistral...), strumenti per sviluppatori (GitHub, npm, Docker Hub...), cloud e hosting (Cloudflare,
Vercel, DigitalOcean...), osservabilità, email e messaggi, produttività, commercio e social. Cerca per nome o prodotto
(`chatgpt`, `copilot`), spunta quelli da cui dipendi e premi **aggiungi**: ognuno diventa un monitor status page con il
nome del servizio, controllato dall'hub ogni 2 minuti (1 o 5 se lo scegli), nel gruppo che scegli (di base un nuovo
gruppo chiamato **servizi**). I servizi già seguiti da un monitor status page mostrano **già seguito** e portano al
monitor. I loghi vengono da [Dashboard Icons](https://dashboardicons.com) e sono inclusi nell'hub, quindi la pagina
non carica nulla dall'esterno.

Viene seguita l'intera pagina. Per seguire solo alcuni componenti modifica il monitor dopo averlo aggiunto. Un servizio
che non è nell'elenco funziona allo stesso modo con un monitor personalizzato di questo tipo, se ha una pagina
Atlassian Statuspage.

API: `GET /api/monitors/presets` (il catalogo, con `monitor_id` del monitor che segue già ogni servizio) e
`POST /api/monitors/presets` con `{"ids": ["claude", "github"], "group_name": "servizi"}`. MCP:
`list_service_presets` e `add_service_presets`.

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
- filtrare l'API: `GET /api/monitors?group=<id>` (o `group=none`) e `?tag=<id>`; aggiungi `include_subgroups=true`
  per avere anche i monitor dei sottogruppi.

Un gruppo può stare dentro un altro (**dentro il gruppo** nella finestra del gruppo), fino a 3 livelli, per esempio
*progetto* con *api*, *sito* e *database*. I sottogruppi compaiono annidati sotto il loro gruppo nella dashboard e
nella pagina dei gruppi, e come *progetto › api* nelle status page, nelle scelte e nei report. L'intestazione di un
gruppo riassume anche i suoi sottogruppi, e scegliere un gruppo per una status page, una manutenzione, un canale o
un report include i suoi sottogruppi. Eliminando un gruppo i suoi sottogruppi salgono di un livello.

Nella dashboard una linea a sinistra collega ogni sottogruppo e i suoi monitor al gruppo sopra, e **comprimi tutti** /
**espandi tutti** accanto al filtro dei gruppi chiude o apre tutte le sezioni. Il filtro **gruppo** è un menu con i
gruppi ad albero e quanti monitor contiene ognuno; scegliendo un gruppo compaiono anche i suoi sottogruppi. Nella
pagina dei gruppi **+ sottogruppo** crea un gruppo dentro quello, la freccia comprime i suoi sottogruppi, e il numero
di monitor apre la dashboard filtrata su quel gruppo.

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
  sistemi per ora lo saltano;
- **saluto SMTP**: per i monitor SMTP, la prima riga che invia il server (es. `421 too many connections`).

I monitor Docker e push non hanno diagnostica: non c'è un percorso di rete da esaminare.

### Leggere la diagnosi

L'hub legge i dati e mette in cima il risultato in parole semplici: la **diagnosi**, gli altri problemi trovati e
l'errore del controllo. Per esempio:

- il nome non esiste (NXDOMAIN), oppure il server DNS di un monitor DNS risponde diversamente dal resolver di sistema;
- la porta rifiuta la connessione (nessun servizio in ascolto) o non risponde (un firewall o un host spento), con l'hop
  dove si ferma il percorso; una porta che funziona in IPv4 ma non in IPv6;
- il certificato è scaduto, non è valido per il nome o non è attendibile (autofirmato o senza certificato intermedio);
- lo stato HTTP spiegato: 502/504 da un proxy senza niente dietro, gli errori 52x di Cloudflare (il problema è
  sull'origine), 503 e 429 con il loro `Retry-After`, 401/403 accesso negato;
- un host raggiungibile che non risponde al ping (ICMP bloccato), o un server di posta che saluta con un errore;
- quando DNS, connessione e TLS funzionano, che il problema è nel servizio stesso.

Sotto, ogni passaggio (DNS, connessione, TLS, risposta HTTP, percorso) ha una riga con ✓ o ✗; un clic sul passaggio
mostra i dati grezzi (indirizzi, nomi del certificato, header della risposta, gli hop del traceroute). **Copia come
testo** copia tutto, diagnosi compresa, per un ticket.

Richiede al massimo 8 secondi e 16 KB, e parte al massimo una volta ogni 10 minuti per monitor e località. Il primo
avviso di un incidente elenca una riga per ogni località in errore, che inizia con la diagnosi, e rimanda alla diagnostica completa nella pagina
incidenti; i webhook ricevono `diagnostics` e `diagnostics_url`, i promemoria solo il link. La diagnostica non compare
mai nelle status page. È anche nell'API (`GET /api/incidents/{id}/diagnostics`) e nel tool MCP
`get_incident_diagnostics`.

Si disattiva in **impostazioni → monitor**, o per singolo monitor. La diagnostica degli incidenti si conserva 30
giorni (configurabile); quella degli errori che non sono diventati incidenti, un giorno.

## Importare da Uptime Kuma

**impostazioni → dati → importa da uptime kuma** legge un file di backup di Uptime Kuma 1.x (lì: Settings → Backup →
Export) e mostra cosa verrà creato prima di cambiare qualsiasi cosa. Gruppi e tag vengono importati e i monitor in
pausa restano in pausa; i canali di notifica non vengono importati.
