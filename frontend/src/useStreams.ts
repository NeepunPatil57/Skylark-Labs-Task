import { useCallback, useEffect, useRef, useState } from 'react'
import { addStream, ApiError, removeStream } from './api'

/** A camera on this browser's wall. */
export interface WallStream {
  url: string // as entered, may contain credentials; never sent anywhere but our backend
  name: string
  id?: string // set once the backend has registered it
  displayUrl: string // credentials masked
  error?: string // registration failed
}

const STORAGE_KEY = 'camera-wall:streams'

interface Saved {
  url: string
  name: string
}

function load(): Saved[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    const list = raw ? (JSON.parse(raw) as Saved[]) : []
    return Array.isArray(list) ? list.filter((s) => typeof s?.url === 'string') : []
  } catch {
    return []
  }
}

function save(list: WallStream[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(list.map(({ url, name }) => ({ url, name }))))
  } catch {
    // Storage unavailable (private mode): the wall just won't survive a refresh.
  }
}

/** Hides the password and the query string (often an access token). */
export function maskUrl(url: string): string {
  return url.replace(/^(rtsps?:\/\/[^:/@]+):[^@/]*@/i, '$1:xxxxx@').replace(/\?.*$/, '?xxxxx')
}

/** Mirrors the backend's checks so most mistakes are caught before a request. */
export function validateUrl(url: string): string | undefined {
  const value = url.trim()
  if (!value) return 'Enter a stream URL'
  if (!/^rtsps?:\/\//i.test(value)) return 'URL must start with rtsp:// or rtsps://'
  try {
    // URL can't parse rtsp:// hosts in every browser; reuse http parsing.
    const parsed = new URL(value.replace(/^rtsps?:/i, 'http:'))
    if (!parsed.hostname) return 'URL is missing a host'
  } catch {
    return 'This doesn’t look like a valid URL'
  }
  return undefined
}

export function useStreams() {
  const [streams, setStreams] = useState<WallStream[]>(() =>
    load().map((s) => ({ ...s, displayUrl: maskUrl(s.url) })),
  )
  // Latest list for callbacks, without re-creating them on every change.
  const streamsRef = useRef(streams)
  useEffect(() => {
    streamsRef.current = streams
  }, [streams])

  const update = useCallback((fn: (list: WallStream[]) => WallStream[]) => {
    setStreams((prev) => {
      const next = fn(prev)
      save(next)
      return next
    })
  }, [])

  const register = useCallback(
    async (url: string) => {
      try {
        const info = await addStream(url)
        update((list) =>
          list.map((s) => (s.url === url ? { ...s, id: info.id, displayUrl: info.url, error: undefined } : s)),
        )
      } catch (err) {
        const message = err instanceof ApiError ? err.message : 'Could not add stream'
        update((list) => list.map((s) => (s.url === url ? { ...s, error: message } : s)))
      }
    },
    [update],
  )

  // The backend keeps streams in memory, so re-register saved ones on load.
  useEffect(() => {
    for (const s of streamsRef.current) void register(s.url)
  }, [register])

  /** Adds a stream; throws ApiError with a user-facing message on failure. */
  const add = useCallback(
    async (url: string, name: string) => {
      url = url.trim()
      const info = await addStream(url, name.trim() || undefined)
      if (streamsRef.current.some((s) => s.id === info.id || s.url === url)) {
        throw new ApiError('This stream is already on your wall')
      }
      // Prefer the name typed here: the server keeps the first name it saw for a URL.
      update((list) => [...list, { url, name: name.trim() || info.name, id: info.id, displayUrl: info.url }])
    },
    [update],
  )

  const remove = useCallback(
    (url: string) => {
      const target = streamsRef.current.find((s) => s.url === url)
      update((list) => list.filter((s) => s.url !== url))
      if (target?.id) {
        removeStream(target.id).catch(() => {
          // Already gone on the server; nothing to undo locally.
        })
      }
    },
    [update],
  )

  return { streams, add, remove, retry: register }
}
