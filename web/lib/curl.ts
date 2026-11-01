// Turns a pasted curl command (as copied from browser dev tools, API docs or
// a terminal) into the fields of an HTTP monitor.

export interface CurlRequest {
  url: string
  method: string
  headers: Record<string, string>
  body: string
  insecure: boolean
}

/** Splits a command line like a POSIX shell: quotes, escapes, line continuations. */
export function shellWords(s: string): string[] {
  const out: string[] = []
  let cur = ""
  let has = false
  let i = 0
  while (i < s.length) {
    const c = s[i]
    if (c === "\\" && (s[i + 1] === "\n" || (s[i + 1] === "\r" && s[i + 2] === "\n"))) {
      i += s[i + 1] === "\r" ? 3 : 2 // line continuation
      continue
    }
    if (/\s/.test(c)) {
      if (has) out.push(cur)
      cur = ""
      has = false
      i++
      continue
    }
    has = true
    if (c === "'") {
      const end = s.indexOf("'", i + 1)
      if (end < 0) throw new Error("unclosed quote")
      cur += s.slice(i + 1, end)
      i = end + 1
    } else if (c === "$" && s[i + 1] === "'") {
      // $'...' (Chrome's "copy as cURL" on bash uses it for odd bodies)
      i += 2
      while (i < s.length && s[i] !== "'") {
        if (s[i] === "\\" && i + 1 < s.length) {
          const n = s[i + 1]
          cur += ({ n: "\n", t: "\t", r: "\r" } as Record<string, string>)[n] ?? n
          i += 2
        } else cur += s[i++]
      }
      if (i >= s.length) throw new Error("unclosed quote")
      i++
    } else if (c === '"') {
      i++
      while (i < s.length && s[i] !== '"') {
        if (s[i] === "\\" && i + 1 < s.length && '"\\$`\n'.includes(s[i + 1])) {
          if (s[i + 1] !== "\n") cur += s[i + 1]
          i += 2
        } else cur += s[i++]
      }
      if (i >= s.length) throw new Error("unclosed quote")
      i++
    } else if (c === "\\" && i + 1 < s.length) {
      cur += s[i + 1]
      i += 2
    } else {
      cur += c
      i++
    }
  }
  if (has) out.push(cur)
  return out
}

/** True when the text looks like a curl command. */
export function isCurl(text: string) {
  return /^\s*curl(\.exe)?\s/i.test(text)
}

// Options that take a value and do not matter for a monitor.
const skipWithValue = new Set([
  "-o", "--output", "-w", "--write-out", "--connect-timeout", "-m", "--max-time",
  "--retry", "-x", "--proxy", "-e", "--referer", "--resolve", "--cacert", "--cert", "--key",
  "-c", "--cookie-jar", "-D", "--dump-header", "--limit-rate", "-T", "--upload-file",
])

/** Parses a curl command. Throws when there is no URL. */
export function parseCurl(cmd: string): CurlRequest {
  const words = shellWords(cmd.trim())
  if (!/^curl(\.exe)?$/i.test(words[0] ?? "")) throw new Error("not a curl command")
  const req: CurlRequest = { url: "", method: "", headers: {}, body: "", insecure: false }
  const data: string[] = []
  let head = false
  let get = false
  for (let i = 1; i < words.length; i++) {
    let w = words[i]
    let val: string | undefined
    // --opt=value
    const eq = w.startsWith("--") ? w.indexOf("=") : -1
    if (eq > 0) {
      val = w.slice(eq + 1)
      w = w.slice(0, eq)
    }
    // -XPOST, -HAccept:... (short option glued to its value)
    if (/^-[XHdbuA][^-]/.test(w) && w.length > 2) {
      val = w.slice(2)
      w = w.slice(0, 2)
    }
    const next = () => val ?? words[++i] ?? ""
    switch (w) {
      case "-X":
      case "--request":
        req.method = next().toUpperCase()
        break
      case "-H":
      case "--header": {
        const h = next()
        const c = h.indexOf(":")
        if (c > 0) req.headers[h.slice(0, c).trim()] = h.slice(c + 1).trim()
        break
      }
      case "-d":
      case "--data":
      case "--data-raw":
      case "--data-binary":
      case "--data-ascii":
      case "--data-urlencode":
        data.push(next())
        break
      case "--json":
        data.push(next())
        req.headers["Content-Type"] ??= "application/json"
        req.headers["Accept"] ??= "application/json"
        break
      case "-u":
      case "--user":
        req.headers["Authorization"] = "Basic " + btoa(next())
        break
      case "-A":
      case "--user-agent":
        req.headers["User-Agent"] = next()
        break
      case "-b":
      case "--cookie":
        req.headers["Cookie"] = next()
        break
      case "-k":
      case "--insecure":
        req.insecure = true
        break
      case "-I":
      case "--head":
        head = true
        break
      case "-G":
      case "--get":
        get = true
        break
      case "--url":
        req.url = next()
        break
      default:
        if (skipWithValue.has(w)) {
          next()
        } else if (!w.startsWith("-") && !req.url) {
          req.url = w
        }
    }
  }
  if (!req.url) throw new Error("no URL in the curl command")
  if (!/^https?:\/\//i.test(req.url)) req.url = "http://" + req.url
  if (get && data.length) {
    req.url += (req.url.includes("?") ? "&" : "?") + data.join("&")
  } else if (data.length) {
    req.body = data.join("&")
    // curl sends form data unless told otherwise
    const hasType = Object.keys(req.headers).some((k) => k.toLowerCase() === "content-type")
    if (!hasType) req.headers["Content-Type"] = "application/x-www-form-urlencoded"
  }
  req.method ||= head ? "HEAD" : req.body ? "POST" : "GET"
  return req
}
