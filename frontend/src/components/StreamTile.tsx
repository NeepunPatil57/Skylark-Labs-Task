import { useEffect, useRef, useState } from 'react'
import { wsUrl } from '../api'
import { MsePlayer, type PlayerState } from '../player/msePlayer'
import type { WallStream } from '../useStreams'
import { ghostButton } from './ui'

interface Props {
  stream: WallStream
  onRemove: () => void
  onRetryRegister: () => void
}

// Full class names so Tailwind can find them.
const tones: Record<PlayerState['phase'], { text: string; bg: string }> = {
  connecting: { text: 'text-ink', bg: 'bg-warn' },
  live: { text: 'text-ink', bg: 'bg-live' },
  reconnecting: { text: 'text-ink', bg: 'bg-warn' },
  error: { text: 'text-error', bg: 'bg-error' },
  paused: { text: 'text-muted', bg: 'bg-muted' },
}

const phaseLabel: Record<PlayerState['phase'], string> = {
  connecting: 'Connecting',
  live: 'Live',
  reconnecting: 'Reconnecting',
  error: 'Error',
  paused: 'Paused',
}

export function StreamTile({ stream, onRemove, onRetryRegister }: Props) {
  const tileRef = useRef<HTMLElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const playerRef = useRef<MsePlayer>(null)
  const [state, setState] = useState<PlayerState>({ phase: 'connecting' })
  const [userPaused, setUserPaused] = useState(false)
  const [visible, setVisible] = useState(true)

  // One player per registered stream id.
  useEffect(() => {
    if (!stream.id || !videoRef.current) return
    const player = new MsePlayer(videoRef.current, wsUrl(stream.id), setState)
    playerRef.current = player
    return () => {
      player.destroy()
      playerRef.current = null
    }
  }, [stream.id])

  // Offscreen tiles stop streaming to save bandwidth and CPU.
  useEffect(() => {
    const el = tileRef.current
    if (!el || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(([entry]) => setVisible(entry.isIntersecting), { rootMargin: '100px' })
    io.observe(el)
    return () => io.disconnect()
  }, [])

  useEffect(() => {
    const player = playerRef.current
    if (!player) return
    if (userPaused || !visible) player.stop()
    else player.start()
  }, [userPaused, visible, stream.id])

  const toggleFullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen()
    else void tileRef.current?.requestFullscreen?.()
  }

  const retry = () => {
    if (!stream.id) {
      onRetryRegister()
      return
    }
    // Reopen the connection: the server retries the camera immediately.
    playerRef.current?.stop()
    playerRef.current?.start()
    setUserPaused(false)
  }

  const phase = stream.error ? 'error' : state.phase
  const message = stream.error ?? state.message
  const tone = tones[phase]
  const showOverlay = phase !== 'live' && !(phase === 'paused' && userPaused)

  return (
    <article
      ref={tileRef}
      className="group @container relative aspect-video overflow-hidden rounded-[3px] bg-black [&:fullscreen]:aspect-auto [&:fullscreen]:rounded-none"
    >
      <video
        ref={videoRef}
        className="absolute inset-0 size-full object-contain"
        muted
        playsInline
        aria-label={`Live video: ${stream.name}`}
      />

      {showOverlay && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-1.5 bg-page/80 px-5 py-12 text-center text-muted">
          {phase === 'connecting' && <p>Connecting to camera…</p>}
          {phase === 'paused' && <p>Paused while off screen</p>}
          {(phase === 'reconnecting' || phase === 'error') && (
            <>
              <p className={`max-w-[40ch] font-semibold ${tone.text}`}>{message ?? 'Stream interrupted'}</p>
              <p className="mb-1.5 text-[13px]">
                {phase === 'reconnecting' ? 'Retrying automatically' : 'Check the URL or camera, then retry'}
              </p>
              <button type="button" className={ghostButton} onClick={retry}>
                Retry now
              </button>
            </>
          )}
        </div>
      )}

      {/* Labels and controls sit at the bottom: cameras burn their own name and
          clock into the top corners. */}
      <footer className="pointer-events-none absolute inset-x-0 bottom-0 z-10 grid grid-cols-[minmax(0,1fr)_auto] items-end gap-3 @min-[30rem]:grid-cols-[minmax(0,1fr)_auto_1fr] bg-linear-to-t from-black/70 to-transparent px-3 pt-8 pb-2.5 [text-shadow:0_1px_2px_rgb(0_0_0/0.8)]">
        <div className="min-w-0">
          <h2 className="truncate text-[15px] leading-tight font-semibold">{stream.name}</h2>
          <p className="pointer-events-auto mt-0.5 truncate font-mono text-[11px] text-white/65" title={stream.displayUrl}>
            {stream.displayUrl}
          </p>
        </div>
        {/* Controls stay clear of the clock cameras burn into the top corners:
            bottom-centre on wide tiles, just above the status pills on narrow
            ones. Shown on hover, focus, touch, or when not live. */}
        <div
          className={`pointer-events-auto absolute right-3 bottom-full flex gap-1.5 [text-shadow:none] transition-opacity @min-[30rem]:static duration-150 group-focus-within:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100 ${phase === 'live' ? 'opacity-0' : ''}`}
        >
          <button
            type="button"
            className={iconButton}
            onClick={() => setUserPaused((p) => !p)}
            aria-label={userPaused ? `Play ${stream.name}` : `Pause ${stream.name}`}
            disabled={!stream.id}
          >
            {userPaused ? <PlayIcon /> : <PauseIcon />}
          </button>
          <button type="button" className={iconButton} onClick={toggleFullscreen} aria-label={`Fullscreen ${stream.name}`}>
            <FullscreenIcon />
          </button>
          <button
            type="button"
            className={`${iconButton} enabled:hover:border-error enabled:hover:bg-error`}
            onClick={onRemove}
            aria-label={`Remove ${stream.name}`}
          >
            <RemoveIcon />
          </button>
        </div>
        <div className="flex items-center justify-end gap-1.5">
          {state.transcoding && (
            <span
              className={`${pill} px-2.5 text-accent`}
              title="This camera sends video browsers can’t play, so the server converts it to H.264. Uses more server CPU."
            >
              Transcoded
            </span>
          )}
          <span
            className={`${pill} inline-flex shrink-0 items-center gap-1.5 pr-2.5 pl-2 ${tone.text}`}
            role="status"
          >
            <span
              className={`size-2 rounded-full ${tone.bg} ${phase === 'live' ? 'animate-tally motion-reduce:animate-none' : ''}`}
              aria-hidden="true"
            />
            {phaseLabel[phase]}
          </span>
        </div>
      </footer>
    </article>
  )
}

// Solid enough to stay readable over bright video.
const pill =
  'rounded-full border border-white/10 bg-black/80 py-0.5 font-mono text-[11px] font-medium uppercase tracking-[0.14em] backdrop-blur-sm [text-shadow:none]'

const iconButton =
  'grid size-8.5 cursor-pointer place-items-center rounded-md border border-white/15 bg-black/80 shadow-md shadow-black/40 backdrop-blur-sm enabled:hover:bg-line disabled:cursor-default disabled:opacity-40'

const iconProps = { width: 18, height: 18, viewBox: '0 0 24 24', 'aria-hidden': true } as const

function PlayIcon() {
  return (
    <svg {...iconProps}>
      <path d="M8 5v14l11-7z" fill="currentColor" />
    </svg>
  )
}

function PauseIcon() {
  return (
    <svg {...iconProps}>
      <path d="M7 5h4v14H7zM13 5h4v14h-4z" fill="currentColor" />
    </svg>
  )
}

function FullscreenIcon() {
  return (
    <svg {...iconProps} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
    </svg>
  )
}

function RemoveIcon() {
  return (
    <svg {...iconProps} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  )
}
