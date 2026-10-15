// Drives MsePlayer with fake WebSocket / MediaSource / video objects, so the
// append queue, codec handling and live-edge logic run without a browser.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MsePlayer as MsePlayerType, PlayerState } from './msePlayer'

class FakeSocket {
  static all: FakeSocket[] = []
  binaryType = ''
  closed = false
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  readonly url: string
  constructor(url: string) {
    this.url = url
    FakeSocket.all.push(this)
  }
  close() {
    this.closed = true
  }
  text(msg: object) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }
  binary(label: string) {
    this.onmessage?.({ data: new TextEncoder().encode(label).buffer })
  }
  drop() {
    this.onclose?.()
  }
}

class FakeSourceBuffer {
  mode = ''
  updating = false
  appended: string[] = []
  removed: [number, number][] = []
  private listeners: Record<string, (() => void)[]> = {}
  addEventListener(type: string, fn: () => void) {
    ;(this.listeners[type] ??= []).push(fn)
  }
  appendBuffer(data: ArrayBuffer) {
    if (this.updating) throw new Error('InvalidStateError: append while updating')
    this.updating = true
    this.appended.push(new TextDecoder().decode(data))
  }
  remove(start: number, end: number) {
    this.updating = true
    this.removed.push([start, end])
  }
  /** Completes the pending append/remove, as the browser would. */
  finish() {
    this.updating = false
    this.listeners.updateend?.forEach((fn) => fn())
  }
}

class FakeMediaSource {
  static all: FakeMediaSource[] = []
  static isTypeSupported = (mime: string) => mime.includes('avc1')
  readyState = 'closed'
  buffer?: FakeSourceBuffer
  mime?: string
  private onOpen?: () => void
  constructor() {
    FakeMediaSource.all.push(this)
  }
  addEventListener(_type: string, fn: () => void) {
    this.onOpen = fn
  }
  open() {
    this.readyState = 'open'
    this.onOpen?.()
  }
  addSourceBuffer(mime: string) {
    this.mime = mime
    this.buffer = new FakeSourceBuffer()
    return this.buffer
  }
  endOfStream() {
    this.readyState = 'ended'
  }
}

function fakeVideo() {
  const ranges: [number, number][] = []
  return {
    ranges,
    muted: false,
    playsInline: false,
    disableRemotePlayback: false,
    currentTime: 0,
    paused: true,
    src: '',
    onplaying: null as null | (() => void),
    buffered: {
      get length() {
        return ranges.length
      },
      start: (i: number) => ranges[i][0],
      end: (i: number) => ranges[i][1],
    },
    play() {
      this.paused = false
      return Promise.resolve()
    },
    pause() {
      this.paused = true
    },
    removeAttribute() {},
    load() {},
  }
}

let MsePlayer: typeof MsePlayerType

beforeAll(async () => {
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.stubGlobal('MediaSource', FakeMediaSource)
  vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:fake')
  vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
  // The module picks its MediaSource implementation at load time.
  ;({ MsePlayer } = await import('./msePlayer'))
})

let video: ReturnType<typeof fakeVideo>
let states: PlayerState[]
let player: MsePlayerType

beforeEach(() => {
  vi.useFakeTimers()
  FakeSocket.all = []
  FakeMediaSource.all = []
  video = fakeVideo()
  states = []
  player = new MsePlayer(video as unknown as HTMLVideoElement, 'ws://test/ws/abc', (s) => states.push(s))
})

afterEach(() => {
  player.destroy()
  vi.useRealTimers()
})

const lastState = () => states[states.length - 1]

/** Starts the player and delivers status, codec and init segment. */
function startLive(codec = 'avc1.4D401F', transcoding = false) {
  player.start()
  const ws = FakeSocket.all.at(-1)!
  ws.text({ type: 'status', state: 'connecting' })
  ws.text({ type: 'init', codec, transcoding })
  ws.binary('init')
  const ms = FakeMediaSource.all.at(-1)
  ms?.open()
  return { ws, ms, sb: ms?.buffer }
}

describe('MsePlayer', () => {
  it('opens the socket and reports connecting', () => {
    player.start()
    expect(FakeSocket.all).toHaveLength(1)
    expect(FakeSocket.all[0].url).toBe('ws://test/ws/abc')
    expect(FakeSocket.all[0].binaryType).toBe('arraybuffer')
    expect(lastState().phase).toBe('connecting')
    expect(video.muted).toBe(true)
  })

  it('appends the init segment, then media in order, one append at a time', () => {
    const { ws, ms, sb } = startLive()
    expect(ms!.mime).toBe('video/mp4; codecs="avc1.4D401F"')
    expect(sb!.appended).toEqual(['init'])

    ws.binary('seg1')
    ws.binary('seg2') // arrives while init is still being appended
    expect(sb!.appended).toEqual(['init'])

    sb!.finish()
    expect(sb!.appended).toEqual(['init', 'seg1'])
    sb!.finish()
    expect(sb!.appended).toEqual(['init', 'seg1', 'seg2'])
  })

  it('ignores media that arrives before an init segment', () => {
    player.start()
    FakeSocket.all[0].binary('orphan')
    expect(FakeMediaSource.all).toHaveLength(0)
  })

  it('reports live only once video actually plays', () => {
    const { ws } = startLive()
    ws.text({ type: 'status', state: 'live' })
    expect(lastState().phase).toBe('connecting')
    video.onplaying!()
    expect(lastState()).toMatchObject({ phase: 'live', codec: 'avc1.4D401F', transcoding: false })
  })

  it('passes the transcoding flag through', () => {
    startLive('avc1.42C01F', true)
    video.onplaying!()
    expect(lastState()).toMatchObject({ phase: 'live', transcoding: true })
  })

  it('shows server errors and reconnecting messages', () => {
    const { ws } = startLive()
    ws.text({ type: 'status', state: 'reconnecting', error: 'Connection to camera lost' })
    expect(lastState()).toMatchObject({ phase: 'reconnecting', message: 'Connection to camera lost' })
    ws.text({ type: 'status', state: 'error', error: 'Bad credentials' })
    expect(lastState()).toMatchObject({ phase: 'error', message: 'Bad credentials' })
  })

  it('reports codecs the browser cannot play', () => {
    startLive('hvc1')
    expect(lastState()).toMatchObject({ phase: 'error', message: 'This browser cannot play hvc1 video' })
    expect(FakeMediaSource.all).toHaveLength(0)
  })

  it('starts a fresh MediaSource when the source restarts', () => {
    const { ws, sb } = startLive()
    sb!.finish()
    ws.text({ type: 'init', codec: 'avc1.4D401F' })
    ws.binary('init2')
    expect(FakeMediaSource.all).toHaveLength(2)
    FakeMediaSource.all[1].open()
    expect(FakeMediaSource.all[1].buffer!.appended).toEqual(['init2'])
    expect(FakeMediaSource.all[0].readyState).toBe('ended')
  })

  it('jumps to the live edge when playback falls behind', () => {
    const { sb } = startLive()
    video.ranges.push([0, 10])
    video.currentTime = 2 // 8s behind the newest frame
    sb!.finish()
    expect(video.currentTime).toBeCloseTo(9.7)
    expect(video.paused).toBe(false)
  })

  it('skips gaps left by dropped segments', () => {
    const { sb } = startLive()
    video.ranges.push([0, 4], [6, 7])
    video.currentTime = 4.5 // stalled in the gap
    sb!.finish()
    // Lands just behind the newest frame of the later range.
    expect(video.currentTime).toBeCloseTo(6.7)
  })

  it('trims old video to bound memory', () => {
    const { sb } = startLive()
    video.ranges.push([0, 40])
    video.currentTime = 39.8
    sb!.finish()
    expect(sb!.removed).toHaveLength(1)
    const [start, end] = sb!.removed[0]
    expect(start).toBe(0)
    expect(end).toBeCloseTo(29.8) // keeps the last 10s behind the playhead
  })

  it('pause closes the socket and play reconnects', () => {
    const { ws } = startLive()
    player.stop()
    expect(ws.closed).toBe(true)
    expect(lastState().phase).toBe('paused')
    expect(video.paused).toBe(true)

    player.start()
    expect(FakeSocket.all).toHaveLength(2)
    expect(lastState().phase).toBe('connecting')
  })

  it('reconnects with backoff when the connection drops', () => {
    const { ws } = startLive()
    ws.drop()
    expect(lastState()).toMatchObject({ phase: 'reconnecting', message: 'Lost connection to server' })
    vi.advanceTimersByTime(999)
    expect(FakeSocket.all).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(FakeSocket.all).toHaveLength(2)

    FakeSocket.all[1].drop()
    vi.advanceTimersByTime(1999)
    expect(FakeSocket.all).toHaveLength(2) // second wait is longer
    vi.advanceTimersByTime(1)
    expect(FakeSocket.all).toHaveLength(3)
  })

  it('does not reconnect after stop', () => {
    const { ws } = startLive()
    player.stop()
    ws.drop()
    vi.advanceTimersByTime(60_000)
    expect(FakeSocket.all).toHaveLength(1)
  })
})
