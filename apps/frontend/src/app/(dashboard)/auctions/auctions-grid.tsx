"use client"

import { useReactTable, getCoreRowModel, type ColumnDef } from "@tanstack/react-table"
import type { Auction } from "@/lib/types"
import { Badge } from "@/components/ui/badge"
import { DataGrid, DataGridContainer } from "@/components/reui/data-grid/data-grid"
import { DataGridTable } from "@/components/reui/data-grid/data-grid-table"
import { DataGridColumnHeader } from "@/components/reui/data-grid/data-grid-column-header"
import { DataGridScrollArea } from "@/components/reui/data-grid/data-grid-scroll-area"
import { DataGridPagination } from "@/components/reui/data-grid/data-grid-pagination"

function formatCents(cents: number) {
  return `$${(cents / 100).toFixed(2)}`
}

const statusColors: Record<string, string> = {
  live: "bg-[var(--color-accent)] text-white",
  scheduled: "bg-[var(--color-text-muted)] text-white",
  closed: "bg-[var(--color-shadow-dark)] text-white",
  draft: "bg-[var(--color-border)] text-[var(--color-text)]",
}

const columns: ColumnDef<Auction, any>[] = [
  {
    accessorKey: "product_name",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Product" />,
  },
  {
    id: "currentBid",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Current Bid" />,
    accessorFn: (row) => formatCents(row.current_price_cents || row.starting_price_cents),
  },
  {
    accessorKey: "status",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Status" />,
    cell: (info) => {
      const status = info.getValue() as string
      return <Badge className={statusColors[status] || ""}>{status}</Badge>
    },
  },
  {
    accessorFn: (row) => new Date(row.ends_at).toLocaleString(),
    id: "endsAt",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Ends At" />,
  },
]

interface AuctionsGridProps {
  data: Auction[]
}

export function AuctionsGrid({ data }: AuctionsGridProps) {
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    state: {
      pagination: { pageIndex: 0, pageSize: 20 },
    },
  })

  return (
    <DataGrid
      table={table}
      recordCount={data.length}
      onRowClick={(row) => {
        window.location.href = `/auctions/${row.id}`
      }}
    >
      <DataGridContainer>
        <DataGridScrollArea>
          <DataGridTable />
        </DataGridScrollArea>
      </DataGridContainer>
      <DataGridPagination />
    </DataGrid>
  )
}
