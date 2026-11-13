import { cn } from "@/lib/utils"

/**
 * Vertical lines that show how deep a row sits in the group tree, one per
 * level. Each line falls under the fold arrow of the group above it, so a
 * subgroup and its monitors read as hanging from their group.
 */
export function TreeGuides({ depth, className }: { depth: number; className?: string }) {
  if (depth <= 0) return null
  return (
    <span aria-hidden className={cn("flex shrink-0 self-stretch", className)}>
      {Array.from({ length: depth }, (_, i) => (
        <span key={i} className="relative w-5 shrink-0">
          <span className="absolute inset-y-0 left-[0.875rem] border-l border-muted-foreground/30" />
        </span>
      ))}
    </span>
  )
}
