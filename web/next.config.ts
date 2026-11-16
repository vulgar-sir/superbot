import type { NextConfig } from "next";

const apiBase = process.env.SUPERBOT_API_BASE ?? "http://127.0.0.1:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  allowedDevOrigins: ["127.0.0.1", "localhost"],
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiBase}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
