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
alla chiusura (e, se vuoi, come promemoria nel frattempo). Qualcuno può [prendere in carico](alerts.md#prendere-in-carico-gli-incidenti)
un incidente aperto per fermare i promemoria e far vedere agli altri chi se ne occupa. La pagina incidenti li elenca con le cause e, se la
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
- Fissa i monitor in cima alla dashboard. I monitor fissati sono salvati sul tuo account, quindi li ritrovi in ogni browser e nell'API (`/api/account/pins`); ogni utente ha i suoi, anche gli utenti in sola lettura.

### Modalità TV

**tv** nella dashboard apre `/tv/`, una vista a schermo intero per un monitor a parete o uno schermo libero: niente
menu, testo grande, una riga generale in alto (*tutto funziona* o quanti monitor sono giù), i monitor giù, degradati o in
manutenzione come riquadri grandi con il motivo e da quanto tempo, e tutti gli altri come riquadri piccoli con l'uptime
delle ultime 24 ore, divisi per gruppo. Non c'è niente da scorrere: i riquadri riempiono lo schermo in colonne, grandi
quanto possono esserlo perché ci stia tutto, e quando sono troppi anche alla dimensione più piccola vengono divisi in
pagine che cambiano ogni 15 secondi (i punti in basso a destra indicano quale è sullo schermo). Tiene il filtro di gruppo e tag della dashboard (`/tv/?group=3&tag=2`), lascia
fuori i monitor in pausa (aggiungi `&paused=1` per mostrarli), si aggiorna da sola ogni 10 secondi, tiene acceso lo
schermo dove il browser lo permette e nasconde il puntatore quando non si muove. Il pulsante in alto a destra passa a
schermo intero.

Per uno schermo che nessuno usa, entra con un [utente in sola lettura](security.md#utenti-e-ruoli): vede tutti i
monitor e non può cambiare niente. Le sessioni durano 30 giorni, poi lo schermo torna alla pagina di accesso.
