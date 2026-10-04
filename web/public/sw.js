/* eslint-env serviceworker */
/**
 * 慎始的 Service Worker：负责「读」这一半的离线能力。
 *
 * 分工（与 web/src/lib/offline.ts 一致，不要单边改）：
 *   - 这里的 SW：只管读。GET 走网络优先、失败回退缓存；导航请求网络优先、失败回退应用外壳。
 *     写请求一律原样放行 —— 离线写入的排队与重放由页面做，那里才有完整的类型信息，
 *     也才能构造出前端期望的响应体。
 *   - 页面侧 offline.ts：outbox 队列、离线新建任务的临时 id、重放与对账。
 *
 * 关于 Safari：iOS/macOS Safari 没有 Background Sync（SyncManager），
 * 所以重放一律由页面的 online 事件 + 定时器驱动，不依赖 sync 事件。
 *
 * BUILD 由 vite.config.ts 在构建时替换（见该文件的 swBuildStamp 插件）。
 * 它是缓存名的一部分：换了 BUILD 才会建新缓存，activate 时旧缓存被整批删掉——
 * 这就是「前端更新后不会残留旧资源」的根本保障。
 */
const BUILD = '__SHENSHI_BUILD__'
const SHELL_CACHE = `shenshi-shell-${BUILD}`
const API_CACHE = `shenshi-api-${BUILD}`
const KEEP = new Set([SHELL_CACHE, API_CACHE])

/** 应用外壳：装好之后断网也能把界面拉起来。 */
const SHELL_URLS = ['/', '/index.html', '/manifest.webmanifest', '/icons/icon-192.png', '/icons/icon-512.png', '/fonts/shenshi-lishu.woff2']

/**
 * 这些接口不能进缓存：
 *   - /api/export*    导出走浏览器下载，体积大且是一次性的，缓存起来只占地方。
 *   - /api/attachments* 附件是二进制流，走的是 attachment URL，不该进 JSON 缓存。
 *   - /api/health     纯粹探活，缓存了会给出「服务正常」的假象。
 */
function isCacheableApi(pathname) {
  if (!pathname.startsWith('/api/')) return false
  if (pathname.startsWith('/api/export')) return false
  if (pathname.startsWith('/api/attachments')) return false
  if (pathname === '/api/health') return false
  // SSE 长连接：一旦被 Cache 接管，流就废了（拿到的是上一次请求的快照正文）。
  if (pathname === '/api/stream') return false
  return true
}

/**
 * 读缓存的条目上限。
 *
 * 任务列表是按「视图 × 排序 × 筛选」拼 URL 的，组合随使用不断增长；
 * 不设上限的话，一个用得越久的人，本机缓存会一直涨——而 PWA 的存储配额
 * 是有上限的，撑爆之后浏览器会直接拒绝写入，离线能力随之失效。
 * 超出后按写入先后淘汰最早的：Cache 的 keys() 保持插入顺序，够用即可，
 * 不值得为了一个缓存去上 LRU。
 */
const MAX_API_ENTRIES = 300

async function trimCache(cacheName, max) {
  const cache = await caches.open(cacheName)
  const keys = await cache.keys()
  if (keys.length <= max) return
  for (const k of keys.slice(0, keys.length - max)) {
    await cache.delete(k).catch(() => undefined)
  }
}

/**
 * 联网时最多等多久才改用缓存。
 *
 * 弱网是移动端的常态：地铁、电梯、信号空档。原来一律「网络优先」，
 * 网络慢就意味着白屏等着 —— 而同样的内容，缓存里其实有一份。
 * 现在改成先给缓存（秒开），网络在 1.2 秒内回来了就用网络的；
 * 回不来就让页面先把旧的显示出来，网络到位后再通知它校正一次。
 */
const NET_WAIT_MS = 1200

/** 值得通知页面的读接口：数据变了才需要重新拉，图标之类的不必。 */
const REVALIDATE_PATHS = ['/api/bootstrap', '/api/tasks', '/api/habits', '/api/stats']

// url → 上次通知时刻，避免同一个请求在列表滚动里反复打扰页面。
const notifiedAt = new Map()

function notifyRevalidated(url) {
  let pathname = ''
  try {
    pathname = new URL(url, self.location.origin).pathname
  } catch {
    return
  }
  if (!REVALIDATE_PATHS.some((p) => pathname === p)) return
  const now = Date.now()
  if (now - (notifiedAt.get(pathname) ?? 0) < 5000) return
  notifiedAt.set(pathname, now)
  self.clients.matchAll({ type: 'window' }).then((list) => {
    list.forEach((c) => c.postMessage({ type: 'SW_REVALIDATED', url: pathname }))
  })
}

/** 有缓存时的取数策略：缓存先给，网络在窗口内回来就换成网络的，否则后台继续拉。 */
async function staleWhileRevalidate(request, cacheName) {
  const cache = await caches.open(cacheName)
  const cached = await cache.match(request)
  if (!cached) return networkFirst(request, cacheName, undefined)

  let handedCache = false
  const net = fetch(request)
    .then(async (resp) => {
      // 出错响应（401 / 500）不进缓存，也不算「有新数据」——
      // 它们会把登出态与服务异常伪装成正常内容。
      if (resp && resp.ok && resp.type !== 'opaque') {
        await cache.put(request, resp.clone())
        await trimCache(cacheName, MAX_API_ENTRIES)
        if (handedCache) notifyRevalidated(request.url)
      }
      return resp
    })
    .catch(() => null)

  const fresh = await Promise.race([
    net,
    new Promise((resolve) => setTimeout(() => resolve(null), NET_WAIT_MS)),
  ])
  if (fresh) return fresh
  handedCache = true
  return cached
}

/** 读缓存的网络优先策略：拿到就顺手更新缓存，拿不到才回退。 */
async function networkFirst(request, cacheName, fallback) {
  const cache = await caches.open(cacheName)
  try {
    const resp = await fetch(request)
    // 只留成功且未被显式声明不可缓存的响应。出错响应（401 / 500）缓存下来
    // 会把「登出态」和「服务异常」伪装成正常数据，比没有缓存更糟。
    if (resp && resp.ok && resp.type !== 'opaque') {
      await cache.put(request, resp.clone())
      await trimCache(cacheName, MAX_API_ENTRIES)
    }
    return resp
  } catch (err) {
    const cached = await cache.match(request)
    if (cached) return cached
    if (fallback !== undefined) return fallback
    throw err
  }
}

/** 带内容哈希的构建产物：命中就直接用，miss 才落网。 */
async function cacheFirst(request, cacheName) {
  const cache = await caches.open(cacheName)
  const cached = await cache.match(request)
  if (cached) return cached
  const resp = await fetch(request)
  if (resp && resp.ok && resp.type !== 'opaque') {
    cache.put(request, resp.clone()).catch(() => undefined)
  }
  return resp
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(SHELL_CACHE)
      // 逐个 add 而不是整体 addAll：某一个图标 404 不该让整次安装失败，
      // 否则连界面都离不了线。缓存里缺哪一项，运行时还会再补。
      await Promise.all(
        SHELL_URLS.map((url) =>
          cache.add(new Request(url, { cache: 'reload' })).catch(() => undefined),
        ),
      )
      // 不在这里 skipWaiting：让用户看到「有新版本」的提示后再点，
      // 否则页面正在编辑的内容会被一次措手不及的重载抹掉。
    })(),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys()
      await Promise.all(names.filter((n) => !KEEP.has(n)).map((n) => caches.delete(n)))
      await self.clients.claim()
    })(),
  )
})

self.addEventListener('message', (event) => {
  const data = event.data
  if (!data || typeof data !== 'object') return
  if (data.type === 'SKIP_WAITING') {
    self.skipWaiting()
    return
  }
  if (data.type === 'CLEAR_CACHES') {
    // 彻底清除：设置里的「强制刷新」走这条，删干净再让页面自己重载。
    event.waitUntil(
      (async () => {
        const names = await caches.keys()
        await Promise.all(names.map((n) => caches.delete(n)))
        const all = await self.clients.matchAll({ type: 'window' })
        all.forEach((c) => c.postMessage({ type: 'CACHES_CLEARED' }))
      })(),
    )
  }
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return

  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return

  // 导航请求：断网时回到应用外壳，让 PWA 从图标启动后仍能进界面。
  // /api/ 下的导航是导出下载，必须原样放行——回退到 index.html 会把
  // 一段 HTML 当成备份文件存进下载目录。
  if (req.mode === 'navigate') {
    if (url.pathname.startsWith('/api/')) return
    // 应用外壳同样走「缓存先给」：从图标冷启动时先看到界面，
    // 网络到位后再换成新版的 HTML。彻底离线的兜底保留在最后。
    event.respondWith(
      staleWhileRevalidate(req, SHELL_CACHE).catch(async () => {
        const cache = await caches.open(SHELL_CACHE)
        const shell = (await cache.match('/index.html')) || (await cache.match('/'))
        if (shell) return shell
        return new Response('离线且没有可用缓存', {
          status: 503,
          headers: { 'Content-Type': 'text/plain; charset=utf-8' },
        })
      }),
    )
    return
  }

  // 读接口：有缓存就先给缓存（弱网秒开），网络窗口内回来则换成最新的；
  // 没有缓存（首次访问）才老老实实等网络。
  if (isCacheableApi(url.pathname)) {
    event.respondWith(staleWhileRevalidate(req, API_CACHE))
    return
  }

  // 构建产物（文件名带内容哈希）与图标：命中即用。
  // 刻意不兜底成空响应：拿不到资源时让 fetch 如实失败，
  // 浏览器会报「脚本加载失败」，好过静默白屏让人以为应用坏了。
  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/icons/')) {
    event.respondWith(cacheFirst(req, SHELL_CACHE))
  }
})

// ---------------------------------------------------------------------------
// Web Push：页面完全关掉也能把提醒送到（iOS 16.4+，且必须已添加到主屏幕）。
//
// 这里只做「展示」：推送服务已经把消息端到端加密好递过来了，SW 只负责解密后的
// 弹出通知与点击跳转。真正的投递由服务端 internal/push 完成（见 webpush.go）。
//
// 载荷字段与 Go 侧的 WebPushPayload 一一对应，改一处必须改另一处。
// ---------------------------------------------------------------------------

self.addEventListener('push', (event) => {
  let payload = {}
  try {
    payload = event.data ? event.data.json() : {}
  } catch {
    // 载荷不是 JSON：仍然把通知显示出来，只是没有标题与深链。
    // 静默失败的话，用户连「到点了」这个信息都收不到。
    payload = { body: event.data ? event.data.text() : '' }
  }
  const title = payload.title || '慎始'
  event.waitUntil(
    self.registration.showNotification(title, {
      body: payload.body || '',
      // tag 用服务端给的台账键：同一条提醒在设备上互相覆盖，不堆成一串。
      tag: payload.tag || undefined,
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      // 持续提醒：iOS 会按「提醒」而非「横幅」处理，用户能留到点开为止。
      requireInteraction: false,
      data: { url: payload.url || '/', ackId: payload.ackId || 0 },
    }),
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const target = (event.notification.data && event.notification.data.url) || '/'
  event.waitUntil(openNotificationTarget(target))
})

/**
 * 点通知后聚焦已打开的窗口，没有就新开一个。
 *
 * 必须用 openWindow 而不是 clients.openWindow：iOS 上从主屏幕图标启动的窗口
 * 常常不在 clients.matchAll 的结果里（尚未被页面接管），直接开新窗口会多出一个
 * 空白应用，用户看到的是「点一下变成两个图标在眼前」。
 */
async function openNotificationTarget(target) {
  const url = new URL(target, self.location.origin).href
  const list = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  for (const c of list) {
    // 同源且已经有导航能力 → 聚焦它并把地址交给页面处理（深链需要）。
    if ('focus' in c && new URL(c.url).origin === self.location.origin) {
      c.postMessage({ type: 'NOTIFICATION_CLICK', url })
      return c.focus()
    }
  }
  return self.clients.openWindow(url)
}

self.addEventListener('notificationclose', () => {
  // 刻意不做事：通知被划掉不等于任务完成，把「关掉」当成任何一种状态都会错。
})
