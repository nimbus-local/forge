/**
 * Middleware tests guard against the class of bugs we hit during checklist-full deployment:
 *
 * 1. Redirect base must come from NEXTAUTH_URL, not the request origin.
 *    Behind CloudFront + Lambda Function URL, the Host header seen by Lambda is the
 *    Lambda URL, not the CloudFront domain. Without NEXTAUTH_URL the redirect sends the
 *    browser to the Lambda domain, where static assets (/_next/static/*) 404.
 *
 * 2. The bare `export { default } from 'next-auth/middleware'` reads NEXTAUTH_SECRET
 *    directly and derives its redirect URL from the request Host — both wrong in this setup.
 *    Use getToken with an explicit secret instead.
 */

import { NextRequest } from 'next/server'

jest.mock('next-auth/jwt', () => ({
  getToken: jest.fn(),
}))

import { getToken } from 'next-auth/jwt'
import { middleware } from '../middleware'

const mockGetToken = getToken as jest.MockedFunction<typeof getToken>

const CLOUDFRONT_URL = 'https://d6ee090je5y94.cloudfront.net'
const LAMBDA_URL = 'https://z4hvlademef7qxvvk5zqkkfi4a0ijjyp.lambda-url.us-east-2.on.aws'

function makeRequest(path: string, { origin = LAMBDA_URL } = {}): NextRequest {
  return new NextRequest(`${origin}${path}`)
}

beforeEach(() => {
  jest.resetAllMocks()
  process.env.NEXTAUTH_URL = CLOUDFRONT_URL
  process.env.SST_SECRET_NEXTAUTH_SECRET = 'test-secret'
})

afterEach(() => {
  delete process.env.NEXTAUTH_URL
  delete process.env.SST_SECRET_NEXTAUTH_SECRET
})

describe('unauthenticated requests', () => {
  beforeEach(() => {
    mockGetToken.mockResolvedValue(null)
  })

  it('redirects to NEXTAUTH_URL/login, not the request origin', async () => {
    // Simulates Lambda receiving a request whose Host is the Lambda URL.
    // Without NEXTAUTH_URL the redirect would land on the Lambda domain and
    // static assets would 404.
    const req = makeRequest('/', { origin: LAMBDA_URL })
    const res = await middleware(req)

    expect(res.status).toBe(307)
    const location = new URL(res.headers.get('location')!)
    expect(location.origin).toBe(CLOUDFRONT_URL)
    expect(location.pathname).toBe('/login')
  })

  it('includes the original URL as callbackUrl', async () => {
    const req = makeRequest('/some/protected/page', { origin: LAMBDA_URL })
    const res = await middleware(req)

    const location = new URL(res.headers.get('location')!)
    expect(location.searchParams.get('callbackUrl')).toBeTruthy()
  })

  it('falls back to request origin when NEXTAUTH_URL is not set', async () => {
    delete process.env.NEXTAUTH_URL
    const req = makeRequest('/', { origin: CLOUDFRONT_URL })
    const res = await middleware(req)

    const location = new URL(res.headers.get('location')!)
    expect(location.pathname).toBe('/login')
  })

  it('passes the correct secret to getToken', async () => {
    const req = makeRequest('/')
    await middleware(req)

    expect(mockGetToken).toHaveBeenCalledWith(
      expect.objectContaining({ secret: 'test-secret' })
    )
  })
})

describe('authenticated requests', () => {
  beforeEach(() => {
    mockGetToken.mockResolvedValue({ sub: 'user-123' } as any)
  })

  it('passes the request through without redirecting', async () => {
    const req = makeRequest('/')
    const res = await middleware(req)

    expect(res.status).toBe(200)
    expect(res.headers.get('location')).toBeNull()
  })
})
