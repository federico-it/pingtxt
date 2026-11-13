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
| PagerDuty | l'integration key Events API v2 di un servizio, e la regione (USA o UE). Un down apre un incidente, i promemoria si uniscono a quello e il ripristino lo risolve; lo stesso per degradato, scadenze e agent. Con l'evento `ack`, la presa in carico in pingtxt fa l'acknowledge anche dell'incidente PagerDuty |
| Opsgenie | la chiave di un'integrazione API, e la regione (USA o UE). Gli avvisi hanno P1 (down, agent offline), P2 (scaduto) o P3 (in scadenza, degradato) e si chiudono da soli al ripristino. Con l'evento `ack`, la presa in carico in pingtxt fa l'acknowledge anche dell'avviso |
| SMS e chiamate (Twilio) | account SID e auth token, il mittente (un numero Twilio o il SID di un messaging service `MG...`) e i numeri in formato internazionale (`+39...`). **invia come** sceglie SMS, chiamata o entrambi: ogni avviso è un SMS breve per numero, le chiamate partono solo per le cattive notizie (down, scadenze, agent offline) e leggono l'avviso ad alta voce in inglese o italiano. Le chiamate richiedono un numero Twilio come mittente. Vedi [chiamate vocali](#chiamate-vocali). Pagato sul tuo account Twilio |
| email | un server SMTP (STARTTLS sulla 587, TLS sulla 465, o nessuno per un relay locale sulla 25), oppure l'API di [Resend](https://resend.com) con una chiave di un dominio verificato; poi mittente e destinatari |
| app pingtxt (push) | le notifiche push attive in **impostazioni → integrazioni → app mobile**; gli utenti (email) i cui telefoni ricevono gli avvisi, vuoto per tutti. Vedi [App iOS](ios-app.md) |
| chi è reperibile | un [turno di reperibilità](#turni-di-reperibilità), e come raggiungere chi è di turno: l'app pingtxt, un canale email da cui inviare e un canale Twilio da cui mandare SMS o chiamare |

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
- **eventi**: `down` (con i suoi promemoria), `up` (ripristino, solo ai canali che hanno ricevuto l'avviso), `ack`
  (qualcuno ha [preso in carico l'incidente](#prendere-in-carico-gli-incidenti), solo ai canali che hanno ricevuto
  l'avviso; spento di default),
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

## Prendere in carico gli incidenti

Quando qualcuno si occupa di un incidente aperto, gli altri devono saperlo e smettere di essere avvisati. Dalla pagina
del monitor, dalla pagina incidenti o da una notifica dell'app iOS, un admin preme **prendi in carico**:

- l'incidente mostra **chi se ne occupa** e da quando, nella dashboard, nel monitor e nella pagina incidenti;
- i promemoria `STILL DOWN` si fermano, e così gli avvisi dei canali che stanno ancora aspettando il loro tempo di
  **invia dopo** (escalation);
- i canali che hanno ricevuto l'avviso di down e hanno l'evento `ack` ricevono un messaggio con chi l'ha preso in
  carico. Nell'app iOS sostituisce la notifica di down.

Il ripristino va comunque a tutti i canali che hanno ricevuto l'avviso. **rilascia** annulla la presa in carico:
promemoria ed escalation ripartono da dove si erano fermati. Un altro admin può **subentrare** in un incidente preso
in carico da qualcun altro. Gli utenti in sola lettura vedono chi se ne occupa ma non possono prenderlo in carico.
API: `POST` e `DELETE /api/incidents/{id}/ack`; le prese in carico finiscono nel [registro attività](security.md#registro-attività).

## Turni di reperibilità

La pagina **turni** dice chi risponde agli avvisi, e quando. Un turno ha dei membri in ordine di rotazione e una durata
(qualche ora, giorno o settimana): dall'inizio del primo turno, ogni membro è reperibile per un turno, poi tocca al
successivo, e così via. I turni giornalieri e settimanali cambiano alla stessa ora locale del fuso orario del turno
tutto l'anno, anche con l'ora legale. Una persona può comparire più volte nella lista.

Una **sostituzione** mette qualcun altro in reperibilità per un periodo, qualunque cosa dica la rotazione: uno
scambio, un giorno libero, le ferie. Dove le sostituzioni si sovrappongono vince la più recente. Ogni turno mostra chi
è reperibile ora e fino a quando, chi viene dopo, i prossimi turni e le sostituzioni in arrivo. Solo gli admin
modificano i turni; tutti li vedono.

Un canale **chi è reperibile** manda i suoi avvisi a chi è di turno nel momento in cui partono:

- sui suoi telefoni, con l'app pingtxt (valgono comunque le sue preferenze di notifica);
- via email, tramite un canale email che scegli: stesso server e mittente, ma all'email di chi è di turno;
- via SMS, chiamata o entrambi, tramite un canale Twilio che scegli: stesso account e mittente, ma al numero di
  telefono di chi è di turno. Ognuno imposta il proprio numero in **impostazioni → account**; chi non ce l'ha riceve
  solo gli avvisi via app ed email, e la pagina dei turni lo segnala. Finché nell'hub non c'è un canale Twilio, il
  numero di telefono, la scelta del canale Twilio qui e queste segnalazioni restano nascosti.

Eventi, monitor, **invia dopo** e **ripeti ogni** funzionano come per qualsiasi canale. Per l'escalation aggiungi un
secondo canale **chi è reperibile** per un turno di riserva con **invia dopo** a 15 o 30 minuti: se nessuno
[prende in carico](#prendere-in-carico-gli-incidenti) l'incidente entro quel tempo, viene avvisata anche la riserva.
Un turno o un canale usato da un canale di reperibilità non si può eliminare finché quel canale lo usa. I turni non
fanno parte dell'[esportazione della configurazione](administration.md#export-e-import-della-configurazione). API: `/api/oncall`; strumento MCP `list_oncall`.

## Chiamate vocali

Un canale Twilio impostato su **chiamata** o **sms e chiamata** chiama ogni numero per gli avvisi di down, scadenza e
agent offline e legge l'avviso ad alta voce due volte, in inglese o italiano. Ripristini, prese in carico e avvisi di
degrado vanno solo via SMS (o per niente con **chiamata**).

Quando l'hub ha un URL pubblico (**impostazioni → generali**) e la chiamata riguarda un monitor down, la chiamata
finisce con "premi 1 per prendere in carico". Premendo 1 l'incidente viene
[preso in carico](#prendere-in-carico-gli-incidenti) dall'admin il cui account ha il numero chiamato
(**impostazioni → account**); gli altri numeri si sentono dire che non possono. Twilio invia il tasto a
`/api/voice/...` firmato con l'auth token dell'account, e l'hub controlla la firma, quindi l'URL pubblico deve essere
quello che Twilio raggiunge.

## Posticipare gli incidenti

Quando non puoi guardare subito un disservizio ma non vuoi perderlo di vista, un admin preme **posticipa** nella pagina
del monitor o nella pagina incidenti e sceglie 15 minuti, 30 minuti, 1, 2, 4, 8 o 24 ore:

- l'incidente mostra **posticipato fino a** e chi l'ha posticipato;
- i promemoria `STILL DOWN` si fermano, e così gli avvisi dei canali che stanno ancora aspettando il loro tempo di
  **invia dopo**;
- quando il tempo finisce e il monitor è ancora down, tutti i canali che hanno ricevuto l'avviso ricevono un promemoria
  `STILL DOWN`, anche quelli senza **ripeti ogni**, e l'escalation riparte da dove si era fermata. Un canale il cui
  tempo di **invia dopo** è passato durante la pausa riceve allora il primo avviso.

**riattiva** termina prima la pausa: promemoria ed escalation ripartono subito, senza il promemoria in più. Posticipare
di nuovo sostituisce la pausa. Un incidente preso in carico resta silenzioso anche dopo la fine della pausa. Gli utenti
in sola lettura vedono la pausa ma non possono cambiarla. API: `POST /api/incidents/{id}/snooze` con `{"minutes": 30}`
(da 5 a 1440) e `DELETE` per terminarla; le pause finiscono nel [registro attività](security.md#registro-attività).

## Finestre di manutenzione

Nella pagina **manut** pianifica le finestre in cui gli avvisi devono tacere: singole o ricorrenti (ogni giorno,
settimana o mese, in qualsiasi fuso orario), per alcuni o tutti i monitor, o interi gruppi e tag.

Durante una finestra i monitor risultano in **manutenzione**: i controlli continuano, ma non si aprono incidenti, non
partono avvisi e quei controlli non contano per l'uptime. Un monitor ancora down quando la finestra finisce riceve
l'avviso in quel momento.

Una finestra può anche essere annunciata sulle status page e inviata via email ai loro iscritti: vedi
[manutenzioni programmate](status-pages.md#manutenzioni-programmate).

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
| `kind` | vuoto per down e up, `ack`, `degraded`, `cert_expiry`, `domain_expiry` o `agent` |
| `status` | `down` / `up`, `acknowledged`, `expiring` / `expired` / `renewed` per le scadenze, `offline` / `online` per gli agent |
| `reminder`, `down_since` | promemoria (`down_since` in unix ms) |
| `incident_id` | eventi di down, up e presa in carico |
| `ack_by` | prese in carico: email di chi ha preso in carico l'incidente |
| `subject`, `expires_at`, `days_left` | eventi di scadenza (nome host o dominio) |
| `agent_id`, `agent`, `down_since` | avvisi di agent offline e online |
| `diagnostics`, `diagnostics_url` | il primo avviso di down, quando è stata raccolta la diagnostica |
| `test` | `true` per le notifiche di prova |

Usa **invia test** per vedere un contenuto reale sul tuo endpoint.
