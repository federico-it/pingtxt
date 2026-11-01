import type { Metadata } from "next"

import { Prompt, REPO, SectionTitle } from "@/components/site-shell"
import { localePath, tx, type Lang } from "@/lib/i18n"
import { pageMeta } from "@/lib/seo"

// Every text is t(english, italiano). When you change one, change the other,
// and the date in both languages.

export function privacyMeta(lang: Lang): Metadata {
  const t = tx(lang)
  return pageMeta({
    lang,
    title: t("privacy policy", "informativa sulla privacy"),
    description: t(
      "Privacy policy of the pingtxt iOS app and the pingtxt.com website: what is sent where, and what is stored.",
      "Informativa sulla privacy dell'app iOS di pingtxt e del sito pingtxt.com: cosa viene inviato, dove, e cosa viene conservato.",
    ),
    path: "/privacy/",
  })
}

// Who answers for this policy. Change it here only.
const OWNER = "Federico Mengascini"
const CONTACT = "privacy@pingtxt.com"
const UPDATED = { en: "November 1, 2026", it: "1 novembre 2026" }

const RELAY_REPO = "https://github.com/federico-it/pingtxt-push-relay"
// The app asks this server whether a newer version exists.
const UPDATES_HOST = "federicomengascini.com"

const toc: [string, string, string][] = [
  ["summary", "summary", "in breve"],
  ["who", "who we are", "chi siamo"],
  ["hub", "your hub", "il tuo hub"],
  ["device", "on your iphone", "sul tuo iphone"],
  ["push", "push notifications", "notifiche push"],
  ["live-activity", "live activity", "live activity"],
  ["updates", "update check", "controllo aggiornamenti"],
  ["not-collected", "what we don't collect", "cosa non raccogliamo"],
  ["third-parties", "third parties", "terze parti"],
  ["retention", "retention + deletion", "conservazione + cancellazione"],
  ["rights", "your rights", "i tuoi diritti"],
  ["children", "children", "minori"],
  ["website", "this website", "questo sito"],
  ["hub-stats", "hub usage stats", "statistiche d'uso dell'hub"],
  ["changes", "changes", "modifiche"],
  ["contact", "contact", "contatti"],
]

function A({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a href={href} className="text-foreground underline underline-offset-4">
      {children}
    </a>
  )
}

function C({ children }: { children: React.ReactNode }) {
  return <code className="bg-muted px-1 text-foreground">{children}</code>
}

function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3 [&_li]:ml-4 [&_li]:list-['-_'] [&_p]:text-muted-foreground [&_ul]:space-y-1 [&_ul]:text-muted-foreground">
      <SectionTitle id={id}>{title}</SectionTitle>
      {children}
    </section>
  )
}

export function PrivacyPage({ lang }: { lang: Lang }) {
  const t = tx(lang)
  const mail = <A href={`mailto:${CONTACT}`}>{CONTACT}</A>
  return (
    <div className="grid gap-10 py-12 lg:grid-cols-[180px_1fr]">
      <aside className="lg:sticky lg:top-20 lg:self-start">
        <div className="mb-2 text-[11px] uppercase tracking-wider text-muted-foreground">{t("contents", "indice")}</div>
        <nav className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
          {toc.map(([id, en, it]) => (
            <a key={id} href={`#${id}`} className="text-muted-foreground transition-colors hover:text-foreground">
              {t(en, it)}
            </a>
          ))}
        </nav>
      </aside>

      <div className="min-w-0 max-w-3xl space-y-12">
        <div className="space-y-2">
          <Prompt>cat PRIVACY</Prompt>
          <h1 className="text-xl font-semibold">{t("privacy policy", "informativa sulla privacy")}</h1>
          <p className="text-muted-foreground">
            {t(
              `For the pingtxt iOS app and the pingtxt.com website. Last updated ${UPDATED.en}.`,
              `Per l'app iOS di pingtxt e il sito pingtxt.com. Ultimo aggiornamento: ${UPDATED.it}.`,
            )}
          </p>
        </div>

        <Section id="summary" title={t("summary", "in breve")}>
          <ul>
            <li>
              {t(
                "pingtxt is self-hosted. The app talks to the hub that you (or your organisation) run, not to a pingtxt cloud.",
                "pingtxt è self-hosted. L'app parla con l'hub gestito da te (o dalla tua organizzazione), non con un cloud di pingtxt.",
              )}
            </li>
            <li>
              {t(
                "We have no user accounts, no advertising and no tracking. We don't sell or share personal data.",
                "Non abbiamo account utente, pubblicità né tracciamento. Non vendiamo né condividiamo dati personali.",
              )}
            </li>
            <li>
              {t(
                <>
                  The app uses two services that we run: the push relay at <C>push.pingtxt.com</C>, which forwards alerts
                  from your hub to your phone through Apple and Google Firebase and stores nothing, and an update check at{" "}
                  <C>{UPDATES_HOST}</C>, which tells the app when a new version is out.
                </>,
                <>
                  L&apos;app usa due servizi gestiti da noi: il relay push su <C>push.pingtxt.com</C>, che inoltra gli avvisi
                  dal tuo hub al tuo telefono tramite Apple e Google Firebase e non conserva nulla, e un controllo degli
                  aggiornamenti su <C>{UPDATES_HOST}</C>, che avvisa l&apos;app quando esce una nuova versione.
                </>,
              )}
            </li>
            <li>
              {t(
                "The update check uses a random installation id to count active installations. It is not linked to you, your hub or your phone.",
                "Il controllo degli aggiornamenti usa un id di installazione casuale per contare le installazioni attive. Non è collegato a te, al tuo hub né al tuo telefono.",
              )}
            </li>
          </ul>
        </Section>

        <Section id="who" title={t("who we are", "chi siamo")}>
          <p>
            {t(
              <>
                pingtxt is an open source project (<A href={`${REPO}/blob/main/LICENSE`}>AGPL-3.0</A>) maintained by {OWNER},
                who is the controller for the data handled by the push relay, the update check and this website. Write to {mail} for anything
                about this policy.
              </>,
              <>
                pingtxt è un progetto open source (<A href={`${REPO}/blob/main/LICENSE`}>AGPL-3.0</A>) mantenuto da {OWNER},
                titolare del trattamento dei dati gestiti dal relay push, dal controllo degli aggiornamenti e da questo sito. Scrivi a {mail} per qualsiasi
                domanda su questa informativa.
              </>,
            )}
          </p>
        </Section>

        <Section id="hub" title={t("your hub", "il tuo hub")}>
          <p>
            {t(
              "To use the app you sign in to a pingtxt hub by entering its address. Sign-in uses OAuth in the system browser: your password is typed on your hub's own page and never passes through the app or through us. Your hub gives the app an access token and a refresh token.",
              "Per usare l'app accedi a un hub pingtxt inserendone l'indirizzo. L'accesso usa OAuth nel browser di sistema: la password si scrive sulla pagina del tuo hub e non passa mai dall'app né da noi. Il tuo hub dà all'app un access token e un refresh token.",
            )}
          </p>
          <p>
            {t(
              "Everything the app shows (monitors, check results, incidents, maintenance windows, status pages, agents, your account) is read from your hub, and every change you make is sent to your hub. The hub stores which phone is signed in: the sign-in, the device name that iOS gives apps (the model, such as \"iPhone\", not the name you chose) and, when notifications are on, an encrypted push handle.",
              "Tutto quello che l'app mostra (monitor, risultati dei controlli, incidenti, finestre di manutenzione, pagine di stato, agent, il tuo account) viene letto dal tuo hub, e ogni modifica che fai viene inviata al tuo hub. L'hub conserva quale telefono ha effettuato l'accesso: l'accesso, il nome del dispositivo che iOS dà alle app (il modello, per esempio \"iPhone\", non il nome che hai scelto) e, se le notifiche sono attive, un identificativo push cifrato.",
            )}
          </p>
          <p>
            {t(
              "If Background App Refresh is on, iOS lets the app load the status of your hubs now and then while it is closed, and the widgets load it from your hub too, so that widgets and Live Activities stay current. These requests go only to your hubs.",
              "Se l'aggiornamento app in background è attivo, iOS lascia che l'app carichi ogni tanto lo stato dei tuoi hub anche quando è chiusa, e anche i widget lo caricano dal tuo hub, così widget e Live Activity restano aggiornati. Queste richieste vanno solo ai tuoi hub.",
            )}
          </p>
          <p>
            {t(
              "Your hub is run by you or by whoever gave you access, not by us. That operator decides what is stored there and for how long; their own privacy policy applies to that data. We have no access to any hub.",
              "Il tuo hub è gestito da te o da chi ti ha dato l'accesso, non da noi. È quel gestore a decidere cosa vi si conserva e per quanto tempo; a quei dati si applica la sua informativa sulla privacy. Noi non abbiamo accesso a nessun hub.",
            )}
          </p>
        </Section>

        <Section id="device" title={t("on your iphone", "sul tuo iphone")}>
          <ul>
            <li>
              {t(
                "Access and refresh tokens are kept in the iOS Keychain, readable only on this device after it was unlocked.",
                "Access token e refresh token sono conservati nel Portachiavi di iOS, leggibili solo su questo dispositivo dopo che è stato sbloccato.",
              )}
            </li>
            <li>
              {t(
                "The addresses of hubs you used, a few preferences, the random installation id of the update check and its last answer are kept in the app's settings (UserDefaults), and the latest hub status is shared with the pingtxt widgets through an App Group on the device.",
                "Gli indirizzi degli hub che hai usato, alcune preferenze, l'id di installazione casuale del controllo aggiornamenti e la sua ultima risposta sono conservati nelle impostazioni dell'app (UserDefaults), e l'ultimo stato dell'hub è condiviso con i widget di pingtxt tramite un App Group sul dispositivo.",
              )}
            </li>
            <li>
              {t(
                "Nothing of this is backed up to us. Signing out removes the tokens of that hub; deleting the app removes everything.",
                "Niente di tutto questo viene salvato da noi. Uscendo da un hub vengono rimossi i suoi token; eliminando l'app viene rimosso tutto.",
              )}
            </li>
          </ul>
        </Section>

        <Section id="push" title={t("push notifications", "notifiche push")}>
          <p>
            {t(
              "Notifications are optional. If you allow them and your hub has push turned on:",
              "Le notifiche sono facoltative. Se le consenti e il tuo hub ha le push attive:",
            )}
          </p>
          <ul>
            <li>
              {t(
                "iOS gives the app a token, which the Google Firebase Cloud Messaging (FCM) SDK turns into an FCM registration token. To do so Firebase uses a Firebase installation ID and basic app and device information (app version, iOS version, device model).",
                "iOS dà all'app un token, che l'SDK di Google Firebase Cloud Messaging (FCM) trasforma in un token di registrazione FCM. Per farlo Firebase usa un ID di installazione Firebase e informazioni di base sull'app e sul dispositivo (versione dell'app, versione di iOS, modello del dispositivo).",
              )}
            </li>
            <li>
              {t(
                <>
                  The app sends the FCM token and your hub&apos;s id to the relay at <C>push.pingtxt.com</C>. The relay
                  encrypts them into a sealed device handle, returns it and keeps nothing. Only the relay can open the handle;
                  your hub never sees the FCM token.
                </>,
                <>
                  L&apos;app invia il token FCM e l&apos;id del tuo hub al relay su <C>push.pingtxt.com</C>. Il relay li cifra
                  in un identificativo del dispositivo sigillato, lo restituisce e non conserva nulla. Solo il relay può
                  aprire l&apos;identificativo; il tuo hub non vede mai il token FCM.
                </>,
              )}
            </li>
            <li>
              {t(
                "The app gives the handle and the device name to your hub.",
                "L'app passa l'identificativo e il nome del dispositivo al tuo hub.",
              )}
            </li>
            <li>
              {t(
                "When a monitor changes state, your hub sends the alert text (for example the monitor's name and the cause) with the handles to the relay. The relay opens the handles and passes the message to FCM, which delivers it through the Apple Push Notification service. The relay does not store messages.",
                "Quando un monitor cambia stato, il tuo hub invia al relay il testo dell'avviso (per esempio il nome del monitor e la causa) insieme agli identificativi. Il relay apre gli identificativi e passa il messaggio a FCM, che lo consegna tramite l'Apple Push Notification service. Il relay non conserva i messaggi.",
              )}
            </li>
          </ul>
          <p>
            {t(
              "You can turn notifications off in iOS Settings at any time. Signing out removes the phone from your hub and deletes the FCM token.",
              "Puoi disattivare le notifiche nelle Impostazioni di iOS in qualsiasi momento. Uscendo dall'account il telefono viene rimosso dal tuo hub e il token FCM viene eliminato.",
            )}
          </p>
        </Section>

        <Section id="live-activity" title="live activity">
          <p>
            {t(
              "During an outage the app can show a Live Activity on the Lock Screen and in the Dynamic Island. To keep it current while the app is closed, iOS gives the app Live Activity tokens, which the app sends to your hub; your hub sends updates (open incidents: monitor name, start time, cause) with those tokens through the relay, FCM and Apple, as above. You can turn Live Activities off in iOS Settings.",
              "Durante un disservizio l'app può mostrare una Live Activity nella schermata di blocco e nella Dynamic Island. Per tenerla aggiornata anche ad app chiusa, iOS dà all'app dei token Live Activity, che l'app invia al tuo hub; il tuo hub invia gli aggiornamenti (incidenti aperti: nome del monitor, ora di inizio, causa) con quei token tramite il relay, FCM e Apple, come sopra. Puoi disattivare le Live Activity nelle Impostazioni di iOS.",
            )}
          </p>
        </Section>

        <Section id="updates" title={t("update check", "controllo aggiornamenti")}>
          <p>
            {t(
              <>
                When the app starts and when you come back to it, it asks <C>{UPDATES_HOST}</C> whether a newer version is
                available or required. The request carries:
              </>,
              <>
                Quando l&apos;app si avvia e quando ci torni, chiede a <C>{UPDATES_HOST}</C> se è disponibile o necessaria una
                versione più recente. La richiesta contiene:
              </>,
            )}
          </p>
          <ul>
            <li>
              {t(
                "the installed app version, the iOS version (for example 26.1) and the platform (ios);",
                "la versione dell'app installata, la versione di iOS (per esempio 26.1) e la piattaforma (ios);",
              )}
            </li>
            <li>
              {t(
                "where the app was installed from (App Store, TestFlight or a development build);",
                "da dove è stata installata l'app (App Store, TestFlight o una build di sviluppo);",
              )}
            </li>
            <li>
              {t(
                "a random installation id made by the app on first use. It is not the device id, the advertising id or anything from your hub, and it changes if you delete and install the app again;",
                "un id di installazione casuale creato dall'app al primo utilizzo. Non è l'id del dispositivo, l'identificativo pubblicitario né qualcosa del tuo hub, e cambia se elimini e reinstalli l'app;",
              )}
            </li>
            <li>
              {t(
                "like any web request, your IP address, which the server does not store or use.",
                "come ogni richiesta web, il tuo indirizzo IP, che il server non conserva né usa.",
              )}
            </li>
          </ul>
          <p>
            {t(
              "The server answers with the latest version and whether yours is still supported. The installation id is stored only as a one-way hash, to roll out new versions gradually and to count active installations and versions. Nothing about your hubs, monitors or account is sent. The request is skipped if the server cannot be reached; the app works the same.",
              "Il server risponde con l'ultima versione e se la tua è ancora supportata. L'id di installazione viene conservato solo come hash non reversibile, per distribuire le nuove versioni in modo graduale e contare installazioni attive e versioni. Non viene inviato nulla sui tuoi hub, monitor o account. Se il server non è raggiungibile la richiesta viene saltata e l'app funziona lo stesso.",
            )}
          </p>
        </Section>

        <Section id="not-collected" title={t("what we don't collect", "cosa non raccogliamo")}>
          <ul>
            <li>
              {t(
                "No analytics SDK or crash reporting: Firebase Analytics is not included and is turned off. The only count we keep is the anonymous one of the update check.",
                "Nessun SDK di analytics né segnalazione dei crash: Firebase Analytics non è incluso ed è disattivato. L'unico conteggio che teniamo è quello anonimo del controllo aggiornamenti.",
              )}
            </li>
            <li>
              {t(
                "No advertising identifier (IDFA), no tracking across apps or websites, no data brokers.",
                "Nessun identificativo pubblicitario (IDFA), nessun tracciamento tra app o siti web, nessun data broker.",
              )}
            </li>
            <li>
              {t(
                "No location, contacts, photos, health or payment data.",
                "Nessun dato su posizione, contatti, foto, salute o pagamenti.",
              )}
            </li>
            <li>
              {t(
                "No account with us: your identity lives only on your hub.",
                "Nessun account presso di noi: la tua identità esiste solo sul tuo hub.",
              )}
            </li>
          </ul>
        </Section>

        <Section id="third-parties" title={t("third parties", "terze parti")}>
          <ul>
            <li>
              {t(
                <>
                  <b className="text-foreground">Google Firebase Cloud Messaging</b> (Google LLC / Google Ireland Ltd), to
                  deliver notifications. See{" "}
                  <A href="https://firebase.google.com/support/privacy">Privacy and Security in Firebase</A>.
                </>,
                <>
                  <b className="text-foreground">Google Firebase Cloud Messaging</b> (Google LLC / Google Ireland Ltd), per
                  consegnare le notifiche. Vedi{" "}
                  <A href="https://firebase.google.com/support/privacy">Privacy and Security in Firebase</A>.
                </>,
              )}
            </li>
            <li>
              {t(
                <>
                  <b className="text-foreground">Apple Push Notification service</b> (Apple Inc.), to deliver notifications
                  and Live Activity updates. See <A href="https://www.apple.com/legal/privacy/">Apple&apos;s privacy policy</A>.
                </>,
                <>
                  <b className="text-foreground">Apple Push Notification service</b> (Apple Inc.), per consegnare le notifiche
                  e gli aggiornamenti delle Live Activity. Vedi l&apos;
                  <A href="https://www.apple.com/it/legal/privacy/">informativa sulla privacy di Apple</A>.
                </>,
              )}
            </li>
          </ul>
          <p>
            {t(
              "These providers may process data outside your country, including in the United States, under their own safeguards (such as the EU Standard Contractual Clauses).",
              "Questi fornitori possono trattare i dati fuori dal tuo paese, anche negli Stati Uniti, con le proprie garanzie (come le Clausole Contrattuali Standard dell'UE).",
            )}
          </p>
        </Section>

        <Section id="retention" title={t("retention + deletion", "conservazione + cancellazione")}>
          <ul>
            <li>
              {t(
                "The relay has no database. Its logs hold hub ids and counts of sent messages, never tokens, message text or IP addresses. IP addresses are held in memory only for rate limiting and dropped within minutes.",
                "Il relay non ha un database. I suoi log contengono gli id degli hub e il numero di messaggi inviati, mai token, testo dei messaggi o indirizzi IP. Gli indirizzi IP sono tenuti in memoria solo per limitare le richieste e scartati entro pochi minuti.",
              )}
            </li>
            <li>
              {t(
                "Firebase keeps an FCM token until it is deleted (at sign-out or when the app is removed) or has been unused for a long time; see Google's documentation.",
                "Firebase conserva un token FCM finché non viene eliminato (all'uscita dall'account o quando l'app viene rimossa) o resta inutilizzato a lungo; vedi la documentazione di Google.",
              )}
            </li>
            <li>
              {t(
                "The update check keeps only the hashed installation id with the app and iOS version it last reported, and the daily counts made from them. It keeps no IP addresses.",
                "Il controllo aggiornamenti conserva solo l'hash dell'id di installazione con l'ultima versione dell'app e di iOS che ha inviato, e i conteggi giornalieri che ne derivano. Non conserva indirizzi IP.",
              )}
            </li>
            <li>
              {t(
                "To delete everything: sign out of each hub in the app, then delete the app. Data kept on a hub can be removed by its operator.",
                "Per cancellare tutto: esci da ogni hub nell'app, poi elimina l'app. I dati conservati su un hub possono essere rimossi dal suo gestore.",
              )}
            </li>
          </ul>
        </Section>

        <Section id="rights" title={t("your rights", "i tuoi diritti")}>
          <p>
            {t(
              <>
                Depending on where you live (for example under the EU/UK GDPR or Californian law) you can ask to access,
                correct, delete or export your personal data, object to or limit its processing, and complain to your data
                protection authority. The legal basis for the push relay is the performance of the service you asked for
                (delivering your alerts); for the update check it is our legitimate interest in keeping the app up to date
                and knowing how many installations use each version. Because the relay stores nothing and the installation id
                is anonymous, we usually hold no data about you; for data on a
                hub, contact its operator. Write to {mail}.
              </>,
              <>
                A seconda di dove vivi (per esempio in base al GDPR dell&apos;UE o del Regno Unito o alla legge della
                California) puoi chiedere di accedere ai tuoi dati personali, correggerli, cancellarli o esportarli,
                opporti al loro trattamento o limitarlo, e presentare reclamo all&apos;autorità per la protezione dei dati (in
                Italia il Garante per la protezione dei dati personali). La base giuridica del relay push è
                l&apos;esecuzione del servizio che hai richiesto (consegnare i tuoi avvisi); per il controllo degli
                aggiornamenti è il nostro legittimo interesse a tenere aggiornata l&apos;app e sapere quante installazioni usano
                ogni versione. Poiché il relay non conserva nulla e l&apos;id di installazione è anonimo, di solito non abbiamo
                dati che ti riguardano; per i dati su un hub, contatta il suo gestore. Scrivi a{" "}
                {mail}.
              </>,
            )}
          </p>
        </Section>

        <Section id="children" title={t("children", "minori")}>
          <p>
            {t(
              "pingtxt is a tool for monitoring servers and is not directed at children under 13 (or 16 in the EU).",
              "pingtxt è uno strumento per monitorare server e non è rivolto a minori di 13 anni (16 nell'UE).",
            )}
          </p>
        </Section>

        <Section id="website" title={t("this website", "questo sito")}>
          <p>
            {t(
              <>
                pingtxt.com is a static site hosted on GitHub Pages. GitHub may log visitors&apos; IP addresses for security;
                see the{" "}
                <A href="https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement">
                  GitHub privacy statement
                </A>
                . The site sets no cookies and has no analytics. Your theme and language choices are kept in your browser&apos;s local
                storage. The page loads the public status of <C>status.pingtxt.com</C> and the public usage totals from{" "}
                <C>telemetry.pingtxt.com</C>; these requests carry no identifier.
              </>,
              <>
                pingtxt.com è un sito statico ospitato su GitHub Pages. GitHub può registrare gli indirizzi IP dei
                visitatori per motivi di sicurezza; vedi il{" "}
                <A href="https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement">
                  GitHub privacy statement
                </A>
                . Il sito non imposta cookie e non ha analytics. Il tema e la lingua che scegli sono conservati nel local
                storage del tuo browser. La pagina carica lo stato pubblico di <C>status.pingtxt.com</C> e i totali
                pubblici d&apos;uso da <C>telemetry.pingtxt.com</C>; queste richieste non portano alcun identificativo.
              </>,
            )}
          </p>
        </Section>

        <Section id="hub-stats" title={t("hub usage stats", "statistiche d'uso dell'hub")}>
          <p>
            {t(
              <>
                The self-hosted hub can send anonymous usage stats (a random id, its version and how many monitors and agents
                are live) once an hour, only if an admin turns it on. It is not part of the app. See{" "}
                <A href={localePath(lang, "/docs/#usage-stats")}>usage stats</A> in the docs.
              </>,
              <>
                L&apos;hub self-hosted può inviare statistiche d&apos;uso anonime (un id casuale, la sua versione e quanti
                monitor e agent sono attivi) una volta all&apos;ora, solo se un admin lo attiva. Non fa parte dell&apos;app.
                Vedi <A href={localePath(lang, "/docs/#usage-stats")}>statistiche d&apos;uso</A> nella documentazione.
              </>,
            )}
          </p>
        </Section>

        <Section id="changes" title={t("changes", "modifiche")}>
          <p>
            {t(
              <>
                We will update this page when the app, the relay or the update check changes what it handles, and change the date at the top.
                The history is public in the <A href={`${REPO}/commits/main/site/content/privacy.tsx`}>repository</A>. The
                relay source code is public too: <A href={RELAY_REPO}>pingtxt-push-relay</A>.
              </>,
              <>
                Aggiorneremo questa pagina quando l&apos;app, il relay o il controllo aggiornamenti cambiano i dati che trattano, e cambieremo la data in
                alto. Lo storico è pubblico nel <A href={`${REPO}/commits/main/site/content/privacy.tsx`}>repository</A>.
                Anche il codice sorgente del relay è pubblico: <A href={RELAY_REPO}>pingtxt-push-relay</A>.
              </>,
            )}
          </p>
        </Section>

        <Section id="contact" title={t("contact", "contatti")}>
          <p>
            {OWNER} · {mail}
          </p>
        </Section>
      </div>
    </div>
  )
}
