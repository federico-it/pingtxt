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

## Azioni nelle notifiche

Tieni premuta una notifica (anche dalla schermata di blocco) per agire senza aprire l'app:

| notifica | azioni |
| --- | --- |
| monitor down | controlla ora, silenzia 1 ora, metti in pausa |
| monitor degradato | controlla ora, silenzia 1 ora |
| scadenza di certificato o dominio | ricontrolla scadenza |
| agent offline | apri agent |

- **controlla ora** esegue subito il controllo da tutte le località e salva il risultato, quindi può chiudere
  l'incidente.
- **silenzia 1 ora** crea una [finestra di manutenzione](alerts.md#finestre-di-manutenzione) di un'ora per quel
  monitor: niente avvisi né promemoria, i controlli continuano. Nel frattempo le status page pubbliche mostrano il
  monitor in manutenzione.
- **metti in pausa** ferma i controlli e chiude l'incidente aperto.

Le azioni che cambiano qualcosa richiedono un accesso in **lettura + scrittura** (admin). Le notifiche di prova non
hanno pulsanti.

## Live Activity

Con le push attive, l'hub tiene aggiornata la Live Activity dell'app anche ad app chiusa: la avvia (iOS 17.2+), la
aggiorna e la chiude tramite il relay quando gli incidenti si aprono o si chiudono. Mostra fino a tre monitor down,
con le loro cause.

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
  `hub_id`, così un'app collegata a più hub sa quale chiamare. `POST /api/monitors/{id}/check` esegue subito un
  controllo.
- Live Activity: l'app invia i suoi token ActivityKit a `PUT /api/mobile/live-activity/start-token` e `/token`.

Tutti gli endpoint sono nel [riferimento API](https://pingtxt.com/docs/api/).
