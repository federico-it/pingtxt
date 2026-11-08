# Status page

Pagine pubbliche che dicono ai tuoi utenti cosa funziona. Si creano nella pagina **status** dell'interfaccia web.

## Creare una pagina

Scegli titolo, slug e monitor, nell'ordine che vuoi. Puoi anche aggiungere interi **gruppi**: ogni monitor del
gruppo viene mostrato, anche quelli aggiunti in seguito, e ogni gruppo è una sezione con il suo stato. Scegli se
mostrare gli incidenti passati. **anteprima** mostra la pagina come la vedranno i visitatori, prima di salvare.

Una pagina pubblicata è pubblica su `https://<hub>/status/<slug>/`. Le bozze sono visibili solo agli utenti con
accesso. I visitatori vedono nomi dei monitor, stato, storico giornaliero dell'uptime e orari degli incidenti, mai
destinazioni, località o messaggi di errore. Lo storico copre il periodo di conservazione, fino a 90 giorni.

## Dominio personalizzato

Per servire una pagina sul suo dominio (per esempio `status.example.com`), impostalo sulla pagina, punta un record DNS
(CNAME o A) dell'host verso l'hub e instrada l'host verso pingtxt nel tuo [reverse proxy](install.md#reverse-proxy),
con il suo certificato TLS. Su quell'host pingtxt serve solo la status page, su `/`: interfaccia web e API non sono
raggiungibili da lì.

## Password

Per tenere privata una pagina, attiva **proteggi con una password**. I visitatori vedono allora un modulo per la
password; quella giusta apre la pagina per 30 giorni in quel browser, insieme ai suoi badge e al modulo di
iscrizione. La password è salvata come hash, i tentativi sbagliati sono limitati e cambiarla disconnette tutti i
visitatori. Agli utenti con accesso non serve mai.

## Link del nome e logo

- **link del nome**: cliccando il nome o il logo della pagina si apre questo indirizzo, per esempio il tuo sito.
- **logo**: SVG, PNG, JPEG, GIF, WebP o AVIF, fino a 4 MB, caricato o importato da un URL. Le immagini vengono
  ridimensionate e servite come AVIF e WebP; i file SVG restano vettoriali, senza script.
- **mostra solo il logo**: per i loghi che contengono già il nome. Il nome resta il titolo della scheda del browser.

## Aspetto

La sezione **aspetto** cambia come appare la pagina; l'anteprima mostra ogni modifica prima di salvare.

- **preset**: un aspetto pronto (pingtxt, pulito, aziendale, morbido, editoriale, alto contrasto). Imposta colori,
  font e forma e tiene testi e link. Poi puoi cambiare qualsiasi cosa.
- **tema**: scelto dal visitatore (con il pulsante chiaro/scuro) oppure sempre chiaro o sempre scuro.
- **colori**: sfondo, superfici, testo, testo secondario, bordi, colore primario e i tre colori di stato, per il
  tema chiaro e quello scuro. I colori vuoti restano quelli predefiniti. Il testo sul colore primario diventa nero
  o bianco da solo, e l'editor mostra il contrasto WCAG dei colori del testo. **colori di stato per daltonici**
  sostituisce verde e rosso con blu e arancio.
- **font**: JetBrains Mono, IBM Plex Sans, Figtree, Atkinson Hyperlegible, Source Serif 4 o il font di sistema del
  visitatore, in tre dimensioni. I font sono serviti dall'hub, mai da terzi.
- **angoli, linee, densità, larghezza, barre uptime**: angoli netti o arrotondati (0-24 px), linee tratteggiate o
  continue oppure schede, spaziatura compatta o ariosa, pagina stretta o larga, barre a blocchi, arrotondate o
  sottili.
- **intestazione**: semplice o una fascia del colore primario, a sinistra o centrata, un sottotitolo e fino a 6
  link.
- **avviso**: un messaggio in evidenza sopra lo stato, per esempio un intervento programmato.
- **footer**: testo libero e fino a 12 link, per esempio dati dell'azienda, contatti e privacy policy. La riga del credito
  "powered by pingtxt" porta a pingtxt.com; **nascondi "powered by pingtxt"** la toglie.
- **usa il logo come icona della scheda**.

Avviso e testo del footer accettano Markdown semplice: `**grassetto**`, `*corsivo*`, `[testo](https://…)` e una
riga vuota per un nuovo paragrafo. I link devono essere indirizzi `http://`, `https://` o `mailto:`.

Il **CSS personalizzato** viene aggiunto così com'è alla pagina pubblica, per tutto ciò che le opzioni non coprono.
Queste classi sono stabili: `.pt-page`, `.pt-header`, `.pt-brand`, `.pt-tagline`, `.pt-notice`, `.pt-banner`,
`.pt-section`, `.pt-list`, `.pt-monitor`, `.pt-group`, `.pt-bars`, `.pt-report`, `.pt-maintenance`, `.pt-subscribe`, `.pt-footer`. I
colori sono variabili CSS (`--background`, `--foreground`, `--primary`, `--muted`, `--border`, `--up`, `--warn`,
`--down`). Immagini o font che il CSS carica da altri siti vengono scaricati da ogni visitatore, che così mostra il
proprio indirizzo a quei siti. Il modulo della password di una pagina protetta usa gli stessi colori, font e CSS,
senza testi e link.

## Incidenti e iscritti via email

Apri gli **incidenti** di una pagina (pulsante nell'elenco delle status page) per spiegare ai visitatori cosa
succede: un titolo, un impatto (nessuno, minore, grave, critico) e una cronologia di aggiornamenti, ognuno con uno
stato (*stiamo indagando*, *causa individuata*, *sotto osservazione*, *risolto*) e un messaggio. Gli incidenti aperti
compaiono in cima alla pagina, e il loro impatto porta il banner almeno a *degradato* (minore), *disservizio
parziale* (grave) o *disservizio grave* (critico). Quelli risolti vanno nello storico.

Per permettere ai visitatori di **iscriversi via email**, scegli un canale email in **iscrizioni via email** nelle
impostazioni della pagina: vengono usati il suo server SMTP o account Resend e il suo mittente, non i destinatari. I
visitatori inseriscono l'indirizzo sulla pagina e lo confermano con un link (valido 7 giorni); poi ricevono ogni
aggiornamento pubblicato con **avvisa gli iscritti via email** spuntato. Ogni email ha un link per disiscriversi e un
header `List-Unsubscribe` con un clic. Gli admin vedono e rimuovono gli iscritti negli incidenti della pagina. I link
puntano al dominio personalizzato della pagina, altrimenti all'URL pubblico delle impostazioni.

## Manutenzioni programmate

Attiva **mostra sulle status page** in una [finestra di manutenzione](alerts.md#finestre-di-manutenzione) per
annunciarla su ogni pagina che mostra uno dei suoi monitor. Il nome è il titolo e sotto compare il **messaggio
pubblico**; le note restano private. Le finestre in arrivo sono elencate in **manutenzioni programmate** con inizio,
fine e i monitor della pagina che riguardano (quelle ricorrenti mostrano la prossima occorrenza); mentre una finestra è
in corso sta in cima alla pagina. Una finestra non cambia il banner della pagina: i suoi monitor risultano in
*manutenzione*.

Con **avvisa gli iscritti via email** attivo, gli iscritti di quelle pagine ricevono un'email quando la finestra viene
salvata o ne cambiano gli orari, se vuoi un **promemoria** prima di ogni occorrenza (da 15 minuti a 7 giorni), quando
inizia, quando finisce e quando viene annullata (eliminata, disattivata o non più pubblica). Gli orari sono nel fuso
della finestra, con accanto l'ora UTC. Le email inviate dallo scheduler (promemoria, inizio, fine) puntano al dominio
personalizzato della pagina o all'URL pubblico delle impostazioni: senza nessuno dei due non vengono inviate. Un
inizio o una fine notati con più di 15 minuti di ritardo, per esempio dopo un riavvio, non vengono inviati.

## Badge

Ogni pagina pubblicata ha badge SVG in stile shields, senza accesso:

| badge | URL |
| --- | --- |
| stato della pagina | `/api/badge/<slug>/status.svg` |
| stato di un monitor | `/api/badge/<slug>/<id monitor>/status.svg` |
| uptime | `/api/badge/<slug>/<id monitor>/uptime.svg` (predefinito 30 giorni) |
| tempo di risposta | `/api/badge/<slug>/<id monitor>/response.svg` (predefinito 24 ore) |

Opzioni: `?period=24h|7d|30d|90d`, `?label=` per il testo a sinistra, `?style=flat-square` per gli angoli squadrati.
Copia il Markdown dalla pagina del monitor o dall'editor della status page:

```markdown
[![uptime](https://status.example.com/api/badge/acme/12/uptime.svg)](https://status.example.com/status/acme/)
```

Un badge non mostra mai più della sua pagina: le bozze e i monitor che non sono nella pagina rispondono con un
"not found" grigio.
