import type { Group } from "@/lib/api"

/** How many levels groups can nest (same limit as the hub). */
export const MAX_GROUP_DEPTH = 3

/** A group with its place in the tree. */
export interface GroupNode extends Group {
  /** 0 for a top-level group */
  depth: number
  /** names from the top-level group down to this one, joined by " › " */
  path: string
  /** parent, its parent and so on up to the top */
  ancestors: number[]
}

export interface GroupTree {
  /** every group, parents before their subgroups, siblings by name */
  ordered: GroupNode[]
  byId: Map<number, GroupNode>
  /** subgroups of a group, theirs and so on */
  descendants: (id: number) => number[]
  /** levels a group and its subgroups take: 1 without subgroups */
  height: (id: number) => number
}

/** Indexes groups as the hub nests them. Unknown parents count as top level. */
export function groupTree(list: Group[]): GroupTree {
  const ids = new Set(list.map((g) => g.id))
  const parentOf = (g: Group) => {
    const p = g.parent_id ?? 0
    return p !== g.id && ids.has(p) ? p : 0
  }
  const children = new Map<number, Group[]>()
  for (const g of list) {
    const p = parentOf(g)
    children.set(p, [...(children.get(p) ?? []), g])
  }
  for (const c of children.values())
    c.sort((a, b) => a.name.localeCompare(b.name))

  const ordered: GroupNode[] = []
  const seen = new Set<number>()
  const walk = (
    parent: number,
    depth: number,
    path: string,
    ancestors: number[]
  ) => {
    for (const g of children.get(parent) ?? []) {
      if (seen.has(g.id)) continue
      seen.add(g.id)
      const node: GroupNode = {
        ...g,
        depth,
        path: path ? `${path} › ${g.name}` : g.name,
        ancestors,
      }
      ordered.push(node)
      walk(g.id, depth + 1, node.path, [g.id, ...ancestors])
    }
  }
  walk(0, 0, "", [])
  // Groups left out by a cycle in stored data still show, at the top.
  for (const g of list)
    if (!seen.has(g.id))
      ordered.push({ ...g, depth: 0, path: g.name, ancestors: [] })

  const byId = new Map(ordered.map((g) => [g.id, g]))
  const descendants = (id: number) =>
    ordered.filter((g) => g.ancestors.includes(id)).map((g) => g.id)
  const height = (id: number) => {
    const d = byId.get(id)?.depth ?? 0
    return (
      1 +
      Math.max(
        0,
        ...ordered
          .filter((g) => g.ancestors.includes(id))
          .map((g) => g.depth - d)
      )
    )
  }
  return { ordered, byId, descendants, height }
}

/** Name of a subgroup without its parent's name in front, where it repeats
 * it ("Infrastructure DNS" in "Infrastructure" shows as "DNS"). */
export function shortGroupName(g: GroupNode, byId: Map<number, GroupNode>) {
  const parent = g.ancestors.length ? byId.get(g.ancestors[0]) : undefined
  if (!parent) return g.name
  const p = parent.name.trim()
  if (!p || !g.name.toLowerCase().startsWith(p.toLowerCase())) return g.name
  const after = g.name.slice(p.length)
  // only at a word boundary: "Infra" does not shorten "Infrastructure"
  if (!/^[\s\-–—:/·›»]/.test(after)) return g.name
  return after.replace(/^[\s\-–—:/·›»]+/, "") || g.name
}

/** Path of a group with short names ("Infrastructure › DNS", not
 * "Infrastructure › Infrastructure DNS"). */
export function shortGroupPath(g: GroupNode, byId: Map<number, GroupNode>) {
  const chain = [...g.ancestors].reverse().map((id) => byId.get(id)).filter((x): x is GroupNode => !!x)
  return [...chain, g].map((n) => shortGroupName(n, byId)).join(" › ")
}
