# Concetti

Le parole usate nel resto della documentazione, nell'ordine in cui le incontri.

## Hub

L'hub è il programma principale: un container Docker con l'API, la pianificazione dei controlli, il database
(SQLite di default, oppure PostgreSQL / MariaDB) e l'interfaccia web. Pianifica ogni controllo, salva i risultati,
apre gli incidenti, invia gli avvisi e serve le status page. Ne serve esattamente uno. Vedi
[Installazione](install.md).

## Località e agent

Una **località** è un posto da cui partono i controlli. L'hub è una località (quella integrata, che puoi rinominare
o disattivare). Ogni **agent** che aggiungi è un'altra località: un piccolo programma che si collega *verso*
l'hub via WebSocket, quindi non apre porte e funziona dietro NAT. L'hub gli manda i controlli da fare, l'agent
rimanda i risultati. Vedi [Località](locations.md).

## Monitor e controlli

Un **monitor** è qualcosa che tieni d'occhio: un URL, una porta, un database, un cron job... Ha un tipo, una
destinazione, un intervallo e le località che lo controllano. Ogni esecuzione da una località è un **controllo**.
Con **tentativi** impostato a N, una località conta come in errore solo dopo N controlli falliti di fila. Vedi
[Monitor](monitors.md).

## Stato

Ogni monitor ha un solo stato, deciso da tutte le sue località:

| stato | quando |
| --- | --- |
| **up** | i controlli passano |
| **degradato** | alcune località falliscono o sono lente, ma non più della metà. La quota è 35% di default (impostazioni → monitor, oppure per monitor, fino al 50%). Con una sola località basta che sia lenta |
| **down** | fallisce più della metà delle località online |
| **in attesa** | ancora nessun risultato, oppure tutte le località del monitor sono agent offline |
| **in pausa** | l'hai messo in pausa: nessun controllo |
| **manutenzione** | è attiva una [finestra di manutenzione](alerts.md#finestre-di-manutenzione): i controlli continuano ma non generano nulla |

Una località è **lenta** quando il suo tempo di risposta supera la
[soglia di lentezza](monitors.md#degradato-e-soglia-di-lentezza) del monitor, se impostata.

## Incidenti

Un **incidente** si apre quando un monitor va down e si chiude quando torna up. Gli avvisi partono all'apertura e
alla chiusura (e, se vuoi, come promemoria nel frattempo). La pagina incidenti li elenca con le cause e, se la
[diagnostica](monitors.md#diagnostica-in-caso-di-errore) è attiva, con quello che ogni località ha visto al primo
errore.

Le status page hanno incidenti propri: aggiornamenti scritti che pubblichi per i visitatori. Vedi
[Status page](status-pages.md#incidenti-e-iscritti-via-email).

## Uptime

L'uptime è la quota di tempo in cui un monitor non era down, su 24 ore, 7, 30 o 90 giorni. Segue lo stato del
monitor, non i singoli controlli: una località in errore tra tante non lo abbassa, e degradato conta come up. I
controlli fatti durante le finestre di manutenzione non contano.

## Canali di notifica

Un **canale** è dove arrivano gli avvisi: email, Slack, Discord, Microsoft Teams, Telegram, Matrix,
ntfy, Gotify, Pushover, PagerDuty, Opsgenie, SMS (Twilio), un webhook o l'app iOS. Ogni canale sceglie i monitor e
gli eventi che riceve. Vedi [Avvisi e notifiche](alerts.md).

## Gruppi e tag

Un monitor può stare in un **gruppo** e avere quanti **tag** vuoi. Entrambi filtrano la dashboard, e status page,
finestre di manutenzione e canali possono scegliere i monitor per gruppo o tag. Vedi
[Monitor](monitors.md#gruppi-e-tag).

## Usare l'interfaccia web

- Premi <kbd>ctrl k</kbd> per saltare a qualsiasi monitor o pagina, <kbd>d</kbd> per passare da chiaro a scuro.
- Seleziona più monitor nella dashboard per metterli in pausa, riattivarli, spostarli o eliminarli insieme.
- **prova** nel modulo del monitor esegue il controllo una volta dalle località scelte, senza salvare.
- Fissa i monitor in cima alla dashboard; i monitor fissati restano salvati nel tuo browser.
