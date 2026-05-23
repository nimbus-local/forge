/** @type {import('next').NextConfig} */
const nextConfig = {
  // next/image optimization requires a separate Lambda that NewNextjsSite does not yet
  // deploy. Setting unoptimized:true makes next/image emit plain <img> tags pointing
  // directly at the source URL, bypassing /_next/image entirely.
  // Remove this once NewNextjsSite adds the open-next image-optimization-function.
  images: {
    unoptimized: true,
  },
}

module.exports = nextConfig
