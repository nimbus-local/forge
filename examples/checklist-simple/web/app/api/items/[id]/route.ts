import { DynamoDBClient } from '@aws-sdk/client-dynamodb'
import { DynamoDBDocumentClient, UpdateCommand, DeleteCommand } from '@aws-sdk/lib-dynamodb'
import { cookies } from 'next/headers'
import { NextResponse } from 'next/server'

const client = new DynamoDBClient({})
const docClient = DynamoDBDocumentClient.from(client)
const tableName = process.env.SST_TABLE_ITEMS_NAME!

type Params = { params: { id: string } }

// PATCH /api/items/:id — toggle the done field.
export async function PATCH(req: Request, { params }: Params) {
  const cookieStore = cookies()
  const userId = cookieStore.get('userId')?.value
  if (!userId) {
    return NextResponse.json({ error: 'no session' }, { status: 401 })
  }

  const { done } = await req.json()

  const result = await docClient.send(
    new UpdateCommand({
      TableName: tableName,
      Key: { userId, itemId: params.id },
      UpdateExpression: 'SET #done = :done',
      // 'done' is a DynamoDB reserved word — use an expression attribute name.
      ExpressionAttributeNames: { '#done': 'done' },
      ExpressionAttributeValues: { ':done': Boolean(done) },
      ReturnValues: 'ALL_NEW',
    })
  )

  return NextResponse.json(result.Attributes)
}

// DELETE /api/items/:id — remove an item.
export async function DELETE(_req: Request, { params }: Params) {
  const cookieStore = cookies()
  const userId = cookieStore.get('userId')?.value
  if (!userId) {
    return NextResponse.json({ error: 'no session' }, { status: 401 })
  }

  await docClient.send(
    new DeleteCommand({
      TableName: tableName,
      Key: { userId, itemId: params.id },
    })
  )

  return new NextResponse(null, { status: 204 })
}
