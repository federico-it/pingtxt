import type { Metadata } from "next"

import { Prompt, REPO, SectionTitle } from "@/components/site-shell"

export const metadata: Metadata = {
  title: "privacy policy",
  description: "Privacy policy of the pingtxt iOS app and the pingtxt.com website: what is sent where, and what is stored.",
  alternates: { canonical: "/privacy/" },
}

// Who answers for this policy. Change it here only.
const OWNER = "Federico Mengascini"
const CONTACT = "privacy@pingtxt.com"
const UPDATED = "November 1, 2026"

const RELAY_REPO = "https://github.com/federico-it/pingtxt-push-relay"

const toc = [
  ["summary", "summary"],
  ["who", "who we are"],
  ["hub", "your hub"],
  ["device", "on your iphone"],
  ["push", "push notifications"],
  ["live-activity", "live activity"],
  ["not-collected", "what we don't collect"],
  ["third-parties", "third parties"],
  ["retention", "retention + deletion"],
  ["rights", "your rights"],
  ["children", "children"],
  ["website", "this website"],
  ["hub-stats", "hub usage stats"],
  ["changes", "changes"],
  ["contact", "contact"],
] as const

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

export default function Privacy() {
  return (
    <div className="grid gap-10 py-12 lg:grid-cols-[180px_1fr]">
      <aside className="lg:sticky lg:top-20 lg:self-start">
        <div className="mb-2 text-[11px] uppercase tracking-wider text-muted-foreground">contents</div>
        <nav className="flex flex-wrap gap-x-3 gap-y-0.5 lg:flex-col">
          {toc.map(([id, label]) => (
            <a key={id} href={`#${id}`} className="text-muted-foreground transition-colors hover:text-foreground">
              {label}
            </a>
          ))}
        </nav>
      </aside>

      <div className="min-w-0 max-w-3xl space-y-12">
        <div className="space-y-2">
          <Prompt>cat PRIVACY</Prompt>
          <h1 className="text-xl font-semibold">privacy policy</h1>
          <p className="text-muted-foreground">
            For the pingtxt iOS app and the pingtxt.com website. Last updated {UPDATED}.
          </p>
        </div>

        <Section id="summary" title="summary">
          <ul>
            <li>pingtxt is self-hosted. The app talks to the hub that you (or your organisation) run, not to a pingtxt cloud.</li>
            <li>
              We have no user accounts, no analytics, no advertising and no tracking. We don&apos;t sell or share personal
              data.
            </li>
            <li>
              The only service we run that the app uses is the push relay at <C>push.pingtxt.com</C>. It forwards alerts
              from your hub to your phone through Apple and Google Firebase, and it stores nothing.
            </li>
          </ul>
        </Section>

        <Section id="who" title="who we are">
          <p>
            pingtxt is an open source project (<A href={`${REPO}/blob/main/LICENSE`}>AGPL-3.0</A>) maintained by {OWNER}, who
            is the controller for the data handled by the push relay and this website. Write to{" "}
            <A href={`mailto:${CONTACT}`}>{CONTACT}</A> for anything about this policy.
          </p>
        </Section>

        <Section id="hub" title="your hub">
          <p>
            To use the app you sign in to a pingtxt hub by entering its address. Sign-in uses OAuth in the system browser:
            your password is typed on your hub&apos;s own page and never passes through the app or through us. Your hub
            gives the app an access token and a refresh token.
          </p>
          <p>
            Everything the app shows (monitors, check results, incidents, maintenance windows, agents, your account) is read
            from your hub, and every change you make is sent to your hub. The hub stores which phone is signed in (the
            sign-in, the device name set in iOS, and an encrypted push handle when notifications are on).
          </p>
          <p>
            Your hub is run by you or by whoever gave you access, not by us. That operator decides what is stored there and
            for how long; their own privacy policy applies to that data. We have no access to any hub.
          </p>
        </Section>

        <Section id="device" title="on your iphone">
          <ul>
            <li>Access and refresh tokens are kept in the iOS Keychain, readable only on this device after it was unlocked.</li>
            <li>
              The addresses of hubs you used and a few preferences are kept in the app&apos;s settings (UserDefaults), and
              the latest hub status is shared with the pingtxt widgets through an App Group on the device.
            </li>
            <li>Nothing of this is backed up to us. Signing out or deleting the app removes it.</li>
          </ul>
        </Section>

        <Section id="push" title="push notifications">
          <p>Notifications are optional. If you allow them and your hub has push turned on:</p>
          <ul>
            <li>
              iOS gives the app a token, which the Google Firebase Cloud Messaging (FCM) SDK turns into an FCM registration
              token. To do so Firebase uses a Firebase installation ID and basic app and device information (app version,
              iOS version, device model).
            </li>
            <li>
              The app sends the FCM token and your hub&apos;s id to the relay at <C>push.pingtxt.com</C>. The relay encrypts
              them into a sealed device handle, returns it and keeps nothing. Only the relay can open the handle; your hub
              never sees the FCM token.
            </li>
            <li>The app gives the handle and the device name to your hub.</li>
            <li>
              When a monitor changes state, your hub sends the alert text (for example the monitor&apos;s name and the cause)
              with the handles to the relay. The relay opens the handles and passes the message to FCM, which delivers it
              through the Apple Push Notification service. The relay does not store messages.
            </li>
          </ul>
          <p>
            You can turn notifications off in iOS Settings at any time. Signing out removes the phone from your hub and
            deletes the FCM token.
          </p>
        </Section>

        <Section id="live-activity" title="live activity">
          <p>
            During an outage the app can show a Live Activity on the Lock Screen and in the Dynamic Island. To keep it
            current while the app is closed, iOS gives the app Live Activity tokens, which the app sends to your hub; your
            hub sends updates (open incidents: monitor name, start time, cause) with those tokens through the relay, FCM
            and Apple, as above. You can turn Live Activities off in iOS Settings.
          </p>
        </Section>

        <Section id="not-collected" title="what we don't collect">
          <ul>
            <li>No analytics or crash reporting: Firebase Analytics is not included and is turned off.</li>
            <li>No advertising identifier (IDFA), no tracking across apps or websites, no data brokers.</li>
            <li>No location, contacts, photos, health or payment data.</li>
            <li>No account with us: your identity lives only on your hub.</li>
          </ul>
        </Section>

        <Section id="third-parties" title="third parties">
          <ul>
            <li>
              <b className="text-foreground">Google Firebase Cloud Messaging</b> (Google LLC / Google Ireland Ltd), to
              deliver notifications. See{" "}
              <A href="https://firebase.google.com/support/privacy">Privacy and Security in Firebase</A>.
            </li>
            <li>
              <b className="text-foreground">Apple Push Notification service</b> (Apple Inc.), to deliver notifications and
              Live Activity updates. See <A href="https://www.apple.com/legal/privacy/">Apple&apos;s privacy policy</A>.
            </li>
          </ul>
          <p>
            These providers may process data outside your country, including in the United States, under their own
            safeguards (such as the EU Standard Contractual Clauses).
          </p>
        </Section>

        <Section id="retention" title="retention + deletion">
          <ul>
            <li>
              The relay has no database. Its logs hold hub ids and counts of sent messages, never tokens, message text or
              IP addresses. IP addresses are held in memory only for rate limiting and dropped within minutes.
            </li>
            <li>
              Firebase keeps an FCM token until it is deleted (at sign-out or when the app is removed) or has been unused
              for a long time; see Google&apos;s documentation.
            </li>
            <li>
              To delete everything: sign out of each hub in the app, then delete the app. Data kept on a hub can be removed
              by its operator.
            </li>
          </ul>
        </Section>

        <Section id="rights" title="your rights">
          <p>
            Depending on where you live (for example under the EU/UK GDPR or Californian law) you can ask to access,
            correct, delete or export your personal data, object to or limit its processing, and complain to your data
            protection authority. The legal basis for the push relay is the performance of the service you asked for
            (delivering your alerts). Because the relay stores nothing, we usually hold no data about you; for data on a hub,
            contact its operator. Write to <A href={`mailto:${CONTACT}`}>{CONTACT}</A>.
          </p>
        </Section>

        <Section id="children" title="children">
          <p>pingtxt is a tool for monitoring servers and is not directed at children under 13 (or 16 in the EU).</p>
        </Section>

        <Section id="website" title="this website">
          <p>
            pingtxt.com is a static site hosted on GitHub Pages. GitHub may log visitors&apos; IP addresses for security;
            see the <A href="https://docs.github.com/site-policy/privacy-policies/github-general-privacy-statement">GitHub
            privacy statement</A>. The site sets no cookies and has no analytics. Your theme choice is kept in your
            browser&apos;s local storage. The page loads the public status of <C>status.pingtxt.com</C> and the public usage
            totals from <C>telemetry.pingtxt.com</C>; these requests carry no identifier.
          </p>
        </Section>

        <Section id="hub-stats" title="hub usage stats">
          <p>
            The self-hosted hub can send anonymous usage stats (a random id, its version and how many monitors and agents
            are live) once an hour, only if an admin turns it on. It is not part of the app. See{" "}
            <A href="/docs/#usage-stats">usage stats</A> in the docs.
          </p>
        </Section>

        <Section id="changes" title="changes">
          <p>
            We will update this page when the app or the relay changes what it handles, and change the date at the top. The
            history is public in the <A href={`${REPO}/commits/main/site/app/privacy/page.tsx`}>repository</A>. The relay
            source code is public too: <A href={RELAY_REPO}>pingtxt-push-relay</A>.
          </p>
        </Section>

        <Section id="contact" title="contact">
          <p>
            {OWNER} · <A href={`mailto:${CONTACT}`}>{CONTACT}</A>
          </p>
        </Section>
      </div>
    </div>
  )
}
