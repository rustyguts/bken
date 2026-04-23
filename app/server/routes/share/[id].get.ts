// GET /share/:id
//
// Returns a standalone HTML page with Open Graph and Twitter Card meta tags
// so that pasting the link into Discord, Twitter, iMessage, etc. produces an
// inline playable video preview. The page itself is a simple branded video
// player with a download link.

import { db, type ClipRow } from '~~/server/utils/db'
import { resolveData } from '~~/server/utils/paths'

interface ShareRow extends ClipRow {
  video_width: number | null
  video_height: number | null
}

function escapeHtml(text: string | null | undefined): string {
  if (!text) return ''
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function baseUrl(event: any): string {
  const envBase = process.env.BKEN_BASE_URL
  if (envBase) return envBase.replace(/\/$/, '')

  const proto = getRequestHeader(event, 'x-forwarded-proto') || 'http'
  const host = getRequestHeader(event, 'x-forwarded-host') || getRequestHeader(event, 'host') || 'localhost'
  return `${proto}://${host}`
}

function truncate(text: string | null | undefined, max: number): string {
  if (!text) return ''
  return text.length > max ? text.slice(0, max - 1) + '…' : text
}

export default defineEventHandler(async (event) => {
  const raw = String(event.context.params?.id ?? '')
  const id = Number(raw.replace(/\.mp4$/, ''))
  if (!Number.isFinite(id) || id <= 0) {
    throw createError({ statusCode: 400, statusMessage: 'invalid clip id' })
  }

  const row = db()
    .prepare(
      `SELECT c.*, v.game, v.recorded_at, v.path AS video_path,
              v.width AS video_width, v.height AS video_height
         FROM candidate_clip c
         JOIN video v ON v.id = c.video_id
        WHERE c.id = ?`,
    )
    .get(id) as ShareRow | undefined

  if (!row || !row.clip_path) {
    throw createError({ statusCode: 404, statusMessage: 'clip not found' })
  }

  const path = await resolveData(row.clip_path)
  if (!await Bun.file(path).exists()) {
    throw createError({ statusCode: 404, statusMessage: 'clip file missing' })
  }


  const origin = baseUrl(event)
  const shareUrl = `${origin}/share/${id}`
  const videoUrl = `${origin}/clip/${id}.mp4`
  const thumbUrl = `${origin}/thumb/${id}.jpg`
  const downloadUrl = `${origin}/clip/${id}.mp4?download=1`

  const title = row.llm_title || `Clip from ${row.game || 'Unknown'}`
  const description = row.llm_desc || row.llm_contents || `${row.game || 'Unknown'} — ${row.recorded_at?.slice(0, 10) || 'undated'}`
  const width = row.video_width || 1920
  const height = row.video_height || 1080

  const html = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>${escapeHtml(title)} — bken</title>
  <meta name="description" content="${escapeHtml(truncate(description, 200))}">

  <!-- Open Graph (Discord, Facebook, Slack, etc.) -->
  <meta property="og:type" content="video.other">
  <meta property="og:url" content="${escapeHtml(shareUrl)}">
  <meta property="og:title" content="${escapeHtml(title)}">
  <meta property="og:description" content="${escapeHtml(truncate(description, 300))}">
  <meta property="og:site_name" content="bken">
  <meta property="og:image" content="${escapeHtml(thumbUrl)}">
  <meta property="og:image:width" content="${width}">
  <meta property="og:image:height" content="${height}">
  <meta property="og:video" content="${escapeHtml(videoUrl)}">
  <meta property="og:video:url" content="${escapeHtml(videoUrl)}">
  <meta property="og:video:secure_url" content="${escapeHtml(videoUrl)}">
  <meta property="og:video:type" content="video/mp4">
  <meta property="og:video:width" content="${width}">
  <meta property="og:video:height" content="${height}">

  <!-- Twitter Card -->
  <meta name="twitter:card" content="player">
  <meta name="twitter:title" content="${escapeHtml(title)}">
  <meta name="twitter:description" content="${escapeHtml(truncate(description, 200))}">
  <meta name="twitter:image" content="${escapeHtml(thumbUrl)}">
  <meta name="twitter:player" content="${escapeHtml(shareUrl)}">
  <meta name="twitter:player:width" content="${width}">
  <meta name="twitter:player:height" content="${height}">
  <meta name="twitter:player:stream" content="${escapeHtml(videoUrl)}">
  <meta name="twitter:player:stream:content_type" content="video/mp4">

  <style>
    :root { color-scheme: dark; }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: #0b0c10;
      color: #c5c6c7;
      font-family: system-ui, -apple-system, Segoe UI, Roboto, Ubuntu, Cantarell, Noto Sans, Helvetica, Arial, "Apple Color Emoji", "Segoe UI Emoji";
      display: flex;
      flex-direction: column;
      align-items: center;
      min-height: 100vh;
      padding: 24px 16px;
    }
    .brand {
      font-weight: 700;
      letter-spacing: .08em;
      text-transform: uppercase;
      font-size: 12px;
      color: #66fcf1;
      margin-bottom: 18px;
      text-decoration: none;
    }
    .card {
      width: 100%;
      max-width: 960px;
      background: #1f2833;
      border-radius: 14px;
      overflow: hidden;
      box-shadow: 0 20px 60px rgba(0,0,0,.45);
    }
    video {
      width: 100%;
      height: auto;
      display: block;
      background: #000;
      outline: none;
    }
    .info {
      padding: 18px 20px 20px;
    }
    h1 {
      font-size: 20px;
      line-height: 1.25;
      color: #fff;
      margin-bottom: 8px;
    }
    .meta {
      font-size: 13px;
      color: #8892b0;
      margin-bottom: 12px;
    }
    .meta span + span::before {
      content: "·";
      margin: 0 8px;
      opacity: .6;
    }
    .desc {
      font-size: 14px;
      line-height: 1.55;
      color: #c5c6c7;
      margin-bottom: 14px;
    }
    .actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    .btn {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 8px 14px;
      border-radius: 8px;
      font-size: 13px;
      font-weight: 600;
      text-decoration: none;
      border: 1px solid transparent;
      cursor: pointer;
      transition: transform .05s ease, opacity .15s ease;
    }
    .btn:active { transform: scale(.98); }
    .btn-primary {
      background: #66fcf1;
      color: #0b0c10;
      border-color: #66fcf1;
    }
    .btn-secondary {
      background: transparent;
      color: #66fcf1;
      border-color: #45a29e;
    }
    .btn-secondary:hover { background: rgba(102,252,241,.08); }
    .tagbar {
      margin-top: 14px;
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
    }
    .tag {
      font-size: 11px;
      font-weight: 600;
      color: #45a29e;
      background: rgba(69,162,158,.12);
      padding: 4px 8px;
      border-radius: 999px;
    }
    footer {
      margin-top: 24px;
      font-size: 12px;
      color: #8892b0;
      text-align: center;
    }
    footer a { color: #66fcf1; text-decoration: none; }
  </style>
</head>
<body>
  <a class="brand" href="/">bken</a>

  <div class="card">
    <video controls playsinline preload="metadata" poster="${escapeHtml(thumbUrl)}"
           width="${width}" height="${height}">
      <source src="${escapeHtml(videoUrl)}" type="video/mp4">
      Your browser does not support the video tag.
    </video>

    <div class="info">
      <h1>${escapeHtml(title)}</h1>
      <div class="meta">
        <span>${escapeHtml(row.game || 'Unknown')}</span>
        <span>${escapeHtml(row.recorded_at?.slice(0, 10) || 'undated')}</span>
        <span>${Math.floor((row.end_s - row.start_s) / 60)}:${String(Math.floor((row.end_s - row.start_s) % 60)).padStart(2, '0')}</span>
      </div>
      ${row.llm_desc ? `<p class="desc">${escapeHtml(row.llm_desc)}</p>` : ''}
      ${row.llm_contents ? `<p class="desc">${escapeHtml(row.llm_contents)}</p>` : ''}

      <div class="actions">
        <a class="btn btn-primary" href="${escapeHtml(downloadUrl)}" download>Download</a>
        <button class="btn btn-secondary" onclick="copyLink()">Copy link</button>
      </div>

      ${row.llm_tags ? (() => {
        let tags: string[] = []
        try { tags = JSON.parse(row.llm_tags) } catch { /* ignore */ }
        if (tags.length) {
          return '<div class="tagbar">' + tags.map((t: string) => `<span class="tag">#${escapeHtml(t)}</span>`).join('') + '</div>'
        }
        return ''
      })() : ''}
    </div>
  </div>

  <footer>
    Shared with <a href="/">bken</a> — clip archive
  </footer>

  <script>
    function copyLink() {
      navigator.clipboard.writeText(location.href).then(() => {
        const btn = document.querySelector('button');
        const old = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => btn.textContent = old, 1200);
      });
    }
  </script>
</body>
</html>`

  setHeader(event, 'content-type', 'text/html; charset=utf-8')
  return html
})
