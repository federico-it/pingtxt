// pingtxt service worker. It does one thing: when a page cannot be loaded
// because the hub is restarting or updating (no connection, or a reverse
// proxy answering 502/503/504 or 404 without the hub's header), it shows the
// "starting / updating" page, which reloads by itself once the hub is back.
// Nothing else is cached: every request still goes to the network first.

const CACHE = "pingtxt-starting-v1"
const PAGE = "/starting.html"
// Every response written by the hub carries this header (see internal/hub).
const HUB_HEADER = "X-Pingtxt-Hub"

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((c) => c.add(new Request(PAGE, { cache: "reload" })))
      .then(() => self.skipWaiting())
  )
})

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

async function startingPage() {
  const cached = await caches.match(PAGE, { cacheName: CACHE })
  if (!cached) return Response.error()
  return new Response(cached.body, {
    status: 503,
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" },
  })
}

self.addEventListener("fetch", (event) => {
  const req = event.request
  if (req.mode !== "navigate" || req.method !== "GET") return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  // Sign-in redirects, downloads and the API are never touched.
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/download/")) return

  event.respondWith(
    fetch(req)
      .then((res) => {
        const proxyError = [404, 502, 503, 504].includes(res.status) && !res.headers.has(HUB_HEADER)
        return proxyError ? startingPage() : res
      })
      .catch(() => startingPage())
  )
})
