export { default } from 'next-auth/middleware'

// Protect all routes except the login page and NextAuth API routes.
export const config = {
  matcher: ['/((?!api/auth|login|_next/static|_next/image|favicon.ico).*)'],
}
