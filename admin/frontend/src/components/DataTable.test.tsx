import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DataTable } from './DataTable.tsx'
import type { Column } from './DataTable.tsx'

interface Row {
  id: string
  name: string
  count: number
}

const columns: Column<Row>[] = [
  { key: 'id', header: 'ID', mono: true },
  { key: 'name', header: 'Name' },
  { key: 'count', header: 'Count' },
]

const rows: Row[] = [
  { id: 'a1', name: 'Alpha', count: 10 },
  { id: 'b2', name: 'Beta', count: 20 },
]

describe('DataTable', () => {
  it('renders column headers', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />)
    expect(screen.getByText('ID')).toBeInTheDocument()
    expect(screen.getByText('Name')).toBeInTheDocument()
    expect(screen.getByText('Count')).toBeInTheDocument()
  })

  it('renders all row data', () => {
    render(<DataTable columns={columns} rows={rows} rowKey={(r) => r.id} />)
    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Beta')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByText('20')).toBeInTheDocument()
  })

  it('renders empty message when rows is empty', () => {
    render(
      <DataTable columns={columns} rows={[]} rowKey={(r) => r.id} emptyMessage="Nothing here" />,
    )
    expect(screen.getByText('Nothing here')).toBeInTheDocument()
  })

  it('renders default empty message', () => {
    render(<DataTable columns={columns} rows={[]} rowKey={(r) => r.id} />)
    expect(screen.getByText('No data')).toBeInTheDocument()
  })

  it('uses custom render function', () => {
    const cols: Column<Row>[] = [
      ...columns,
      { key: 'custom', header: 'Custom', render: (r) => <strong>{r.name.toUpperCase()}</strong> },
    ]
    render(<DataTable columns={cols} rows={[rows[0]]} rowKey={(r) => r.id} />)
    expect(screen.getByText('ALPHA')).toBeInTheDocument()
  })

  it('accepts index-based rowKey', () => {
    expect(() =>
      render(<DataTable columns={columns} rows={rows} rowKey={(_, i) => String(i)} />),
    ).not.toThrow()
  })
})
