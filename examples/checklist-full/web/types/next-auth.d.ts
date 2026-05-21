import 'next-auth'

// Extend the built-in session type to include the user's ID (GitHub subject claim).
declare module 'next-auth' {
  interface Session {
    user: {
      id: string
      name?: string | null
      email?: string | null
      image?: string | null
    }
  }
}
