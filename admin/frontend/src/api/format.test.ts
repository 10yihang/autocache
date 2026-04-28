import { describe, it, expect } from 'vitest'
import { formatBytes, formatNumber, formatDuration, formatTimestamp, hashColor } from './format.ts'

describe('formatBytes', () => {
  it('returns 0 B for zero', () => expect(formatBytes(0)).toBe('0 B'))
  it('returns — for negative', () => expect(formatBytes(-1)).toBe('—'))
  it('formats bytes under 1 KB', () => expect(formatBytes(512)).toBe('512 B'))
  it('formats KB', () => expect(formatBytes(1024)).toBe('1 KB'))
  it('formats MB', () => expect(formatBytes(2 * 1024 * 1024)).toBe('2 MB'))
  it('formats GB with decimals', () => {
    const result = formatBytes(1.5 * 1024 * 1024 * 1024)
    expect(result).toBe('1.50 GB')
  })
})

describe('formatNumber', () => {
  it('formats small numbers as-is', () => expect(formatNumber(42)).toBe('42'))
  it('formats thousands', () => expect(formatNumber(1500)).toBe('1.5K'))
  it('formats millions', () => expect(formatNumber(2_500_000)).toBe('2.5M'))
  it('formats billions', () => expect(formatNumber(3_000_000_000)).toBe('3.0B'))
})

describe('formatDuration', () => {
  it('returns ∞ for negative', () => expect(formatDuration(-1)).toBe('∞'))
  it('returns 0s for zero', () => expect(formatDuration(0)).toBe('0s'))
  it('formats seconds', () => expect(formatDuration(45)).toBe('45s'))
  it('formats minutes and seconds', () => expect(formatDuration(125)).toBe('2m 5s'))
  it('formats days hours minutes seconds', () => expect(formatDuration(90065)).toBe('1d 1h 1m 5s'))
})

describe('formatTimestamp', () => {
  it('formats unix epoch number', () => {
    const result = formatTimestamp(0)
    expect(result).toBe('1970-01-01 00:00:00Z')
  })
  it('formats ISO string', () => {
    const result = formatTimestamp('2024-01-15T12:30:00.000Z')
    expect(result).toBe('2024-01-15 12:30:00Z')
  })
  it('returns — for invalid input', () => {
    expect(formatTimestamp('not-a-date')).toBe('—')
  })
})

describe('hashColor', () => {
  it('returns an HSL string', () => {
    const color = hashColor('node-1')
    expect(color).toMatch(/^hsl\(\d+,\d+%,\d+%\)/)
  })
  it('produces consistent output for the same input', () => {
    expect(hashColor('abc')).toBe(hashColor('abc'))
  })
  it('produces different colors for different inputs', () => {
    expect(hashColor('node-1')).not.toBe(hashColor('node-2'))
  })
  it('handles empty string without throwing', () => {
    expect(() => hashColor('')).not.toThrow()
  })
})
