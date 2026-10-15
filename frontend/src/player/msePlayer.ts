// Plays the backend's fMP4-over-WebSocket stream through Media Source
// Extensions. Protocol: see backend/internal/ws.

export type PlayerPhase = 'connecting' | 'live' | 'reconnecting' | 'error' | 'paused'

export interface PlayerState {
  phase: PlayerPhase
  message?: string
  codec?: string
  /** The server re-encodes this camera's video to H.264 (e.g. from H.265). */
  transcoding?: boolean
}

type ServerMessage =
  | { type: 'status'; state: 'idle' | 'connecting' | 'live' | 'reconnecting' | 'error'; error?: string }
  | { type: 'init'; codec: string; transcoding?: boolean }

const MAX_LATENCY = 2.5 // seconds behind the newest frame before jumping ahead
const KEEP_BEHIND = 10 // seconds of played video kept in the buffer
const TRIM_AFTER = 30 // trim once this much played video is buffered
const MAX_QUEUE = 60 // segments waiting to be appended (~1 min)
const RECONNECT_MAX = 10_000

// Safari on iOS 17.1+ only has ManagedMediaSource.
const MediaSourceImpl: typeof MediaSource | undefined =
  (globalThis as { ManagedMediaSource?: typeof MediaSource }).ManagedMediaSource ?? globalThis.MediaSource

export function mseSupported(): boolean {
  return MediaSourceImpl !== undefined
}

export class MsePlayer {
  private ws?: WebSocket
  private mediaSource?: MediaSource
  private objectUrl?: string
  private sourceBuffer?: SourceBuffer
  private queue: ArrayBuffer[] = []
  private pendingCodec?: string
  private codec?: string
  private transcoding = false
  private retryTimer?: ReturnType<typeof setTimeout>
  private retryDelay = 1000
  private running = false
  private state: PlayerState = { phase: 'paused' }
  private readonly video: HTMLVideoElement
  private readonly url: string
  private readonly onState: (s: PlayerState) => void

  constructor(video: HTMLVideoElement, url: string, onState: (s: PlayerState) => void) {
    this.video = video
    this.url = url
    this.onState = onState
    video.muted = true
    video.playsInline = true
    // Required for ManagedMediaSource; harmless elsewhere.
    video.disableRemotePlayback = true
  }

  start(): void {
    if (this.running) return
    if (!MediaSourceImpl) {
      this.setState({ phase: 'error', message: 'This browser cannot play live video (no Media Source Extensions)' })
      return
    }
    this.running = true
    this.retryDelay = 1000
    this.setState({ phase: 'connecting' })
    this.connect()
  }

  /** Closes the connection but keeps the last frame on screen. */
  stop(): void {
    if (!this.running) return
    this.running = false
    clearTimeout(this.retryTimer)
    this.closeSocket()
    this.video.pause()
    this.setState({ phase: 'paused', codec: this.codec })
  }

  destroy(): void {
    this.stop()
    this.resetMedia()
    this.video.removeAttribute('src')
    this.video.load()
  }

  private connect(): void {
    const ws = new WebSocket(this.url)
    ws.binaryType = 'arraybuffer'
    this.ws = ws

    ws.onmessage = (e) => {
      if (typeof e.data === 'string') {
        this.onText(e.data)
      } else {
        this.onBinary(e.data as ArrayBuffer)
      }
    }
    ws.onclose = () => {
      if (this.ws !== ws || !this.running) return
      this.ws = undefined
      this.setState({ phase: 'reconnecting', message: 'Lost connection to server', codec: this.codec })
      this.retryTimer = setTimeout(() => this.running && this.connect(), this.retryDelay)
      this.retryDelay = Math.min(this.retryDelay * 2, RECONNECT_MAX)
    }
  }

  private closeSocket(): void {
    const ws = this.ws
    this.ws = undefined
    if (ws) {
      ws.onmessage = null
      ws.onclose = null
      ws.close()
    }
  }

  private onText(text: string): void {
    let msg: ServerMessage
    try {
      msg = JSON.parse(text) as ServerMessage
    } catch {
      return
    }
    if (msg.type === 'init') {
      // The init segment follows as the next binary message.
      this.pendingCodec = msg.codec
      this.transcoding = msg.transcoding ?? false
      return
    }
    this.retryDelay = 1000
    switch (msg.state) {
      case 'live':
        // Shown as live once video actually plays; see onPlaying.
        if (this.state.phase !== 'live') this.setState({ phase: 'connecting', codec: this.codec })
        break
      case 'idle':
      case 'connecting':
        this.setState({ phase: 'connecting', codec: this.codec })
        break
      case 'reconnecting':
        this.setState({ phase: 'reconnecting', message: msg.error, codec: this.codec })
        break
      case 'error':
        this.setState({ phase: 'error', message: msg.error, codec: this.codec })
        break
    }
  }

  private onBinary(data: ArrayBuffer): void {
    if (this.pendingCodec !== undefined) {
      const codec = this.pendingCodec
      this.pendingCodec = undefined
      this.openMedia(codec, data)
      return
    }
    if (!this.sourceBuffer) return // media before init: wait for the next init
    if (this.queue.length >= MAX_QUEUE) this.queue.shift() // tab was hidden; drop oldest
    this.queue.push(data)
    this.pump()
  }

  /** Starts a fresh MediaSource for a new init segment (first connect or source restart). */
  private openMedia(codec: string, init: ArrayBuffer): void {
    const mime = `video/mp4; codecs="${codec}"`
    if (!MediaSourceImpl!.isTypeSupported(mime)) {
      this.setState({ phase: 'error', message: `This browser cannot play ${codec || 'this'} video`, codec })
      return
    }
    this.resetMedia()
    this.codec = codec
    this.queue = [init]

    const ms = new MediaSourceImpl!()
    this.mediaSource = ms
    this.objectUrl = URL.createObjectURL(ms)
    ms.addEventListener('sourceopen', () => {
      if (this.mediaSource !== ms) return
      try {
        const sb = ms.addSourceBuffer(mime)
        sb.mode = 'segments'
        sb.addEventListener('updateend', () => this.onUpdateEnd())
        sb.addEventListener('error', () => this.fail('Video decoder error'))
        this.sourceBuffer = sb
        this.pump()
      } catch {
        this.fail(`This browser cannot play ${codec} video`)
      }
    }, { once: true })
    this.video.src = this.objectUrl
    this.video.onplaying = () => this.setState({ phase: 'live', codec: this.codec })
  }

  private resetMedia(): void {
    this.sourceBuffer = undefined
    this.queue = []
    if (this.mediaSource?.readyState === 'open') {
      try {
        this.mediaSource.endOfStream()
      } catch {
        // already ending
      }
    }
    this.mediaSource = undefined
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl)
      this.objectUrl = undefined
    }
  }

  private pump(): void {
    const sb = this.sourceBuffer
    if (!sb || sb.updating || this.queue.length === 0) return
    const next = this.queue.shift()!
    try {
      sb.appendBuffer(next)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'QuotaExceededError') {
        // Buffer full: free played video and retry this segment.
        this.queue.unshift(next)
        this.trim(true)
      } else {
        this.fail('Video decoder error')
      }
    }
  }

  private onUpdateEnd(): void {
    if (!this.sourceBuffer) return
    this.followLiveEdge()
    if (!this.trim(false)) this.pump()
  }

  /** Keeps playback near the newest frame and skips gaps from dropped segments. */
  private followLiveEdge(): void {
    const v = this.video
    const b = v.buffered
    if (b.length === 0) return
    const end = b.end(b.length - 1)
    let inRange = false
    for (let i = 0; i < b.length; i++) {
      if (v.currentTime >= b.start(i) && v.currentTime < b.end(i)) inRange = true
    }
    if (!inRange || end - v.currentTime > MAX_LATENCY) {
      v.currentTime = Math.max(b.start(b.length - 1), end - 0.3)
    }
    if (v.paused && this.running) {
      v.play().catch(() => {
        // Autoplay blocked: the user can press play.
      })
    }
  }

  /** Removes video well behind the playhead. Returns true if a removal started. */
  private trim(force: boolean): boolean {
    const sb = this.sourceBuffer
    const b = this.video.buffered
    if (!sb || sb.updating || b.length === 0) return false
    const start = b.start(0)
    const cutoff = this.video.currentTime - KEEP_BEHIND
    if (cutoff - start < (force ? 1 : TRIM_AFTER - KEEP_BEHIND)) return false
    sb.remove(start, cutoff)
    return true
  }

  /** Local playback failure: reconnect so the server resends a fresh init segment. */
  private fail(message: string): void {
    this.resetMedia()
    this.setState({ phase: 'error', message, codec: this.codec })
    if (!this.running) return
    this.closeSocket()
    clearTimeout(this.retryTimer)
    this.retryTimer = setTimeout(() => this.running && this.connect(), this.retryDelay)
    this.retryDelay = Math.min(this.retryDelay * 2, RECONNECT_MAX)
  }

  private setState(s: PlayerState): void {
    if (!this.running && s.phase !== 'paused' && s.phase !== 'error') return
    s = { ...s, codec: this.codec, transcoding: this.transcoding }
    this.state = s
    this.onState(s)
  }
}
