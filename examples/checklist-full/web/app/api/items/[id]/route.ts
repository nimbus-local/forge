import { getServerSession } from 'next-auth'
import { NextResponse } from 'next/server'
import { authOptions } from '@/auth'

const API_URL = (process.env.SST_API_GATEWAY_URL ?? process.env.API_URL ?? '').replace(/\/$/, '')
const INTERNAL_KEY = process.env.SST_SECRET_INTERNAL_KEY ?? process.env.INTERNAL_API_KEY ?? ''

type Params = { params: Promise<{ id: string }> }

async function proxyRequest(method: string, path: string, userId: string, body?: unknown) {
  return fetch(`${API_URL}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      'x-internal-key': INTERNAL_KEY,
      'x-user-id': userId,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
}

// PATCH /api/items/:id — toggle the done state.
export async function PATCH(req: Request, { params }: Params) {
  const session = await getServerSession(authOptions)
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const { id } = await params
  const body = await req.json()
  const res = await proxyRequest('PATCH', `/items/${id}`, session.user.id, body)
  return NextResponse.json(await res.json(), { status: res.status })
}

// DELETE /api/items/:id — remove an item.
export async function DELETE(_req: Request, { params }: Params) {
  const session = await getServerSession(authOptions)
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const { id } = await params
  const res = await proxyRequest('DELETE', `/items/${id}`, session.user.id)
  return new NextResponse(null, { status: res.status })
}
