// Nuxt 4 config for the bken admin panel.
//
// Notes:
// - `ssr: false` — internal tool; SSR adds no SEO value and would force
//   `<ClientOnly>` wrappers around every Vidstack web component.
// - `isCustomElement` keeps Vue from trying to resolve `<media-player>`,
//   `<media-provider>`, etc. as Vue components.
// - `bun:sqlite` is a Bun built-in; Rollup should treat it as external.

export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2025-11-01',
  devtools: { enabled: false },
  future: { compatibilityVersion: 4 },

  modules: ['@nuxt/ui'],

  css: ['~/assets/css/main.css'],

  vue: {
    compilerOptions: {
      isCustomElement: (tag) => tag.startsWith('media-'),
    },
  },

  vite: {
    vue: {
      template: {
        compilerOptions: {
          isCustomElement: (tag) => tag.startsWith('media-'),
        },
      },
    },
  },

  nitro: {
    externals: {
      external: ['bun:sqlite'],
    },
  },

  icon: {
    serverBundle: { collections: ['lucide'] },
  },

  app: {
    head: {
      title: 'bken · clip archive',
    },
  },
})
