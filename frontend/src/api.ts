// Backend REST client. In development Vite proxies /api and /ws to the Go
// server (see vite.config.ts); in production VITE_API_URL points at it.

export interface StreamInfo {
  id: string
  name: string
  url: string // credentials masked by the server
  state: 'idle' | 'connecting' | 'live' | 'reconnecting' | 'error'
  error?: string
  codec?: string
  viewers: number
}

const apiBase = (import.meta.env.VITE_API_URL ?? '').replace(/\/$/, '')

export function wsUrl(id: string): string {
  const explicit = import.meta.env.VITE_WS_URL
  const base = explicit
    ? explicit.replace(/\/$/, '')
    : apiBase
      ? apiBase.replace(/^http/, 'ws')
      : `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}`
  return `${base}/ws/${encodeURIComponent(id)}`
}

export class ApiError extends Error {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(apiBase + path, init)
  } catch {
    throw new ApiError('Cannot reach the server. Check that the backend is running.')
  }
  if (res.status === 204) return undefined as T
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(body.error ?? `Server error (${res.status})`)
  return body as T
}

export function addStream(url: string, name?: string): Promise<StreamInfo> {
  return request('/api/streams', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, name }),
  })
}

export function removeStream(id: string): Promise<void> {
  return request(`/api/streams/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
