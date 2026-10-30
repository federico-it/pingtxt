"use client"

import Link from "next/link"

import { Empty, PageHeader } from "@/components/page-header"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useApi, type Incident } from "@/lib/api"
import { useNow } from "@/hooks/use-now"
import { datetime, duration } from "@/lib/format"

export default function IncidentsPage() {
  const { data } = useApi<Incident[]>("/api/incidents")
  const now = useNow()
  return (
    <>
      <PageHeader cmd="pingtxt incidents ls --limit 100" title="incidents" />
      {data && data.length === 0 ? (
        <Empty>no incidents recorded.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>state</TableHead>
              <TableHead>monitor</TableHead>
              <TableHead>started</TableHead>
              <TableHead>duration</TableHead>
              <TableHead className="hidden md:table-cell">cause</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {(data ?? []).map((i) => (
              <TableRow key={i.id}>
                <TableCell>{i.resolved_at ? <span className="text-up">resolved</span> : <span className="animate-pulse text-down">ongoing</span>}</TableCell>
                <TableCell>
                  <Link href={`/monitors/view/?id=${i.monitor_id}`} className="underline-offset-4 hover:underline">
                    {i.monitor_name}
                  </Link>
                </TableCell>
                <TableCell className="text-muted-foreground">{datetime(i.started_at)}</TableCell>
                <TableCell>{duration((i.resolved_at || now) - i.started_at)}</TableCell>
                <TableCell className="hidden max-w-md truncate text-muted-foreground md:table-cell">{i.cause}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </>
  )
}
