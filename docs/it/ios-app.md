# App iOS

L'app iOS di pingtxt mostra monitor, incidenti e agent, riceve gli avvisi come notifiche push e tiene una Live
Activity sulla schermata di blocco durante i disservizi. Parla con il tuo hub: non esiste un account pingtxt.

## Accesso

Nell'app inserisci l'indirizzo del tuo hub (il suo URL pubblico). L'app apre la pagina di accesso dell'hub, quindi
funzionano password, autenticazione a due fattori e single sign-on, e nessuna password arriva mai all'app. Gli admin
scelgono **sola lettura** o **lettura + scrittura** nella pagina di consenso; gli utenti in sola lettura ricevono
sempre la sola lettura.

Ogni telefono compare in **impostazioni → token → app collegate**, da dove puoi scollegarlo. L'app può accedere a più
hub.

## Notifiche push

1. In **impostazioni → integrazioni → app mobile** attiva **notifiche push all'app mobile**. L'hub si registra da solo
   presso il relay push (`https://push.pingtxt.com` di default).
2. Nella pagina **notifiche** aggiungi un canale di tipo **app pingtxt (push)**. Lascia *utenti* vuoto per avvisare
   tutti, oppure elenca alcune email. Ogni utente riceve gli avvisi su tutti i telefoni collegati con l'app.
3. Scegli monitor ed eventi come per qualsiasi altro [canale](alerts.md#quali-monitor-ed-eventi-riceve-ogni-canale).

Un'app può ricevere notifiche solo da un server che ha le credenziali Firebase dell'app, che non possono stare dentro
ogni hub installato in proprio. Per questo l'hub manda ogni avviso al
[relay push](https://github.com/federico-it/pingtxt-push-relay), che lo inoltra a Firebase e Apple. Il relay non ha
database e non registra né il testo degli avvisi né i token dei telefoni; un hub può raggiungere solo i telefoni
collegati a lui. Cambiare l'indirizzo del relay registra di nuovo l'hub, e ogni telefono si registra di nuovo al
successivo avvio. Cosa invia l'app è descritto nella [privacy policy](https://pingtxt.com/privacy/).

## Scegliere cosa riceve ogni telefono

Ogni utente sceglie nell'app cosa ricevono i suoi telefoni:

- **eventi**: down, di nuovo up, degradato, scadenza di certificato e dominio, agent offline.
- **monitor**: tutti, oppure solo i monitor di alcuni gruppi (con i sottogruppi) e tag, più monitor scelti uno per
  uno. Agent offline riguarda gli agent e segue solo gli eventi.
- **silenzia**: tutto, o un monitor, per un po' o finché non lo riattivi.
- **ricevi su**: tutti i telefoni, o solo uno (per esempio l'iPhone e non l'iPad), per non ricevere ogni avviso due
  volte. Se quel telefono si scollega, gli altri tornano a ricevere gli avvisi.

Queste preferenze di account valgono per tutti i tuoi telefoni. Un telefono può invece avere eventi e monitor suoi, e
si può silenziare da solo: un avviso arriva a un telefono solo se né l'account né il telefono sono silenziati.

Le preferenze sono personali, quindi le impostano anche gli utenti in sola lettura. Non cambiano nulla per le altre
persone, i canali o le status page, e tolgono solo notifiche: un telefono non riceve mai qualcosa che i suoi canali
non inviano. Le notifiche di prova arrivano sempre, e la Live Activity non cambia.

## Azioni nelle notifiche

Tieni premuta una notifica (anche dalla schermata di blocco) per agire senza aprire l'app:

| notifica | azioni |
| --- | --- |
| monitor down | prendi in carico, controlla ora, silenzia 1 ora per me, silenzia 1 ora per tutti, metti in pausa |
| monitor degradato | controlla ora, silenzia 1 ora per me, silenzia 1 ora per tutti |
| scadenza di certificato o dominio | ricontrolla scadenza, silenzia 1 ora per me |
| agent offline | apri agent |

- **prendi in carico** fa sapere che te ne occupi tu: promemoria ed escalation si fermano e gli altri vedono chi l'ha
  preso (vedi [Prendere in carico gli incidenti](alerts.md#prendere-in-carico-gli-incidenti)).
- **controlla ora** esegue subito il controllo da tutte le località e salva il risultato, quindi può chiudere
  l'incidente.
- **silenzia 1 ora per me** silenzia quel monitor solo su questo telefono (vedi sopra). Possono usarlo tutti.
- **silenzia 1 ora per tutti** crea una [finestra di manutenzione](alerts.md#finestre-di-manutenzione) di un'ora per quel
  monitor: niente avvisi né promemoria, i controlli continuano. Nel frattempo le status page pubbliche mostrano il
  monitor in manutenzione.
- **metti in pausa** ferma i controlli e chiude l'incidente aperto.

Le altre azioni cambiano qualcosa per tutti e richiedono un accesso in **lettura + scrittura** (admin): gli altri
telefoni ricevono solo le azioni che possono usare. Le notifiche di prova non hanno pulsanti.

## Live Activity

Con le push attive, l'hub tiene aggiornata la Live Activity dell'app anche ad app chiusa: la avvia (iOS 17.2+), la
aggiorna e la chiude tramite il relay quando gli incidenti si aprono o si chiudono. Mostra fino a tre monitor down,
con le loro cause. Quando si chiude l'ultimo incidente, l'attività termina con un riepilogo ("api is up, down for
12m", oppure "All monitors are up" con il tempo di down di ogni monitor) e resta sulla schermata di blocco per 15
minuti.

## Per chi sviluppa l'app

L'app accede con OAuth 2.1 (authorization code + PKCE). È un client pubblico integrato: nessun secret e nessuna
registrazione.

| | |
| --- | --- |
| client_id | `pingtxt-ios` |
| redirect URI | `pingtxt://oauth` |
| autorizzazione | `GET /oauth/authorize` con `response_type=code`, `code_challenge` + `code_challenge_method=S256`, `state`, `scope=read write` |
| token | `POST /oauth/token` (body form): `grant_type=authorization_code` con `code`, `redirect_uri`, `code_verifier`, oppure `grant_type=refresh_token` con `refresh_token` |
| uscita | `POST /oauth/revoke` con `token` e `client_id` |
| metadati | `/.well-known/oauth-authorization-server`, `/.well-known/oauth-protected-resource/api` |

- La risposta con i token dice quale `scope` è stato concesso.
- Gli access token (`pma_...`) durano un'ora e valgono su tutta la REST API come un token API con lo stesso scope, mai
  sugli endpoint dell'account (password, 2FA, token, utenti, impostazioni).
- I refresh token (`pmr_...`) durano 30 giorni dall'ultimo uso e ruotano a ogni rinnovo. Riusare un refresh token
  vecchio revoca l'accesso, quindi rinnova una richiesta alla volta e salva il nuovo token prima di usarlo.
- Push: l'app registra il suo token FCM tramite `/api/mobile/config` e `/api/mobile/devices`. Le notifiche hanno un
  `aps.category` (`MONITOR_DOWN`, `MONITOR_UP`, `MONITOR_DEGRADED`, `EXPIRY`, `AGENT_OFFLINE`, `AGENT_ONLINE`) e
  `hub_id`, così un'app collegata a più hub sa quale chiamare. I telefoni il cui accesso non può cambiare i dati
  ricevono `MONITOR_DOWN_READONLY`, `MONITOR_DEGRADED_READONLY` e `EXPIRY_READONLY` al posto delle categorie con azioni
  di scrittura. `POST /api/monitors/{id}/check` esegue subito un controllo. Le push di down, up e presa in carico
  hanno `incident_id`; `POST /api/incidents/{id}/ack` prende in carico l'incidente, e la notifica `MONITOR_ACKED`
  (stesso collapse id dell'avviso di down) dice agli altri telefoni chi se ne occupa.
- Preferenze: `GET` e `PUT /api/mobile/preferences` per l'account (con `primary_device_id`), e
  `/api/mobile/devices/{id}` per un telefono (`current` = il telefono dell'accesso che chiama, `follow_account`).
  Silenzia con `POST .../mute` (`monitor_id`, `minutes`) su entrambi, riattiva con `DELETE .../mute`.
- Live Activity: l'app invia i suoi token ActivityKit a `PUT /api/mobile/live-activity/start-token` e `/token`.

Tutti gli endpoint sono nel [riferimento API](https://pingtxt.com/docs/api/).
