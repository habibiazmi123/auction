"use client"

import { useReactTable, getCoreRowModel, type ColumnDef } from "@tanstack/react-table"
import type { Product } from "@/lib/types"
import { Badge } from "@/components/ui/badge"
import { DataGrid, DataGridContainer } from "@/components/reui/data-grid/data-grid"
import { DataGridTable } from "@/components/reui/data-grid/data-grid-table"
import { DataGridColumnHeader } from "@/components/reui/data-grid/data-grid-column-header"
import { DataGridScrollArea } from "@/components/reui/data-grid/data-grid-scroll-area"
import { DataGridPagination } from "@/components/reui/data-grid/data-grid-pagination"

const statusColors: Record<string, string> = {
  available: "bg-green-100 text-green-700",
  locked: "bg-amber-100 text-amber-700",
  draft: "bg-gray-100 text-gray-600",
}

const columns: ColumnDef<Product, any>[] = [
  {
    accessorKey: "name",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Name" />,
  },
  {
    accessorKey: "quantity",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Quantity" />,
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
    accessorFn: (row) => new Date(row.created_at).toLocaleDateString(),
    id: "created",
    header: ({ column }) => <DataGridColumnHeader column={column} title="Created" />,
  },
]

interface ProductsGridProps {
  data: Product[]
}

export function ProductsGrid({ data }: ProductsGridProps) {
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
        window.location.href = `/products/${row.id}`
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
