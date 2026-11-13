# iOS app

The pingtxt iOS app shows your monitors, incidents and agents, gets alerts as push notifications and keeps a Live
Activity on the lock screen during outages. It talks to your own hub: there is no pingtxt account.

## Sign in

In the app, enter the address of your hub (its public URL). The app opens the hub's own login page, so passwords,
two-factor authentication and single sign-on all work, and no password ever reaches the app. Admins choose
**read only** or **read + write** on the consent page; read-only users always get read only.

Each phone shows up in **settings → tokens → iOS app sign-ins**, where you can disconnect it. The app can be signed in
to several hubs.

## Push notifications

1. In **settings → integrations → mobile app**, turn on **push notifications to the mobile app**. The hub registers
   with the push relay (`https://push.pingtxt.com` by default) by itself.
2. On the **notify** page add a channel of type **pingtxt app (push)**. Leave *users* empty to alert every user, or
   list some emails. Each user gets the alerts on every phone signed in with the app.
3. Pick its monitors and events like any other [channel](alerts.md#which-monitors-and-events-each-channel-gets).

An app can receive notifications only from a server that holds the app's Firebase credentials, which cannot ship
inside every self-hosted hub. So the hub sends each alert to the
[push relay](https://github.com/federico-it/pingtxt-push-relay), which forwards it to Firebase and Apple. The relay
has no database and does not log the text of the alerts or the phones' tokens; a hub can only reach the phones
signed in to it. Changing the relay address registers the hub again, and every phone enrolls again on its next
start. What the app itself sends is described in the [privacy policy](https://pingtxt.com/privacy/).

## Choose what each phone gets

Each user chooses in the app what their phones get:

- **events**: down, back up, degraded, certificate and domain expiry, agent offline.
- **monitors**: all of them, or only the monitors of some groups (with their subgroups) and tags, plus monitors picked
  one by one. Agent offline is about agents and follows the events only.
- **mute**: everything, or one monitor, for a while or until you turn it back on.
- **receive on**: every phone, or only one of them (for example the iPhone and not the iPad), to avoid getting each
  alert twice. If that phone signs out, the others get the alerts again.

These account preferences apply to all your phones. A phone can instead have events and monitors of its own, and can
be muted by itself: an alert reaches a phone only when neither the account nor the phone is muted.

The preferences are personal, so read-only users set them too. They change nothing for other people, channels or
status pages, and they only leave notifications out: a phone never gets something its channels do not send. Test
notifications always arrive, and the Live Activity is not affected.

## Actions in notifications

Press and hold a notification (also on the lock screen) to act on it without opening the app:

| notification | actions |
| --- | --- |
| monitor down | acknowledge, check now, mute for me for 1 hour, mute for everyone for 1 hour, pause monitor |
| monitor degraded | check now, mute for me for 1 hour, mute for everyone for 1 hour |
| certificate or domain expiry | check expiry again, mute for me for 1 hour |
| agent offline | open agent |

- **acknowledge** says you are on it: reminders and escalation stop and the others see who took it (see
  [Acknowledging incidents](alerts.md#acknowledging-incidents)).
- **check now** runs the check right away from every location and records the result, so it can close the incident.
- **mute for me for 1 hour** mutes that monitor on this phone only (see above). Everyone can use it.
- **mute for everyone for 1 hour** creates a one-hour [maintenance window](alerts.md#maintenance-windows) for that monitor: no
  alerts or reminders, checks keep running. Public status pages show the monitor in maintenance meanwhile.
- **pause monitor** stops its checks and closes the open incident.

The other actions change something for everyone and need a **read + write** sign-in (admins): the other phones get
only the actions they can use. Test notifications have no buttons.

## Live Activity

With push on, the hub keeps the app's Live Activity current even when the app is closed: it starts (iOS 17.2+),
updates and ends the activity through the relay whenever incidents open or close. It shows up to three monitors
that are down, with their causes. When the last incident closes, the activity ends with a summary ("api is up, down for
12m", or "All monitors are up" with each monitor's downtime) and stays on the lock screen for 15 minutes.

## For app developers

The app signs in with OAuth 2.1 (authorization code + PKCE). It is a built-in public client: no secret and no
registration.

| | |
| --- | --- |
| client_id | `pingtxt-ios` |
| redirect URI | `pingtxt://oauth` |
| authorize | `GET /oauth/authorize` with `response_type=code`, `code_challenge` + `code_challenge_method=S256`, `state`, `scope=read write` |
| tokens | `POST /oauth/token` (form body): `grant_type=authorization_code` with `code`, `redirect_uri`, `code_verifier`, or `grant_type=refresh_token` with `refresh_token` |
| sign out | `POST /oauth/revoke` with `token` and `client_id` |
| metadata | `/.well-known/oauth-authorization-server`, `/.well-known/oauth-protected-resource/api` |

- The token response says which `scope` was granted.
- Access tokens (`pma_...`) last an hour and work on the whole REST API like an API token of the same scope, never on
  account endpoints (password, 2FA, tokens, users, settings).
- Refresh tokens (`pmr_...`) last 30 days from their last use and rotate at every refresh. Using an old refresh token
  again revokes the sign-in, so refresh one request at a time and store the new token before using it.
- Push: the app enrolls its FCM token through `/api/mobile/config` and `/api/mobile/devices`. Notifications carry an
  `aps.category` (`MONITOR_DOWN`, `MONITOR_UP`, `MONITOR_DEGRADED`, `EXPIRY`, `AGENT_OFFLINE`, `AGENT_ONLINE`) and
  `hub_id`, so an app signed in to several hubs knows which one to call. Phones whose sign-in cannot change data get
  `MONITOR_DOWN_READONLY`, `MONITOR_DEGRADED_READONLY` and `EXPIRY_READONLY` instead of the categories with write
  actions. `POST /api/monitors/{id}/check` runs a check now. Down, up and acknowledgement pushes carry `incident_id`;
  `POST /api/incidents/{id}/ack` acknowledges, and the `MONITOR_ACKED` notification (same collapse id as the down
  alert) tells the other phones who is on it.
- Preferences: `GET` and `PUT /api/mobile/preferences` for the account (with `primary_device_id`), and
  `/api/mobile/devices/{id}` for one phone (`current` = the phone of the calling sign-in, `follow_account`). Mute with
  `POST .../mute` (`monitor_id`, `minutes`) on either, unmute with `DELETE .../mute`.
- Live Activity: the app sends its ActivityKit tokens to `PUT /api/mobile/live-activity/start-token` and `/token`.

Every endpoint is in the [API reference](https://pingtxt.com/docs/api/).
