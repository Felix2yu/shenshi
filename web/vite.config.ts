import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// 开发模式下把 /api 代理到 Go 服务（默认 :8787），
// 生产构建产物由 Go 进程直接托管，无需额外 Web 服务器。
// 可用 SHENSHI_API 覆盖后端地址：SHENSHI_API=http://127.0.0.1:9000 npm run dev
const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      '/api': {
        target: env.SHENSHI_API || 'http://127.0.0.1:8787',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 1500,
  },
})
