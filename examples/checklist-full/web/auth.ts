import type { NextAuthOptions } from 'next-auth'
import GitHubProvider from 'next-auth/providers/github'

// In production, forge injects secrets as SST_SECRET_* env vars via the
// NewSecret construct. In local dev, set them in web/.env.local instead.
export const authOptions: NextAuthOptions = {
  providers: [
    GitHubProvider({
      clientId: process.env.SST_SECRET_GITHUB_ID ?? '',
      clientSecret: process.env.SST_SECRET_GITHUB_SECRET ?? '',
    }),
  ],
  secret: process.env.SST_SECRET_NEXTAUTH_SECRET ?? process.env.NEXTAUTH_SECRET,
  pages: {
    signIn: '/login',
  },
  callbacks: {
    // Expose the GitHub user ID in the session so the API proxy can forward it
    // to the Go Lambda as the x-user-id header.
    session({ session, token }) {
      if (session.user) {
        session.user.id = token.sub!
      }
      return session
    },
  },
}
