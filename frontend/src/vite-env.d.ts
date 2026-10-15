/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Backend origin, e.g. https://api.example.com. Empty in dev (Vite proxy). */
  readonly VITE_API_URL?: string
  /** WebSocket origin; derived from VITE_API_URL when unset. */
  readonly VITE_WS_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
