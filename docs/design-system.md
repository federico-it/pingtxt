# Pingtxt design system

The visual language of Pingtxt, extracted from the hub web UI (`web/`) and the
HTML emails (`internal/hub/mailview.go`). It is written for anyone building
another client, first of all the native SwiftUI iOS app, so it includes
suggested SwiftUI tokens next to every value.

Source of truth stays the code: if this document and the code disagree, the
code wins and this file should be updated.

| What | Where |
| --- | --- |
| Color and radius tokens | `web/app/globals.css` |
| Status glyphs and colors | `web/components/status.tsx` |
| Page header, sections, empty state, key/value | `web/components/page-header.tsx` |
| Header and navigation | `web/components/shell.tsx` |
| Uptime bars | `web/components/uptime-bars.tsx` |
| Latency chart | `web/components/latency-chart.tsx` |
| Tags and groups | `web/components/labels.tsx` |
| Number and time formatting | `web/lib/format.ts` |
| Copy (English source strings) | `web/messages/en.json` |
| Emails | `internal/hub/mailview.go` |

---

## 1. Principles

Pingtxt looks like a terminal that learned some manners.

- **Monospace everywhere.** One family (JetBrains Mono) for headings, body,
  numbers and buttons. No proportional font anywhere.
- **Text first, chrome last.** Status is a glyph and a word, not a pill.
  Sections are separated by dashed rules, not cards with shadows.
- **Shell metaphors.** Every page has a `$ command` line above its title, the
  logo is `> pingtxt_` with a blinking cursor, the active item is `[bracketed]`,
  menus are `[≡]` / `[x]`, filters read `down:3`.
- **Color means state.** The neutral palette is almost grayscale; green, red,
  amber and blue are reserved for up, down, degraded/warning and
  maintenance/info. Dark mode uses a green accent as the primary color.
- **Dense but calm.** Base size is 13 pt with relaxed line height, square
  corners (2 pt radius), 1 pt hairlines.
- **Lowercase copy.** Labels, buttons, statuses and navigation are lowercase.

---

## 2. Color

The web defines colors in OKLCH. The hex values below are the sRGB
conversions; use them in an asset catalog with "Any / Dark" appearances.

### 2.1 Neutrals and surfaces

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `background` | `#FAFAF9` | `#0B0D0B` | Screen background (also cards) |
| `foreground` | `#171613` | `#DAE0DA` | Primary text |
| `popover` | `#FEFDFC` | `#121413` | Tooltips, menus, sheets |
| `muted` | `#EFEEEC` | `#171917` | Subtle fills (group header rows, empty bars, note boxes) |
| `mutedForeground` | `#6A6966` | `#828883` | Secondary text, labels, `$ command` lines |
| `accent` | `#E9E8E5` | `#1D201D` | Selected filter / nav item, row hover/press |
| `secondary` | `#EFEEEC` | `#191B19` | Secondary button fill |
| `border` | `#D8D7D4` | `#2B2F2C` | All hairlines (solid and dashed) |
| `input` | `#D2D1CE` | `#303431` | Text field border |

Light neutrals are slightly warm (hue 90), dark neutrals slightly green
(hue 150): keep that tint, plain gray looks off next to the UI.

### 2.2 Brand / primary

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `primary` | `#171613` | `#6DE18B` | Primary button fill, `$` and `>` prompts, cursor, links |
| `primaryForeground` | `#FAFAF9` | `#0C0E0C` | Text on primary |
| `ring` | `#298646` | `#53BE70` | Focus ring (at 50% opacity) |

In light mode the primary is near-black ink; in dark mode it is terminal
green. The logo prompt and cursor use `primary`, so they are black in light
mode and green in dark mode on the web. The app icon itself always uses
`#4ADE80` on `#111111`.

### 2.3 Status

| Token | Light | Dark | Meaning |
| --- | --- | --- | --- |
| `up` | `#1B9247` | `#56D57B` | Up, ok, healthy |
| `down` | `#D72828` | `#FC5855` | Down, failing, expired |
| `warn` | `#D48E00` | `#F0BB3B` | Degraded, uptime < 99%, expiring ≤ 30 days |
| `info` | `#3275B4` | `#55AEE8` | Maintenance, groups, informational |
| `destructive` | `#CC2827` | `#F75D59` | Destructive actions (delete) |

Paused and pending use `mutedForeground`.

Emails use a darker amber for text on light backgrounds, `#A86F00`, because
`#D48E00` is too light for small text on `#FAFAF9`. Do the same for small
amber text in light mode on iOS (use `#D48E00` for fills and bars, `#A86F00`
for text), or check contrast in the asset catalog.

### 2.4 Chart series

Lines in the latency chart are colored by location, in this order:

| # | Light | Dark |
| --- | --- | --- |
| 1 | `#1B9247` | `#56D57B` |
| 2 | `#3275B4` | `#55AEE8` |
| 3 | `#C37F00` | `#F0BB3B` |
| 4 | `#9B49AE` | `#CE83DE` |
| 5 | `#00959C` | `#42C3C9` |

### 2.5 Tag colors

Tags have a user-chosen color: none, red, orange, yellow, green, cyan, blue,
purple, pink, gray. The chip is outline-only: border at ~50% of the color,
text in the color (a darker shade in light mode, lighter in dark mode), and a
15% fill only when the tag is selected as a filter. The web uses Tailwind's
palette (`red-600` / `red-400` text, `red-500/50` border, and so on); the
iOS system colors (`.red`, `.orange`, `.yellow`, `.green`, `.cyan`, `.blue`,
`.purple`, `.pink`, `.gray`) are a fine match. "None" uses `foreground` at
30% for the border and 80% for the text.

### 2.6 SwiftUI tokens

Create one color set per token in `Assets.xcassets` (e.g. `Colors/up`) with
the light and dark values above, then expose them:

```swift
import SwiftUI

extension Color {
    enum PT {
        // Surfaces
        static let background       = Color("background")
        static let foreground       = Color("foreground")
        static let popover          = Color("popover")
        static let muted            = Color("muted")
        static let mutedForeground  = Color("mutedForeground")
        static let accent           = Color("accent")
        static let secondary        = Color("secondary")
        static let border           = Color("border")
        static let input            = Color("input")
        // Brand
        static let primary          = Color("primary")
        static let primaryForeground = Color("primaryForeground")
        static let ring             = Color("ring")
        // Status
        static let up               = Color("up")
        static let down             = Color("down")
        static let warn             = Color("warn")
        static let warnText         = Color("warnText")   // #A86F00 light, #F0BB3B dark
        static let info             = Color("info")
        static let destructive      = Color("destructive")
        // Chart
        static let chart: [Color]   = (1...5).map { Color("chart\($0)") }
    }
}
```

If you prefer code over an asset catalog:

```swift
extension Color {
    init(light: UInt32, dark: UInt32) {
        self.init(UIColor { $0.userInterfaceStyle == .dark ? UIColor(hex: dark) : UIColor(hex: light) })
    }
}

extension UIColor {
    convenience init(hex: UInt32) {
        self.init(red: CGFloat((hex >> 16) & 0xFF) / 255,
                  green: CGFloat((hex >> 8) & 0xFF) / 255,
                  blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

// e.g. static let up = Color(light: 0x1B9247, dark: 0x56D57B)
```

Set `.tint(Color.PT.primary)` at the root so system controls (toggles,
pickers, links) follow the brand, and give every screen
`Color.PT.background` instead of the system grouped background.

---

## 3. Typography

### 3.1 Family

**JetBrains Mono** (variable, OFL licence) for everything. The web loads
`@fontsource-variable/jetbrains-mono`; on iOS bundle the static TTFs you need
(Regular, Medium, SemiBold, Bold) or the variable font, and register them in
`Info.plist` (`UIAppFonts`).

Font features: the web turns **off** contextual alternates (`calt` 0, no
`->` ligatures) and turns **on** slashed zero (`zero` 1), so `0` and `O` are
never confused in hostnames and percentages. Do the same on iOS (see 3.4).

Fallback if the font fails to load: `.system(.body, design: .monospaced)`
(SF Mono).

### 3.2 Scale

Base body size is **13 pt** with relaxed line height (~1.6). The web scale:

| Token | Size | Weight | Use |
| --- | --- | --- | --- |
| `title` | 18 pt | SemiBold, tight tracking | Page title (under the `$ command` line) |
| `logo` | 13–14 pt | SemiBold / Bold | `> pingtxt_` |
| `value` | 16 pt | Regular | Key/value figures (uptime 24h, avg ms) |
| `body` | 13 pt | Regular | Default text, table rows |
| `bodyStrong` | 13 pt | Medium | Monitor names, group names |
| `button` | 13 pt (12.8 small) | Medium | Buttons |
| `caption` | 12 pt | Regular | Secondary lines, filters, tooltips, `$ command` line |
| `label` | 11 pt | Regular, UPPERCASE | Column headers, key labels, section titles |
| `micro` | 10 pt | Regular | Tag chips, chart axis labels |

The status page banner uses 16 pt Medium for the headline and 18 pt for its
glyph.

### 3.3 Numbers

All numbers are monospaced by nature, so columns align without extra work.
Formats (from `web/lib/format.ts`), keep them identical:

| Value | Format | Examples |
| --- | --- | --- |
| Percent | 2 decimals, `100%` without decimals, `--.--%` when no data | `99.87%`, `100%`, `--.--%` |
| Latency | `<10` one decimal, `<1000` integer ms, else seconds with 2 decimals, `--` when none | `4.2ms`, `183ms`, `1.24s` |
| Relative time | `just now` (<5 s), then `12s ago`, `4m ago`, `3h ago`, `2d ago`, `never` | |
| Duration | two largest units | `2d 4h`, `3h 12m`, `5m 8s`, `42s` |
| Interval | single unit | `30s`, `1m`, `5m`, `1h`, `1d` |
| Date-time | ISO-like, 24h, local time | `2026-10-31 14:57:04` |
| Day (bars) | UTC date | `2026-10-31` |

Units are attached without a space (`183ms`, `5m`). Uptime text turns `warn`
below 99% and `down` below 90%.

### 3.4 SwiftUI tokens

```swift
extension Font {
    enum PT {
        private static func mono(_ size: CGFloat, _ weight: Font.Weight = .regular,
                                 relativeTo style: Font.TextStyle) -> Font {
            .custom("JetBrainsMono-Regular", size: size, relativeTo: style).weight(weight)
        }
        static let title      = mono(18, .semibold, relativeTo: .title3)
        static let value      = mono(16, relativeTo: .body)
        static let body       = mono(13, relativeTo: .body)
        static let bodyStrong = mono(13, .medium, relativeTo: .body)
        static let button     = mono(13, .medium, relativeTo: .body)
        static let caption    = mono(12, relativeTo: .caption)
        static let label      = mono(11, relativeTo: .caption2)   // use with .textCase(.uppercase)
        static let micro      = mono(10, relativeTo: .caption2)
    }
}
```

Using `relativeTo:` keeps Dynamic Type working. 13 pt is small for a phone:
it is fine to bump the iOS base to 14–15 pt and scale the rest proportionally,
as long as the relationships hold (label < caption < body < value < title).

Slashed zero and no ligatures, if you use the variable font through UIKit:

```swift
let features: [[UIFontDescriptor.FeatureKey: Int]] = [
    [.type: kTypographicExtrasType, .selector: kSlashedZeroOnSelector],
    [.type: kLigaturesType, .selector: kContextualAlternatesOffSelector],
]
let descriptor = UIFont(name: "JetBrainsMono-Regular", size: 13)!.fontDescriptor
    .addingAttributes([.featureSettings: features])
```

---

## 4. Spacing, shape, lines

### 4.1 Spacing

The web uses Tailwind's 4 pt grid. Values that recur:

| Token | Value | Use |
| --- | --- | --- |
| `xxs` | 2 pt | Gap between chips, filter padding vertical |
| `xs` | 4 pt | Icon–text gap, between filter buttons |
| `sm` | 8 pt | Row horizontal padding, gaps in rows |
| `md` | 10 pt | Row vertical padding (list rows are 8 h × 10 v) |
| `lg` | 16 pt | Screen side gutter (phone), gap between columns |
| `xl` | 24 pt | Gutter on wide screens, space under page header |
| `xxl` | 32 pt | Space above a section |

```swift
enum Spacing {
    static let xxs: CGFloat = 2, xs: CGFloat = 4, sm: CGFloat = 8, md: CGFloat = 10
    static let lg: CGFloat = 16, xl: CGFloat = 24, xxl: CGFloat = 32
}
```

Content width on the web is capped at 1152 pt (status pages 768 pt); on iPad
cap readable content similarly.

### 4.2 Corners

Almost square. Base radius is **2 pt**; buttons, inputs and boxes use it.
Badges are ~5 pt. Only status dots are round (8 pt circles).
Never use iOS's default 10–12 pt card radius.

```swift
enum Radius { static let base: CGFloat = 2; static let badge: CGFloat = 5 }
```

### 4.3 Lines

Hairlines (1 px) in `border`, mostly **dashed**:

- Dashed: list row separators, table header, header bottom edge, section
  rule, empty state box, inactive tag/group toggle, email row separators.
- Solid: input borders, the status page banner, tooltips, selected toggles.
- Chart gridlines: dashed `2 3`; hover crosshair dashed `2 2`.

```swift
struct DashedRule: View {
    var body: some View {
        Line().stroke(style: StrokeStyle(lineWidth: 1 / UIScreen.main.scale, dash: [3, 3]))
            .foregroundStyle(Color.PT.border).frame(height: 1)
    }
}
struct Line: Shape {
    func path(in r: CGRect) -> Path { Path { $0.move(to: .init(x: 0, y: r.midY)); $0.addLine(to: .init(x: r.maxX, y: r.midY)) } }
}
```

### 4.4 Elevation

None. No shadows except a very small one on tooltips. Hierarchy comes from
lines and the `muted` / `accent` fills. The sticky header is
`background` at 90% with a blur, which maps directly to `.bar` /
`.ultraThinMaterial` tinted with the background.

---

## 5. Status language

Every status is a **glyph + lowercase word**, both in the status color.

| Status | Glyph | Color | Notes |
| --- | --- | --- | --- |
| up | `●` | `up` | |
| down | `●` | `down` | Glyph pulses (opacity animation, ~2 s) |
| degraded | `◐` | `warn` | Some locations failing, below the down threshold |
| paused | `○` | `mutedForeground` | |
| pending | `◌` | `mutedForeground` | No result yet |
| maintenance | `◇` | `info` | Inside a maintenance window |

Check results (single location) are `ok` in `up` or `fail` in `down`.

Overall summary in the header: an 8 pt round dot (`down` if anything is down,
else `info` if something is in maintenance, else `up`), then `3↓ · ` in red
when monitors are down, then `4/5 agents`. Full text: `all systems go`,
`3 down`, `1 in maintenance`, joined by ` · `.

Expiry (SSL / domain): ≤ 7 days or expired in `down`, ≤ 30 days in `warn`,
otherwise default text.

Sort order in lists: down, degraded, pending, maintenance, up, paused, then by
name.

```swift
enum MonitorStatus: String, Codable { case up, down, degraded, paused, pending, maintenance }

extension MonitorStatus {
    var glyph: String {
        switch self { case .up, .down: "●"; case .degraded: "◐"; case .paused: "○"; case .pending: "◌"; case .maintenance: "◇" }
    }
    var color: Color {
        switch self {
        case .up: .PT.up; case .down: .PT.down; case .degraded: .PT.warn
        case .maintenance: .PT.info; case .paused, .pending: .PT.mutedForeground
        }
    }
}

struct StatusText: View {
    let status: MonitorStatus
    @State private var dim = false
    var body: some View {
        HStack(spacing: 6) {
            Text(status.glyph).opacity(status == .down && dim ? 0.4 : 1)
            Text(LocalizedStringKey("status.\(status.rawValue)"))
        }
        .font(.PT.body).foregroundStyle(status.color)
        .onAppear {
            guard status == .down else { return }
            withAnimation(.easeInOut(duration: 1).repeatForever()) { dim = true }
        }
    }
}
```

Respect Reduce Motion: skip the pulse and the cursor blink when
`accessibilityReduceMotion` is on.

---

## 6. Components

### 6.1 Logo and header

- Logo: `>` in `primary`, space, `pingtxt` in `foreground` SemiBold, `_` in
  `primary` blinking (step animation, 1.1 s period, visible half the time).
- Header: 48 pt tall, bottom edge dashed, background 90% + blur, sticky.
- Navigation items are lowercase words in `mutedForeground`; the active one is
  `[monitors]` in `foreground` on an `accent` fill. On narrow screens the nav
  collapses into a `[≡]` button that toggles a panel starting with
  `$ ls ~/`, items shown as `> monitors/` (the `>` only on the active one).

On iOS a `TabView` or `NavigationStack` is fine, but keep the flavor: the
navigation title can be the plain lowercase word with the `$ command` line
above it (see 6.2), and tab labels lowercase.

### 6.2 Page header

```
$ pingtxt monitors ls                       [grep...] [+ new]
monitors
```

- Line 1: `$` in `primary`, then the command in `mutedForeground`, 12 pt.
- Line 2: title 18 pt SemiBold, 4 pt below, truncates.
- Actions on the right, wrapping below on narrow widths. 24 pt below the
  header before content.

Commands used on the web: `pingtxt monitors ls`, `pingtxt monitors show 12`,
`curl status` (public status page), `ls ~/` (menu). Make up new ones in the
same spirit (`pingtxt agents ls`, `pingtxt incidents ls`).

### 6.3 Section

```
── LAST 24H ------------------------------------------ [action]
```

11 pt UPPERCASE `mutedForeground`, `── ` prefix, then a dashed rule filling
the width, optional actions at the end. 32 pt above, 8 pt below.

### 6.4 Key / value

Small uppercase label (11 pt, `mutedForeground`) above a 16 pt value. Used in
a grid for status, uptime 24h / 7d / 30d, average latency.

### 6.5 Monitor list row

```
● api.example.com  #prod #eu                 ▮▮▮▮▮▮▮▮▮▮▮▮▮▮  99.98%  183ms  12s ago
  http · https://api.example.com · every 1m · 3 locations
```

- Top border dashed on the list, every row has a dashed bottom border.
- Padding 8 pt horizontal, 10 pt vertical. Pressed state: `accent` at 60%.
- Left: status glyph, then name (13 pt Medium, truncates) with tag chips,
  then the detail line (12 pt `mutedForeground`, truncates):
  `{type} · {target} · every {interval}` or
  `heartbeat · expected every {interval}` for push monitors.
- Right / below: uptime bars (24 h), uptime % (colored by threshold),
  average latency in `mutedForeground`, last check time 12 pt.
- On phones the bars move to a full-width line under the name and the
  status word appears before the uptime.
- With groups: a group header row in `muted` at 40%, name Medium + count,
  and `· 2 down` in `down` if any.

Column headers (wide layouts only) are 11 pt UPPERCASE `mutedForeground`.

### 6.6 Filters

A row of text buttons, 12 pt: `all:12 up:9 down:1 degraded:1 maintenance:0
paused:1`. Unselected `mutedForeground`; selected `foreground` on `accent`.
Padding 8 × 2 pt, square. On iOS a horizontally scrolling row of these is a
good match; avoid the system segmented control.

### 6.7 Buttons

| Variant | Light | Dark | Use |
| --- | --- | --- | --- |
| default | `primary` fill, `primaryForeground` text | green fill, near-black text | Main action (`+ new`, `save`) |
| outline | `border` stroke, background fill | `input` stroke, `input` 30% fill | Secondary actions |
| ghost | no fill; `muted` when pressed | same | Header actions, `[≡]`, `logout` |
| destructive | `destructive` 10% fill, `destructive` text | 20% fill | `delete` |
| link | `primary` text, underline on press | same | Inline links |

Sizes: default 32 pt tall, small 28 pt, large 36 pt; horizontal padding
10 pt, icon–text gap 6 pt, icons 16 pt (lucide icons on the web, SF Symbols on
iOS). Radius 2 pt. Pressed: move down 1 pt (web `translate-y-px`) or 80%
opacity of the fill. Disabled: 50% opacity. On iOS keep hit areas at least
44 pt even if the visual button is 32 pt.

Labels are lowercase. The add button is a `+` icon followed by `new`.
Toggles that cycle a value show it in brackets: the theme toggle reads
`[system]`, `[light]`, `[dark]`.

```swift
struct PTButtonStyle: ButtonStyle {
    enum Kind { case primary, outline, ghost, destructive }
    var kind: Kind = .primary
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.PT.button)
            .padding(.horizontal, 10).frame(minHeight: 32)
            .foregroundStyle(fg)
            .background(bg.opacity(configuration.isPressed ? 0.8 : 1))
            .overlay(RoundedRectangle(cornerRadius: Radius.base)
                .stroke(kind == .outline ? Color.PT.border : .clear, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: Radius.base))
            .offset(y: configuration.isPressed ? 1 : 0)
            .contentShape(Rectangle())
    }
    private var fg: Color {
        switch kind { case .primary: .PT.primaryForeground; case .destructive: .PT.destructive; default: .PT.foreground }
    }
    private var bg: Color {
        switch kind {
        case .primary: .PT.primary; case .destructive: .PT.destructive.opacity(0.12)
        case .outline: .PT.background; case .ghost: .clear
        }
    }
}
```

### 6.8 Text fields

32 pt tall, 1 pt `input` border, transparent fill (dark: `input` at 30%),
radius 2 pt, 10 pt horizontal padding, placeholder in `mutedForeground`.
Focus: border `ring` plus a 3 pt `ring` 50% glow. Invalid: `destructive`
border. Placeholders follow the shell tone: the search field says
`grep...`.

### 6.9 Tags and groups

- Tag chip: `#name`, 10 pt, 16 pt tall, 4 pt horizontal padding, 1 pt
  solid border in the tag color (see 2.5), no fill, no radius.
- Tag toggle (filters, pickers): 12 pt, dashed border and `mutedForeground`
  text when off; solid colored border, colored text and 15% fill when on.
- Group toggle: `[name]`, dashed when off; solid `info` border, `info` 15%
  fill when on.
- "New tag" field: dashed box with a `+` button.

### 6.10 Badges and small labels

The common badge is text in brackets in a status color, not a filled pill:
`[draft]` in `warn` on a status page, `[DOWN]` / `[UP]` in email titles.
The `read-only` marker is a dashed box with 12 pt `mutedForeground` text.
A filled badge exists (20 pt tall, 12 pt Medium, `primary` fill) but is
rare: prefer the bracket form.

### 6.11 Uptime bars

A row of thin vertical bars, one per time slot, filling the width.

- Height 20 pt in lists (taller on the monitor screen), 1 pt gap, each bar
  at least 2 pt wide, square, 90% opacity (100% when touched).
- Color per slot:
  - no data: `muted`; no data during maintenance: `info`
  - more than 50% of checks taken while the monitor was down: `down`
  - down + degraded checks ≥ 20%: `warn`
  - otherwise `up`
- Touching a bar shows a tooltip: date-time (or UTC day for daily bars),
  then `99.50% · 199/200 up · avg 183ms`, plus `3 checks while degraded` in
  `warn` and `+ 2 checks in maintenance` in `info` when present.

```swift
struct UptimeBars: View {
    let bars: [Bar]
    var height: CGFloat = 20
    var body: some View {
        HStack(spacing: 1) {
            ForEach(bars) { b in
                Rectangle().fill(color(b)).opacity(0.9).frame(minWidth: 2)
            }
        }
        .frame(height: height)
    }
    func color(_ b: Bar) -> Color {
        guard b.total > 0 else { return b.maint > 0 ? .PT.info : .PT.muted }
        let down = Double(b.total - b.ok) / Double(b.total)
        if down > 0.5 { return .PT.down }
        if down + Double(b.degraded) / Double(b.total) >= 0.2 { return .PT.warn }
        return .PT.up
    }
}
```

### 6.12 Latency chart

- 180 pt tall line chart, one line per location in the chart series colors,
  1.5 pt stroke, round joins, no fill, no smoothing.
- Lines join consecutive points even across empty slots; they break only on a
  gap longer than max(3, 4 × median spacing) slots. A lone point is a 2 pt
  dot.
- Y axis: 10 pt `mutedForeground` labels on the left (`0`, `100ms`...),
  dashed `border` gridlines, max rounded to a nice number (1, 2, 2.5, 5, 10 ×
  10ⁿ) with 10% headroom.
- X axis: start and end time only, 10 pt.
- Failures: small `down` ticks (3 × 5 pt) under the axis at each slot that had
  failed checks.
- Scrubbing: dashed vertical crosshair, tooltip box (`popover`, 1 pt border)
  with time, one row per location `name  183ms`, and `n failed` in `down`.
- Legend below: 12 pt `mutedForeground`, a colored swatch per location.
- Empty: a 180 pt dashed box with the "no data" text centered.

Swift Charts maps well: `LineMark` per series with
`.foregroundStyle(by:)` and `.chartForegroundStyleScale` set to the chart
colors, `RuleMark` for the crosshair, `.chartYAxis` with dashed grid lines.
Break lines by giving each segment its own `series` value.

The monitor screen also has a phase chart (DNS, connect, TLS, wait/TTFB, transfer) that
follows the same rules.

### 6.13 Empty, loading, errors

- Empty state: dashed box, 32 pt padding, centered `mutedForeground` text,
  lowercase, with an inline underlined `primary` link: `no monitors yet.
  create the first one`.
- Loading: the word followed by a blinking `_` cursor: `loading_`,
  `fetching_`. No spinners.
- Hub offline / restarting: a heartbeat line animation; on iOS a simple
  `connecting_` with the cursor is enough.
- Toasts: bottom of the screen, plain text, square.

### 6.14 Notes and callouts

In emails and incident updates, a note is a `muted` box with a 2 pt left
border in the tone color (up / down / warn / info) and 10 × 14 pt padding,
pre-wrapped text. Reuse it for incident updates and alerts in the app.

### 6.15 Public status page banner

A full-width box with a solid border, 16 pt padding: glyph (18 pt, pulsing
when there is an outage), headline 16 pt Medium, `updated 2m ago` on the right
in 12 pt `mutedForeground`.

---

## 7. Copy and tone

- **Lowercase** for UI labels, buttons, statuses, nav, empty states. Proper
  nouns and user content keep their case.
- **Short and literal.** `monitors`, `maint`, `notify`, `status`,
  `settings`. `no monitors match`. `all systems go`.
- **Shell flavor, used lightly.** `$ command` lines, `grep...` for search,
  `[brackets]` for current state, `>` for the current item, `_` cursor while
  waiting. Don't turn real sentences into fake commands.
- **Middle dot ` · ` joins facts** on one line: `http · example.com · every 1m`.
- **Numbers before words** where possible: `3 down`, `4/5 agents`,
  `down:3`.
- **No exclamation marks, no emoji.** Glyphs (`● ◐ ○ ◌ ◇ ↓ →`) do that job.
- Arrows: `→` after action links in emails (`open monitor →`).

Localization: the web keeps English source strings in
`web/messages/en.json` and Crowdin manages every other language (Italian
included). Reuse those keys and wording in the iOS app so the two clients
say the same thing, and consider pointing Crowdin at the app's
`Localizable.xcstrings` as well.

---

## 8. Dark mode and theme

- Three settings: `system` (default), `light`, `dark`. The web toggles with
  the `d` key; on iOS follow the system and offer an in-app override.
- Dark mode is not an inverted light mode: the primary color changes from ink
  to green, and neutrals pick up a green tint. Always test both.

## 9. Accessibility

- Status is never color-only: there is always a glyph and a word.
- Keep 44 pt touch targets even though visuals are compact.
- Support Dynamic Type (fonts use `relativeTo:`); let rows grow vertically
  instead of truncating key numbers.
- Respect Reduce Motion (no pulse, no cursor blink) and Increase Contrast
  (use `foreground` instead of `mutedForeground` for secondary text).
