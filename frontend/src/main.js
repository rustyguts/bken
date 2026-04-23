import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'

// Register Vidstack's web components. These imports are side-effect only —
// each one calls `customElements.define(...)` for its slice of the element
// catalogue. Using them explicitly (rather than `vidstack/bundle`, whose
// package export resolves to a `.d.ts`-only entry in v1.12 and therefore
// *registers nothing at runtime*, which is what left us with a silently-
// black <media-player> shell).
import 'vidstack/player'
import 'vidstack/player/ui'
import 'vidstack/player/layouts/default'

import 'vidstack/player/styles/default/theme.css'
import 'vidstack/player/styles/default/layouts/video.css'

createApp(App).mount('#app')
