// Pure helpers for composer text transforms: emoticon replacement, emoji
// shortcode autocomplete, and Markdown wrapping for the formatting toolbar.

const emoticonReplacements = new Map<string, string>([
  [':)', '🙂'],
  [':-)', '🙂'],
  [':(', '🙁'],
  [':-(', '🙁'],
  [';)', '😉'],
  [';-)', '😉'],
  [':D', '😀'],
  [':-D', '😀'],
  [':P', '😛'],
  [':-P', '😛'],
  [':p', '😛'],
  [':o', '😮'],
  [':O', '😮'],
  ['<3', '❤️'],
  ['xD', '😆'],
  ['XD', '😆'],
])

// Replaces whole-word emoticons outside of inline code spans. Matching is
// token-based so text such as URLs is left alone.
export function replaceEmoticons(text: string): string {
  return text
    .split(/(`[^`]*`)/)
    .map((segment, index) => {
      if (index % 2 === 1) return segment
      return segment
        .split(/(\s+)/)
        .map((token) => emoticonReplacements.get(token) ?? token)
        .join('')
    })
    .join('')
}

const emojiShortcodes: ReadonlyArray<readonly [string, string]> = [
  ['smile', '😄'],
  ['grinning', '😀'],
  ['joy', '😂'],
  ['sweat_smile', '😅'],
  ['laughing', '😆'],
  ['wink', '😉'],
  ['sunglasses', '😎'],
  ['thinking', '🤔'],
  ['pensive', '😔'],
  ['sob', '😭'],
  ['heart', '❤️'],
  ['thumbsup', '👍'],
  ['thumbsdown', '👎'],
  ['clap', '👏'],
  ['raised_hands', '🙌'],
  ['ok_hand', '👌'],
  ['wave', '👋'],
  ['pray', '🙏'],
  ['eyes', '👀'],
  ['fire', '🔥'],
  ['tada', '🎉'],
  ['rocket', '🚀'],
  ['star', '⭐'],
  ['100', '💯'],
  ['white_check_mark', '✅'],
  ['x', '❌'],
  ['warning', '⚠️'],
  ['coffee', '☕'],
]

export type EmojiQuery = { start: number; end: number; query: string }

// Detects an in-progress `:shortcode` typed at the caret. The colon must start
// the text or follow whitespace so times and URLs do not trigger suggestions.
export function emojiQueryAt(value: string, cursor: number): EmojiQuery | null {
  const match = /(^|\s):([a-z0-9_]{2,})$/i.exec(value.slice(0, cursor))
  if (!match) return null
  return { start: cursor - match[0].length + match[1].length, end: cursor, query: match[2].toLowerCase() }
}

export function suggestEmoji(query: string): Array<[string, string]> {
  return emojiShortcodes.filter(([name]) => name.startsWith(query)).slice(0, 6).map(([name, emoji]) => [name, emoji])
}

export function insertEmoji(value: string, start: number, end: number, emoji: string) {
  const suffix = value.slice(end)
  const separator = suffix.startsWith(' ') || suffix.length === 0 ? '' : ' '
  const insertion = emoji + separator
  return { text: value.slice(0, start) + insertion + suffix, caret: start + insertion.length }
}

export function wrapSelection(value: string, start: number, end: number, marker: string) {
  const before = value.slice(0, start)
  const selected = value.slice(start, end)
  const after = value.slice(end)
  return {
    text: before + marker + selected + marker + after,
    selectionStart: start + marker.length,
    selectionEnd: end + marker.length,
  }
}
