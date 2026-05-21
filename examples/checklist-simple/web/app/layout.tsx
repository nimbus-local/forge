import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'Checklist',
  description: 'A simple anonymous checklist',
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
