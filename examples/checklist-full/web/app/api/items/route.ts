import { getServerSession } from 'next-auth'
import { NextResponse } from 'next/server'
import { authOptions } from '@/auth'

// forge injects the API Gateway URL as SST_API_GATEWAY_URL via the Link field.
// In local dev set SST_API_GATEWAY_URL (or API_URL) in web/.env.local.
const API_URL = (process.env.SST_API_GATEWAY_URL ?? process.env.API_URL ?? '').replace(/\/$/, '')
const INTERNAL_KEY = process.env.SST_SECRET_INTERNAL_KEY ?? process.env.INTERNAL_API_KEY ?? ''

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

// GET /api/items — list the authenticated user's items.
export async function GET() {
  const session = await getServerSession(authOptions)
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const res = await proxyRequest('GET', '/items', session.user.id)
  return NextResponse.json(await res.json(), { status: res.status })
}

// POST /api/items — create a new item.
export async function POST(req: Request) {
  const session = await getServerSession(authOptions)
  if (!session?.user?.id) {
    return NextResponse.json({ error: 'Unauthorized' }, { status: 401 })
  }

  const body = await req.json()
  const res = await proxyRequest('POST', '/items', session.user.id, body)
  return NextResponse.json(await res.json(), { status: res.status })
}
