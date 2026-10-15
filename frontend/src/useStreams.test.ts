import { describe, expect, it } from 'vitest'
import { maskUrl, validateUrl } from './useStreams'

describe('validateUrl', () => {
  it.each([
    'rtsp://192.168.1.20:554/stream1',
    'rtsps://user:pass@cam.example.com/live',
    '  RTSP://Cam.local/live  ',
    'rtsp://rtsp.example.com/live/test?token=abc',
  ])('accepts %s', (url) => {
    expect(validateUrl(url)).toBeUndefined()
  })

  it.each([
    ['', 'Enter a stream URL'],
    ['   ', 'Enter a stream URL'],
    ['hello', 'URL must start with rtsp:// or rtsps://'],
    ['http://cam/live', 'URL must start with rtsp:// or rtsps://'],
    ['rtsp://', 'This doesn’t look like a valid URL'],
  ])('rejects %j', (url, message) => {
    expect(validateUrl(url)).toBe(message)
  })
})

describe('maskUrl', () => {
  it.each([
    ['rtsp://cam/live', 'rtsp://cam/live'],
    ['rtsp://admin:secret@cam:554/live', 'rtsp://admin:xxxxx@cam:554/live'],
    ['rtsp://cam/live?token=abc&x=1', 'rtsp://cam/live?xxxxx'],
    ['rtsp://admin:secret@cam/live?token=abc', 'rtsp://admin:xxxxx@cam/live?xxxxx'],
    ['rtsp://useronly@cam/live', 'rtsp://useronly@cam/live'],
  ])('%s → %s', (url, masked) => {
    expect(maskUrl(url)).toBe(masked)
  })
})
