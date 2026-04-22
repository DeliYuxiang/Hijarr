// Centralized API client for the Hijarr admin backend
import type { SRNEvent } from '@srn/client'

// Re-export for use in views
export type { SRNEvent }

const BASE = '/api/frontend'
const SRN_BASE = '/srn/api'

async function get<T>(url: string): Promise<T> {
  const r = await fetch(url)
  if (!r.ok) throw new Error(`GET ${url} → ${r.status}`)
  return r.json()
}

async function post<T>(url: string, body?: unknown): Promise<T> {
  const r = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body != null ? JSON.stringify(body) : undefined,
  })
  if (!r.ok) {
    const text = await r.text()
    throw new Error(text || `POST ${url} → ${r.status}`)
  }
  return r.json()
}

async function del<T>(url: string, srnKey?: string): Promise<T> {
  const headers: Record<string, string> = {}
  if (srnKey) headers['X-SRN-Api-Key'] = srnKey
  const r = await fetch(url, { method: 'DELETE', headers })
  if (!r.ok) {
    const text = await r.text()
    throw new Error(text || `DELETE ${url} → ${r.status}`)
  }
  return r.json()
}

// ── Admin API ──────────────────────────────────────────────────────────────

export const api = {
  config: () => get<Record<string, string>>(`${BASE}/config`),
  status: () => get<Record<string, string>>(`${BASE}/status`),
  jobs: () => get<{ jobs: { name: string; interval: string }[] }>(`${BASE}/jobs`),
  stats: () => get<Record<string, number>>(`${BASE}/stats`),

  // Media library
  mediaLibrary: () => get<any>(`${BASE}/media-library`),
  mediaSeries: (id: number) => get<any>(`${BASE}/media-library/${id}`),
  searchEpisode: (body: unknown) => post<any>(`${BASE}/search-episode`, body),
  applySubtitle: (body: unknown) => post<any>(`${BASE}/apply-subtitle`, body),
  searchSRN: (params: string) => get<SRNEvent[]>(`${SRN_BASE}/search${params}`),
  tmdbSeasonCount: (tmdbId: number) => get<{ count: number }>(`${BASE}/tmdb/season-count?id=${tmdbId}`),
  tmdbSearch: (q: string) => get<{ results: Array<{ Title: string, TMDBID: number }> }>(`${BASE}/tmdb/search?q=${encodeURIComponent(q)}`),

  // DB Admin
  db: {
    mc: (params: string) => get<any>(`${BASE}/db/metadata-cache${params}`),
    mcDelete: (body: unknown) => post<any>(`${BASE}/db/metadata-cache/delete`, body),
    mcUpsert: (body: unknown) => post<any>(`${BASE}/db/metadata-cache/upsert`, body),
    se: (params: string) => get<any>(`${BASE}/db/srn-events${params}`),
    seDelete: (body: unknown) => post<any>(`${BASE}/db/srn-events/delete`, body),
    sf: (params: string) => get<any>(`${BASE}/db/seen-files${params}`),
    sfDelete: (body: unknown) => post<any>(`${BASE}/db/seen-files/delete`, body),
    ff: (params: string) => get<any>(`${BASE}/db/failed-files${params}`),
    ffDelete: (body: unknown) => post<any>(`${BASE}/db/failed-files/delete`, body),
  },

  // Subtitle preferences (blacklist + pins)
  preferences: {
    list: () => get<{ blacklist: any[]; pins: any[] }>(`${BASE}/preferences`),
    addBlacklist: (body: { event_hash: string; cache_key?: string; reason?: string }) =>
      post<any>(`${BASE}/preferences/blacklist`, body),
    removeBlacklist: (hash: string) => del<any>(`${BASE}/preferences/blacklist/${encodeURIComponent(hash)}`),
    setPin: (body: { cache_key: string; event_id: string }) =>
      post<any>(`${BASE}/preferences/pin`, body),
    removePin: (cacheKey: string) =>
      del<any>(`${BASE}/preferences/pin?key=${encodeURIComponent(cacheKey)}`),
  },

}
