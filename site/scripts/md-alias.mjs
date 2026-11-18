// After `next build`: every out/<dir>/index.html.md (the llms.txt name for
// the Markdown of a page) is also copied to out/<dir>.md, so the Markdown of
// pingtxt.com/docs/install/ is at pingtxt.com/docs/install.md too.
import { copyFileSync, readdirSync } from "node:fs"
import path from "node:path"

const out = path.join(process.cwd(), "out")
let n = 0
for (const f of readdirSync(out, { recursive: true })) {
  if (path.basename(f) !== "index.html.md") continue
  const dir = path.dirname(path.join(out, f))
  if (dir === out) continue
  copyFileSync(path.join(out, f), `${dir}.md`)
  n++
}
if (!n) throw new Error("md-alias: no index.html.md in out/")
console.log(`md-alias: ${n} Markdown pages`)
