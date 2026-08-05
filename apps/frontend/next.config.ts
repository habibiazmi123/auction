import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      {
        source: "/v1/:path*",
        destination: `${process.env.API_GATEWAY_URL || "http://localhost:8080"}/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
