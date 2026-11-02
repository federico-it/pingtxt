# Utenti e sicurezza

Chi può accedere e cosa può fare: ruoli, single sign-on, autenticazione a due fattori, il registro di ogni
modifica, e come rientrare quando resti fuori.

## Utenti e ruoli

Ogni utente è **admin** o in **sola lettura**. Gli admin aggiungono utenti in **impostazioni → accesso** (con una
password, o senza per accedere con il single sign-on usando la stessa email) e lì ne cambiano il ruolo.

- Gli utenti in **sola lettura** vedono monitor, incidenti, storico, agent, manutenzioni e status page, ma ogni
  modifica viene rifiutata dall'API e i pulsanti sono nascosti. Le password nelle destinazioni dei monitor, header e
  body delle richieste, URL push e impostazioni dei canali sono nascosti. Possono comunque cambiare la propria
  password, la 2FA e i token di lettura.
- Impostazioni dell'istanza e utenti sono solo per gli admin. Un admin non può cambiare il proprio ruolo, quindi
  resta sempre almeno un admin.

## Single sign-on

In **impostazioni → accesso** attiva GitHub, Google o un provider OpenID Connect generico (Authentik, Keycloak,
Authelia, Pocket ID, Zitadel, Okta, Entra ID e altri), incolla client id e secret, e registra presso il provider l'URL
di callback mostrato lì (`<url pubblico>/api/auth/oauth/<provider>/callback`). Se l'hub è dietro un reverse proxy,
imposta prima l'[URL pubblico](install.md#configurazione), così il callback è giusto.

- Un accesso tramite provider viene associato a un utente esistente tramite l'email verificata, poi collegato. Gli
  utenti possono collegare account anche da **impostazioni → account**.
- Attiva *crea gli account al primo single sign-on* per far entrare persone nuove, eventualmente solo da alcuni domini
  email. I nuovi account ricevono il ruolo scelto lì (sola lettura di default).
- Quando un provider funziona puoi disattivare l'accesso con password.

## Autenticazione a due fattori

In **impostazioni → account** inquadra il codice QR con un'app di autenticazione (1Password, Bitwarden, Aegis, Google
Authenticator...) e conferma un codice. Ricevi 10 codici di recupero monouso. Da lì in poi gli accessi con password
chiedono un codice; quelli con single sign-on si affidano all'MFA del provider.

Telefono e codici di recupero persi? Un admin può cliccare **azzera 2fa** accanto all'utente in
**impostazioni → accesso**, oppure vedi [accesso bloccato](#accesso-bloccato).

## Registro attività

**impostazioni → registro attività** mostra chi ha cambiato cosa, quando e da dove: ogni modifica fatta
dall'interfaccia web, da un token API, da un client MCP o dall'app mobile, più gli accessi (anche falliti), le
modifiche alla 2FA, token e accessi delle app, backup, importazioni e le opzioni di ripristino da riga di comando.
Ogni voce conserva i campi cambiati (prima → dopo). I segreti non vengono mai salvati: una password o un token
cambiati appaiono come `********`. Le modifiche rifiutate a utenti in sola lettura e token di lettura sono registrate
come *negate*.

- Solo gli admin lo vedono, lo filtrano e lo esportano (CSV o JSON Lines), dal browser o con un token API
  (`GET /api/audit`, `/api/audit/export`). Le pagine dei monitor e delle status page mostrano le loro ultime modifiche.
- Ogni utente vede i propri accessi, token e app recenti in **impostazioni → account**.
- L'indirizzo del client è il primo IP pubblico negli header del proxy (`CF-Connecting-IP`, `X-Forwarded-For`...);
  viene conservato anche l'indirizzo della connessione, perché gli header si possono falsificare.
- Le voci si conservano 365 giorni (**impostazioni → generali**, da 30 a 3650) e fanno parte dei backup del database.

## Accesso bloccato

Eseguili sul server, poi riavvia l'hub. In Docker, mettici davanti `docker exec -u pingtxt pingtxt`.

| comando | cosa fa |
| --- | --- |
| `pingtxt -enable-password-login` | riattiva l'accesso con password, quando il single sign-on non funziona |
| `pingtxt -reset-2fa you@example.com` | disattiva l'autenticazione a due fattori per quell'utente |
| `pingtxt -make-admin you@example.com` | dà a quell'utente il ruolo di admin |

## Sicurezza dell'hub

- I tentativi di accesso sono limitati (i client IPv6 per /64). Sessioni, token API e token delle app sono salvati
  come hash.
- I container girano con un utente senza privilegi, vedi [container senza root](install.md#container-senza-root).
- Le release sono firmate, vedi [verificare una release](administration.md#verificare-una-release).
- Le vulnerabilità si segnalano in privato, come descritto in [SECURITY.md](../../SECURITY.md).
