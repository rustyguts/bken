// GET /api/clip/:id/vision
//
// Full per-frame payload for a single clip: caption + OCR + detections
// per sampled frame, ordered by ts_s.

import { db } from '~~/server/utils/db'

export interface VisionFrameDto {
  ts_s: number
  sampled: string
  caption: string | null
  ocr: { text: string; conf: number | null; bbox_x1: number; bbox_y1: number; bbox_x2: number; bbox_y2: number; area_frac: number | null }[]
  detections: { label: string; conf: number; bbox_x1: number; bbox_y1: number; bbox_x2: number; bbox_y2: number; area_frac: number | null }[]
}

export default defineEventHandler((event) => {
  const idParam = event.context.params?.id
  const clipId = idParam ? Number(idParam) : NaN
  if (!Number.isFinite(clipId)) {
    throw createError({ statusCode: 400, statusMessage: 'Invalid clip id' })
  }

  const clip = db().prepare('SELECT video_id, start_s, end_s FROM candidate_clip WHERE id = ?').get(clipId) as
    | { video_id: number; start_s: number; end_s: number }
    | undefined

  if (!clip) {
    throw createError({ statusCode: 404, statusMessage: 'Clip not found' })
  }

  const frames = db()
    .prepare(
      `SELECT id, ts_s, sampled, caption
       FROM vision_frame
       WHERE video_id = ? AND ts_s BETWEEN ? AND ?
       ORDER BY ts_s`
    )
    .all(clip.video_id, clip.start_s, clip.end_s) as { id: number; ts_s: number; sampled: string; caption: string | null }[]

  const frameIds = frames.map((f) => f.id)
  if (!frameIds.length) {
    return { frames: [] }
  }

  const placeholders = frameIds.map(() => '?').join(',')

  const ocrRows = db()
    .prepare(
      `SELECT frame_id, text, conf, bbox_x1, bbox_y1, bbox_x2, bbox_y2, area_frac
       FROM vision_ocr
       WHERE frame_id IN (${placeholders})
       ORDER BY frame_id, conf DESC`
    )
    .all(...frameIds) as { frame_id: number; text: string; conf: number | null; bbox_x1: number; bbox_y1: number; bbox_x2: number; bbox_y2: number; area_frac: number | null }[]

  const detRows = db()
    .prepare(
      `SELECT frame_id, label, conf, bbox_x1, bbox_y1, bbox_x2, bbox_y2, area_frac
       FROM vision_detection
       WHERE frame_id IN (${placeholders})
       ORDER BY frame_id, conf DESC`
    )
    .all(...frameIds) as { frame_id: number; label: string; conf: number; bbox_x1: number; bbox_y1: number; bbox_x2: number; bbox_y2: number; area_frac: number | null }[]

  const ocrByFrame = new Map<number, VisionFrameDto['ocr']>()
  for (const r of ocrRows) {
    const list = ocrByFrame.get(r.frame_id) || []
    list.push(r)
    ocrByFrame.set(r.frame_id, list)
  }

  const detByFrame = new Map<number, VisionFrameDto['detections']>()
  for (const r of detRows) {
    const list = detByFrame.get(r.frame_id) || []
    list.push(r)
    detByFrame.set(r.frame_id, list)
  }

  const result: VisionFrameDto[] = frames.map((f) => ({
    ts_s: f.ts_s,
    sampled: f.sampled,
    caption: f.caption,
    ocr: ocrByFrame.get(f.id) || [],
    detections: detByFrame.get(f.id) || [],
  }))

  return { frames: result }
})
