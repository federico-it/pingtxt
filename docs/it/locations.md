# Località (agent)

I controlli partono dalle **località**. L'hub è una di queste (la località integrata, che si rinomina o disattiva in
**impostazioni → generali**); ogni agent che aggiungi è un'altra. Più località distinguono un disservizio vero da un
problema di rete vicino a una di loro, e mostrano i tempi di risposta da dove si trovano i tuoi utenti.

## Aggiungere un agent

Nell'interfaccia vai in **agent → aggiungi agent**: ricevi un token e i comandi pronti da incollare.

```sh
# Docker
docker run -d --name pingtxt-agent --restart unless-stopped --cap-add NET_RAW \
  -e PINGTXT_HUB=https://status.example.com -e PINGTXT_TOKEN=pt_xxx \
  -v pingtxt-agent:/data ghcr.io/federico-it/pingtxt-agent:latest
```

```sh
# Linux + systemd (binario scaricato dal tuo hub)
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --hub https://status.example.com --token pt_xxx

# disinstallazione
curl -fsSL https://status.example.com/install-agent.sh | sudo sh -s -- --uninstall
```

L'agent è un binario statico di circa 8 MB per Linux (amd64, arm64, arm) e macOS. Si collega solo in uscita verso
l'hub via WebSocket: nessuna porta aperta, funziona dietro NAT, si ricollega con backoff e non ha stato. Funzionano
anche gli hub su IPv6 (`PINGTXT_HUB=http://[2001:db8::1]:8090`).

Quando un nuovo agent si collega, l'hub propone di aggiungerlo a tutti i monitor che può eseguire. Altrimenti spunta
la località nei monitor che vuoi: l'hub pianifica ogni controllo e lo manda agli agent.

La pagina degli agent mostra di ognuno versione, indirizzo, se raggiunge IPv6 e quando è stato visto l'ultima volta.
**rigenera** dà a un agent un nuovo token: quello vecchio smette subito di funzionare e l'agent si disconnette.

## Come votano le località

Con più località il monitor è **down** quando fallisce più della metà delle località online, e **degradato** quando
almeno la quota di degradato fallisce o è lenta (35% di default). L'uptime segue lo stato del monitor, quindi una
località in errore tra tante non lo abbassa. Vedi [Concetti](concepts.md#stato).

Ogni località ha anche la sua linea dei tempi di risposta nella pagina del monitor, e può avere la sua
[soglia di lentezza automatica](monitors.md#degradato-e-soglia-di-lentezza).

## Avvisi di agent offline

Quando un agent resta disconnesso più a lungo del **periodo di tolleranza** (5 minuti di default, impostato nella
pagina degli agent, da 1 a 1440), i canali con l'evento `agent_offline` ricevono un avviso *offline*, e uno *online*
quando l'agent si ricollega. Le interruzioni brevi non inviano nulla.

Nessun avviso parte mentre l'agent si aggiorna (almeno 10 minuti di tolleranza), dopo che un admin ne ha rigenerato
il token, quando l'interruttore **avvisa quando questo agent va offline** dell'agent è spento, o mentre tutti i
monitor che controlla sono in manutenzione (parte quando la finestra finisce). I webhook includono `kind: "agent"`,
`status` (`offline` o `online`), `agent_id`, `agent`, `target` (il suo nome host) e `down_since`.

Un monitor che gira solo su agent offline oltre la tolleranza non riceve controlli: passa **in attesa** (nessun
incidente, nessun downtime) finché un agent non si ricollega. La dashboard mostra un banner con gli agent offline.

I nuovi canali hanno l'evento attivo di default. L'evento riguarda gli agent, quindi ignora l'ambito dei monitor del
canale.

## Aggiornamento degli agent

L'hub include i binari degli agent compilati con la sua stessa versione. Quando aggiorni l'hub, gli agent
(installati con lo script o in Docker) si aggiornano da soli appena si ricollegano: l'hub manda il percorso di
download e lo SHA-256 del binario per la piattaforma dell'agent, l'agent lo scarica dall'hub, verifica il checksum e
che il nuovo binario riporti la versione attesa, sostituisce il file e si riavvia. Un aggiornamento fallito lascia in
funzione il binario precedente e compare nella pagina degli agent; l'hub riprova dopo 6 ore, o dopo pochi minuti se
i file della release erano ancora in caricamento.

- **agent → aggiorna gli agent automaticamente** lo attiva o disattiva (attivo di default). Ogni agent non
  aggiornato ha anche un pulsante **aggiorna**.
- Per tenere un agent alla sua versione, avvialo con `-no-auto-update` (oppure `PINGTXT_NO_AUTO_UPDATE=1` in
  `/etc/pingtxt-agent.env`).
- Gli agent si aggiornano da soli solo da un hub raggiunto in **https** (o in http semplice su un indirizzo IP di
  loopback o privato): in http semplice chiunque sul percorso potrebbe sostituire binario e checksum. Per accettare
  il rischio, usa `-insecure-update` (`PINGTXT_INSECURE_UPDATE=1`).
- Gli agent installati prima degli aggiornamenti automatici girano come `DynamicUser` systemd in sola lettura:
  esegui di nuovo lo script di installazione una volta. Ora installa in `/opt/pingtxt-agent`, di proprietà
  dell'utente `pingtxt-agent`.

### Checksum da GitHub

Prima di installare, gli agent confrontano il checksum dell'hub con il file `SHA256SUMS` della release GitHub di
quella versione (l'hub include gli stessi binari, compilati in modo riproducibile). Una differenza, o una release
senza checksum per la piattaforma, rifiuta l'aggiornamento, così un hub manomesso non può distribuire un suo binario.
Un hub nuovo aspetta fino a 30 minuti che la sua release su GitHub abbia `SHA256SUMS` (caricato pochi minuti dopo
l'immagine Docker); con il [controllo degli aggiornamenti](administration.md#controllo-degli-aggiornamenti) spento
aggiorna subito gli agent.

`-verify-update` (`PINGTXT_VERIFY_UPDATE`) stabilisce quanto essere severi:

| valore | |
| --- | --- |
| `auto` (predefinito) | verifica le versioni di release; se GitHub non è raggiungibile l'aggiornamento procede con un avviso nel log dell'agent. Le build di sviluppo non vengono verificate |
| `strict` | installa solo release pubblicate con checksum corrispondente. Usalo quando gli agent raggiungono github.com |
| `off` | si fida dell'hub, per fork e build private (oppure punta `-release-url` / `PINGTXT_RELEASE_URL` ai download delle tue release) |

### Agent in Docker

Gli agent in Docker tengono il nuovo binario nel volume `/data` (`PINGTXT_UPDATE_DIR`) e lo eseguono da lì a ogni
riavvio, quindi non serve il socket Docker. Monta un volume con nome (`-v pingtxt-agent:/data`) per non perdere gli
aggiornamenti quando il container viene ricreato. Quando scarichi una nuova immagine riparte il suo binario, il
volume viene svuotato e l'hub lo aggiorna se serve.

Se invece vuoi container immutabili (Kubernetes, file system in sola lettura), avvia l'agent con
`PINGTXT_NO_AUTO_UPDATE=1` e lascia che un aggiornatore di immagini scarichi `:latest`, come
[What's Up Docker](https://github.com/getwud/wud) o il [fork mantenuto di Watchtower](https://github.com/nicholas-fedor/watchtower).
Serve il socket Docker, e l'agent segue così l'ultima release invece della versione dell'hub.

## Impostazioni dell'agent

| env | opzione | |
| --- | --- | --- |
| `PINGTXT_HUB` | `-hub` | URL dell'hub |
| `PINGTXT_TOKEN` | `-token` | il token dell'agent |
| `PINGTXT_NO_AUTO_UPDATE` | `-no-auto-update` | non si aggiorna mai da solo |
| `PINGTXT_INSECURE_UPDATE` | `-insecure-update` | permette gli aggiornamenti da un hub in http semplice |
| `PINGTXT_VERIFY_UPDATE` | `-verify-update` | `auto`, `strict` o `off`, vedi sopra |
| `PINGTXT_RELEASE_URL` | `-release-url` | da dove scaricare i checksum delle release |
| `PINGTXT_UPDATE_DIR` | `-update-dir` | in un container, il volume che conserva gli aggiornamenti |
| | `-concurrency` | controlli eseguiti in parallelo (32) |
| `PINGTXT_IPV6` | | `on` o `off` salta la verifica della raggiungibilità IPv6 |

L'installazione systemd le tiene in `/etc/pingtxt-agent.env`. I controlli Docker richiedono il socket Docker
sull'agent, vedi [container docker](monitors.md#database-container-grpc-e-smtp).
