import { writeFileSync, readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// 开发模式下把 /api 代理到 Go 服务（默认 :8787），
// 生产构建产物由 Go 进程直接托管，无需额外 Web 服务器。
// 可用 SHENSHI_API 覆盖后端地址：SHENSHI_API=http://127.0.0.1:9000 npm run dev
const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {}

/**
 * 把构建号打进 sw.js。
 *
 * 为什么必须这样：sw.js 放在 public/ 下，Vite 原样复制，不做任何内容替换。
 * 而缓存名要靠这个构建号区分版本 —— 换了构建号才会建新缓存，
 * 新 SW 的 activate 阶段把旧缓存整批删掉，前端更新才不会残留旧资源。
 *
 * 走 writeBundle 而不是 generateBundle：public/ 下的文件不进 bundle 对象。
 */
function swBuildStamp(outDir: string): Plugin {
  return {
    name: 'shenshi-sw-build-stamp',
    apply: 'build',
    writeBundle() {
      const swPath = resolve(outDir, 'sw.js')
      if (!existsSync(swPath)) return
      // 用时间戳而非内容哈希：同一天内连续发两次构建也要被认成新版本。
      const stamp = Date.now().toString(36)
      writeFileSync(swPath, readFileSync(swPath, 'utf8').replace('__SHENSHI_BUILD__', stamp), 'utf8')
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), swBuildStamp('dist')],
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
