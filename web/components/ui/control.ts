import { cva } from "class-variance-authority"

/**
 * One size scale for every control in the UI (buttons, inputs, selects,
 * tabs, segmented filters, chips). Pick the size by where the control sits:
 *
 *   md (h-8, 13px)  form fields, page header actions, dialog and form buttons
 *   sm (h-7, 12px)  dense bars: the top header, the bulk action bar
 *   xs (h-6, 12px)  actions inside table rows, chips, segmented filters
 *
 * Controls that sit next to each other use the same size. Inputs stay at
 * 16px text on phones so iOS does not zoom in on focus; their height still
 * follows the scale.
 */
export const controlHeight = {
  md: "h-8",
  sm: "h-7",
  xs: "h-6",
} as const

export const controlText = {
  md: "text-[13px]",
  sm: "text-xs",
  xs: "text-xs",
} as const

/** Text toggles in a row, like "all · up · down" or "1h 6h 24h". */
export const segment = cva(
  "inline-flex shrink-0 items-center px-2 whitespace-nowrap text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      active: { true: "bg-accent text-foreground", false: "" },
      size: { xs: "h-6 text-xs", sm: "h-7 text-xs" },
    },
    defaultVariants: { active: false, size: "xs" },
  }
)

/** Dashed box for picking one option, like theme or backup mode. */
export const option = cva(
  "inline-flex h-8 shrink-0 items-center border border-dashed px-3 text-xs whitespace-nowrap transition-colors outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60",
  {
    variants: {
      active: { true: "border-solid border-primary text-primary", false: "" },
    },
    defaultVariants: { active: false },
  }
)

/** Small dashed toggle for tags, groups and weekdays. Colors come from the caller. */
export const chip =
  "inline-flex h-6 shrink-0 items-center border border-dashed px-2 text-xs whitespace-nowrap transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
