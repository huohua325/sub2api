import { defineConfig, loadEnv, Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import checker from 'vite-plugin-checker'
import { resolve } from 'path'
import { execFile } from 'node:child_process'
import { isAbsolute } from 'node:path'
import { promisify } from 'node:util'

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  })[character] || character)
}

function isSafeImageUrl(value: string): boolean {
  const trimmed = value.trim()
  if ((trimmed.startsWith('/') && !trimmed.startsWith('//')) || /^data:image\//i.test(trimmed)) {
    return true
  }
  try {
    const parsed = new URL(trimmed)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function injectBranding(html: string, config: { site_name?: string; site_logo?: string }): string {
  let brandedHtml = html
  const siteName = config.site_name?.trim()
  if (siteName) {
    brandedHtml = brandedHtml.replace(
      /<title>[^<]*<\/title>/i,
      `<title>${escapeHtml(siteName)} - AI API Gateway</title>`,
    )
  }

  const siteLogo = config.site_logo?.trim()
  if (siteLogo && isSafeImageUrl(siteLogo)) {
    brandedHtml = brandedHtml.replace(
      /<link\s+rel=["']icon["'][^>]*>/i,
      `<link rel="icon" href="${escapeHtml(siteLogo)}" />`,
    )
  }
  return brandedHtml
}

/**
 * Vite 插件：开发模式下注入公开配置到 index.html
 * 与生产模式的后端注入行为保持一致，消除闪烁
 */
function injectPublicSettings(backendUrl: string): Plugin {
  return {
    name: 'inject-public-settings',
    apply: 'serve',
    transformIndexHtml: {
      order: 'pre',
      async handler(html) {
        try {
          const response = await fetch(`${backendUrl}/api/v1/settings/public`, {
            signal: AbortSignal.timeout(2000)
          })
          if (response.ok) {
            const data = await response.json()
            if (data.code === 0 && data.data) {
              const script = `<script>window.__APP_CONFIG__=${JSON.stringify(data.data)};</script>`
              return injectBranding(html, data.data).replace('</head>', `${script}\n</head>`)
            }
          }
        } catch (e) {
          console.warn('[vite] 无法获取公开配置，将回退到 API 调用:', (e as Error).message)
        }
        return html
      }
    }
  }
}

function localT3Pairing(backendUrl: string, env: Record<string, string>): Plugin {
  const native = env.VITE_DEV_WORKSPACE_NATIVE_URL
  const cli = env.DEV_WORKSPACE_NATIVE_CLI || ''
  const dataDir = env.DEV_WORKSPACE_NATIVE_DATA_DIR || ''
  const run = promisify(execFile)
  const loopback = /^http:\/\/127\.0\.0\.1:[1-9]\d{0,4}\/?$/
  return {
    name: 'local-t3-pairing',
    apply: 'serve',
    configureServer(server) {
      if (server.config.server.host !== '127.0.0.1' || !loopback.test(native || '') ||
          !loopback.test(backendUrl) || !isAbsolute(cli) || !isAbsolute(dataDir)) return
      const t3Origin = new URL(native).origin
      server.middlewares.use(async (request, response, next) => {
        if (request.url !== '/api/v1/local-t3/pair') return next()
        response.setHeader('Cache-Control', 'no-store')
        response.setHeader('Content-Type', 'application/json')
        const localHost = `127.0.0.1:${request.socket.localPort}`
        if (request.method !== 'POST' || request.socket.remoteAddress !== '127.0.0.1' ||
            request.headers.host !== localHost || request.headers.origin !== `http://${localHost}` ||
            !request.headers.authorization?.startsWith('Bearer ')) {
          response.statusCode = 403
          return response.end()
        }
        try {
          const status = await fetch(new URL('/api/v1/admin/development-workspace/status', backendUrl), {
            headers: {
              Authorization: request.headers.authorization,
              'User-Agent': request.headers['user-agent'] || '',
              'X-Admin-UI-Request': '1',
            },
            redirect: 'error',
            signal: AbortSignal.timeout(5000),
          })
          if (!status.ok || (await status.json()).data?.enabled !== true) {
            response.statusCode = status.ok ? 503 : status.status
            return response.end()
          }
          const { stdout } = await run(cli, [
            'auth', 'pairing', 'create', '--base-dir', dataDir, '--base-url', t3Origin,
            '--ttl', '1m', '--label', 'MILE local preview', '--json',
          ], { timeout: 8000, maxBuffer: 16384, windowsHide: true })
          const grant = JSON.parse(stdout) as { credential?: string }
          if (typeof grant.credential !== 'string' || !grant.credential) throw new Error('Missing T3 pairing grant')
          const url = new URL('/pair', t3Origin)
          url.hash = new URLSearchParams({ token: grant.credential }).toString()
          response.end(JSON.stringify({ code: 0, message: 'success', data: { url: url.href } }))
        } catch {
          response.statusCode = 502
          response.end()
        }
      })
    },
  }
}

export default defineConfig(({ mode }) => {
  // 加载环境变量
  const env = loadEnv(mode, process.cwd(), '')
  const backendUrl = env.VITE_DEV_PROXY_TARGET || 'http://localhost:8080'
  const devPort = Number(env.VITE_DEV_PORT || 3000)

  return {
    plugins: [
      vue(),
      checker({
        vueTsc: true
      }),
      injectPublicSettings(backendUrl),
      localT3Pairing(backendUrl, env)
    ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      // 使用 vue-i18n 运行时版本，避免 CSP unsafe-eval 问题；
      // v10 起 JIT 编译恒开启，消息编译为 AST 对象而非 JS 代码，无需 unsafe-eval
      'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js'
    }
  },
  build: {
    outDir: '../backend/internal/web/dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        /**
         * 手动分包配置
         * 分离第三方库并按功能合并应用代码，避免循环依赖
         */
        manualChunks(id: string) {
          if (id.includes('node_modules')) {
            // Vue 核心库
            if (
              id.includes('/vue/') ||
              id.includes('/vue-router/') ||
              id.includes('/pinia/') ||
              id.includes('/@vue/')
            ) {
              return 'vendor-vue'
            }

            // UI 工具库（较大，单独分离）
            if (id.includes('/xlsx/')) {
              return 'vendor-export'
            }
            if (id.includes('/@vueuse/')) {
              return 'vendor-ui'
            }

            // 图表库
            if (id.includes('/chart.js/') || id.includes('/vue-chartjs/')) {
              return 'vendor-chart'
            }

            // 国际化
            if (id.includes('/vue-i18n/') || id.includes('/@intlify/')) {
              return 'vendor-i18n'
            }

            // Stripe 仅在支付流程中按需加载，避免进入首页公共依赖。
            if (id.includes('/@stripe/stripe-js/')) {
              return 'vendor-stripe'
            }
            if (id.includes('/@airwallex/components-sdk/')) {
              return 'vendor-airwallex'
            }

            // 其他小型第三方库合并
            return 'vendor-misc'
          }

          // 应用代码：按入口点自动分包，不手动干预
          // 这样可以避免循环依赖，同时保持合理的 chunk 数量
        }
      }
    }
  },
    server: {
      host: '0.0.0.0',
      port: devPort,
      proxy: {
        '/api': {
          target: backendUrl,
          changeOrigin: true
        },
        '/v1': {
          target: backendUrl,
          changeOrigin: true
        },
        '/setup': {
          target: backendUrl,
          changeOrigin: true
        }
      }
    }
  }
})
