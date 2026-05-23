import { getToken } from 'next-auth/jwt'
import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

// next-auth/middleware derives the redirect base URL from the request Host header.
// Behind CloudFront, the Host header seen by Lambda is the Lambda Function URL, not
// the CloudFront domain. Using NEXTAUTH_URL as the base keeps the browser on the
// correct domain so static assets resolve against CloudFront.
export async function middleware(req: NextRequest) {
  const token = await getToken({
    req,
    secret: process.env.SST_SECRET_NEXTAUTH_SECRET ?? process.env.NEXTAUTH_SECRET,
  })

  if (!token) {
    const base = process.env.NEXTAUTH_URL ?? req.nextUrl.origin
    const loginUrl = new URL('/login', base)
    loginUrl.searchParams.set('callbackUrl', req.nextUrl.href)
    return NextResponse.redirect(loginUrl)
  }

  return NextResponse.next()
}

// Protect all routes except the login page and NextAuth API routes.
export const config = {
  matcher: ['/((?!api/auth|login|_next/static|_next/image|favicon.ico).*)'],
}
