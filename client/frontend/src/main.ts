import './style.css'
import { createApp } from 'vue'
import App from './App.vue'

// Apply saved theme before mount to avoid flash of wrong theme
const savedTheme = localStorage.getItem('bken-theme')
if (savedTheme) {
  document.documentElement.setAttribute('data-theme', savedTheme)
}

createApp(App).mount('#app')
