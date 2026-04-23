import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// Vue compiler must treat Vidstack's <media-*> web components as custom
// elements so it doesn't warn/fail trying to resolve them as Vue components.
export default defineConfig({
  plugins: [
    vue({
      template: {
        compilerOptions: {
          isCustomElement: (tag) => tag.startsWith('media-'),
        },
      },
    }),
  ],
  // Dev server proxies /api, /thumb, /clip to the FastAPI backend.
  // Target is overridable via VITE_API_HOST so the same Vite config works
  // on the host (defaults to localhost:8765) and inside compose
  // (VITE_API_HOST=http://backend:8765).
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: (() => {
      const target = process.env.VITE_API_HOST || 'http://127.0.0.1:8765'
      return {
        '/api':   target,
        '/thumb': target,
        '/clip':  target,
      }
    })(),
  },
  // Build straight into the Python package so `uv run vidpipe web` has no
  // separate static-file hand-off.
  build: {
    outDir: fileURLToPath(new URL('../src/vidpipe/static/app', import.meta.url)),
    emptyOutDir: true,
    sourcemap: false,
  },
})
