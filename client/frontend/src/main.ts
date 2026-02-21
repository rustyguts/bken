import './style.css'
import { createApp } from 'vue'
import App from './App.vue'
import { THEME_STORAGE_KEY } from './constants'

// Apply saved theme before mount to avoid flash of wrong theme
const savedTheme = localStorage.getItem(THEME_STORAGE_KEY)
if (savedTheme) {
  document.documentElement.setAttribute('data-theme', savedTheme)
}

createApp(App).mount('#app')
