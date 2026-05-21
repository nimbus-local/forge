import { DynamoDBClient } from '@aws-sdk/client-dynamodb'
import { DynamoDBDocumentClient, QueryCommand, PutCommand } from '@aws-sdk/lib-dynamodb'
import { cookies } from 'next/headers'
import { NextResponse } from 'next/server'

const client = new DynamoDBClient({})
const docClient = DynamoDBDocumentClient.from(client)
const tableName = process.env.SST_TABLE_ITEMS_NAME!

// GET /api/items — return all items for the cookie-identified user.
export async function GET() {
  const cookieStore = cookies()
  const userId = cookieStore.get('userId')?.value

  // New browser: no cookie yet, so no items.
  if (!userId) {
    return NextResponse.json([])
  }

  const result = await docClient.send(
    new QueryCommand({
      TableName: tableName,
      KeyConditionExpression: 'userId = :uid',
      ExpressionAttributeValues: { ':uid': userId },
    })
  )

  const items = (result.Items ?? []).sort(
    (a, b) => (b.createdAt as string).localeCompare(a.createdAt as string)
  )

  return NextResponse.json(items)
}

// POST /api/items — create an item, setting the userId cookie on first use.
export async function POST(req: Request) {
  const { text } = await req.json()
  if (!text?.trim()) {
    return NextResponse.json({ error: 'text is required' }, { status: 400 })
  }

  const cookieStore = cookies()
  const existingId = cookieStore.get('userId')?.value
  const isNew = !existingId
  const userId = existingId ?? crypto.randomUUID()

  const item = {
    userId,
    itemId: crypto.randomUUID(),
    text: (text as string).trim(),
    done: false,
    createdAt: new Date().toISOString(),
  }

  await docClient.send(new PutCommand({ TableName: tableName, Item: item }))

  const response = NextResponse.json(item, { status: 201 })
  if (isNew) {
    response.cookies.set('userId', userId, {
      httpOnly: true,
      secure: process.env.NODE_ENV === 'production',
      sameSite: 'lax',
      maxAge: 60 * 60 * 24 * 365,
      path: '/',
    })
  }
  return response
}
