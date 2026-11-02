# Documentazione di pingtxt

pingtxt è un monitor di uptime da ospitare sui tuoi server. Un container esegue l'**hub** (API, pianificazione,
database e interfaccia web); piccoli **agent** controllano i tuoi servizi da altri luoghi. Quasi tutto si configura
dall'interfaccia web.

## Da dove iniziare

1. [Installa l'hub](install.md) con Docker e crea l'account admin.
2. [Aggiungi una località](locations.md) se vuoi controlli da più di un posto (facoltativo).
3. [Crea il primo monitor](monitors.md).
4. [Aggiungi un canale di notifica](alerts.md) per sapere quando qualcosa si rompe.
5. [Pubblica una status page](status-pages.md) per i tuoi utenti (facoltativo).

Nuovo su pingtxt? [Concetti](concepts.md) spiega hub, località, stati e incidenti in due minuti.

## Tutte le pagine

| pagina | cosa contiene |
| --- | --- |
| [Concetti](concepts.md) | hub, località, monitor, stati (up, degradato, down...), incidenti, uptime |
| [Installazione](install.md) | Docker, configurazione, PostgreSQL o MariaDB, reverse proxy, IPv6, senza Docker |
| [Località (agent)](locations.md) | aggiungere un agent, come votano le località, avvisi offline, aggiornamento degli agent |
| [Monitor](monitors.md) | tutti i tipi di controllo, regole JSON, soglia di lentezza, IPv6, gruppi e tag, diagnostica |
| [Avvisi e notifiche](alerts.md) | canali, quali eventi riceve ogni canale, escalation, manutenzione, scadenze, report |
| [Status page](status-pages.md) | pagine pubbliche, dominio personalizzato, password, logo, incidenti e iscritti, badge |
| [App iOS](ios-app.md) | accesso, notifiche push e le loro azioni, Live Activity |
| [Utenti e sicurezza](security.md) | ruoli, single sign-on, autenticazione a due fattori, registro attività |
| [Integrazioni](integrations.md) | token API e REST API, server MCP per gli assistenti AI, Prometheus |
| [Amministrazione](administration.md) | aggiornamenti, backup, export e import della configurazione, statistiche d'uso, verifica delle release |

Il [riferimento della REST API](https://pingtxt.com/docs/api/) elenca tutti gli endpoint. Per lavorare su pingtxt,
vedi [CONTRIBUTING.md](../../CONTRIBUTING.md) (in inglese).

Queste pagine sono anche su [pingtxt.com/it/docs](https://pingtxt.com/it/docs/), in italiano e
[inglese](https://pingtxt.com/docs/). La fonte è la cartella `docs/` del repository: correggi una pagina lì e la
modifica compare sia sul sito sia su GitHub.
