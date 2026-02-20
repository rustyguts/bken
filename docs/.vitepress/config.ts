import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'BKEN',
  description: 'Self-hosted voice chat. Encrypted, fast, lightweight.',
  // Matches the GitHub Pages URL: https://<org>.github.io/bken/
  base: '/bken/',

  head: [
    ['meta', { name: 'theme-color', content: '#34d399' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'BKEN — Self-hosted voice chat' }],
    ['meta', { property: 'og:description', content: 'Encrypted, low-latency voice chat you run yourself. No accounts. No cloud.' }],
  ],

  themeConfig: {
    nav: [
      { text: 'Download', link: '/download' },
    ],

    sidebar: [
      { text: 'Download', link: '/download' },
    ],

    search: {
      provider: 'local',
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/rustyguts/bken' },
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Your voice, your server.',
    },
  },
})
