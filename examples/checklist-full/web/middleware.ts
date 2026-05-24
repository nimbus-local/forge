import { getToken } from 'next-auth/jwt'
import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

// The NewNextjsSite CloudFront viewer-request function copies Host → x-forwarded-host
// before forwarding to the Lambda origin, so the public CloudFront domain is always
// available here regardless of what the Lambda Function URL host header says.
export async function middleware(req: NextRequest) {
  const token = await getToken({
    req,
    secret: process.env.SST_SECRET_NEXTAUTH_SECRET ?? process.env.NEXTAUTH_SECRET,
  })

  if (!token) {
    const host = req.headers.get('x-forwarded-host') ?? req.nextUrl.host
    const loginUrl = new URL('/login', `https://${host}`)
    loginUrl.searchParams.set('callbackUrl', req.nextUrl.href)
    return NextResponse.redirect(loginUrl)
  }

  return NextResponse.next()
}

// Protect all routes except the login page and NextAuth API routes.
export const config = {
  matcher: ['/((?!api/auth|login|_next/static|_next/image|favicon.ico).*)'],
}
