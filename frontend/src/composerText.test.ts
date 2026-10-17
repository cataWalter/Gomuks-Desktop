import { describe, expect, it } from 'vitest'
import { emojiQueryAt, insertEmoji, replaceEmoticons, suggestEmoji, wrapSelection } from './composerText'

describe('replaceEmoticons', () => {
  it('replaces whole-word emoticons', () => {
    expect(replaceEmoticons('hi :) see you ;-)')).toBe('hi 🙂 see you 😉')
  })

  it('leaves words that merely contain an emoticon alone', () => {
    expect(replaceEmoticons('https://example.com/a:D')).toBe('https://example.com/a:D')
    expect(replaceEmoticons('constructor')).toBe('constructor')
  })

  it('skips inline code spans', () => {
    expect(replaceEmoticons('use `:)` but not :)')).toBe('use `:)` but not 🙂')
  })
})

describe('emojiQueryAt', () => {
  it('detects a shortcode at the caret after whitespace or at the start', () => {
    expect(emojiQueryAt('hello :sm', 9)).toEqual({ start: 6, end: 9, query: 'sm' })
    expect(emojiQueryAt(':fi', 3)).toEqual({ start: 0, end: 3, query: 'fi' })
  })

  it('ignores colons inside words and single characters', () => {
    expect(emojiQueryAt('meet at 10:30', 13)).toBeNull()
    expect(emojiQueryAt('hi :s', 5)).toBeNull()
  })
})

describe('suggestEmoji', () => {
  it('returns prefix matches', () => {
    expect(suggestEmoji('thu')).toEqual([['thumbsup', '👍'], ['thumbsdown', '👎']])
    expect(suggestEmoji('zzz')).toEqual([])
  })
})

describe('insertEmoji', () => {
  it('replaces the shortcode and places the caret after the emoji', () => {
    const result = insertEmoji('hello :fi', 6, 9, '🔥')
    expect(result.text).toBe('hello 🔥')
    expect(result.caret).toBe('hello 🔥'.length)
  })
})

describe('wrapSelection', () => {
  it('wraps the selected text and keeps it selected', () => {
    expect(wrapSelection('say hi now', 4, 6, '**')).toEqual({ text: 'say **hi** now', selectionStart: 6, selectionEnd: 8 })
  })

  it('inserts paired markers at a collapsed caret', () => {
    expect(wrapSelection('ab', 1, 1, '`')).toEqual({ text: 'a``b', selectionStart: 2, selectionEnd: 2 })
  })
})
