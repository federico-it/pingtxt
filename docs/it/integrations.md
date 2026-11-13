# Integrazioni

Usa pingtxt da altri strumenti: script e app con la REST API, assistenti AI con il server MCP, Prometheus con
`/metrics`.

## Token API

Crea token personali in **impostazioni → token** e inviali come bearer token:

```sh
curl -H "Authorization: Bearer ptk_..." https://status.example.com/api/monitors
```

- I token in **sola lettura** possono chiamare gli endpoint `GET`. I token **lettura + scrittura** possono anche
  creare, modificare, mettere in pausa ed eliminare.
- Un token non ha mai più diritti del suo proprietario: gli utenti in sola lettura hanno solo token di lettura, e i
  token di scrittura di un admin che diventa in sola lettura smettono di scrivere.
- I token possono scadere dopo 30, 90 o 365 giorni, o mai. Vengono mostrati una sola volta e salvati come hash.
- I token non arrivano mai agli endpoint dell'account (password, 2FA, token, utenti, impostazioni dell'istanza): lì
  serve un accesso dal browser.

## REST API

Tutti gli endpoint, con parametri, ruoli ed esempi, sono nel [riferimento API](https://pingtxt.com/docs/api/). Ogni hub
serve anche:

- il documento OpenAPI 3.1 su `/api/openapi.json`, per generare un client;
- una Swagger UI interattiva su `/api/docs` (con un link da **impostazioni → token**), dove le richieste usano la tua
  sessione del browser o un token impostato in *Authorize*.

`PUT` su un monitor, una finestra di manutenzione, un gruppo, un tag o una status page, e sulle impostazioni generali,
dei backup e dei report, cambia solo i campi presenti nel body: gli altri tengono il loro valore. Manda un campo per
cambiarlo, anche per svuotarlo (`[]`, `""`, `0`). Così uno script o un'app più vecchia che non conosce un campo nuovo
non lo azzera mai.

## Server MCP

L'hub è anche un server [MCP](https://modelcontextprotocol.io) su `/mcp` (Streamable HTTP), così un assistente AI come
Claude può rispondere a "cosa è down adesso?", guardare incidenti, tempi di risposta e diagnostica, o creare per te un
monitor o una finestra di manutenzione. **Impostazioni → mcp** mostra l'indirizzo del server, come collegarsi e gli
assistenti collegati. (Per *controllare* un server MCP, invece, vedi
[server MCP](monitors.md#server-mcp).)

### Collegarsi con OAuth

Niente da copiare: aggiungi `https://status.example.com/mcp` come connettore.

- claude.ai e Claude Desktop: **settings → connectors → add custom connector**.
- Claude Code: `claude mcp add --transport http pingtxt https://status.example.com/mcp`, poi `/mcp` per accedere.

L'assistente apre pingtxt nel browser, accedi come sempre e scegli **sola lettura** o **lettura + scrittura** (solo
admin). L'hub supporta discovery (RFC 9728 / RFC 8414), registrazione dinamica dei client, PKCE (S256) e refresh token
a rotazione. Gli access token durano un'ora e valgono su `/mcp` e sulla REST API. Gli assistenti collegati sono elencati in
**impostazioni → mcp**, da dove puoi scollegarli. Dietro un proxy, imposta l'URL pubblico in
**impostazioni → generali** perché i redirect OAuth puntino all'indirizzo giusto.

### Collegarsi con un token API

Per i client senza OAuth, invia `Authorization: Bearer ptk_...`. Un token in sola lettura (o il token di un utente in
sola lettura) ha solo i tool di lettura; un token lettura + scrittura di un admin ha anche i tool che modificano i dati.

Claude Code:

```sh
claude mcp add --transport http pingtxt https://status.example.com/mcp \
  --header "Authorization: Bearer ptk_..."
```

Claude Desktop (`claude_desktop_config.json`, tramite [mcp-remote](https://www.npmjs.com/package/mcp-remote)):

```json
{
  "mcpServers": {
    "pingtxt": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://status.example.com/mcp", "--header", "Authorization:${PINGTXT_AUTH}"],
      "env": { "PINGTXT_AUTH": "Bearer ptk_..." }
    }
  }
}
```

Cursor, Windsurf e altri client che accettano un URL e degli header:

```json
{ "mcpServers": { "pingtxt": { "url": "https://status.example.com/mcp", "headers": { "Authorization": "Bearer ptk_..." } } } }
```

### Tool

| tool | cosa fa |
| --- | --- |
| `get_overview` | monitor per stato, agent online, incidenti aperti |
| `list_monitors` | stato, uptime 24h, tempo di risposta e ultimo controllo; filtri per stato, gruppo, tag o nome |
| `get_monitor`, `get_monitor_results` | impostazioni, uptime 24h/7g/30g, stato per località, scadenze; ultimi risultati dei controlli |
| `list_incidents`, `get_incident_diagnostics` | incidenti (solo aperti, per monitor); cosa ha visto ogni località al primo errore |
| `list_maintenances` | finestre di manutenzione con la prossima occorrenza |
| `list_groups`, `list_tags`, `list_agents`, `list_status_pages` | tutto il resto che serve a un modello per trovare gli id |
| `create_monitor`, `update_monitor`, `pause_monitor`, `delete_monitor` | solo token di scrittura di un admin |
| `acknowledge_incident` | solo token di scrittura di un admin; l'incidente viene preso in carico dall'utente del token |
| `create_maintenance`, `delete_maintenance` | solo token di scrittura di un admin; orari in ISO 8601 |
| `create_group`, `update_group`, `delete_group` | solo token di scrittura di un admin; `parent_id` crea un sottogruppo o sposta un gruppo, `list_groups` con `parent_id` elenca i sottogruppi di un gruppo |
| `create_tag`, `update_tag`, `delete_tag` | solo token di scrittura di un admin |

Ruoli, validazione e segreti nascosti sono esattamente quelli della REST API, e ogni scrittura finisce nel
[registro attività](security.md#registro-attività). Gli orari nei risultati sono in ISO 8601 (UTC). Il server è senza
stato (niente sessioni, niente stream di eventi), quindi funziona dietro qualsiasi reverse proxy e con più repliche
dell'hub.

## Prometheus

`GET /metrics` serve le metriche nel formato testuale di Prometheus. Richiede un token API (basta la lettura), a meno
che non sia attivo **impostazioni → integrazioni → metriche prometheus → /metrics pubblico**. L'output resta in cache
per 10 secondi.

```yaml
scrape_configs:
  - job_name: pingtxt
    scheme: https
    authorization:
      credentials: ptk_...
    static_configs:
      - targets: ["status.example.com"]
```

| metrica | etichette | significato |
| --- | --- | --- |
| `pingtxt_monitor_up` | monitor_id, monitor, type | 1 up o degradato, 0 down (assente se in attesa, in pausa, in manutenzione) |
| `pingtxt_monitor_status` | + status | 1 per lo stato attuale (up, down, degraded, pending, paused, maintenance) |
| `pingtxt_monitor_incident_open` | monitor | 1 mentre un incidente è aperto |
| `pingtxt_monitor_uptime_ratio` | + window (24h, 7d, 30d) | quota di controlli fatti mentre il monitor era up o degradato, manutenzione esclusa |
| `pingtxt_monitor_latency_avg_seconds` | + window | tempo di risposta medio dei controlli riusciti |
| `pingtxt_check_up`, `pingtxt_check_latency_seconds`, `pingtxt_check_status_code`, `pingtxt_check_timestamp_seconds` | + location, location_name | ultimo controllo da ogni località |
| `pingtxt_check_phase_seconds` | + location, location_name, phase (dns, connect, tls, ttfb) | tempo per fase dell'ultimo controllo HTTP da ogni località |
| `pingtxt_cert_expiry_days`, `pingtxt_domain_expiry_days` (e `_timestamp_seconds`) | + subject | giorni rimasti alla scadenza |
| `pingtxt_agent_connected`, `pingtxt_agent_last_seen_timestamp_seconds` | agent_id, agent, location, location_name, version | connessione degli agent |
| `pingtxt_monitors`, `pingtxt_agents`, `pingtxt_incidents_open`, `pingtxt_info` | | totali e versione dell'hub |
