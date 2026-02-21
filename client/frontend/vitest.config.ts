import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/__tests__/setup.ts'],
    include: ['src/**/*.test.ts', 'src/**/*.spec.ts'],
    // Suppress unhandled promise rejections from component internals
    // (e.g. template ref .focus() calls that jsdom doesn't support)
    dangerouslyIgnoreUnhandledErrors: true,
  },
  resolve: {
    alias: {
      // The wailsjs runtime accesses window.runtime which doesn't exist in test.
      // We provide mock modules instead via setup.ts.
    },
  },
})
