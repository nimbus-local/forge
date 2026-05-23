import type { Metadata } from 'next'
import { getServerSession } from 'next-auth'
import { authOptions } from '@/auth'
import { Providers } from './providers'
import './globals.css'

export const metadata: Metadata = {
  title: 'Checklist',
  description: 'A personal checklist with GitHub login',
}

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const session = await getServerSession(authOptions)
  return (
    <html lang="en">
      <body>
        <Providers session={session}>{children}</Providers>
      </body>
    </html>
  )
}
