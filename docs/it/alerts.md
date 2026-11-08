# Avvisi e notifiche

Gli avvisi partono attraverso i **canali**, che si aggiungono nella pagina **notifiche**. Ogni canale sceglie quali
monitor ed eventi riceve, e può aspettare o ripetere. **invia test** prova un canale prima di salvarlo.

## Canali

| tipo | cosa serve |
| --- | --- |
| webhook (JSON) | un URL; pingtxt invia in POST un body JSON (campi [sotto](#contenuto-dei-webhook)) |
| Discord, Slack | l'URL del webhook in ingresso del canale |
| Telegram | il token di un bot e l'id della chat |
| Microsoft Teams | l'URL di un workflow di Teams: nel canale, **⋯ → Workflow → Post to a channel when a webhook request is received** (il nome del modello cambia con la lingua di Teams). Funzionano anche i vecchi URL dei connettori (`webhook.office.com`). Gli avvisi sono Adaptive Card |
| Matrix | l'URL dell'homeserver, l'access token di un account bot e la stanza (`!id:server` o `#alias:server`). Invita il bot nella stanza e accetta prima l'invito |
| ntfy | l'URL del topic, e un access token per i topic protetti |
| Gotify | l'URL del server e il token di un'applicazione Gotify. I problemi hanno priorità 8 (l'app Android suona), degradato 5, i ripristini 2 |
| Pushover | la tua chiave utente (o di gruppo) e il token di un'applicazione Pushover; facoltativo un solo dispositivo. I problemi partono con priorità alta |
| PagerDuty | l'integration key Events API v2 di un servizio, e la regione (USA o UE). Un down apre un incidente, i promemoria si uniscono a quello e il ripristino lo risolve; lo stesso per degradato, scadenze e agent |
| Opsgenie | la chiave di un'integrazione API, e la regione (USA o UE). Gli avvisi hanno P1 (down, agent offline), P2 (scaduto) o P3 (in scadenza, degradato) e si chiudono da soli al ripristino |
| SMS (Twilio) | account SID e auth token, il mittente (un numero Twilio o il SID di un messaging service `MG...`) e i numeri in formato internazionale (`+39...`). Ogni avviso è un SMS breve per numero, pagato sul tuo account Twilio |
| email | un server SMTP (STARTTLS sulla 587, TLS sulla 465, o nessuno per un relay locale sulla 25), oppure l'API di [Resend](https://resend.com) con una chiave di un dominio verificato; poi mittente e destinatari |
| app pingtxt (push) | le notifiche push attive in **impostazioni → integrazioni → app mobile**; gli utenti (email) i cui telefoni ricevono gli avvisi, vuoto per tutti. Vedi [App iOS](ios-app.md) |

I link negli avvisi (Teams, Pushover, Gotify, Matrix, PagerDuty, Opsgenie) portano alla diagnostica, o al monitor
quando l'URL pubblico è impostato nelle **impostazioni**. Un test inviato a PagerDuty o Opsgenie apre un incidente e lo
risolve subito.

Ogni avviso dice quale monitor, cosa è successo e, per gli avvisi di down, la causa per località e un link alla
[diagnostica](monitors.md#diagnostica-in-caso-di-errore). Le email usano la stessa grafica dell'interfaccia web, in
chiaro e scuro.

**Notifiche desktop**: nella pagina notifiche ognuno può attivare anche le notifiche del browser. Finché una scheda di
pingtxt è aperta, anche in background, quel browser avvisa quando un monitor va down o torna up. Non viene inviato
nulla ad altri.

## Quali monitor ed eventi riceve ogni canale

Ogni canale ha un **ambito** e degli **eventi**:

- **ambito**: *tutti i monitor* (anche quelli aggiunti in seguito), oppure i monitor di alcuni **gruppi** e **tag**,
  oppure nessuno (solo i monitor collegati uno per uno).
- **eventi**: `down` (con i suoi promemoria), `up` (ripristino, solo ai canali che hanno ricevuto l'avviso),
  `degraded` (alcune località falliscono o sono lente, e quando finisce; spento di default), `cert_expiry`,
  `domain_expiry` e `agent_offline` ([avvisi di agent offline](locations.md#avvisi-di-agent-offline): riguarda gli
  agent, quindi ignora l'ambito e i collegamenti dei monitor).

Un monitor si può anche collegare a un canale **uno per uno**, dal canale (il suo elenco di monitor) o dal modulo del
monitor (*notifiche*). Il collegamento ha eventi propri e vince sull'ambito, quindi aggiunge un monitor, cambia cosa
invia o esclude il monitor da un canale su *tutti i monitor*.

## Escalation e promemoria

Ogni canale ha due impostazioni facoltative, in minuti:

- **invia dopo**: il canale riceve l'avviso solo se dopo questo tempo il monitor è ancora down (0 = subito). Aggiungi
  un secondo canale (il telefono di reperibilità, l'email di un responsabile) con 15 o 30 minuti per l'escalation dei
  disservizi lunghi: i problemi brevi non lo raggiungono mai, e riceve il ripristino solo se ha ricevuto l'avviso.
- **ripeti ogni**: il canale riceve un promemoria `STILL DOWN` a questo intervallo finché il monitor resta down (0 =
  una volta sola, almeno 5). I webhook includono `reminder` (1, 2, ...) e `down_since` (unix ms).

I tempi contano da quando si è aperto l'incidente. Quello che ogni canale ha ricevuto viene salvato con l'incidente,
quindi un riavvio non ripete né salta avvisi.

## Finestre di manutenzione

Nella pagina **manut** pianifica le finestre in cui gli avvisi devono tacere: singole o ricorrenti (ogni giorno,
settimana o mese, in qualsiasi fuso orario), per alcuni o tutti i monitor, o interi gruppi e tag.

Durante una finestra i monitor risultano in **manutenzione**: i controlli continuano, ma non si aprono incidenti, non
partono avvisi e quei controlli non contano per l'uptime. Un monitor ancora down quando la finestra finisce riceve
l'avviso in quel momento.

## Dipendenze

Quando molti monitor girano sullo stesso server, di' a ognuno da cosa dipende: nel modulo del monitor, sotto
**dipendenze**, aggiungi il monitor del server (un ping, per esempio). Se il server va giù ricevi un solo avviso per il
server, non uno per ogni servizio che ci gira sopra.

- Finché un monitor da cui dipende è giù, un monitor che fallisce è **bloccato da** quel monitor: continua i controlli
  e mostra il suo stato (giù, così uptime e status page restano corretti), ma non apre incidenti e non manda avvisi. La
  dashboard e la pagina del monitor dicono quale monitor lo blocca. Le dipendenze si concatenano: se il server blocca
  il sito e il sito blocca l'app, l'app è bloccata dal server.
- Quando il server torna su, ogni monitor bloccato viene ricontrollato subito, e mandano un avviso solo quelli che
  continuano a fallire: quello è un problema diverso.
- I controlli di monitor diversi non partono nello stesso istante. Quando un servizio fallisce per primo, il suo
  avviso aspetta un nuovo controllo del server (fatto subito, più i suoi tentativi, fino a 10 minuti): se anche il
  server è giù, il servizio resta in silenzio.

Un monitor può dipendere da al massimo 20 monitor, e l'hub rifiuta i cicli. Nell'API il campo è `depends_on` (id dei
monitor), e `blocked_by` nell'elenco dei monitor finché è bloccato.

## Avvisi di scadenza

pingtxt tiene d'occhio il certificato TLS dei monitor https e la registrazione del loro dominio (RDAP, con WHOIS per i
registri senza RDAP, come il .it). Attiva l'uno o l'altro per monitor. Le soglie in giorni si impostano in
**impostazioni → monitor** (predefinite 14/7/3/1 per i certificati, 30/14/7/1 per i domini): un avviso per ogni soglia
superata, uno alla scadenza e uno al rinnovo. I canali li ricevono con gli eventi `cert_expiry` e `domain_expiry`.

## Report via email

I **report periodici**, nella pagina notifiche, mandano via email uptime, incidenti e tempi di inattività
dell'ultima settimana (ogni lunedì) o dell'ultimo mese (il giorno 1), per gruppo di monitor, a confronto con il
periodo precedente. Scegli i canali email, eventualmente alcuni gruppi, l'ora e il fuso orario. **anteprima** mostra
un report e **invia ora** ne manda uno prima di attivarli.

## Contenuto dei webhook

I webhook ricevono un body JSON con `monitor_id`, `monitor`, `target`, `status`, `cause` e `at` (unix ms), più:

| campo | quando |
| --- | --- |
| `kind` | vuoto per down e up, `degraded`, `cert_expiry`, `domain_expiry` o `agent` |
| `status` | `down` / `up`, `expiring` / `expired` / `renewed` per le scadenze, `offline` / `online` per gli agent |
| `reminder`, `down_since` | promemoria (`down_since` in unix ms) |
| `subject`, `expires_at`, `days_left` | eventi di scadenza (nome host o dominio) |
| `agent_id`, `agent`, `down_since` | avvisi di agent offline e online |
| `diagnostics`, `diagnostics_url` | il primo avviso di down, quando è stata raccolta la diagnostica |
| `test` | `true` per le notifiche di prova |

Usa **invia test** per vedere un contenuto reale sul tuo endpoint.
