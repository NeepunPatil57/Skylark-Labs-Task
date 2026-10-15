import { useState } from 'react'
import { AddStreamForm } from './components/AddStreamForm'
import { StreamTile } from './components/StreamTile'
import { chip } from './components/ui'
import { mseSupported } from './player/msePlayer'
import { useStreams } from './useStreams'

// Served by the MediaMTX instance next to the backend, so "localhost" is
// resolved on the server, not in the browser.
const TEST_CAMERAS = [
  { name: 'Test pattern', url: 'rtsp://localhost:8554/cam1' },
  { name: 'Life', url: 'rtsp://localhost:8554/cam2' },
  { name: 'Colour bars', url: 'rtsp://localhost:8554/cam3' },
  { name: 'H.265 camera', url: 'rtsp://localhost:8554/cam4' }, // exercises transcoding
]

const LAYOUTS = ['auto', '1', '2', '3', '4'] as const
type Layout = (typeof LAYOUTS)[number]
const LAYOUT_KEY = 'camera-wall:layout'

// Phones always get one column; the picker applies from md up.
const gridColumns: Record<Layout, string> = {
  auto: 'grid-cols-[repeat(auto-fill,minmax(min(100%,420px),1fr))]',
  '1': 'grid-cols-1',
  '2': 'grid-cols-1 md:grid-cols-2',
  '3': 'grid-cols-1 md:grid-cols-3',
  '4': 'grid-cols-1 md:grid-cols-4',
}

function loadLayout(): Layout {
  try {
    const v = localStorage.getItem(LAYOUT_KEY)
    return LAYOUTS.includes(v as Layout) ? (v as Layout) : 'auto'
  } catch {
    return 'auto'
  }
}

export default function App() {
  const { streams, add, remove, retry } = useStreams()
  const [layout, setLayout] = useState<Layout>(loadLayout)
  const [testError, setTestError] = useState<string>()

  const chooseLayout = (l: Layout) => {
    setLayout(l)
    try {
      localStorage.setItem(LAYOUT_KEY, l)
    } catch {
      // not persisted
    }
  }

  const addTestCamera = async (url: string, name: string) => {
    setTestError(undefined)
    try {
      await add(url, name)
    } catch (err) {
      setTestError(err instanceof Error ? err.message : 'Could not add camera')
    }
  }

  const onWall = new Set(streams.map((s) => s.url))

  return (
    <div>
      <header className="flex flex-wrap items-end justify-between gap-x-10 gap-y-4 border-b border-line bg-panel px-4 py-4.5 md:px-6">
        <div>
          <h1 className="text-[26px] leading-tight font-bold tracking-[0.01em] font-stretch-[72%]">Camera wall</h1>
          <p className="text-[13px] text-muted">
            {streams.length === 0 ? 'No cameras yet' : `${streams.length} camera${streams.length === 1 ? '' : 's'}`}
          </p>
        </div>
        <AddStreamForm onAdd={add} />
      </header>

      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2.5 px-4 py-2.5 text-[13px] md:px-6">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 text-muted">Test cameras</span>
          {TEST_CAMERAS.map((cam) => (
            <button
              key={cam.url}
              type="button"
              className={`${chip} disabled:cursor-default disabled:border-dashed disabled:text-muted`}
              disabled={onWall.has(cam.url)}
              onClick={() => void addTestCamera(cam.url, cam.name)}
              title={cam.url}
            >
              {onWall.has(cam.url) ? `${cam.name} added` : `Add ${cam.name}`}
            </button>
          ))}
          {testError && (
            <span className="text-error" role="alert">
              {testError}
            </span>
          )}
        </div>
        <div className="hidden flex-wrap items-center gap-1.5 md:flex" role="radiogroup" aria-label="Columns">
          <span className="mr-1 text-muted">Columns</span>
          {LAYOUTS.map((l) => (
            <button
              key={l}
              type="button"
              role="radio"
              aria-checked={layout === l}
              className={`${chip} min-w-8.5 tabular-nums aria-checked:border-accent aria-checked:text-accent`}
              onClick={() => chooseLayout(l)}
            >
              {l === 'auto' ? 'Auto' : l}
            </button>
          ))}
        </div>
      </div>

      {!mseSupported() && (
        <p className="mx-4 mb-2.5 rounded-md border border-warn px-3.5 py-2.5 text-warn md:mx-6">
          This browser can’t play live video. Use a recent Chrome, Edge, Firefox or Safari (iOS 17.1 or later).
        </p>
      )}

      <main className={`grid min-h-[calc(100vh-160px)] content-start gap-1 bg-field p-1 ${gridColumns[layout]}`}>
        {streams.length === 0 ? (
          <div className="col-span-full mx-auto my-[12vh] max-w-[46ch] px-4 text-center">
            <h2 className="mb-2 text-[22px] font-semibold font-stretch-[80%]">Add a camera to start watching</h2>
            <p className="text-muted">
              Paste an <code>rtsp://</code> address above, or add one of the test cameras. Each stream plays live in
              its own tile.
            </p>
          </div>
        ) : (
          streams.map((s) => (
            <StreamTile key={s.url} stream={s} onRemove={() => remove(s.url)} onRetryRegister={() => void retry(s.url)} />
          ))
        )}
      </main>
    </div>
  )
}
