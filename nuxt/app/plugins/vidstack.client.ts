// Register Vidstack's web components on the client only.
//
// `.client.ts` plugins only run in the browser, so the imports don't try to
// touch `customElements` during SSR / the Nitro build. Explicit imports are
// required — the `vidstack/bundle` export in v1.12 is a `.d.ts`-only entry
// that wouldn't register anything at runtime.

import 'vidstack/player'
import 'vidstack/player/ui'
import 'vidstack/player/layouts/default'

import 'vidstack/player/styles/default/theme.css'
import 'vidstack/player/styles/default/layouts/video.css'

export default defineNuxtPlugin(() => {})
