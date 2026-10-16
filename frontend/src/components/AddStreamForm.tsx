import { useId, useState, type SubmitEvent } from 'react'
import { ApiError } from '../api'
import { caption, control, primaryButton } from './ui'
import { validateUrl } from '../useStreams'

interface Props {
  onAdd: (url: string, name: string) => Promise<void>
}

export function AddStreamForm({ onAdd }: Props) {
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const errorId = useId()

  const submit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    const invalid = validateUrl(url)
    if (invalid) {
      setError(invalid)
      return
    }
    setBusy(true)
    setError(undefined)
    try {
      await onAdd(url, name)
      setUrl('')
      setName('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not add stream')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="max-w-220 flex-[1_1_560px]" onSubmit={submit} noValidate>
      <div className="grid items-end gap-2.5 md:grid-cols-[minmax(0,1fr)_minmax(0,200px)_auto]">
        <label className="grid gap-1">
          <span className={caption}>Stream URL</span>
          <input
            type="text"
            className={control}
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder="rtsp://user:pass@192.168.1.20:554/stream1"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value)
              setError(undefined)
            }}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
          />
        </label>
        <label className="grid gap-1">
          <span className={caption}>Name (optional)</span>
          <input
            type="text"
            className={control}
            autoComplete="off"
            maxLength={64}
            placeholder="Front door"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <button type="submit" className={primaryButton} disabled={busy}>
          {busy ? 'Adding…' : 'Add stream'}
        </button>
      </div>
      {error && (
        <p id={errorId} className="mt-1.5 text-[13px] text-error" role="alert">
          {error}
        </p>
      )}
    </form>
  )
}
