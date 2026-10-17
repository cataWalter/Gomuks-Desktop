import { Fragment, lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FormEvent, type KeyboardEvent, type ReactElement } from 'react'
import { createPortal } from 'react-dom'
import { Events } from '@wailsio/runtime'
import {
  ArrowDown,
  ArrowLeft,
  Archive,
  ArchiveRestore,
  ChevronDown,
  Bell,
  BellOff,
  BellRing,
  Check,
  Clock3,
  CircleAlert,
  CircleDot,
  Copy,
  FileText,
  LockKeyhole,
  MessageCircleMore,
  Paperclip,
  Pencil,
  Reply,
  RotateCcw,
  Search,
  Settings,
  Send,
  ShieldCheck,
  Languages,
  Smile,
  Sparkles,
  Star,
  Trash2,
  UsersRound,
  X,
  Download,
  EyeOff,
  HardDrive,
  LogOut,
  Mic,
  Plus,
  Upload,
  Square,
  ZoomIn,
  ZoomOut,
} from 'lucide-react'
import { AppService } from '../bindings/mychat/desktop'
import type { AIMessage, Device, MediaCacheUsage, MediaCleanupResult, Member, Message, MessageSearchRequest, MessageSearchResult, Room, RoomStorage, ScheduledJob, Snapshot, StorageUsage, UpdateStatus } from '../bindings/mychat/desktop/models.js'
import { emojiQueryAt, insertEmoji, replaceEmoticons, suggestEmoji, wrapSelection, type EmojiQuery } from './composerText'
const MarkdownMessage = lazy(() => import('./MarkdownMessage'))

type MatrixUpdate = {
  rooms: Room[]
  timelineChangedRoomId: string[] | null
  typingUsers?: Record<string, string[]> | null
  notifications?: MatrixNotification[] | null
  initialSyncComplete: boolean
  needsVerification: boolean
  connectionState: string
  error: string
}

type MatrixNotification = {
  roomId: string
  room: string
  sender: string
  body: string
  mention: boolean
  category: string
}

type PreferenceValues = NonNullable<Snapshot['preferences']>
type RoomScope = 'all' | 'direct' | 'group' | 'archived'
type RoomRefinements = { unread: boolean; unanswered: boolean; favorite: boolean }
type PendingSend = { roomID: string; text: string; replyTo: Message | null; mentionUserIDs: string[]; mentionBindings: { token: string; userID: string }[]; timer: number }
type MessageMenuItem = { key: string; label: string; ariaLabel: string; icon: ReactElement; danger?: boolean; disabled?: boolean; run: () => void }
type SettingsTab = 'General' | 'Appearance' | 'Notifications' | 'Keyboard' | 'History' | 'Account' | 'Devices' | 'Storage' | 'Security' | 'Help' | 'About' | 'Integrations'
type AIPanel = { title: string; busy: boolean; result: string; error: string }
type ScheduledDraft =
  | { kind: 'message'; roomID: string; text: string; replyToEventID?: string; replyToSender?: string; replyToPreview?: string }
  | { kind: 'reminder'; roomID: string; eventID: string; text: string; archiveAfter?: boolean }

function describeError(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

function isTextEntryTarget(target: EventTarget | null) {
  return target instanceof HTMLElement && (target.isContentEditable || Boolean(target.closest('input, textarea, select, [contenteditable="true"]')))
}

function displayMessageBody(message: Message) {
  if (message.redacted) return 'Message removed'
  if (message.decryptionError || /^\s*Decrypting message from .+ failed, waiting for sender to re-send\b/i.test(message.body)) {
    return "This message couldn't be opened. Ask the sender to resend it."
  }
  return message.body
}

function initials(label: string) {
  const words = label.trim().split(/\s+/).filter(Boolean)
  const parts = words.length > 1 ? words.slice(0, 2) : [words[0]?.slice(0, 2) ?? '?']
  return parts
    .map((part) => part[0])
    .join('')
    .toUpperCase()
}

function avatarHue(label: string) {
  let hash = 0
  for (let i = 0; i < label.length; i++) hash = (hash * 31 + label.charCodeAt(i)) | 0
  return Math.abs(hash) % 360
}

function avatarStyle(label: string) {
  const hue = avatarHue(label || '?')
  return {
    '--avatar-hue': String(hue),
    background: `linear-gradient(135deg, hsl(${hue} 32% 88%) 0%, hsl(${(hue + 24) % 360} 30% 80%) 100%)`,
    color: `hsl(${hue} 32% 26%)`,
  } as React.CSSProperties
}

const avatarSources = new Map<string, string>()
const missingAvatarSources = new Set<string>()
const avatarRequests = new Map<string, Promise<string | null>>()

function loadAvatarSource(uri: string) {
  const cached = avatarSources.get(uri)
  if (cached) return Promise.resolve(cached)
  if (missingAvatarSources.has(uri)) return Promise.resolve(null)

  let request = avatarRequests.get(uri)
  if (!request) {
    request = AppService.DownloadAvatar(uri)
      .then((source) => {
        if (source?.startsWith('data:image/')) {
          avatarSources.set(uri, source)
          return source
        }
        missingAvatarSources.add(uri)
        return null
      })
      .catch(() => null)
      .finally(() => avatarRequests.delete(uri))
    avatarRequests.set(uri, request)
  }
  return request
}

function Avatar({ uri, label, className, style, deferLoad = false, 'aria-hidden': ariaHidden }: {
  uri?: string
  label: string
  className: string
  style?: React.CSSProperties
  deferLoad?: boolean
  'aria-hidden'?: boolean
}) {
  const avatarRef = useRef<HTMLDivElement>(null)
  const [source, setSource] = useState(() => uri ? avatarSources.get(uri) ?? null : null)
  const [nearViewport, setNearViewport] = useState(!deferLoad)

  useEffect(() => {
    if (!deferLoad || nearViewport || !uri) return
    const element = avatarRef.current
    if (!element || typeof IntersectionObserver === 'undefined') {
      setNearViewport(true)
      return
    }

    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        setNearViewport(true)
        observer.disconnect()
      }
    }, { rootMargin: '96px' })
    observer.observe(element)
    return () => observer.disconnect()
  }, [deferLoad, nearViewport, uri])

  useEffect(() => {
    let active = true
    const cached = uri ? avatarSources.get(uri) : undefined
    if (!uri || cached) {
      setSource(cached ?? null)
      return () => { active = false }
    }
    if (!nearViewport) {
      setSource(null)
      return () => { active = false }
    }

    setSource(null)
    void loadAvatarSource(uri).then((loadedSource) => {
      if (active) setSource(loadedSource)
    })
    return () => { active = false }
  }, [uri, nearViewport])

  return (
    <div ref={avatarRef} className={className} style={style} aria-hidden={ariaHidden}>
      {source ? <img className="avatar-image" src={source} alt="" /> : initials(label)}
    </div>
  )
}

function isSameDay(left: number, right: number) {
  if (!left || !right) return true
  const a = new Date(left)
  const b = new Date(right)
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

function formatDayLabel(timestamp: number) {
  if (!timestamp) return ''
  const date = new Date(timestamp)
  const today = new Date()
  const yesterday = new Date()
  yesterday.setDate(today.getDate() - 1)
  const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
  if (sameDay(date, today)) return 'Today'
  if (sameDay(date, yesterday)) return 'Yesterday'
  return new Intl.DateTimeFormat(undefined, { weekday: 'long', month: 'long', day: 'numeric' }).format(date)
}

function localDateTimeInputValue(timestamp: number) {
  const localTime = new Date(timestamp - new Date(timestamp).getTimezoneOffset() * 60_000)
  return localTime.toISOString().slice(0, 16)
}

function formatScheduledTime(timestamp: number) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(timestamp))
}

function preferenceBoolean(preferences: PreferenceValues, key: string, fallback = false) {
  const value = preferences[key]
  return typeof value === 'boolean' ? value : fallback
}

function preferenceString(preferences: PreferenceValues, key: string, fallback: string) {
  const value = preferences[key]
  return typeof value === 'string' ? value : fallback
}

function preferenceNumber(preferences: PreferenceValues, key: string, fallback: number, minimum = 0, maximum = 100) {
  const value = preferences[key]
  const number = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN
  return Number.isFinite(number) ? Math.min(maximum, Math.max(minimum, number)) : fallback
}

function parseTimeSuggestionMinutes(value: string) {
  const parts = value.split(',').map((part) => part.trim())
  if (parts.length < 1 || parts.length > 6 || parts.some((part) => !/^\d+$/.test(part))) return []
  const minutes = parts.map(Number)
  if (minutes.some((value) => value < 1 || value > 10080) || new Set(minutes).size !== minutes.length) return []
  return minutes
}

function formatTimeSuggestion(minutes: number) {
  if (minutes < 60) return `${minutes} min`
  if (minutes % 60 === 0) return `${minutes / 60} hr`
  return `${Math.floor(minutes / 60)} hr ${minutes % 60} min`
}

function mergeTimelineMessages(current: Message[], incoming: Message[]) {
  const merged = [...current]
  const indexes = new Map<string, number>()
  const keysFor = (message: Message) => [message.id, message.transactionId].filter(Boolean)

  merged.forEach((message, index) => {
    for (const key of keysFor(message)) indexes.set(key, index)
  })

  for (const message of incoming) {
    const existingIndex = keysFor(message).map((key) => indexes.get(key)).find((index) => index !== undefined)
    if (existingIndex !== undefined) {
      merged[existingIndex] = { ...merged[existingIndex], ...message }
      for (const key of keysFor(merged[existingIndex])) indexes.set(key, existingIndex)
      continue
    }

    const index = merged.push(message) - 1
    for (const key of keysFor(message)) indexes.set(key, index)
  }

  return merged.sort((left, right) => left.timestamp - right.timestamp || left.id.localeCompare(right.id))
}

const accentPalettes = [
  { name: 'Forest', base: '#2f7653', bright: '#3f9464', soft: 'rgba(47, 118, 83, 0.15)' },
  { name: 'Ocean', base: '#28718f', bright: '#3d91ad', soft: 'rgba(40, 113, 143, 0.15)' },
  { name: 'Rose', base: '#a74761', bright: '#c45f79', soft: 'rgba(167, 71, 97, 0.15)' },
  { name: 'Gold', base: '#98702d', bright: '#b58d45', soft: 'rgba(152, 112, 45, 0.16)' },
]

const chatWallpapers = [
  { name: 'None', swatch: 'var(--paper)' },
  { name: 'Meadow', swatch: 'linear-gradient(135deg, #e8f2e8, #f7f5e9)' },
  { name: 'Tide', swatch: 'linear-gradient(135deg, #e3eff1, #f1f3eb)' },
  { name: 'Rose', swatch: 'linear-gradient(135deg, #f1e5e8, #f5efe4)' },
] as const

type DialogSurfaceProps = {
  children: React.ReactNode
  className: string
  role?: 'dialog' | 'alertdialog'
  labelledBy?: string
  label?: string
  onClose: () => void
  onClick?: React.MouseEventHandler<HTMLElement>
}

function DialogSurface({ children, className, role = 'dialog', labelledBy, label, onClose, onClick }: DialogSurfaceProps) {
  const dialogRef = useRef<HTMLElement>(null)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const focusableSelector = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
    const firstFocusable = dialog.querySelector<HTMLElement>(focusableSelector)
    ;(firstFocusable ?? dialog).focus()

    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      const openDialogs = document.querySelectorAll<HTMLElement>('[aria-modal="true"]')
      if (openDialogs[openDialogs.length - 1] !== dialog) return
      if (event.key === 'Escape') {
        event.preventDefault()
        onCloseRef.current()
        return
      }
      if (event.key !== 'Tab') return

      const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector))
      if (focusable.length === 0) {
        event.preventDefault()
        dialog.focus()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && (document.activeElement === first || !dialog.contains(document.activeElement))) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && (document.activeElement === last || !dialog.contains(document.activeElement))) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      if (previousFocus?.isConnected) previousFocus.focus()
    }
  }, [])

  return (
    <section
      ref={dialogRef}
      className={className}
      role={role}
      aria-modal="true"
      aria-labelledby={labelledBy}
      aria-label={label}
      tabIndex={-1}
      onClick={onClick}
    >
      {children}
    </section>
  )
}

function formatTime(timestamp: number, use24Hour = false) {
  return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit', hour12: !use24Hour }).format(new Date(timestamp))
}

function formatTimelineTime(timestamp: number, preferences: PreferenceValues) {
  if (preferenceBoolean(preferences, 'appearance.relative_timestamps')) {
    const minutes = Math.round((timestamp - Date.now()) / 60_000)
    const absoluteMinutes = Math.abs(minutes)
    const relative = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
    if (absoluteMinutes < 60) return relative.format(minutes, 'minute')
    const hours = Math.round(minutes / 60)
    if (Math.abs(hours) < 24) return relative.format(hours, 'hour')
    const days = Math.round(hours / 24)
    if (Math.abs(days) < 7) return relative.format(days, 'day')
    return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(new Date(timestamp))
  }
  return formatTime(timestamp, preferenceBoolean(preferences, 'appearance.time_24_hour'))
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  const unitIndex = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)) - 1, units.length - 1)
  return `${(bytes / 1024 ** (unitIndex + 1)).toFixed(1)} ${units[unitIndex]}`
}

function fuzzyScore(query: string, target: string) {
  const queryCharacters = [...query.toLowerCase()]
  const targetCharacters = [...target.toLowerCase()]
  if (queryCharacters.length === 0) return 0
  let queryIndex = 0
  let score = 0
  let lastIndex = -2
  for (let index = 0; index < targetCharacters.length; index++) {
    const character = targetCharacters[index]
    if (character !== queryCharacters[queryIndex]) continue
    score += 10
    if (index === 0 || [' ', ':', '_', '-'].includes(targetCharacters[index - 1])) score += 8
    if (index === lastIndex + 1) score += 5
    lastIndex = index
    queryIndex++
    if (queryIndex === queryCharacters.length) return score - targetCharacters.length + queryCharacters.length
  }
  return null
}

function rankQuickSwitcherRooms(rooms: Room[], query: string) {
  const normalizedQuery = query.trim()
  if (!normalizedQuery) return rooms
  const ranked: { room: Room; score: number; index: number }[] = []
  rooms.forEach((room, index) => {
    const nameScore = fuzzyScore(normalizedQuery, room.name)
    const idScore = fuzzyScore(normalizedQuery, room.id)
    if (nameScore === null && idScore === null) return
    ranked.push({ room, score: Math.max(nameScore ?? Number.NEGATIVE_INFINITY, idScore ?? Number.NEGATIVE_INFINITY), index })
  })
  return ranked.sort((left, right) => right.score - left.score || left.index - right.index).map(({ room }) => room)
}

const notificationTones: Record<string, { type: OscillatorType; frequency: number; duration: number; gain: number }> = {
  Chime: { type: 'sine', frequency: 720, duration: 0.12, gain: 0.035 },
  Pulse: { type: 'square', frequency: 480, duration: 0.09, gain: 0.012 },
  Soft: { type: 'triangle', frequency: 360, duration: 0.2, gain: 0.02 },
}

const builtInQuickReactions = ['👍', '❤️', '😂', '🎉', '👀', '🙏']

function playNotificationSound(volume: number, tone = 'Chime') {
  const profile = notificationTones[tone]
  if (volume <= 0 || !profile) return
  try {
    const audio = new AudioContext()
    const oscillator = audio.createOscillator()
    const gain = audio.createGain()
    oscillator.type = profile.type
    oscillator.frequency.setValueAtTime(profile.frequency, audio.currentTime)
    gain.gain.setValueAtTime(profile.gain * volume / 100, audio.currentTime)
    gain.gain.exponentialRampToValueAtTime(0.0001, audio.currentTime + profile.duration)
    oscillator.connect(gain)
    gain.connect(audio.destination)
    oscillator.onended = () => { void audio.close() }
    oscillator.start()
    oscillator.stop(audio.currentTime + profile.duration)
  } catch {
    return
  }
}

function notificationAllowed(preferences: PreferenceValues, roomID: string, category: string) {
  if (!preferenceBoolean(preferences, 'notifications.enabled', true)) return false
  if (preferenceBoolean(preferences, `room.muted.${roomID}`)) return false
  switch (category) {
    case 'group': return preferenceBoolean(preferences, 'notifications.group')
    case 'mention': return preferenceBoolean(preferences, 'notifications.mentions', true)
    case 'direct': return preferenceBoolean(preferences, 'notifications.direct', true)
    default: return true
  }
}

function notificationTone(preferences: PreferenceValues, category: string) {
  const keys: Record<string, string> = { direct: 'notifications.sound_direct', mention: 'notifications.sound_mention', group: 'notifications.sound_group' }
  return preferenceString(preferences, keys[category] ?? 'notifications.sound_mention', 'Chime')
}

function notificationText(preferences: PreferenceValues, notification: MatrixNotification, roomName: string) {
  let title = notification.sender ? `${notification.sender} · ${roomName}` : roomName
  let body = notification.body
  switch (preferenceString(preferences, 'notifications.privacy', 'Sender and message preview')) {
    case 'Sender only':
      body = 'New message'
      break
    case 'Hidden content':
      title = 'Matrix'
      body = 'New activity'
      break
  }
  return { title, body }
}

function detectOneTimeCode(body: string) {
  if (!/\b(code|otp|one[- ]time|verification|passcode|2fa)\b/i.test(body)) return null
  return /\b(\d{4,8})\b/.exec(body)?.[1] ?? null
}

function networkHue(label: string) {
  let hash = 0
  for (const char of label) hash = (hash * 31 + char.charCodeAt(0)) % 360
  return hash
}

function quickReactionOptions(preferences: PreferenceValues) {
  const custom = preferenceString(preferences, 'reactions.quick_source', 'Frequent') === 'Custom'
    ? preferenceString(preferences, 'reactions.custom_emojis', '').split(/\s+/).filter(Boolean)
    : []
  const base = custom.length > 0 ? custom : builtInQuickReactions
  const defaultEmoji = preferenceString(preferences, 'reactions.default_emoji', '')
  return [...new Set(defaultEmoji ? [defaultEmoji, ...base] : base)].slice(0, 6)
}

function encodeBase64(bytes: Uint8Array) {
  let binary = ''
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000))
  }
  return btoa(binary)
}

function createMediaObjectURL(encoded: string, mimeType: string) {
  const binary = atob(encoded)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return URL.createObjectURL(new Blob([bytes.buffer as ArrayBuffer], { type: mimeType || 'application/octet-stream' }))
}

function encodeVoiceNoteWav(audio: AudioBuffer) {
  const dataLength = audio.length * 2
  if (dataLength + 44 > 50 * 1024 * 1024) throw new Error('Voice note cannot exceed 50 MiB.')
  const buffer = new ArrayBuffer(dataLength + 44)
  const view = new DataView(buffer)
  const channels = Array.from({ length: audio.numberOfChannels }, (_, index) => audio.getChannelData(index))
  const writeText = (offset: number, value: string) => {
    for (let index = 0; index < value.length; index++) view.setUint8(offset + index, value.charCodeAt(index))
  }
  writeText(0, 'RIFF')
  view.setUint32(4, dataLength + 36, true)
  writeText(8, 'WAVE')
  writeText(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, audio.sampleRate, true)
  view.setUint32(28, audio.sampleRate * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  writeText(36, 'data')
  view.setUint32(40, dataLength, true)
  for (let frame = 0; frame < audio.length; frame++) {
    let sample = 0
    for (const channel of channels) sample += channel[frame] / channels.length
    const clamped = Math.max(-1, Math.min(1, sample))
    view.setInt16(44 + frame * 2, clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff, true)
  }
  return new Blob([buffer], { type: 'audio/wav' })
}

function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [systemPrefersDark, setSystemPrefersDark] = useState(() =>
    typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches,
  )
  const [selectedRoomID, setSelectedRoomID] = useState('')
  const [mobilePane, setMobilePane] = useState<'rooms' | 'conversation'>('rooms')
  const [search, setSearch] = useState('')
  const [roomScope, setRoomScope] = useState<RoomScope>('all')
  const [roomRefinements, setRoomRefinements] = useState<RoomRefinements>({ unread: false, unanswered: false, favorite: false })
  const [inboxAccountScope, setInboxAccountScope] = useState('all')
  const [otherAccountRooms, setOtherAccountRooms] = useState<Room[]>([])
  const [messages, setMessages] = useState<Message[]>([])
  const [olderCursor, setOlderCursor] = useState(0)
  const [hasOlder, setHasOlder] = useState(false)
  const hasOlderRef = useRef(false)
  const [loadingOlder, setLoadingOlder] = useState(false)
  // Scroll events fire far faster than state updates commit, so a scroll
  // handler calling loadOlderMessages() could start several fetches before
  // `loadingOlder` re-renders. This ref closes that window.
  const loadingOlderRef = useRef(false)
  const [showJumpToLatest, setShowJumpToLatest] = useState(false)
  const [composeText, setComposeText] = useState('')
  const [mentionBindings, setMentionBindings] = useState<{ token: string; userID: string }[]>([])
  const [mentionStart, setMentionStart] = useState(-1)
  const [mentionEnd, setMentionEnd] = useState(0)
  const [mentionQuery, setMentionQuery] = useState('')
  const [emojiMatch, setEmojiMatch] = useState<EmojiQuery | null>(null)
  const [emojiActiveIndex, setEmojiActiveIndex] = useState(0)
  const [mentionActiveIndex, setMentionActiveIndex] = useState(0)
  const [replyTarget, setReplyTarget] = useState<Message | null>(null)
  const [editTarget, setEditTarget] = useState<Message | null>(null)
  const [reactingMessageID, setReactingMessageID] = useState('')
  const [redactTarget, setRedactTarget] = useState<Message | null>(null)
  const [roomInfoOpen, setRoomInfoOpen] = useState(false)
  const [roomMembers, setRoomMembers] = useState<Member[]>([])
  const [roomMembersLoading, setRoomMembersLoading] = useState(false)
  const [roomMembersError, setRoomMembersError] = useState('')
  const [typingUsers, setTypingUsers] = useState<string[]>([])
  const [leaveConfirmOpen, setLeaveConfirmOpen] = useState(false)
  const [leaveRoomError, setLeaveRoomError] = useState('')
  const [leavingRoom, setLeavingRoom] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [messageSearch, setMessageSearch] = useState('')
  const [searchRoomID, setSearchRoomID] = useState('')
  const [searchSender, setSearchSender] = useState('')
  const [searchFrom, setSearchFrom] = useState('')
  const [searchTo, setSearchTo] = useState('')
  const [searchResults, setSearchResults] = useState<MessageSearchResult[]>([])
  const [searchNextBatch, setSearchNextBatch] = useState('')
  const [searchStatus, setSearchStatus] = useState('')
  const [searching, setSearching] = useState(false)
  const [scheduledJobs, setScheduledJobs] = useState<ScheduledJob[]>([])
  const [scheduledJobsOpen, setScheduledJobsOpen] = useState(false)
  const [scheduledDraft, setScheduledDraft] = useState<ScheduledDraft | null>(null)
  const [scheduledDueAt, setScheduledDueAt] = useState('')
  const [scheduledTimes, setScheduledTimes] = useState<Record<string, string>>({})
  const [scheduledLoading, setScheduledLoading] = useState(false)
  const [scheduledSaving, setScheduledSaving] = useState(false)
  const [scheduledBusyID, setScheduledBusyID] = useState('')
  const [scheduledJobsError, setScheduledJobsError] = useState('')
  const [scheduledDialogError, setScheduledDialogError] = useState('')
  const [scheduledNotice, setScheduledNotice] = useState('')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('General')
  const [accountManagerOpen, setAccountManagerOpen] = useState(false)
  const [addingAccount, setAddingAccount] = useState(false)
  const [accountHomeserver, setAccountHomeserver] = useState('https://matrix.org')
  const [accountUsername, setAccountUsername] = useState('')
  const [accountPassword, setAccountPassword] = useState('')
  const [accountError, setAccountError] = useState('')
  const [undoRoomID, setUndoRoomID] = useState('')
  const [roomContextMenu, setRoomContextMenu] = useState<{ roomID: string; x: number; y: number } | null>(null)
  // Replaces the always-visible per-message action bar: the actions only
  // surface as a floating menu when the user right-clicks a message.
  const [messageContextMenu, setMessageContextMenu] = useState<{ message: Message; x: number; y: number } | null>(null)
  const [quickSwitcherOpen, setQuickSwitcherOpen] = useState(false)
  const [quickSwitcherQuery, setQuickSwitcherQuery] = useState('')
  const [quickSwitcherIndex, setQuickSwitcherIndex] = useState(0)
  const [homeserver, setHomeserver] = useState('https://matrix.org')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [recoveryKey, setRecoveryKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [pendingSendPreview, setPendingSendPreview] = useState<string | null>(null)
  const [autoPlayNextMessageId, setAutoPlayNextMessageId] = useState<string | null>(null)
  const [voiceCaptureStatus, setVoiceCaptureStatus] = useState<'recording' | 'processing' | null>(null)
  const [audioInputDevices, setAudioInputDevices] = useState<MediaDeviceInfo[]>([])
  const [selectedAudioInputDeviceId, setSelectedAudioInputDeviceId] = useState<string>(preferenceString(snapshot?.preferences ?? {}, 'audio.input_device', ''))
  const [isDraggingFiles, setIsDraggingFiles] = useState(false)
  const [loadingHistory, setLoadingHistory] = useState(false)
  const [historyRevision, setHistoryRevision] = useState(0)
  const [historyPageRevision, setHistoryPageRevision] = useState(0)
  const lastObservedHistoryRevisionRef = useRef(0)
  const [error, setError] = useState('')
  const selectedRoomRef = useRef('')
  const historyRoomIDRef = useRef('')
  const pendingHistoryRefreshRoomIDRef = useRef('')
  const editTargetRef = useRef<Message | null>(null)
  const markRoomAnsweredRef = useRef<(roomID: string) => Promise<void>>(async () => { })
  const preferencesRef = useRef<PreferenceValues>({})
  const roomsRef = useRef<Room[]>([])
  const messagesRef = useRef<Message[]>([])
  const nativeNotificationsRef = useRef(false)
  const lastReadEventByRoom = useRef(new Map<string, string>())
  const draftsRef = useRef(new Map<string, string>())
  const triageBusyRef = useRef(false)
  const dragDepthRef = useRef(0)
  const timelineRef = useRef<HTMLDivElement>(null)
  const timelineHeightBeforePrepend = useRef<number | null>(null)
  const stickToBottomRef = useRef(true)
  const roomSwitchRef = useRef(false)
  const pendingSendRef = useRef<PendingSend | null>(null)
  const notificationBatchesRef = useRef(new Map<string, { notifications: MatrixNotification[]; timer: number }>())
  const repeatRemindersRef = useRef(new Map<string, number>())

  useEffect(() => () => {
    notificationBatchesRef.current.forEach((batch) => window.clearTimeout(batch.timer))
    repeatRemindersRef.current.forEach((timer) => window.clearTimeout(timer))
  }, [])
  const attachmentInputRef = useRef<HTMLInputElement>(null)
  const voiceRecorderRef = useRef<MediaRecorder | null>(null)
  const voiceChunksRef = useRef<Blob[]>([])
  const voiceStreamRef = useRef<MediaStream | null>(null)
  const voiceRoomIDRef = useRef('')
  const voiceTimeoutRef = useRef<number | null>(null)
  const roomSearchInputRef = useRef<HTMLInputElement>(null)
  const composerInputRef = useRef<HTMLTextAreaElement>(null)
  const [aiPanel, setAIPanel] = useState<AIPanel | null>(null)
  const aiRequestRef = useRef(0)
  const quickSwitcherInputRef = useRef<HTMLInputElement>(null)
  const pendingSearchJumpRef = useRef<{ roomID: string; eventID: string; timelineCursor: number } | null>(null)

  useEffect(() => {
    let active = true
    void AppService.NativeNotificationsSupported()
      .then((supported) => {
        if (active) nativeNotificationsRef.current = supported
      })
      .catch(() => {})
    return () => { active = false }
  }, [])

  const selectedRoom = (snapshot?.rooms ?? []).find((room) => room.id === selectedRoomID)
  const selectedRoomAvatar = selectedRoom?.avatar || (
    historyRoomIDRef.current === selectedRoomID
      ? [...messages].reverse().find((message) => !message.own && message.avatar)?.avatar ?? ''
      : ''
  )
  const preferences = snapshot?.preferences ?? {}
  const appZoom = preferenceNumber(preferences, 'appearance.zoom', 100, 75, 125)
  const scheduleTimeSuggestions = parseTimeSuggestionMinutes(preferenceString(preferences, 'schedule.suggestions_minutes', '5,15,60'))
  const answeredShortcut = preferenceString(preferences, 'shortcuts.mark_answered', 'E').trim().toUpperCase() || 'E'
  const singleKeyShortcuts = preferenceBoolean(preferences, 'shortcuts.single_keys', true)
  const rooms = snapshot?.rooms ?? []
  const visibleMessages = preferenceBoolean(preferences, 'appearance.hide_deleted_messages')
    ? messages.filter((message) => !message.redacted)
    : messages
  preferencesRef.current = preferences
  roomsRef.current = rooms
  messagesRef.current = messages

  useEffect(() => {
    setSelectedAudioInputDeviceId(preferenceString(preferences, 'audio.input_device', ''))
  }, [preferences['audio.input_device']])

  useEffect(() => {
    let active = true
    async function enumerate() {
      try {
        if (!navigator.mediaDevices?.enumerateDevices) return
        const devices = await navigator.mediaDevices.enumerateDevices()
        if (active) {
          setAudioInputDevices(devices.filter((device) => device.kind === 'audioinput'))
        }
      } catch {
        // ignore
      }
    }
    void enumerate()
    return () => { active = false }
  }, [])

  useEffect(() => {
    let active = true
    const mediaDevices = navigator.mediaDevices
    if (!mediaDevices?.enumerateDevices) return

    const refreshAudioInputDevices = () => {
      void mediaDevices.enumerateDevices().then((devices) => {
        if (active) setAudioInputDevices(devices.filter((device) => device.kind === 'audioinput'))
      }).catch(() => {})
    }
    refreshAudioInputDevices()
    mediaDevices.addEventListener?.('devicechange', refreshAudioInputDevices)
    return () => {
      active = false
      mediaDevices.removeEventListener?.('devicechange', refreshAudioInputDevices)
    }
  }, [])
  hasOlderRef.current = hasOlder
  editTargetRef.current = editTarget
  const inboxAccountCount = snapshot?.accounts?.length ?? 0
  const activeAccountID = snapshot?.activeAccountId ?? ''
  useEffect(() => {
    if (inboxAccountCount < 2) return
    let active = true
    const refreshOtherAccountRooms = () => {
      void AppService.AccountRooms().then((accountRooms) => {
        if (active) setOtherAccountRooms((accountRooms ?? []).filter((room) => room.accountId !== activeAccountID))
      }).catch(() => undefined)
    }
    refreshOtherAccountRooms()
    const timer = window.setInterval(refreshOtherAccountRooms, 15000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [inboxAccountCount, activeAccountID])
  // Rooms of signed-in accounts other than the active one are cached and refreshed on an interval.
  const effectiveAccountScope = inboxAccountCount > 1 ? inboxAccountScope : 'all'
  const crossAccountRooms = inboxAccountCount > 1 ? otherAccountRooms : []
  const scopedRooms = effectiveAccountScope === 'all'
    ? (crossAccountRooms.length > 0 ? [...rooms, ...crossAccountRooms].sort((left, right) => right.lastEventAt - left.lastEventAt) : rooms)
    : effectiveAccountScope === activeAccountID ? rooms : crossAccountRooms.filter((room) => room.accountId === effectiveAccountScope)
  const filteredRooms = scopedRooms.filter((room) => {
    const query = search.trim().toLowerCase()
    const matchesQuery = !query || `${room.name} ${room.id} ${room.preview}`.toLowerCase().includes(query)
    const archived = preferenceBoolean(preferences, archivedPreferenceKey(room.id, room.accountId))
    const matchesArchive = roomScope === 'archived' ? archived : !archived
    const matchesScope = roomScope === 'all' || roomScope === 'archived' || (roomScope === 'direct' ? room.direct : !room.direct)
    const matchesRefinements = (!roomRefinements.unread || room.unread > 0 || room.markedUnread)
      && (!roomRefinements.unanswered || room.unanswered)
      && (!roomRefinements.favorite || room.favorite)
    return matchesQuery && matchesArchive && matchesScope && matchesRefinements
  }).sort((left, right) => preferenceString(preferences, 'sidebar.pinned_style', 'Top of list') === 'Top of list' ? Number(right.favorite) - Number(left.favorite) : 0)
  const quickSwitcherRooms = rankQuickSwitcherRooms(rooms, quickSwitcherQuery)
  const unansweredCount = scopedRooms.filter((room) => room.unanswered && !preferenceBoolean(preferences, archivedPreferenceKey(room.id, room.accountId))).length
  const readRoomCount = rooms.filter((room) => room.unread === 0 && !room.markedUnread && !preferenceBoolean(preferences, archivedPreferenceKey(room.id))).length
  const scheduledJobCount = scheduledJobs.length
  const mentionOptions = mentionStart < 0 ? [] : roomMembers.filter((member) => {
    const query = mentionQuery.toLowerCase()
    return `${member.displayName} ${member.id}`.toLowerCase().includes(query)
  }).slice(0, 8)
  const themePreference = preferenceString(preferences, 'appearance.theme', 'System default').toLowerCase().replace(/ /g, '-')
  const themeChoice = themePreference === 'system-default' ? systemPrefersDark ? 'dark' : 'light' : themePreference
  const defaultBubbleColor = themeChoice === 'bosco' ? '#276d4b' : themeChoice === 'rosa' ? '#ad3f60' : '#236b52'
  const bubbleColor = preferenceString(preferences, 'appearance.bubble_color', defaultBubbleColor)
  const safeBubbleColor = /^#[0-9a-fA-F]{6}$/.test(bubbleColor) ? bubbleColor : defaultBubbleColor
  const accentPalette = accentPalettes.find((palette) => palette.name === preferenceString(preferences, 'appearance.accent_color', 'Forest')) ?? accentPalettes[0]
  const composerContextMessage = editTarget ?? replyTarget

  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const updateSystemTheme = (event: MediaQueryListEvent) => setSystemPrefersDark(event.matches)
    setSystemPrefersDark(mediaQuery.matches)
    mediaQuery.addEventListener('change', updateSystemTheme)
    return () => mediaQuery.removeEventListener('change', updateSystemTheme)
  }, [])

  async function refreshScheduledJobs() {
    setScheduledLoading(true)
    try {
      const jobs = await AppService.ScheduledJobs()
      const nextJobs = jobs ?? []
      setScheduledJobs(nextJobs)
      setScheduledTimes(Object.fromEntries(nextJobs.map((job) => [job.id, localDateTimeInputValue(job.dueAt)])))
      setScheduledJobsError('')
    } catch (jobsError) {
      setScheduledJobsError(describeError(jobsError))
    } finally {
      setScheduledLoading(false)
    }
  }

  function openScheduledJobs() {
    setScheduledJobsOpen(true)
    setScheduledJobsError('')
    void refreshScheduledJobs()
  }

  function openScheduleMessage() {
    if (!selectedRoomID || !composeText.trim() || editTarget) return
    setScheduledDraft({
      kind: 'message',
      roomID: selectedRoomID,
      text: composeText.trim(),
      replyToEventID: replyTarget?.id,
      replyToSender: replyTarget?.senderName || replyTarget?.sender,
      replyToPreview: replyTarget?.body,
    })
    setScheduledDueAt(localDateTimeInputValue(Date.now() + defaultScheduleDelayMinutes('schedule.default_send_minutes', 5) * 60_000))
    setScheduledDialogError('')
  }

  function openReminder(message: Message) {
    if (!selectedRoomID || !message.id || message.id.startsWith('~') || message.redacted) return
    setScheduledDraft({ kind: 'reminder', roomID: selectedRoomID, eventID: message.id, text: '', archiveAfter: false })
    setScheduledDueAt(localDateTimeInputValue(Date.now() + defaultScheduleDelayMinutes('schedule.default_reminder_minutes', 60) * 60_000))
    setScheduledDialogError('')
  }

  function defaultScheduleDelayMinutes(key: string, fallback: number) {
    const minutes = Number(preferenceString(preferences, key, String(fallback)))
    return Number.isInteger(minutes) && minutes >= 1 && minutes <= 10080 ? minutes : fallback
  }

  useEffect(() => {
    selectedRoomRef.current = selectedRoomID
    void AppService.SetActiveRoom(selectedRoomID)
  }, [selectedRoomID])

  useEffect(() => {
    const handleGlobalAnsweredShortcut = (event: globalThis.KeyboardEvent) => {
      if (!singleKeyShortcuts || event.repeat || event.altKey || event.metaKey || event.ctrlKey || event.key.toUpperCase() !== answeredShortcut) return
      const target = event.target
      if (target instanceof HTMLElement && (target.isContentEditable || target.closest('input, textarea, select, [contenteditable="true"]'))) return
      if (settingsOpen || roomInfoOpen || leaveConfirmOpen || redactTarget || quickSwitcherOpen) return
      const roomID = roomContextMenu?.roomID ?? selectedRoomID
      if (!roomID || !roomsRef.current.find((room) => room.id === roomID)?.unanswered) return
      event.preventDefault()
      void markRoomAnsweredRef.current(roomID)
    }
    window.addEventListener('keydown', handleGlobalAnsweredShortcut)
    return () => window.removeEventListener('keydown', handleGlobalAnsweredShortcut)
  }, [answeredShortcut, singleKeyShortcuts, leaveConfirmOpen, quickSwitcherOpen, roomContextMenu, roomInfoOpen, redactTarget, selectedRoomID, settingsOpen])

  useEffect(() => {
    if (!roomContextMenu && !messageContextMenu) return
    const dismissOnOutsideClick = (event: globalThis.PointerEvent) => {
      if (event.target instanceof Element) {
        if (event.target.closest('.room-context-menu')) return
        if (event.target.closest('.message-context-menu')) return
      }
      setRoomContextMenu(null)
      setMessageContextMenu(null)
    }
    const dismissOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setRoomContextMenu(null)
      setMessageContextMenu(null)
    }
    // Only one menu is ever open: opening one clears the other.
    const frame = window.requestAnimationFrame(() => document.querySelector<HTMLButtonElement>(roomContextMenu ? '.room-context-menu button' : '.message-context-menu button')?.focus())
    document.addEventListener('pointerdown', dismissOnOutsideClick)
    window.addEventListener('keydown', dismissOnEscape)
    return () => {
      window.cancelAnimationFrame(frame)
      document.removeEventListener('pointerdown', dismissOnOutsideClick)
      window.removeEventListener('keydown', dismissOnEscape)
    }
  }, [roomContextMenu, messageContextMenu])

  // The reaction picker used to be toggled off by its own toolbar button, which
  // now lives in the right-click menu. Give it a dismissal path of its own.
  useEffect(() => {
    if (!reactingMessageID) return
    const dismissOnOutsideClick = (event: globalThis.PointerEvent) => {
      if (event.target instanceof Element && event.target.closest('.reaction-picker')) return
      setReactingMessageID('')
    }
    const dismissOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') setReactingMessageID('')
    }
    document.addEventListener('pointerdown', dismissOnOutsideClick)
    window.addEventListener('keydown', dismissOnEscape)
    return () => {
      document.removeEventListener('pointerdown', dismissOnOutsideClick)
      window.removeEventListener('keydown', dismissOnEscape)
    }
  }, [reactingMessageID])

  function isNearBottom(element: HTMLElement, tolerance = 96) {
    return element.scrollHeight - element.scrollTop - element.clientHeight <= tolerance
  }

  function isNearTop(element: HTMLElement, tolerance = 240) {
    return element.scrollTop <= tolerance
  }

  function scrollTimelineToBottom(behavior: ScrollBehavior = 'auto') {
    const timeline = timelineRef.current
    if (!timeline) return
    timeline.scrollTo({ top: timeline.scrollHeight, behavior })
  }

  function jumpToLatest() {
    stickToBottomRef.current = true
    setShowJumpToLatest(false)
    scrollTimelineToBottom('smooth')
  }

  function focusTimelineMessage(eventID: string) {
    const timeline = timelineRef.current
    if (!timeline || !eventID) return
    const target = Array.from(timeline.querySelectorAll<HTMLElement>('[data-event-id]'))
      .find((element) => element.dataset.eventId === eventID)
    if (!target) return
    target.scrollIntoView?.({ behavior: 'smooth', block: 'center' })
    target.focus({ preventScroll: true })
  }

  function focusTimelineItem(index: number) {
    const timeline = timelineRef.current
    const items = timeline ? Array.from(timeline.querySelectorAll<HTMLElement>('[data-event-id]')) : []
    const target = items[index]
    if (!target) return
    target.scrollIntoView?.({ behavior: 'smooth', block: 'center' })
    target.focus({ preventScroll: true })
  }

  function moveTimelineFocus(direction: number, boundary?: 'first' | 'last') {
    const timeline = timelineRef.current
    if (!timeline) return
    const items = Array.from(timeline.querySelectorAll<HTMLElement>('[data-event-id]'))
    if (items.length === 0) return
    if (boundary) {
      focusTimelineItem(boundary === 'first' ? 0 : items.length - 1)
      return
    }
    const activeItem = document.activeElement instanceof HTMLElement
      ? document.activeElement.closest<HTMLElement>('[data-event-id]')
      : null
    const currentIndex = activeItem ? items.indexOf(activeItem) : -1
    const nextIndex = currentIndex < 0
      ? direction > 0 ? 0 : items.length - 1
      : currentIndex + direction
    if (nextIndex >= 0 && nextIndex < items.length) focusTimelineItem(nextIndex)
  }

  function handleTimelineKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.metaKey || event.ctrlKey || event.altKey || isTextEntryTarget(event.target)) return
    const target = event.target instanceof HTMLElement ? event.target : null
    const row = target?.closest<HTMLElement>('[data-event-id]')
    if (!row && target !== event.currentTarget) return

    if (!singleKeyShortcuts && event.key.length === 1) return

    if (event.key === 'ArrowDown' || event.key === 'j' || event.key === 'J') {
      if (event.key.startsWith('Arrow') && target !== row && target !== event.currentTarget) return
      event.preventDefault()
      moveTimelineFocus(1)
    } else if (event.key === 'ArrowUp' || event.key === 'k' || event.key === 'K') {
      if (event.key.startsWith('Arrow') && target !== row && target !== event.currentTarget) return
      event.preventDefault()
      moveTimelineFocus(-1)
    } else if (event.key === 'Home' && (target === row || target === event.currentTarget)) {
      event.preventDefault()
      moveTimelineFocus(1, 'first')
    } else if (event.key === 'End' && (target === row || target === event.currentTarget)) {
      event.preventDefault()
      moveTimelineFocus(-1, 'last')
    } else if (event.key.toLowerCase() === 'r' && row) {
      const message = messages.find((item) => item.id === row.dataset.eventId || item.transactionId === row.dataset.eventId)
      if (!message?.id || message.id.startsWith('~')) return
      event.preventDefault()
      setEditTarget(null)
      setReplyTarget(message)
      requestAnimationFrame(() => composerInputRef.current?.focus())
    }
  }

  useLayoutEffect(() => {
    const timeline = timelineRef.current
    if (!timeline) return
    const previousHeight = timelineHeightBeforePrepend.current
    if (previousHeight !== null) {
      timeline.scrollTop += timeline.scrollHeight - previousHeight
      timelineHeightBeforePrepend.current = null
      return
    }
    if (roomSwitchRef.current || stickToBottomRef.current) {
      roomSwitchRef.current = false
      timeline.scrollTop = timeline.scrollHeight
      setShowJumpToLatest(false)
    }
  }, [messages])

  useEffect(() => {
    setSearchOpen(false)
    setMessageSearch('')
    setSearchResults([])
    setSearchStatus('')
    setRoomInfoOpen(false)
    setLeaveConfirmOpen(false)
    setMentionBindings([])
    setMentionStart(-1)
    setTypingUsers([])
    setComposeText(draftsRef.current.get(selectedRoomID) ?? '')
    setShowJumpToLatest(false)
    stickToBottomRef.current = true
    setReplyTarget(null)
    setEditTarget(null)
  }, [selectedRoomID])

  useEffect(() => {
    if (!quickSwitcherOpen) return
    const frame = window.requestAnimationFrame(() => quickSwitcherInputRef.current?.focus())
    return () => window.cancelAnimationFrame(frame)
  }, [quickSwitcherOpen])

  useEffect(() => {
    setQuickSwitcherIndex(0)
  }, [quickSwitcherQuery])

  useEffect(() => {
    if (!selectedRoomID) {
      setRoomMembers([])
      setRoomMembersLoading(false)
      setRoomMembersError('')
      return
    }
    let active = true
    setRoomMembers([])
    setRoomMembersLoading(true)
    setRoomMembersError('')
    AppService.RoomMembers(selectedRoomID)
      .then((members) => { if (active) setRoomMembers(members ?? []) })
      .catch((membersError: unknown) => { if (active) setRoomMembersError(describeError(membersError)) })
      .finally(() => { if (active) setRoomMembersLoading(false) })
    return () => { active = false }
  }, [selectedRoomID])

  useEffect(() => {
    if (!selectedRoomID || !preferenceBoolean(preferences, 'privacy.send_typing', true)) return
    const timeout = window.setTimeout(() => {
      const timeoutMilliseconds = composeText.trim() ? 5000 : 0
      void AppService.SetTyping(selectedRoomID, timeoutMilliseconds).catch((typingError: unknown) => setError(describeError(typingError)))
    }, 350)
    return () => window.clearTimeout(timeout)
  }, [composeText, selectedRoomID, preferences])

  useEffect(() => {
    let active = true
    const unsubscribe = Events.On('matrix:update', (event: any) => {
      const update = event.data as MatrixUpdate
      setSnapshot((current) => current ? {
        ...current,
        rooms: update.rooms ?? [],
        syncReady: current.syncReady || update.initialSyncComplete,
        needsVerification: update.needsVerification,
        profile: { ...current.profile, verified: !update.needsVerification },
        connectionState: update.connectionState || current.connectionState,
      } : current)
      setTypingUsers(update.typingUsers?.[selectedRoomRef.current] ?? [])
      for (const notification of update.notifications ?? []) {
        const currentPreferences = preferencesRef.current
        if (!notificationAllowed(currentPreferences, notification.roomId, notification.category)) continue
        if (notification.roomId === selectedRoomRef.current) continue
        if (preferenceBoolean(currentPreferences, 'notifications.only_when_unfocused', true) && document.hasFocus()) continue
        const oneTimeCode = detectOneTimeCode(notification.body)
        if (oneTimeCode && preferenceBoolean(currentPreferences, 'notifications.otp_copy')) {
          void navigator.clipboard?.writeText(oneTimeCode).catch(() => undefined)
        }
        const batchSeconds = preferenceNumber(currentPreferences, 'notifications.batch_seconds', 0, 0, 30)
        const immediate = Boolean(oneTimeCode) && preferenceBoolean(currentPreferences, 'notifications.otp_immediate', true)
        if (batchSeconds > 0 && !immediate) {
          queueNotificationBatch(notification, batchSeconds)
          continue
        }
        const { title, body } = notificationText(currentPreferences, notification, notificationRoomName(notification))
        deliverNotification(currentPreferences, notification, title, body)
      }
      if (update.error) setError(update.error)
      // Belt-and-braces: if the selected room's sidebar preview moved past the
      // loaded timeline (e.g. a coalesced/dropped timeline signal), force a
      // history refresh so left room list and right message pane stay in sync.
      const selectedID = selectedRoomRef.current
      const timelineTouched = (update.timelineChangedRoomId ?? []).includes(selectedID)
      if (timelineTouched) {
        setHistoryRevision((revision) => revision + 1)
      } else if (selectedID) {
        const selected = (update.rooms ?? []).find((room) => room.id === selectedID)
        const newestLoaded = [...messagesRef.current].reverse().find((message) => message.id && !message.id.startsWith('~'))
        if (selected && newestLoaded && selected.lastEventId && selected.lastEventId !== newestLoaded.id) {
          const newestTimestamp = newestLoaded.timestamp ?? 0
          if (selected.lastSeen > newestTimestamp || (selected.lastSeen === newestTimestamp && selected.lastEventAt >= newestTimestamp)) {
            setHistoryRevision((revision) => revision + 1)
          }
        }
      }
    })
    const unsubscribeHostError = Events.On('matrix:host-error', (event: any) => {
      const message = (event.data as { error?: string } | undefined)?.error
      if (active && message) setError(message)
    })
    const unsubscribeNotificationOpenRoom = Events.On('notification:open-room', (event: any) => {
      const roomID = event.data as string | undefined
      if (active && roomID) {
        window.focus()
        showRoom(roomID)
      }
    })
    const unsubscribeAttachmentSent = Events.On('matrix:attachment-sent', (event: any) => {
      if (active && event.data === selectedRoomRef.current) {
        setHistoryRevision((revision) => revision + 1)
      }
    })
    const unsubscribeScheduledReminder = Events.On('scheduler:reminder', (event: any) => {
      if (!active) return
      const reminder = event.data as ScheduledJob
      const roomName = roomsRef.current.find((room) => room.id === reminder.roomId)?.name || reminder.roomId
      const body = reminder.text.trim() || `You set a reminder for ${roomName}.`
      setScheduledNotice(`${roomName}: ${body}`)
      void refreshScheduledJobs()
      if (nativeNotificationsRef.current) {
        void AppService.SendNotification(`Reminder · ${roomName}`, body, reminder.roomId).catch((notificationError: unknown) => setScheduledJobsError(describeError(notificationError)))
      } else if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
        try {
          const notification = new Notification(`Reminder · ${roomName}`, { body })
          notification.onclick = () => {
            window.focus()
            showRoom(reminder.roomId)
            setScheduledJobsOpen(true)
          }
        } catch (notificationError) {
          setScheduledJobsError(describeError(notificationError))
        }
      }
    })

    void refreshScheduledJobs()
    AppService.Snapshot()
      .then((state) => {
        if (!active) return
        setSnapshot(state)
        setSelectedRoomID((current) => current || (state.rooms ?? [])[0]?.id || '')
      })
      .catch((snapshotError: unknown) => {
        if (active) setError(describeError(snapshotError))
      })

    return () => {
      active = false
      unsubscribe()
      unsubscribeHostError()
      unsubscribeNotificationOpenRoom()
      unsubscribeAttachmentSent()
      unsubscribeScheduledReminder()
    }
  }, [])

  useEffect(() => {
    if (!selectedRoomID) {
      historyRoomIDRef.current = ''
      setMessages([])
      setOlderCursor(0)
      setHasOlder(false)
      return
    }

    let active = true
    const searchJump = pendingSearchJumpRef.current?.roomID === selectedRoomID ? pendingSearchJumpRef.current : null
    if (pendingSearchJumpRef.current && !searchJump) pendingSearchJumpRef.current = null
    historyRoomIDRef.current = ''
    setLoadingHistory(true)
    setOlderCursor(0)
    setHasOlder(false)
    setLoadingOlder(false)
    timelineHeightBeforePrepend.current = null
    roomSwitchRef.current = true
    stickToBottomRef.current = true
    void (async () => {
      try {
        let page = await AppService.History(selectedRoomID, 0)
        if (!active) return
        let loadedMessages = page.messages ?? []
        let nextBefore = page.before
        let hasMore = page.hasMore
        let jumpPages = 0
        while (
          searchJump && searchJump.timelineCursor > 0 &&
          !loadedMessages.some((message) => message.id === searchJump.eventID) &&
          hasMore && nextBefore > searchJump.timelineCursor && jumpPages < 500
        ) {
          page = await AppService.History(selectedRoomID, nextBefore)
          if (!active) return
          const olderMessages = page.messages ?? []
          if (olderMessages.length === 0 || page.before === nextBefore) {
            hasMore = false
            break
          }
          const loadedKeys = new Set(loadedMessages.map((message) => message.id || message.transactionId))
          loadedMessages = [...olderMessages.filter((message) => !loadedKeys.has(message.id || message.transactionId)), ...loadedMessages]
          nextBefore = page.before
          hasMore = page.hasMore
          jumpPages++
        }
        historyRoomIDRef.current = selectedRoomID
        setMessages(loadedMessages)
        setOlderCursor(nextBefore)
        setHasOlder(hasMore)
        if (searchJump && !loadedMessages.some((message) => message.id === searchJump.eventID)) {
          pendingSearchJumpRef.current = null
          setError('Could not load that message from local history.')
        }
        if (pendingHistoryRefreshRoomIDRef.current === selectedRoomID) {
          pendingHistoryRefreshRoomIDRef.current = ''
          setHistoryRevision((revision) => revision + 1)
        }
      } catch (historyError) {
        if (active) setError(describeError(historyError))
      } finally {
        if (active) setLoadingHistory(false)
      }
    })()

    return () => { active = false }
  }, [selectedRoomID, historyPageRevision])

  useEffect(() => {
    const searchJump = pendingSearchJumpRef.current
    if (!searchJump || searchJump.roomID !== selectedRoomID || loadingHistory || historyRoomIDRef.current !== selectedRoomID) return
    if (!messages.some((message) => message.id === searchJump.eventID)) return
    pendingSearchJumpRef.current = null
    requestAnimationFrame(() => focusTimelineMessage(searchJump.eventID))
  }, [loadingHistory, messages, selectedRoomID])

  useEffect(() => {
    const revisionChanged = historyRevision !== lastObservedHistoryRevisionRef.current
    lastObservedHistoryRevisionRef.current = historyRevision
    if (!selectedRoomID || historyRevision === 0) return
    const roomID = selectedRoomID
    if (historyRoomIDRef.current !== roomID) {
      if (revisionChanged) pendingHistoryRefreshRoomIDRef.current = roomID
      return
    }

    let active = true
    AppService.History(roomID, 0)
      .then((page) => {
        if (!active || selectedRoomRef.current !== roomID) return
        setMessages((current) => mergeTimelineMessages(current, page.messages ?? []))
        if (!hasOlderRef.current) setOlderCursor(page.before)
        if (page.hasMore) setHasOlder(true)
      })
      .catch((historyError: unknown) => {
        if (active) setError(describeError(historyError))
      })

    return () => { active = false }
  }, [historyRevision, selectedRoomID])

  useEffect(() => {
    if (!selectedRoomID || loadingHistory || historyRoomIDRef.current !== selectedRoomID || preferenceBoolean(preferences, 'privacy.incognito')) return
    const newestEvent = [...messages].reverse().find((message) => message.id && !message.id.startsWith('~'))
    if (!newestEvent || lastReadEventByRoom.current.get(selectedRoomID) === newestEvent.id) return
    lastReadEventByRoom.current.set(selectedRoomID, newestEvent.id)
    void AppService.MarkRead(selectedRoomID, newestEvent.id).catch((readError: unknown) => {
      if (lastReadEventByRoom.current.get(selectedRoomID) === newestEvent.id) lastReadEventByRoom.current.delete(selectedRoomID)
      setError(describeError(readError))
    })
  }, [loadingHistory, messages, selectedRoomID, preferences])

  async function loadOlderMessages() {
    if (!selectedRoomID || !hasOlder || loadingOlder || loadingOlderRef.current) return
    const roomID = selectedRoomID
    timelineHeightBeforePrepend.current = timelineRef.current?.scrollHeight ?? null
    loadingOlderRef.current = true
    setLoadingOlder(true)
    try {
      const page = await AppService.History(roomID, olderCursor)
      if (selectedRoomRef.current !== roomID) return
      setMessages((current) => {
        const currentKeys = new Set(current.map((message) => message.id || message.transactionId))
        const olderMessages = (page.messages ?? []).filter((message) => !currentKeys.has(message.id || message.transactionId))
        // Nothing new arrived, so drop the saved height: the prepend-detecting
        // layout effect only runs on a `messages` change and would otherwise
        // apply this stale delta to the next unrelated update.
        if (olderMessages.length === 0) timelineHeightBeforePrepend.current = null
        return [...olderMessages, ...current]
      })
      setOlderCursor(page.before)
      setHasOlder(page.hasMore && page.before !== olderCursor)
    } catch (historyError) {
      setError(describeError(historyError))
      timelineHeightBeforePrepend.current = null
    } finally {
      loadingOlderRef.current = false
      setLoadingOlder(false)
    }
  }

  // Keep the viewport filled. A room whose newest page is shorter than the
  // timeline box can't be scrolled at all, so the scroll handler below would
  // never fire and older history would stay stuck behind the button. This
  // terminates: a page either grows the content (stopping the fill) or stops
  // advancing the cursor, which makes loadOlderMessages clear `hasOlder`.
  useEffect(() => {
    const timeline = timelineRef.current
    if (!selectedRoomID || loadingHistory || loadingOlder || !hasOlder) return
    if (!timeline || timeline.clientHeight <= 0) return
    if (timeline.scrollHeight > timeline.clientHeight) return
    void loadOlderMessages()
  }, [selectedRoomID, loadingHistory, loadingOlder, hasOlder, messages])

  function messageSearchRequest(nextBatch = ''): MessageSearchRequest | null {
    const query = messageSearch.trim()
    if (!query) return null
    if (searchFrom && searchTo && searchFrom > searchTo) {
      setSearchStatus('The start date must be before the end date.')
      return null
    }
    const minTimestamp = searchFrom ? new Date(`${searchFrom}T00:00:00`).getTime() : 0
    const maxTimestamp = searchTo ? new Date(`${searchTo}T23:59:59.999`).getTime() : 0
    return {
      query,
      roomID: searchRoomID,
      sender: searchSender.trim(),
      minTimestamp,
      maxTimestamp,
      limit: 50,
      nextBatch,
    }
  }

  async function submitMessageSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const request = messageSearchRequest()
    if (!request || searching) return
    setSearching(true)
    setSearchStatus('')
    setSearchResults([])
    setSearchNextBatch('')
    try {
      const found = await AppService.SearchMessages(request)
      const results = found.results ?? []
      setSearchResults(results)
      setSearchNextBatch(found.nextBatch)
      setSearchStatus(results.length === 0 ? 'No matching messages in local history.' : `${results.length} results shown`)
    } catch (searchError) {
      setSearchResults([])
      setSearchStatus(describeError(searchError))
    } finally {
      setSearching(false)
    }
  }

  async function loadMoreSearchResults() {
    const request = messageSearchRequest(searchNextBatch)
    if (!request || !searchNextBatch || searching) return
    setSearching(true)
    setSearchStatus('')
    try {
      const page = await AppService.SearchMessages(request)
      const nextResults = page.results ?? []
      setSearchResults((current) => [...current, ...nextResults])
      setSearchNextBatch(page.nextBatch)
      setSearchStatus(`${searchResults.length + nextResults.length} results shown`)
    } catch (searchError) {
      setSearchStatus(describeError(searchError))
    } finally {
      setSearching(false)
    }
  }

  function openSearchResult(result: MessageSearchResult) {
    const eventID = result.message.id
    if (!eventID) return
    setSearchOpen(false)
    if (selectedRoomID === result.roomID && messages.some((message) => message.id === eventID)) {
      requestAnimationFrame(() => focusTimelineMessage(eventID))
      return
    }
    pendingSearchJumpRef.current = { roomID: result.roomID, eventID, timelineCursor: result.message.timelineCursor }
    if (selectedRoomID === result.roomID) setHistoryPageRevision((revision) => revision + 1)
    else showRoom(result.roomID)
  }

  function closeMessageSearch() {
    setSearchOpen(false)
    requestAnimationFrame(() => document.querySelector<HTMLButtonElement>('[aria-label="Search messages"]')?.focus())
  }

  async function setPreference(key: string, value: boolean | string) {
    const previous = snapshot?.preferences?.[key]
    const roomPreference = /^room\.(favorite|muted)\.(.+)$/.exec(key)
    setSnapshot((current) => {
      if (!current) return current
      const rooms = roomPreference && typeof value === 'boolean'
        ? (current.rooms ?? []).map((room) => room.id === roomPreference[2] ? { ...room, [roomPreference[1]]: value } : room)
        : current.rooms
      return { ...current, preferences: { ...current.preferences, [key]: value }, rooms }
    })
    try {
      if (typeof value === 'boolean') {
        await AppService.SetBooleanPreference(key, value)
      } else {
        await AppService.SetStringPreference(key, value)
      }
      if (key === 'history.initial_messages') setHistoryPageRevision((revision) => revision + 1)
      return true
    } catch (preferenceError) {
      setSnapshot((current) => {
        if (!current) return current
        const nextPreferences = { ...current.preferences }
        if (previous === undefined) delete nextPreferences[key]
        else nextPreferences[key] = previous
        const rooms = roomPreference && (previous === undefined || typeof previous === 'boolean')
          ? (current.rooms ?? []).map((room) => room.id === roomPreference[2] ? { ...room, [roomPreference[1]]: previous === true } : room)
          : current.rooms
        return { ...current, preferences: nextPreferences, rooms }
      })
      setError(describeError(preferenceError))
      return false
    }
  }

  function archivedPreferenceKey(roomID: string, accountID = '') {
    return `room.archived.${accountID || snapshot?.activeAccountId || 'primary'}.${roomID}`
  }

  function nextRoomByBehavior(excludedRoomIDs: Set<string>, behavior: string) {
    const candidates = filteredRooms.filter((room) => !excludedRoomIDs.has(room.id) && isActiveAccountRoom(room))
    if (behavior === 'none') return ''
    if (behavior === 'next_unread') return candidates.find((room) => room.unread > 0 || room.markedUnread)?.id ?? ''
    return candidates[0]?.id ?? ''
  }

  function nextRoomAfterArchive(archivedRoomIDs: Set<string>) {
    const behavior = preferenceString(preferences, 'archive.next_room', 'next_visible')
    return nextRoomByBehavior(archivedRoomIDs, behavior)
  }

  async function markRoomRead(roomID: string) {
    const room = rooms.find((candidate) => candidate.id === roomID)
    if (!room || (room.unread === 0 && !room.markedUnread) || !room.lastEventId) return
    try {
      await AppService.MarkRead(room.id, room.lastEventId)
      setSnapshot((current) => current ? {
        ...current,
        rooms: (current.rooms ?? []).map((candidate) => candidate.id === room.id ? { ...candidate, unread: 0, markedUnread: false, mentions: 0 } : candidate),
      } : current)
      setRoomContextMenu(null)
      const behavior = preferenceString(preferences, 'inbox.after_read', 'stay')
      if (behavior !== 'stay') showRoom(nextRoomByBehavior(new Set([roomID]), behavior))
    } catch (readError) {
      setError(describeError(readError))
    }
  }

  async function markRoomUnread(roomID: string) {
    const room = rooms.find((candidate) => candidate.id === roomID)
    if (!room || room.unread > 0 || room.markedUnread) return
    try {
      await AppService.MarkUnread(room.id)
      setSnapshot((current) => current ? {
        ...current,
        rooms: (current.rooms ?? []).map((candidate) => candidate.id === room.id ? { ...candidate, markedUnread: true } : candidate),
      } : current)
      setRoomContextMenu(null)
      const behavior = preferenceString(preferences, 'inbox.after_read', 'stay')
      if (behavior !== 'stay') showRoom(nextRoomByBehavior(new Set([roomID]), behavior))
    } catch (readError) {
      setError(describeError(readError))
    }
  }

  async function setRoomArchived(roomID: string, archived: boolean) {
    const room = rooms.find((candidate) => candidate.id === roomID)
    if (!room) return
    if (archived && (room.unread > 0 || room.markedUnread) && room.lastEventId) {
      try {
        await AppService.MarkRead(room.id, room.lastEventId)
        setSnapshot((current) => current ? {
          ...current,
          rooms: (current.rooms ?? []).map((candidate) => candidate.id === room.id ? { ...candidate, unread: 0, markedUnread: false, mentions: 0 } : candidate),
        } : current)
      } catch (readError) {
        setError(describeError(readError))
        return
      }
    }
    const saved = await setPreference(archivedPreferenceKey(roomID), archived)
    setRoomContextMenu(null)
    if (saved && archived && selectedRoomID === roomID) {
      showRoom(nextRoomAfterArchive(new Set([roomID])))
    }
  }

  async function archiveAllReadRooms() {
    const readRooms = rooms.filter((room) => room.unread === 0 && !room.markedUnread && !preferenceBoolean(preferences, archivedPreferenceKey(room.id)))
    for (const room of readRooms) await setPreference(archivedPreferenceKey(room.id), true)
    if (readRooms.some((room) => room.id === selectedRoomID)) {
      showRoom(nextRoomAfterArchive(new Set(readRooms.map((room) => room.id))))
    }
  }

  async function archiveInactiveRooms() {
    const ageDays = Number(preferenceString(preferences, 'archive.older_than_days', '90'))
    const cutoff = Date.now() - ageDays * 24 * 60 * 60 * 1000
    const inactiveRooms = rooms.filter((room) => room.lastEventAt > 0
      && room.lastEventAt < cutoff
      && !preferenceBoolean(preferences, archivedPreferenceKey(room.id)))
    const archivedRoomIDs = new Set<string>()
    for (const room of inactiveRooms) {
      if ((room.unread > 0 || room.markedUnread) && room.lastEventId) {
        try {
          await AppService.MarkRead(room.id, room.lastEventId)
          setSnapshot((current) => current ? {
            ...current,
            rooms: (current.rooms ?? []).map((candidate) => candidate.id === room.id ? { ...candidate, unread: 0, markedUnread: false, mentions: 0 } : candidate),
          } : current)
        } catch (readError) {
          setError(describeError(readError))
          continue
        }
      }
      if (await setPreference(archivedPreferenceKey(room.id), true)) archivedRoomIDs.add(room.id)
    }
    if (archivedRoomIDs.has(selectedRoomID)) showRoom(nextRoomAfterArchive(archivedRoomIDs))
  }

  function openRoomContextMenu(roomID: string, x: number, y: number) {
    const menuWidth = 232
    const menuHeight = 152
    setMessageContextMenu(null)
    setRoomContextMenu({
      roomID,
      x: Math.max(8, Math.min(x, window.innerWidth - menuWidth - 8)),
      y: Math.max(8, Math.min(y, window.innerHeight - menuHeight - 8)),
    })
  }

  function updateRoomUnanswered(roomID: string, unanswered: boolean) {
    setSnapshot((current) => current ? {
      ...current,
      rooms: (current.rooms ?? []).map((room) => room.id === roomID ? { ...room, unanswered } : room),
    } : current)
  }

  function openMessageContextMenu(message: Message, x: number, y: number) {
    // A selection means the user wants the platform's own menu (Copy), so leave
    // the native context menu alone in that case.
    const selection = window.getSelection()
    if (selection && !selection.isCollapsed && selection.toString().trim()) return
    const menuWidth = 232
    const menuHeight = 268
    setRoomContextMenu(null)
    setMessageContextMenu({
      message,
      x: Math.max(8, Math.min(x, window.innerWidth - menuWidth - 8)),
      y: Math.max(8, Math.min(y, window.innerHeight - menuHeight - 8)),
    })
  }

  // The same eligibility rules the old inline action bar used, expressed once
  // so the menu can't drift from what the backend allows.
  function messageContextMenuItems(message: Message): MessageMenuItem[] {
    const unsent = !message.id || message.id.startsWith('~')
    const manageable = !unsent && !message.redacted
    const items: MessageMenuItem[] = [
      { key: 'copy', label: 'Copy message', ariaLabel: 'Copy message', icon: <Copy size={14} />, disabled: !message.body, run: () => void copyMessage(message) },
      { key: 'reply', label: 'Reply', ariaLabel: `Reply to ${message.senderName || message.sender}`, icon: <Reply size={14} />, disabled: unsent, run: () => { setEditTarget(null); setReplyTarget(message) } },
      { key: 'remind', label: 'Set reminder', ariaLabel: 'Set reminder for message', icon: <BellRing size={14} />, disabled: !manageable, run: () => openReminder(message) },
      { key: 'react', label: 'React', ariaLabel: 'Add reaction', icon: <Smile size={14} />, disabled: unsent, run: () => startReaction(message) },
      { key: 'translate', label: 'Translate', ariaLabel: 'Translate message with local AI', icon: <Languages size={14} />, disabled: !aiEnabled || !message.body || Boolean(message.redacted), run: () => translateMessage(message) },
    ]
    if (message.own && manageable) {
      items.push({ key: 'edit', label: 'Edit', ariaLabel: 'Edit message', icon: <Pencil size={13} />, run: () => { setReplyTarget(null); setEditTarget(message); setComposeText(message.body); setMentionBindings([]); setMentionStart(-1) } })
      items.push({ key: 'delete', label: 'Delete', ariaLabel: 'Delete message', danger: true, icon: <Trash2 size={13} />, run: () => setRedactTarget(message) })
    }
    if (message.sendError && message.sendError !== 'not sent' && message.transactionId) {
      items.push({ key: 'retry', label: 'Retry sending', ariaLabel: 'Retry sending message', icon: <RotateCcw size={13} />, run: () => void retryMessage(message) })
    }
    return items
  }

  async function copyMessage(message: Message) {
    const body = displayMessageBody(message)
    const timestamp = preferenceBoolean(preferences, 'clipboard.copy_timestamp')
      ? `[${new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(message.timestamp))}] ${message.senderName || message.sender}: `
      : ''
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard access is unavailable.')
      await navigator.clipboard.writeText(`${timestamp}${body}`)
    } catch (copyError) {
      setError(describeError(copyError))
    }
  }

  async function markRoomAnswered(roomID: string) {
    setRoomContextMenu(null)
    if (triageBusyRef.current || !roomsRef.current.find((room) => room.id === roomID)?.unanswered) return
    triageBusyRef.current = true
    setError('')
    const wasSelected = selectedRoomRef.current === roomID
    const visibleIndex = filteredRooms.findIndex((room) => room.id === roomID)
    const nextRoom = visibleIndex >= 0 && filteredRooms.length > 1
      ? filteredRooms[(visibleIndex + 1) % filteredRooms.length]
      : roomsRef.current.find((room) => room.id !== roomID)
    try {
      await AppService.SetRoomUnanswered(roomID, false)
      updateRoomUnanswered(roomID, false)
      setUndoRoomID(roomID)
      if (wasSelected && nextRoom && isActiveAccountRoom(nextRoom)) showRoom(nextRoom.id)
    } catch (triageError) {
      setError(describeError(triageError))
    } finally {
      triageBusyRef.current = false
    }
  }
  markRoomAnsweredRef.current = markRoomAnswered

  function handleMarkAnsweredKeyDown(event: KeyboardEvent<HTMLElement>, roomID: string) {
    if (!singleKeyShortcuts || event.altKey || event.metaKey || event.ctrlKey || event.key.toUpperCase() !== answeredShortcut) return
    if (!roomsRef.current.find((room) => room.id === roomID)?.unanswered) return
    event.preventDefault()
    event.stopPropagation()
    void markRoomAnswered(roomID)
  }

  async function undoAnswered() {
    const roomID = undoRoomID
    if (!roomID || triageBusyRef.current) return
    triageBusyRef.current = true
    setError('')
    try {
      await AppService.SetRoomUnanswered(roomID, true)
      updateRoomUnanswered(roomID, true)
      setUndoRoomID('')
      showRoom(roomID)
    } catch (triageError) {
      setError(describeError(triageError))
    } finally {
      triageBusyRef.current = false
    }
  }

  function showRoom(roomID: string) {
    setSelectedRoomID(roomID)
    setMobilePane(roomID ? 'conversation' : 'rooms')
  }

  function isActiveAccountRoom(room: Room) {
    return !room.accountId || room.accountId === snapshot?.activeAccountId
  }

  // Rooms from other accounts switch to their account before opening.
  function openInboxRoom(room: Room) {
    if (isActiveAccountRoom(room)) showRoom(room.id)
    else void switchAccount(room.accountId, room.id)
  }

  function selectRoomByOffset(direction: number, unansweredOnly = false) {
    const availableRooms = filteredRooms.filter((room) => !unansweredOnly || room.unanswered)
    if (availableRooms.length === 0) return
    const currentIndex = availableRooms.findIndex((room) => room.id === selectedRoomRef.current && isActiveAccountRoom(room))
    const nextIndex = currentIndex < 0
      ? direction > 0 ? 0 : availableRooms.length - 1
      : (currentIndex + direction + availableRooms.length) % availableRooms.length
    openInboxRoom(availableRooms[nextIndex])
  }

  function handleApplicationKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (settingsOpen || roomInfoOpen || leaveConfirmOpen || redactTarget || quickSwitcherOpen || roomContextMenu) return
    if (event.key === 'Escape' && searchOpen) {
      event.preventDefault()
      closeMessageSearch()
      return
    }
    if (isTextEntryTarget(event.target)) return

    const key = event.key.toLowerCase()
    const hasPrimaryModifier = event.metaKey || event.ctrlKey
    const accountShortcutNumber = event.code.match(/^Digit([1-9])$/)?.[1] ?? (/^[1-9]$/.test(event.key) ? event.key : '')
    if (hasPrimaryModifier && event.altKey && accountShortcutNumber) {
      const account = snapshot?.accounts?.[Number(accountShortcutNumber) - 1]
      if (account) {
        event.preventDefault()
        void switchAccount(account.id)
      }
      return
    }
    if (!hasPrimaryModifier && !event.altKey && event.key === '?') {
      event.preventDefault()
      setSettingsTab('Keyboard')
      setSettingsOpen(true)
      return
    }
    if (!hasPrimaryModifier && !event.altKey && event.key === '/') {
      event.preventDefault()
      roomSearchInputRef.current?.focus()
      return
    }
    if (hasPrimaryModifier && !event.altKey) {
      if (key === 'k') {
        event.preventDefault()
        if (roomsRef.current.length > 0) {
          setQuickSwitcherQuery('')
          setQuickSwitcherIndex(0)
          setQuickSwitcherOpen(true)
        }
        return
      }
      if (event.key === '+' || event.key === '=') {
        event.preventDefault()
        void setPreference('appearance.zoom', String(Math.min(125, appZoom + 5)))
        return
      }
      if (event.key === '-') {
        event.preventDefault()
        void setPreference('appearance.zoom', String(Math.max(75, appZoom - 5)))
        return
      }
      if (event.key === '0') {
        event.preventDefault()
        void setPreference('appearance.zoom', '100')
        return
      }
      if (key === 'f') {
        event.preventDefault()
        setSearchOpen(true)
        return
      }
      if (event.key === ',' || event.code === 'Comma') {
        event.preventDefault()
        setSettingsOpen(true)
        return
      }
      if (event.shiftKey && key === 's') {
        event.preventDefault()
        roomSearchInputRef.current?.focus()
        return
      }
      if (event.shiftKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
        event.preventDefault()
        selectRoomByOffset(event.key === 'ArrowDown' ? 1 : -1, true)
        return
      }
      if (event.shiftKey && key === 'a') {
        event.preventDefault()
        setRoomScope('all')
        setRoomRefinements({ unread: false, unanswered: false, favorite: false })
        return
      }
      if (event.shiftKey && key === 'u') {
        event.preventDefault()
        setRoomScope('all')
        setRoomRefinements({ unread: false, unanswered: true, favorite: false })
        return
      }
      if (event.shiftKey && key === 'l') {
        event.preventDefault()
        jumpToLatest()
        return
      }
      if (/^[1-9]$/.test(event.key)) {
        const room = filteredRooms[Number(event.key) - 1]
        if (room) {
          event.preventDefault()
          openInboxRoom(room)
        }
        return
      }
      if (key === 'z' && undoRoomID) {
        event.preventDefault()
        void undoAnswered()
        return
      }
    }
    if (event.altKey && !event.ctrlKey && !event.metaKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
      event.preventDefault()
      selectRoomByOffset(event.key === 'ArrowDown' ? 1 : -1)
    }
  }

  function handleRoomKeyDown(event: KeyboardEvent<HTMLButtonElement>, room: Room, index: number) {
    if (isActiveAccountRoom(room)) handleMarkAnsweredKeyDown(event, room.id)
    if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return
    if (isActiveAccountRoom(room) && ((event.key === 'F10' && event.shiftKey) || event.key === 'ContextMenu')) {
      event.preventDefault()
      const bounds = event.currentTarget.getBoundingClientRect()
      openRoomContextMenu(room.id, bounds.right - 216, bounds.bottom)
      return
    }
    const direction = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? filteredRooms.length - 1 : index + direction
    if (direction === 0 && event.key !== 'Home' && event.key !== 'End') return
    const nextButton = document.querySelectorAll<HTMLButtonElement>('.room-list .room-button')[nextIndex]
    if (nextButton) {
      event.preventDefault()
      nextButton.focus()
    }
  }

  async function submitLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await AppService.Login(homeserver.trim(), username.trim(), password)
      setPassword('')
      const state = await AppService.Snapshot()
      setSnapshot(state)
      setSelectedRoomID((state.rooms ?? [])[0]?.id ?? '')
    } catch (loginError) {
      setError(describeError(loginError))
    } finally {
      setBusy(false)
    }
  }

  function resetAccountWorkspace(state: Snapshot) {
    setSnapshot(state)
    setSelectedRoomID((state.rooms ?? [])[0]?.id ?? '')
    setMobilePane('rooms')
    setMessages([])
    setComposeText('')
    setMentionBindings([])
    setReplyTarget(null)
    setEditTarget(null)
    setSearchOpen(false)
    setSearchResults([])
    setSearchNextBatch('')
    setScheduledJobs([])
    setScheduledTimes({})
    draftsRef.current.clear()
    lastReadEventByRoom.current.clear()
    historyRoomIDRef.current = ''
    pendingHistoryRefreshRoomIDRef.current = ''
    void refreshScheduledJobs()
  }

  async function switchAccount(accountID: string, roomID = '') {
    if (accountID === snapshot?.activeAccountId) {
      setAccountManagerOpen(false)
      if (roomID) showRoom(roomID)
      return
    }
    setBusy(true)
    setAccountError('')
    try {
      await flushPendingSend()
      const state = await AppService.SwitchAccount(accountID)
      resetAccountWorkspace(state)
      if (roomID) showRoom(roomID)
      setAccountManagerOpen(false)
    } catch (switchError) {
      setAccountError(describeError(switchError))
    } finally {
      setBusy(false)
    }
  }

  async function submitAddAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setAccountError('')
    try {
      const state = await AppService.AddAccount(accountHomeserver.trim(), accountUsername.trim(), accountPassword)
      resetAccountWorkspace(state)
      setAccountPassword('')
      setAccountUsername('')
      setAddingAccount(false)
      setAccountManagerOpen(false)
    } catch (addError) {
      setAccountError(describeError(addError))
    } finally {
      setBusy(false)
    }
  }

  async function submitVerification(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await AppService.Verify(recoveryKey.trim())
      setRecoveryKey('')
      setSnapshot(await AppService.Snapshot())
    } catch (verificationError) {
      setError(describeError(verificationError))
    } finally {
      setBusy(false)
    }
  }

  async function submitScheduledDraft(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!scheduledDraft || scheduledSaving) return
    const dueAt = new Date(scheduledDueAt).getTime()
    if (!Number.isFinite(dueAt) || dueAt <= Date.now()) {
      setScheduledDialogError('Choose a time in the future.')
      return
    }
    setScheduledSaving(true)
    setScheduledDialogError('')
    try {
      if (scheduledDraft.kind === 'message') {
        if (scheduledDraft.replyToEventID) {
          await AppService.ScheduleReply(scheduledDraft.roomID, scheduledDraft.text, scheduledDraft.replyToEventID, dueAt)
        } else {
          await AppService.ScheduleMessage(scheduledDraft.roomID, scheduledDraft.text, dueAt)
        }
        draftsRef.current.delete(scheduledDraft.roomID)
        if (selectedRoomRef.current === scheduledDraft.roomID) setComposeText('')
        setMentionBindings([])
        setMentionStart(-1)
        setReplyTarget(null)
      } else {
        await AppService.ScheduleReminder(scheduledDraft.roomID, scheduledDraft.eventID, scheduledDraft.text, dueAt)
        if (scheduledDraft.archiveAfter) await setRoomArchived(scheduledDraft.roomID, true)
      }
      setScheduledDraft(null)
      await refreshScheduledJobs()
    } catch (scheduleError) {
      setScheduledDialogError(describeError(scheduleError))
    } finally {
      setScheduledSaving(false)
    }
  }

  async function manageScheduledJob(id: string, action: 'reschedule' | 'cancel' | 'retry' | 'dismiss') {
    if (scheduledBusyID) return
    setScheduledBusyID(id)
    setScheduledJobsError('')
    try {
      if (action === 'reschedule') {
        const dueAt = new Date(scheduledTimes[id] ?? '').getTime()
        if (!Number.isFinite(dueAt) || dueAt <= Date.now()) throw new Error('Choose a time in the future.')
        await AppService.UpdateScheduledJob(id, dueAt)
      } else if (action === 'cancel') {
        await AppService.CancelScheduledJob(id)
      } else if (action === 'retry') {
        await AppService.RetryScheduledMessage(id)
      } else {
        await AppService.DismissScheduledJob(id)
      }
      await refreshScheduledJobs()
    } catch (jobError) {
      setScheduledJobsError(describeError(jobError))
    } finally {
      setScheduledBusyID('')
    }
  }

  const undoSendSeconds = Number(preferenceString(preferences, 'composer.undo_send_seconds', '0')) || 0

  async function dispatchMessage(message: Pick<PendingSend, 'roomID' | 'text' | 'replyTo' | 'mentionUserIDs'>) {
    if (message.replyTo) await AppService.SendReply(message.roomID, message.text, message.replyTo.id)
    else if (message.mentionUserIDs.length > 0) await AppService.SendMessageWithMentions(message.roomID, message.text, message.mentionUserIDs)
    else await AppService.SendMessage(message.roomID, message.text)
    if (preferenceString(preferencesRef.current, 'sounds.effects', 'Off') === 'Sent messages') {
      playNotificationSound(preferenceNumber(preferencesRef.current, 'notifications.volume', 50), 'Soft')
    }
  }

  function notificationRoomName(notification: MatrixNotification) {
    return notification.room || roomsRef.current.find((room) => room.id === notification.roomId)?.name || notification.roomId
  }

  function playNotificationAlert(preferences: PreferenceValues, category: string) {
    if (!preferenceBoolean(preferences, 'notifications.sound')) return
    playNotificationSound(preferenceNumber(preferences, 'notifications.volume', 50), notificationTone(preferences, category))
  }

  function deliverNotification(preferences: PreferenceValues, notification: MatrixNotification, title: string, body: string, scheduleRepeat = true) {
    if (nativeNotificationsRef.current) {
      void AppService.SendNotification(title, body, notification.roomId)
        .then((delivered) => {
          if (delivered) playNotificationAlert(preferences, notification.category)
        })
        .catch((notificationError: unknown) => setError(describeError(notificationError)))
    } else if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
      try {
        const systemNotification = new Notification(title, { body })
        systemNotification.onclick = () => {
          window.focus()
          showRoom(notification.roomId)
        }
        playNotificationAlert(preferences, notification.category)
      } catch (notificationError) {
        setError(describeError(notificationError))
      }
    }
    if (scheduleRepeat) scheduleRepeatReminder(preferences, notification)
  }

  function queueNotificationBatch(notification: MatrixNotification, batchSeconds: number) {
    const batches = notificationBatchesRef.current
    const existing = batches.get(notification.roomId)
    if (existing) {
      existing.notifications.push(notification)
      return
    }
    const timer = window.setTimeout(() => flushNotificationBatch(notification.roomId), batchSeconds * 1000)
    batches.set(notification.roomId, { notifications: [notification], timer })
  }

  function flushNotificationBatch(roomID: string) {
    const batch = notificationBatchesRef.current.get(roomID)
    notificationBatchesRef.current.delete(roomID)
    if (!batch || batch.notifications.length === 0) return
    const preferences = preferencesRef.current
    if (selectedRoomRef.current === roomID) return
    const [first] = batch.notifications
    if (!notificationAllowed(preferences, roomID, first.category)) return
    if (batch.notifications.length === 1) {
      const { title, body } = notificationText(preferences, first, notificationRoomName(first))
      deliverNotification(preferences, first, title, body)
      return
    }
    const hidden = preferenceString(preferences, 'notifications.privacy', 'Sender and message preview') === 'Hidden content'
    const senders = [...new Set(batch.notifications.map((item) => item.sender).filter(Boolean))]
    const title = hidden ? 'Matrix' : notificationRoomName(first)
    const body = hidden ? 'New activity' : `${batch.notifications.length} new messages${senders.length > 0 ? ` from ${senders.slice(0, 3).join(', ')}` : ''}`
    deliverNotification(preferences, first, title, body)
  }

  function scheduleRepeatReminder(preferences: PreferenceValues, notification: MatrixNotification) {
    const minutes = preferenceNumber(preferences, 'notifications.repeat_minutes', 0, 0, 60)
    const reminders = repeatRemindersRef.current
    const existing = reminders.get(notification.roomId)
    if (existing !== undefined) window.clearTimeout(existing)
    reminders.delete(notification.roomId)
    if (minutes <= 0) return
    const timer = window.setTimeout(() => {
      reminders.delete(notification.roomId)
      const current = preferencesRef.current
      const room = roomsRef.current.find((item) => item.id === notification.roomId)
      if (!room || (room.unread <= 0 && !room.markedUnread) || selectedRoomRef.current === notification.roomId) return
      if (!notificationAllowed(current, notification.roomId, notification.category)) return
      const roomName = notificationRoomName(notification)
      const hidden = preferenceString(current, 'notifications.privacy', 'Sender and message preview') === 'Hidden content'
      deliverNotification(current, notification, hidden ? 'Matrix' : `Still unread · ${roomName}`, 'You still have unread messages.', false)
    }, minutes * 60_000)
    reminders.set(notification.roomId, timer)
  }

  function startReaction(message: Message) {
    const preferences = preferencesRef.current
    if (preferenceBoolean(preferences, 'reactions.quick_enabled', true)) {
      setReactingMessageID(message.id || '')
      return
    }
    void sendReaction(message, preferenceString(preferences, 'reactions.default_emoji', '') || '👍')
  }

  function handleMessageDoubleClick(message: Message) {
    if (!message.id || message.id.startsWith('~') || message.redacted || message.sending) return
    switch (preferenceString(preferencesRef.current, 'messages.double_click', 'None')) {
      case 'Reply':
        setEditTarget(null)
        setReplyTarget(message)
        break
      case 'React':
        startReaction(message)
        break
    }
  }

  function restorePendingSend(pending: PendingSend) {
    const existing = draftsRef.current.get(pending.roomID) ?? ''
    const restored = existing ? `${pending.text}\n${existing}` : pending.text
    draftsRef.current.set(pending.roomID, restored)
    if (selectedRoomRef.current !== pending.roomID) return
    setComposeText(restored)
    setReplyTarget(pending.replyTo)
    setMentionBindings(pending.mentionBindings)
  }

  async function flushPendingSend() {
    const pending = pendingSendRef.current
    if (!pending) return
    clearTimeout(pending.timer)
    pendingSendRef.current = null
    setPendingSendPreview(null)
    try {
      await dispatchMessage(pending)
      setHistoryRevision((revision) => revision + 1)
    } catch (sendError) {
      restorePendingSend(pending)
      setError(describeError(sendError))
    }
  }

  function cancelPendingSend() {
    const pending = pendingSendRef.current
    if (!pending) return
    clearTimeout(pending.timer)
    pendingSendRef.current = null
    setPendingSendPreview(null)
    restorePendingSend(pending)
  }

  async function submitMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const roomID = selectedRoomID
    const text = (preferenceBoolean(preferences, 'emoji.replace_emoticons') ? replaceEmoticons(composeText) : composeText).trim()
    if (!roomID || !text || busy) return
    const mentionUserIDs = [...new Set(mentionBindings.filter(({ token }) => text.includes(token)).map(({ userID }) => userID))]
    if (!editTarget && undoSendSeconds > 0) {
      void flushPendingSend()
      const timer = window.setTimeout(() => void flushPendingSend(), undoSendSeconds * 1000)
      pendingSendRef.current = { roomID, text, replyTo: replyTarget, mentionUserIDs, mentionBindings, timer }
      setPendingSendPreview(text)
      setError('')
      draftsRef.current.delete(roomID)
      if (selectedRoomRef.current === roomID) setComposeText('')
      setMentionBindings([])
      setMentionStart(-1)
      setReplyTarget(null)
      return
    }
    setBusy(true)
    setError('')
    try {
      if (editTarget) {
        await AppService.EditMessage(roomID, text, editTarget.id)
      } else {
        await dispatchMessage({ roomID, text, replyTo: replyTarget, mentionUserIDs })
      }
      const nextDraft = editTarget ? draftsRef.current.get(roomID) ?? '' : ''
      if (!editTarget) draftsRef.current.delete(roomID)
      if (selectedRoomRef.current === roomID) setComposeText(nextDraft)
      setMentionBindings([])
      setMentionStart(-1)
      setReplyTarget(null)
      setEditTarget(null)
      stickToBottomRef.current = true
      scrollTimelineToBottom()
      setHistoryRevision((revision) => revision + 1)
    } catch (sendError) {
      setError(describeError(sendError))
    } finally {
      setBusy(false)
    }
  }

  async function sendAttachmentFiles(files: File[], roomID = selectedRoomID, voiceDurationMilliseconds = 0) {
    if (files.length === 0) return
    if (!roomID) {
      setError('Select a room before uploading files.')
      return
    }
    if (busy) return
    setBusy(true)
    setError('')
    try {
      let uploadedAny = false
      for (const file of files) {
        if (file.size > 50 * 1024 * 1024) {
          setError(`${file.name || 'Attachments'} cannot exceed 50 MiB.`)
          continue
        }
        const bytes = new Uint8Array(await file.arrayBuffer())
        await AppService.SendAttachment(roomID, file.name, encodeBase64(bytes), voiceDurationMilliseconds > 0, voiceDurationMilliseconds)
        uploadedAny = true
      }
      if (uploadedAny) setHistoryRevision((revision) => revision + 1)
    } catch (uploadError) {
      setError(describeError(uploadError))
    } finally {
      setBusy(false)
    }
  }

  function uploadAttachment(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.currentTarget.files?.[0]
    event.currentTarget.value = ''
    if (file) void sendAttachmentFiles([file])
  }

  async function startVoiceNoteRecording() {
    if (!selectedRoomID || busy || voiceCaptureStatus) return
    if (!navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === 'undefined') {
      setError('Voice recording is not supported in this environment.')
      return
    }
    let stream: MediaStream | null = null
    try {
      const audioConstraints: MediaStreamConstraints['audio'] = selectedAudioInputDeviceId
        ? { deviceId: { exact: selectedAudioInputDeviceId } }
        : true
      stream = await navigator.mediaDevices.getUserMedia({ audio: audioConstraints })
      void navigator.mediaDevices.enumerateDevices?.().then((devices) => {
        setAudioInputDevices(devices.filter((device) => device.kind === 'audioinput'))
      }).catch(() => {})
      const recorder = new MediaRecorder(stream)
      voiceChunksRef.current = []
      voiceRoomIDRef.current = selectedRoomID
      voiceStreamRef.current = stream
      voiceRecorderRef.current = recorder
      recorder.ondataavailable = (event) => { if (event.data.size > 0) voiceChunksRef.current.push(event.data) }
      recorder.onstop = () => {
        const roomID = voiceRoomIDRef.current
        const chunks = voiceChunksRef.current
        voiceChunksRef.current = []
        voiceRecorderRef.current = null
        voiceStreamRef.current?.getTracks().forEach((track) => track.stop())
        voiceStreamRef.current = null
        if (voiceTimeoutRef.current !== null) window.clearTimeout(voiceTimeoutRef.current)
        voiceTimeoutRef.current = null
        if (chunks.length === 0) {
          setVoiceCaptureStatus(null)
          return
        }
        setVoiceCaptureStatus('processing')
        void (async () => {
          const audioContext = new AudioContext()
          try {
            const decoded = await audioContext.decodeAudioData(await new Blob(chunks).arrayBuffer())
            const wav = encodeVoiceNoteWav(decoded)
            const durationMilliseconds = Math.round(decoded.duration * 1000)
            const file = new File([wav], `voice-note-${Date.now()}.wav`, { type: 'audio/wav' })
            await sendAttachmentFiles([file], roomID, durationMilliseconds)
          } catch (recordError) {
            setError(describeError(recordError))
          } finally {
            await audioContext.close().catch(() => {})
            setVoiceCaptureStatus(null)
          }
        })()
      }
      recorder.onerror = () => {
        voiceStreamRef.current?.getTracks().forEach((track) => track.stop())
        voiceStreamRef.current = null
        voiceRecorderRef.current = null
        if (voiceTimeoutRef.current !== null) window.clearTimeout(voiceTimeoutRef.current)
        voiceTimeoutRef.current = null
        setVoiceCaptureStatus(null)
        setError('Microphone recording failed.')
      }
      recorder.start(250)
      setError('')
      setVoiceCaptureStatus('recording')
      voiceTimeoutRef.current = window.setTimeout(stopVoiceNoteRecording, 4 * 60 * 1000)
    } catch (recordError) {
      stream?.getTracks().forEach((track) => track.stop())
      voiceStreamRef.current = null
      voiceRecorderRef.current = null
      setError(describeError(recordError))
    }
  }

  function stopVoiceNoteRecording() {
    const recorder = voiceRecorderRef.current
    if (!recorder || recorder.state === 'inactive') return
    if (voiceTimeoutRef.current !== null) window.clearTimeout(voiceTimeoutRef.current)
    voiceTimeoutRef.current = null
    recorder.stop()
  }

  function handleComposerPaste(event: ClipboardEvent<HTMLTextAreaElement>) {
    const imageItem = Array.from(event.clipboardData.items).find((item) => item.type.startsWith('image/'))
    const image = imageItem?.getAsFile()
    if (!image) return
    event.preventDefault()
    const extension = image.type === 'image/jpeg' ? 'jpg' : image.type.split('/')[1]?.split('+')[0] || 'png'
    const file = image.name ? image : new File([image], `pasted-image-${Date.now()}.${extension}`, { type: image.type })
    void sendAttachmentFiles([file])
  }

  function handleFileDragEnter(event: DragEvent<HTMLElement>) {
    if (!Array.from(event.dataTransfer.types).includes('Files')) return
    event.preventDefault()
    dragDepthRef.current++
    setIsDraggingFiles(true)
  }

  function handleFileDragOver(event: DragEvent<HTMLElement>) {
    if (!Array.from(event.dataTransfer.types).includes('Files')) return
    event.preventDefault()
    event.dataTransfer.dropEffect = 'copy'
  }

  function handleFileDragLeave(event: DragEvent<HTMLElement>) {
    if (!Array.from(event.dataTransfer.types).includes('Files')) return
    if (event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)) return
    dragDepthRef.current = 0
    setIsDraggingFiles(false)
  }

  function handleFileDrop(event: DragEvent<HTMLElement>) {
    const files = Array.from(event.dataTransfer.files)
    if (files.length === 0) return
    event.preventDefault()
    dragDepthRef.current = 0
    setIsDraggingFiles(false)
    void sendAttachmentFiles(files)
  }

  async function sendReaction(message: Message, emoji: string) {
    if (!selectedRoomID || !message.id || message.id.startsWith('~')) return
    setReactingMessageID('')
    try {
      await AppService.React(selectedRoomID, message.id, emoji)
      setHistoryRevision((revision) => revision + 1)
    } catch (reactionError) {
      setError(describeError(reactionError))
    }
  }

  async function redactMessage() {
    if (!selectedRoomID || !redactTarget?.id) return
    setBusy(true)
    try {
      await AppService.RedactMessage(selectedRoomID, redactTarget.id)
      setRedactTarget(null)
      setHistoryRevision((revision) => revision + 1)
    } catch (redactError) {
      setError(describeError(redactError))
    } finally {
      setBusy(false)
    }
  }

  async function retryMessage(message: Message) {
    if (!message.transactionId) return
    try {
      await AppService.RetryMessage(message.transactionId)
      setHistoryRevision((revision) => revision + 1)
    } catch (retryError) {
      setError(describeError(retryError))
    }
  }

  function openRoomInfo() {
    if (selectedRoomID) setRoomInfoOpen(true)
  }

  const emojiOptions: Array<[string, string]> = emojiMatch && preferenceBoolean(preferences, 'emoji.autocomplete')
    && composeText.slice(emojiMatch.start, emojiMatch.end).toLowerCase() === `:${emojiMatch.query}`
    ? suggestEmoji(emojiMatch.query)
    : []

  function updateMentionQuery(value: string, cursor: number) {
    const match = /(^|\s)@([^\s@]*)$/.exec(value.slice(0, cursor))
    if (!match) {
      setMentionStart(-1)
      setMentionQuery('')
      return
    }
    setMentionStart(cursor - match[0].length + match[1].length)
    setMentionEnd(cursor)
    setMentionQuery(match[2])
    setMentionActiveIndex(0)
  }

  function applyMention(member: Member) {
    if (mentionStart < 0) return
    const token = `@${member.displayName || member.id}`
    const suffix = composeText.slice(mentionEnd)
    const separator = suffix.startsWith(' ') ? '' : ' '
    const nextText = composeText.slice(0, mentionStart) + token + separator + suffix
    setComposeText(nextText)
    if (!editTarget && selectedRoomID) draftsRef.current.set(selectedRoomID, nextText)
    setMentionBindings((current) => [
      ...current.filter((binding) => binding.token !== token && nextText.includes(binding.token)),
      { token, userID: member.id },
    ])
    setMentionStart(-1)
    setMentionQuery('')
    requestAnimationFrame(() => {
      const input = composerInputRef.current
      if (!input) return
      const caret = mentionStart + token.length + separator.length
      input.focus()
      input.setSelectionRange(caret, caret)
    })
  }

  function updateEmojiQuery(value: string, cursor: number) {
    setEmojiMatch(emojiQueryAt(value, cursor))
    setEmojiActiveIndex(0)
  }

  const aiEndpoint = preferenceString(preferences, 'ai.endpoint', '')
  const aiEnabled = aiEndpoint !== ''
  const aiUnavailableTitle = 'Set an AI endpoint in Settings > Integrations'

  function recentConversation(limit = 50): AIMessage[] {
    const lines = messages
      .filter((message) => message.body && !message.redacted)
      .slice(-limit)
      .map((message) => `${message.senderName || message.sender}: ${message.body}`)
    return lines.length > 0 ? [{ role: 'user', content: `Recent messages in this room:\n${lines.join('\n')}` }] : []
  }

  async function runAIRequest(title: string, request: AIMessage[]) {
    const requestID = aiRequestRef.current + 1
    aiRequestRef.current = requestID
    setAIPanel({ title, busy: true, result: '', error: '' })
    try {
      const reply = await AppService.AIComplete(request)
      if (aiRequestRef.current === requestID) setAIPanel((current) => current ? { ...current, busy: false, result: reply } : current)
    } catch (aiError) {
      if (aiRequestRef.current === requestID) setAIPanel((current) => current ? { ...current, busy: false, error: describeError(aiError) } : current)
    }
  }

  function askAI() {
    const question = composeText.trim()
    if (!question) {
      setError('Type a question in the composer, then choose Ask AI.')
      return
    }
    void runAIRequest('Ask AI', [
      { role: 'system', content: 'You are a helpful assistant inside a chat app. Use the recent room messages when they help answer the question. Reply in plain text.' },
      ...recentConversation(),
      { role: 'user', content: question },
    ])
  }

  function summarizeRoom() {
    const transcript = recentConversation()
    if (transcript.length === 0) {
      setError('There are no messages to summarize yet.')
      return
    }
    void runAIRequest('Summarize room', [
      { role: 'system', content: 'Summarize the conversation in a few short bullet points. Stick to what the messages say.' },
      ...transcript,
    ])
  }

  function translateMessage(message: Message) {
    const language = preferenceString(preferences, 'ai.translate_language', '').trim() || 'English'
    void runAIRequest('Translate message', [
      { role: 'system', content: `Translate the user's message into ${language}. Reply with only the translated text.` },
      { role: 'user', content: message.body },
    ])
  }

  function useAIResultAsDraft() {
    if (!aiPanel?.result || editTarget) return
    setComposeDraft(aiPanel.result)
    setAIPanel(null)
    composerInputRef.current?.focus()
  }

  function setComposeDraft(text: string) {
    setComposeText(text)
    if (!editTarget && selectedRoomID) {
      if (text) draftsRef.current.set(selectedRoomID, text)
      else draftsRef.current.delete(selectedRoomID)
    }
  }

  function applyEmojiSuggestion(emoji: string) {
    if (!emojiMatch) return
    const { text, caret } = insertEmoji(composeText, emojiMatch.start, emojiMatch.end, emoji)
    setComposeDraft(text)
    setEmojiMatch(null)
    requestAnimationFrame(() => {
      const input = composerInputRef.current
      if (!input) return
      input.focus()
      input.setSelectionRange(caret, caret)
    })
  }

  function applyFormatting(marker: string) {
    const input = composerInputRef.current
    const start = input?.selectionStart ?? composeText.length
    const end = input?.selectionEnd ?? composeText.length
    const result = wrapSelection(composeText, start, end, marker)
    setComposeDraft(result.text)
    requestAnimationFrame(() => {
      const target = composerInputRef.current
      if (!target) return
      target.focus()
      target.setSelectionRange(result.selectionStart, result.selectionEnd)
    })
  }

  async function leaveSelectedRoom() {
    if (!selectedRoomID || leavingRoom) return
    const roomID = selectedRoomID
    const remainingRooms = rooms.filter((room) => room.id !== roomID)
    setLeaveRoomError('')
    setLeavingRoom(true)
    try {
      await AppService.LeaveRoom(roomID)
      setSnapshot((current) => current ? { ...current, rooms: (current.rooms ?? []).filter((room) => room.id !== roomID) } : current)
      setSelectedRoomID((current) => current === roomID ? remainingRooms[0]?.id ?? '' : current)
      setRoomInfoOpen(false)
      setLeaveConfirmOpen(false)
    } catch (leaveError) {
      setLeaveRoomError(describeError(leaveError))
    } finally {
      setLeavingRoom(false)
    }
  }

  function handleComposerKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'ArrowUp' && !composeText && preferenceBoolean(preferences, 'composer.arrow_up_focus_timeline')) {
      event.preventDefault()
      timelineRef.current?.focus()
      return
    }
    if (mentionStart >= 0 && mentionOptions.length > 0) {
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setMentionActiveIndex((index) => (index + 1) % mentionOptions.length)
        return
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setMentionActiveIndex((index) => (index - 1 + mentionOptions.length) % mentionOptions.length)
        return
      }
      if ((event.key === 'Enter' && !event.shiftKey) || event.key === 'Tab') {
        event.preventDefault()
        applyMention(mentionOptions[mentionActiveIndex] ?? mentionOptions[0])
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        setMentionStart(-1)
        return
      }
    }
    if (emojiOptions.length > 0) {
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setEmojiActiveIndex((index) => (index + 1) % emojiOptions.length)
        return
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setEmojiActiveIndex((index) => (index - 1 + emojiOptions.length) % emojiOptions.length)
        return
      }
      if ((event.key === 'Enter' && !event.shiftKey) || event.key === 'Tab') {
        event.preventDefault()
        applyEmojiSuggestion((emojiOptions[emojiActiveIndex] ?? emojiOptions[0])[1])
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        setEmojiMatch(null)
        return
      }
    }
    const ctrlEnter = preferenceBoolean(preferences, 'composer.ctrl_enter')
    if (event.key === 'Enter' && !event.shiftKey && (!ctrlEnter || event.ctrlKey)) {
      event.preventDefault()
      event.currentTarget.form?.requestSubmit()
    }
  }

  if (!snapshot) {
    return (
      <main className="boot-screen">
        <div className="boot-mark"><MessageCircleMore size={20} /></div>
        <span>Pinger</span>
        {error && <p role="alert">{error}</p>}
      </main>
    )
  }

  if (!snapshot.loggedIn) {
    return (
      <main className="auth-screen">
        <section className="auth-panel" aria-labelledby="auth-title">
          <div className="auth-brand">
            <span className="brand-glyph"><MessageCircleMore size={22} strokeWidth={2.1} /></span>
            <span>Pinger</span>
          </div>
          <div className="auth-heading">
            <span className="eyebrow">MATRIX ACCOUNT</span>
            <h1 id="auth-title">Sign in</h1>
            <p>Use your homeserver credentials to continue.</p>
          </div>
          <form className="auth-form" onSubmit={submitLogin}>
            <label>
              <span>Homeserver</span>
              <input value={homeserver} onChange={(event) => setHomeserver(event.target.value)} type="url" autoComplete="url" required />
            </label>
            <label>
              <span>Username</span>
              <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" autoCapitalize="none" required />
            </label>
            <label>
              <span>Password</span>
              <input value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete="current-password" required />
            </label>
            {error && <div className="form-error" role="alert"><CircleAlert size={16} />{error}</div>}
            <button className="primary-button auth-submit" type="submit" disabled={busy}>
              {busy ? 'Connecting…' : 'Continue'}
              {!busy && <ArrowDown className="button-arrow" size={17} />}
            </button>
          </form>
          <div className="auth-footnote"><LockKeyhole size={14} /><span>Session and encryption keys stay on this device.</span></div>
        </section>
        <div className="auth-side" aria-hidden="true">
          <div className="auth-side-mark">P.</div>
          <div className="auth-side-grid" />
          <div className="auth-side-bottom"><span>PINGER</span><span>01 / 01</span></div>
        </div>
      </main>
    )
  }

  if (snapshot.needsVerification && !snapshot.syncReady && snapshot.connectionState !== 'offline') {
    return (
      <main className="boot-screen">
        <div className="boot-mark"><ShieldCheck size={20} /></div>
        <span>Checking device verification…</span>
      </main>
    )
  }

  if (snapshot.needsVerification) {
    return (
      <main className="auth-screen">
        <section className="auth-panel" aria-labelledby="verify-title">
          <div className="auth-brand">
            <span className="brand-glyph"><ShieldCheck size={21} /></span>
            <span>Pinger</span>
          </div>
          <div className="auth-heading">
            <span className="eyebrow">ENCRYPTION SETUP</span>
            <h1 id="verify-title">Verify this device</h1>
            <p>Enter your recovery key or security passphrase.</p>
          </div>
          <form className="auth-form" onSubmit={submitVerification}>
            <label>
              <span>Recovery key or passphrase</span>
              <input value={recoveryKey} onChange={(event) => setRecoveryKey(event.target.value)} type="password" autoComplete="off" required />
            </label>
            {error && <div className="form-error" role="alert"><CircleAlert size={16} />{error}</div>}
            <button className="primary-button auth-submit" type="submit" disabled={busy}>
              {busy ? 'Verifying…' : 'Verify device'}
              {!busy && <ShieldCheck size={17} />}
            </button>
          </form>
          <div className="auth-footnote"><LockKeyhole size={14} /><span>Your key is used locally to verify encryption.</span></div>
        </section>
        <div className="auth-side" aria-hidden="true">
          <div className="auth-side-mark">P.</div>
          <div className="auth-side-grid" />
          <div className="auth-side-bottom"><span>PINGER</span><span>01 / 01</span></div>
        </div>
      </main>
    )
  }

  return (
    <main
      className="app-shell"
      onKeyDown={handleApplicationKeyDown}
      onDragEnter={handleFileDragEnter}
      onDragOver={handleFileDragOver}
      onDragLeave={handleFileDragLeave}
      onDrop={handleFileDrop}
      data-theme={themeChoice}
      data-mobile-pane={mobilePane}
      data-compact={preferenceBoolean(preferences, 'appearance.compact')}
      data-message-animations={preferenceBoolean(preferences, 'appearance.message_animations', true)}
      data-show-avatars={preferenceBoolean(preferences, 'appearance.show_avatars', true)}
      data-font-size={preferenceString(preferences, 'appearance.font_size', 'Medium').toLowerCase()}
      data-wallpaper={preferenceString(preferences, 'appearance.chat_wallpaper', 'None').toLowerCase()}
      data-room-previews={preferenceBoolean(preferences, 'sidebar.show_previews', true)}
      data-unread-counts={preferenceBoolean(preferences, 'sidebar.show_unread_counts', true)}
      data-compact-chat-list={preferenceBoolean(preferences, 'sidebar.compact_chat_list')}
      data-pointer-cursor={preferenceBoolean(preferences, 'appearance.pointer_cursor', true)}
      data-balanced-text={preferenceBoolean(preferences, 'appearance.balanced_text')}
      style={{
        '--own-bubble': safeBubbleColor,
        '--app-zoom': `${appZoom}%`,
        '--accent': accentPalette.base,
        '--accent-bright': accentPalette.bright,
        '--forest': accentPalette.base,
        '--forest-bright': accentPalette.bright,
        '--green': accentPalette.base,
        '--green-soft': accentPalette.soft,
      } as React.CSSProperties}
    >
      <aside className={`sidebar ${preferenceBoolean(preferences, 'sidebar.transparent') ? 'is-transparent' : ''}`}>
        <div className="sidebar-brand">
          <span className="brand-glyph"><MessageCircleMore size={19} strokeWidth={2.1} /></span>
          <span>Pinger</span>
          <span className="brand-edition">DESKTOP</span>
        </div>
        <div className="room-list-heading">
          <div>
            <span className="eyebrow">WORKSPACE</span>
            <h1>Messages</h1>
          </div>
        </div>
        <label className="room-search">
          <Search size={16} />
          <input ref={roomSearchInputRef} value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Find a room" aria-label="Find a room" />
          {search && <button type="button" aria-label="Clear room search" onClick={() => setSearch('')}>×</button>}
        </label>
        <div className="room-filters" role="group" aria-label="Filter rooms">
          {inboxAccountCount > 1 && (
            <select className="room-account-filter" aria-label="Filter rooms by account" value={effectiveAccountScope} onChange={(event) => setInboxAccountScope(event.target.value)}>
              <option value="all">All accounts</option>
              {(snapshot?.accounts ?? []).map((account) => <option key={account.id} value={account.id}>{account.profile.displayName || account.profile.userId}</option>)}
            </select>
          )}
          <div className="room-scope-filters" role="group" aria-label="Room type">
            <button type="button" className={roomScope === 'all' ? 'is-active' : ''} aria-pressed={roomScope === 'all'} onClick={() => setRoomScope('all')}>All</button>
            <button type="button" className={roomScope === 'direct' ? 'is-active' : ''} aria-pressed={roomScope === 'direct'} onClick={() => setRoomScope('direct')}>DMs</button>
            <button type="button" className={roomScope === 'group' ? 'is-active' : ''} aria-pressed={roomScope === 'group'} onClick={() => setRoomScope('group')}>Groups</button>
            <button type="button" className={roomScope === 'archived' ? 'is-active' : ''} aria-label="Archived rooms" aria-pressed={roomScope === 'archived'} onClick={() => { setRoomScope('archived'); setRoomRefinements({ unread: false, unanswered: false, favorite: false }) }}><Archive size={13} /></button>
          </div>
          <div className="room-refinement-filters" role="group" aria-label="Room refinements">
            <button type="button" className={roomRefinements.unread ? 'is-active' : ''} aria-label="Unread rooms" aria-pressed={roomRefinements.unread} title="Unread" onClick={() => setRoomRefinements((current) => ({ ...current, unread: !current.unread }))}><CircleDot size={14} /></button>
            <button type="button" className={roomRefinements.unanswered ? 'is-active' : ''} aria-label="Needs reply" aria-pressed={roomRefinements.unanswered} aria-keyshortcuts={answeredShortcut} title={`Needs reply (press ${answeredShortcut} to mark answered)`} onClick={() => setRoomRefinements((current) => ({ ...current, unanswered: !current.unanswered }))} onKeyDown={(event) => handleMarkAnsweredKeyDown(event, selectedRoomID)}>
              <Reply size={14} />{unansweredCount > 0 && <span>{unansweredCount}</span>}
            </button>
            <button type="button" className={roomRefinements.favorite ? 'is-active' : ''} aria-label="Pinned rooms" aria-pressed={roomRefinements.favorite} title="Pinned" onClick={() => setRoomRefinements((current) => ({ ...current, favorite: !current.favorite }))}><Star size={14} /></button>
          </div>
        </div>
        <div className="room-section-label"><span>{roomScope === 'archived' ? 'ARCHIVED' : 'ROOMS'}</span><span className="room-section-actions">{roomScope !== 'archived' && <button className="room-archive-all" type="button" aria-label="Archive all read rooms" title="Archive all read rooms" disabled={readRoomCount === 0} onClick={() => void archiveAllReadRooms()}><Archive size={13} /></button>}{filteredRooms.length.toString().padStart(2, '0')}</span></div>
        <nav className="room-list" aria-label="Rooms">
          {filteredRooms.map((room, index) => (
            <RoomButton
              key={`${room.accountId}:${room.id}`}
              room={room}
              active={room.id === selectedRoomID && isActiveAccountRoom(room)}
              lastMessageByMe={isActiveAccountRoom(room) && Boolean(room.lastEventSender && room.lastEventSender === snapshot.profile.userId)}
              use24Hour={preferenceBoolean(preferences, 'appearance.time_24_hour')}
              showNetwork={preferenceBoolean(preferences, 'sidebar.network_icons')}
              onSelect={() => openInboxRoom(room)}
              onKeyDown={(event) => handleRoomKeyDown(event, room, index)}
              onContextMenu={(event) => {
                event.preventDefault()
                if (isActiveAccountRoom(room)) openRoomContextMenu(room.id, event.clientX, event.clientY)
              }}
            />
          ))}
          {filteredRooms.length === 0 && (
            <div className="room-empty">{scopedRooms.length === 0 ? 'No rooms yet' : search ? 'No matching rooms' : roomScope === 'archived' ? 'No archived rooms' : 'No rooms match these filters'}</div>
          )}
        </nav>
        {roomContextMenu && <div className="room-context-menu" role="menu" style={{ left: roomContextMenu.x, top: roomContextMenu.y }}>
          {rooms.find((room) => room.id === roomContextMenu.roomID)?.unanswered && <button type="button" role="menuitem" aria-keyshortcuts={answeredShortcut} onClick={() => void markRoomAnswered(roomContextMenu.roomID)} onKeyDown={(event) => handleMarkAnsweredKeyDown(event, roomContextMenu.roomID)}>
            <Check size={15} /><span>Remove from Needs reply</span><kbd>{answeredShortcut}</kbd>
          </button>}
          {(() => {
            const room = rooms.find((candidate) => candidate.id === roomContextMenu.roomID)
            if (!room) return null
            const isUnread = room.unread > 0 || room.markedUnread
            return <button type="button" role="menuitem" onClick={() => void (isUnread ? markRoomRead(room.id) : markRoomUnread(room.id))}>
              {isUnread ? <><Check size={15} /><span>Mark room read</span></> : <><CircleDot size={15} /><span>Mark room unread</span></>}
            </button>
          })()}
          <button type="button" role="menuitem" onClick={() => void setRoomArchived(roomContextMenu.roomID, !preferenceBoolean(preferences, archivedPreferenceKey(roomContextMenu.roomID)))}>
            {preferenceBoolean(preferences, archivedPreferenceKey(roomContextMenu.roomID)) ? <ArchiveRestore size={15} /> : <Archive size={15} />}
            <span>{preferenceBoolean(preferences, archivedPreferenceKey(roomContextMenu.roomID)) ? 'Restore room' : 'Archive room'}</span>
          </button>
        </div>}
        <div className="sidebar-profile">
          <button className="account-switcher-trigger" type="button" aria-label="Manage Matrix accounts" title="Manage Matrix accounts" onClick={() => { setAccountError(''); setAddingAccount(false); setAccountManagerOpen(true) }}>
            <Avatar uri={snapshot.profile.avatarUri} label={snapshot.profile.displayName || snapshot.profile.userId} className="profile-avatar" />
            <span className="profile-copy">
              <strong>{snapshot.profile.displayName || snapshot.profile.userId}</strong>
              <span>{snapshot.profile.userId}</span>
            </span>
            <ChevronDown size={14} />
          </button>
          <button className="sidebar-icon" type="button" aria-label="Settings" title="Settings" onClick={() => setSettingsOpen(true)}>
            <Settings size={16} />
          </button>
        </div>
      </aside>

      <section className="conversation" aria-label="Conversation">
        <header className="conversation-header">
          <button className="mobile-back-button icon-button" type="button" aria-label="Back to rooms" onClick={() => setMobilePane('rooms')}>
            <ArrowLeft size={17} />
          </button>
          {preferenceBoolean(preferences, 'privacy.incognito') && <span className="incognito-indicator" role="status" aria-label="Incognito mode is on" title="Incognito mode is on"><EyeOff size={14} /><span>Incognito</span></span>}
          {selectedRoom ? (
            <>
              <Avatar uri={selectedRoomAvatar} label={selectedRoom.name || selectedRoom.id} className="conversation-room-avatar" />
              <div className="conversation-title">
                <h2>{selectedRoom.name || selectedRoom.id}</h2>
                <span>{selectedRoom.direct ? 'Direct message' : selectedRoom.topic || selectedRoom.id}</span>
              </div>
              <div className="conversation-actions">
                <button className="icon-button scheduled-jobs-button" type="button" aria-label="Scheduled messages and reminders" title="Scheduled messages and reminders" onClick={openScheduledJobs}>
                  <Clock3 size={16} />{scheduledJobCount > 0 && <span className="scheduled-job-count">{scheduledJobCount}</span>}
                </button>
                {selectedRoom.unanswered && <button className="icon-button" type="button" aria-label="Mark room answered" title="Mark answered" onClick={() => void markRoomAnswered(selectedRoom.id)}><Check size={16} /></button>}
                <button className="icon-button" type="button" aria-label="Room details" title="Room details" onClick={() => void openRoomInfo()}>
                  <UsersRound size={16} />
                </button>
                <button
                  className={`icon-button ${selectedRoom.favorite ? 'is-selected' : ''}`}
                  type="button"
                  aria-label={selectedRoom.favorite ? 'Unpin room' : 'Pin room'}
                  aria-pressed={selectedRoom.favorite}
                  title={selectedRoom.favorite ? 'Unpin room' : 'Pin room'}
                  onClick={() => void setPreference(`room.favorite.${selectedRoom.id}`, !selectedRoom.favorite)}
                >
                  <Star size={16} fill={selectedRoom.favorite ? 'currentColor' : 'none'} />
                </button>
                <button
                  className={`icon-button ${selectedRoom.muted ? 'is-selected' : ''}`}
                  type="button"
                  aria-label={selectedRoom.muted ? 'Unmute room' : 'Mute room'}
                  aria-pressed={selectedRoom.muted}
                  title={selectedRoom.muted ? 'Unmute room' : 'Mute room'}
                  onClick={() => void setPreference(`room.muted.${selectedRoom.id}`, !selectedRoom.muted)}
                >
                  {selectedRoom.muted ? <BellOff size={16} /> : <Bell size={16} />}
                </button>
                <button className="icon-button" type="button" aria-label="Search messages" title="Search all messages" onClick={() => setSearchOpen(true)}>
                  <Search size={16} />
                </button>
              </div>
            </>
          ) : (
            <>
              <div className="conversation-title">
                <h2>Matrix</h2>
                <span>Choose a room to start chatting</span>
              </div>
              <div className="conversation-actions">
                <button className="icon-button scheduled-jobs-button" type="button" aria-label="Scheduled messages and reminders" title="Scheduled messages and reminders" onClick={openScheduledJobs}>
                  <Clock3 size={16} />{scheduledJobCount > 0 && <span className="scheduled-job-count">{scheduledJobCount}</span>}
                </button>
                <button className="icon-button" type="button" aria-label="Search messages" title="Search all messages" onClick={() => setSearchOpen(true)}>
                  <Search size={16} />
                </button>
              </div>
            </>
          )}
        </header>

        {typingUsers.length > 0 && <div className="typing-indicator" aria-live="polite"><UsersRound size={13} />{typingUsers.length === 1 ? `${typingUsers[0]} is typing…` : `${typingUsers.length} people are typing…`}</div>}

        {searchOpen && (
          <section className="message-search-panel" aria-label="Search messages across rooms">
            <div className="message-search-heading">
              <h3>Search messages</h3>
              <button className="icon-button" type="button" aria-label="Close message search" title="Close search" onClick={closeMessageSearch}>
                <X size={16} />
              </button>
            </div>
            <form className="message-search-form" onSubmit={submitMessageSearch}>
              <input
                autoFocus
                value={messageSearch}
                onChange={(event) => setMessageSearch(event.target.value)}
                placeholder="Search all chats"
                aria-label="Search local message history"
              />
              <button className="search-submit" type="submit" aria-label="Search" disabled={!messageSearch.trim() || searching}>
                <Search size={16} />
              </button>
            </form>
            <details className="message-search-filters">
              <summary>Filters</summary>
              <div className="message-search-filter-grid">
                <label>
                  <span>Chat</span>
                  <select aria-label="Filter by room" value={searchRoomID} onChange={(event) => setSearchRoomID(event.target.value)}>
                    <option value="">All chats</option>
                    {rooms.map((room) => <option key={room.id} value={room.id}>{room.name || room.id}</option>)}
                  </select>
                </label>
                <label>
                  <span>Sender</span>
                  <input aria-label="Filter by sender" value={searchSender} onChange={(event) => setSearchSender(event.target.value)} placeholder="@user:server" />
                </label>
                <label>
                  <span>From</span>
                  <input aria-label="Search from date" type="date" value={searchFrom} onChange={(event) => setSearchFrom(event.target.value)} />
                </label>
                <label>
                  <span>To</span>
                  <input aria-label="Search to date" type="date" value={searchTo} onChange={(event) => setSearchTo(event.target.value)} />
                </label>
              </div>
            </details>
            {searchStatus && <p className="message-search-status" role="status">{searchStatus}</p>}
            <div className="message-search-results" aria-live="polite">
              {searching && <div className="message-search-empty"><span className="loading-mark" />Searching local history</div>}
              {!searching && searchResults.map((result, index) => {
                const room = rooms.find((candidate) => candidate.id === result.roomID)
                const message = result.message
                return <button className="message-search-result" key={`${result.roomID}:${message.id || message.transactionId || index}`} type="button" aria-label={`Open message in ${room?.name || result.roomID}`} onClick={() => openSearchResult(result)}>
                  <span className="message-search-room">{room?.name || result.roomID}{room?.bridge && <small>{room.bridge}</small>}</span>
                  <div className="message-search-meta">
                    <strong>{message.senderName || message.sender}</strong>
                    {message.timestamp > 0 && <time>{new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(message.timestamp))}</time>}
                  </div>
                  <p>{message.body}</p>
                </button>
              })}
              {!searching && searchNextBatch && <button className="message-search-more" type="button" onClick={() => void loadMoreSearchResults()}>Load more results</button>}
            </div>
          </section>
        )}

        {error && <div className="inline-error" role="alert"><CircleAlert size={15} />{error}<button aria-label="Dismiss error" onClick={() => setError('')}>×</button></div>}

        <div className="timeline" key={selectedRoomID} ref={timelineRef} role="region" aria-label="Message timeline" tabIndex={0} onKeyDown={handleTimelineKeyDown} onScroll={(event) => {
          const element = event.currentTarget
          stickToBottomRef.current = isNearBottom(element)
          setShowJumpToLatest(!stickToBottomRef.current)
          // Infinite scroll: reaching the top of the loaded history fetches the
          // next older page. The refs keep a burst of scroll events from
          // starting several fetches before `loadingOlder` commits.
          if (hasOlderRef.current && !loadingOlderRef.current && isNearTop(element)) {
            void loadOlderMessages()
          }
        }}>
          {!selectedRoom && <div className="timeline-empty"><MessageCircleMore size={25} /><span>Select a room</span></div>}
          {selectedRoom && loadingHistory && messages.length === 0 && <div className="timeline-empty"><span className="loading-mark" />Loading messages</div>}
          {selectedRoom && !loadingHistory && visibleMessages.length === 0 && <div className="timeline-empty"><MessageCircleMore size={25} /><span>{messages.length > 0 ? 'Deleted messages are hidden' : hasOlder ? 'No messages in the past 24 hours' : 'No messages yet'}</span></div>}
          {selectedRoom && hasOlder && (
            <button className="load-older" type="button" onClick={loadOlderMessages} disabled={loadingOlder}>
              {loadingOlder ? <><span className="loading-mark" />Loading older messages</> : 'Load older messages'}
            </button>
          )}
          {visibleMessages.map((message, index) => {
            const previous = index > 0 ? visibleMessages[index - 1] : null
            const grouped = Boolean(previous && !previous.redacted && previous.sender === message.sender && message.timestamp - previous.timestamp < 5 * 60_000 && isSameDay(previous.timestamp, message.timestamp))
            const showDayDivider = !previous || !isSameDay(previous?.timestamp ?? 0, message.timestamp)
            const displayBody = displayMessageBody(message)
            return (
              <Fragment key={message.id || message.transactionId}>
                {showDayDivider && message.timestamp > 0 && <div className="day-divider" aria-label={formatDayLabel(message.timestamp)}><span>{formatDayLabel(message.timestamp)}</span></div>}
                <article className={`message-row ${message.own ? 'is-own' : ''} ${grouped ? 'is-grouped' : 'is-first'}`} data-event-id={message.id || message.transactionId} tabIndex={message.id || message.transactionId ? -1 : undefined} style={{ '--message-index': Math.min(index, 12) } as React.CSSProperties} onDoubleClick={() => handleMessageDoubleClick(message)} onContextMenu={(event) => {
                  if (!selectedRoomID) return
                  event.preventDefault()
                  openMessageContextMenu(message, event.clientX, event.clientY)
                }} onKeyDown={(event) => {
                  // Same affordance as the room list: Shift+F10 or the menu key
                  // opens the actions for keyboard users.
                  if (event.key !== 'ContextMenu' && !(event.key === 'F10' && event.shiftKey)) return
                  event.preventDefault()
                  const bounds = event.currentTarget.getBoundingClientRect()
                  openMessageContextMenu(message, bounds.left + 8, bounds.bottom)
                }}>
                  <Avatar uri={grouped ? '' : message.avatar} label={message.senderName || message.sender} className="message-avatar" style={grouped ? undefined : avatarStyle(message.senderName || message.sender)} aria-hidden={grouped} />
                  <div className="message-body-wrap">
                    {!grouped && <div className="message-meta">
                      <strong>{message.senderName || message.sender}</strong>
                      {message.timestamp > 0 && preferenceBoolean(preferences, 'appearance.show_timestamps', true) && <time>{formatTimelineTime(message.timestamp, preferences)}</time>}
                      {message.edited && <span className="edited-mark" title={message.editHistory?.length ? message.editHistory.join('\n') : 'Edited message'}>edited{(message.editHistory?.length ?? 0) > 0 && ` (${message.editHistory?.length} prior)`}</span>}
                      {message.encrypted && <span title="Encrypted message"><LockKeyhole size={11} /></span>}
                    </div>}
                    {message.replyToPreview && <button className="reply-preview" type="button" disabled={!message.replyToEventId} title="Jump to quoted message" onClick={() => focusTimelineMessage(message.replyToEventId)}>
                      <span className="reply-preview-sender">{message.replyToSender || 'Reply'}</span>
                      <span className="reply-preview-text">{message.replyToPreview}</span>
                    </button>}
                    <Suspense fallback={<div className="message-body">{displayBody}</div>}>
                      <MarkdownMessage body={displayBody} />
                    </Suspense>
                    {message.mediaUri && !message.redacted && !message.decryptionError && (
                      <MessageMedia
                        message={message}
                        autoPlayNext={autoPlayNextMessageId === message.id}
                        onAudioEnded={() => {
                          if (!preferenceBoolean(preferences, 'voice.autoplay_next')) return
                          const messagesList = visibleMessages
                          const idx = messagesList.findIndex((m) => m.id === message.id)
                          for (let i = idx + 1; i < messagesList.length; i++) {
                            const m = messagesList[i]
                            const mime = m.mediaMime || ''
                            if (m.mediaUri && !m.redacted && !m.decryptionError && (mime.startsWith('audio/') || m.mediaType === 'm.audio')) {
                              setAutoPlayNextMessageId(m.id)
                              return
                            }
                          }
                          setAutoPlayNextMessageId(null)
                        }}
                      />
                    )}
                    {Object.keys(message.reactions ?? {}).length > 0 && <div className="message-reactions">
                      {Object.entries(message.reactions ?? {}).map(([emoji, count]) => (
                        <button key={emoji} type="button" className={`reaction-chip ${message.reacted?.[emoji] ? 'is-reacted' : ''}`} aria-pressed={message.reacted?.[emoji] ?? false} onClick={() => void sendReaction(message, emoji)}>{emoji} {typeof count === 'number' ? count : 0}</button>
                      ))}
                    </div>}
                    {message.sendError && message.sendError !== 'not sent' && <span className="message-error">{message.sendError}</span>}
                    {message.sending && !message.sendError && <span className="message-pending">Sending…</span>}
                    {reactingMessageID === message.id && <div className="reaction-picker" aria-label="Quick reactions">
                      {quickReactionOptions(preferences).map((emoji) => <button key={emoji} type="button" aria-label={`React with ${emoji}`} onClick={() => void sendReaction(message, emoji)}>{emoji}</button>)}
                    </div>}
                  </div>
                </article>
              </Fragment>
            )
          })}
        </div>
        {messageContextMenu && <div className="message-context-menu" role="menu" aria-label="Message actions" style={{ left: messageContextMenu.x, top: messageContextMenu.y }}>
          {messageContextMenuItems(messageContextMenu.message).map((item) => (
            <button
              key={item.key}
              type="button"
              role="menuitem"
              className={item.danger ? 'is-danger' : undefined}
              aria-label={item.ariaLabel}
              disabled={item.disabled}
              onClick={() => { setMessageContextMenu(null); item.run() }}
            >
              {item.icon}<span>{item.label}</span>
            </button>
          ))}
        </div>}
        {selectedRoom && showJumpToLatest && <button className="jump-to-latest" type="button" onClick={jumpToLatest}>
          <ArrowDown size={15} />Latest messages
        </button>}

        <form className="composer" onSubmit={submitMessage}>
          <input ref={attachmentInputRef} className="visually-hidden" type="file" aria-hidden="true" tabIndex={-1} onChange={(event) => void uploadAttachment(event)} />
          {emojiOptions.length > 0 && <div className="mention-menu emoji-menu" role="listbox" aria-label="Emoji suggestions">
            {emojiOptions.map(([name, emoji], index) => <button key={name} className={`mention-option ${index === emojiActiveIndex ? 'is-active' : ''}`} type="button" role="option" aria-selected={index === emojiActiveIndex} onMouseDown={(event) => event.preventDefault()} onMouseEnter={() => setEmojiActiveIndex(index)} onClick={() => applyEmojiSuggestion(emoji)}><span aria-hidden="true">{emoji}</span> :{name}:</button>)}
          </div>}
          {mentionOptions.length > 0 && <div className="mention-menu" role="listbox" aria-label="Mention someone">
            {mentionOptions.map((member, index) => <button key={member.id} className={`mention-option ${index === mentionActiveIndex ? 'is-active' : ''}`} type="button" role="option" aria-selected={index === mentionActiveIndex} onMouseDown={(event) => event.preventDefault()} onMouseEnter={() => setMentionActiveIndex(index)} onClick={() => applyMention(member)}>
              <span className="room-member-avatar">{initials(member.displayName || member.id)}</span><span><strong>{member.displayName || member.id}</strong><small>{member.id}</small></span>
            </button>)}
          </div>}
          {composerContextMessage && <div className={`composer-context ${editTarget ? 'is-editing' : 'is-replying'}`}>
            <span className="composer-context-icon" aria-hidden="true">{editTarget ? <Pencil size={15} /> : <Reply size={15} />}</span>
            <div className="composer-context-copy">
              <strong>{editTarget ? 'Editing message' : `Replying to ${replyTarget?.senderName || replyTarget?.sender}`}</strong>
              <span title={composerContextMessage.body}>{composerContextMessage.body}</span>
            </div>
            <button className="composer-context-link" type="button" onClick={() => focusTimelineMessage(composerContextMessage.id)}>View message</button>
            <button type="button" className="icon-button" aria-label="Cancel reply or edit" title="Cancel" onClick={() => {
              const nextDraft = editTarget ? draftsRef.current.get(selectedRoomID) ?? '' : ''
              if (!editTarget) draftsRef.current.delete(selectedRoomID)
              setReplyTarget(null)
              setEditTarget(null)
              setComposeText(nextDraft)
              setMentionBindings([])
              setMentionStart(-1)
            }}><X size={14} /></button>
          </div>}
          {pendingSendPreview !== null && <div className="composer-context is-queued" role="status">
            <div className="composer-context-copy"><strong>Message queued</strong><small>{pendingSendPreview.length > 80 ? `${pendingSendPreview.slice(0, 80)}…` : pendingSendPreview}</small></div>
            <button className="composer-context-link" type="button" onClick={cancelPendingSend}>Undo send</button>
          </div>}
          <textarea
            ref={composerInputRef}
            value={composeText}
            spellCheck={preferenceBoolean(preferences, 'composer.spellcheck', true)}
            onChange={(event) => {
              const value = event.currentTarget.value
              setComposeText(value)
              if (!editTarget && selectedRoomID) {
                if (value) draftsRef.current.set(selectedRoomID, value)
                else draftsRef.current.delete(selectedRoomID)
              }
              setMentionBindings((current) => current.filter(({ token }) => value.includes(token)))
              updateMentionQuery(value, event.currentTarget.selectionStart)
              updateEmojiQuery(value, event.currentTarget.selectionStart)
            }}
            onKeyDown={handleComposerKeyDown}
            onPaste={handleComposerPaste}
            placeholder={selectedRoom ? `Message ${selectedRoom.name || selectedRoom.id}` : 'Select a room to start'}
            aria-label="Message"
            disabled={!selectedRoom || busy || voiceCaptureStatus !== null}
            rows={1}
          />
          {preferenceBoolean(preferences, 'composer.formatting_toolbar') && <div className="composer-format-toolbar" role="toolbar" aria-label="Formatting">
            <button className="composer-format-button" type="button" aria-label="Bold" title="Bold" disabled={!selectedRoom || busy} onMouseDown={(event) => event.preventDefault()} onClick={() => applyFormatting('**')}><strong>B</strong></button>
            <button className="composer-format-button" type="button" aria-label="Italic" title="Italic" disabled={!selectedRoom || busy} onMouseDown={(event) => event.preventDefault()} onClick={() => applyFormatting('_')}><em>I</em></button>
            <button className="composer-format-button" type="button" aria-label="Inline code" title="Inline code" disabled={!selectedRoom || busy} onMouseDown={(event) => event.preventDefault()} onClick={() => applyFormatting('`')}><code>{'</>'}</code></button>
            <button className="composer-format-button" type="button" aria-label="Strikethrough" title="Strikethrough" disabled={!selectedRoom || busy} onMouseDown={(event) => event.preventDefault()} onClick={() => applyFormatting('~~')}><s>S</s></button>
          </div>}
          <div className="composer-footer">
            <div className="composer-tools">
              <button className="icon-button" type="button" aria-label="Attach file" title="Attach file" disabled={!selectedRoom || busy || voiceCaptureStatus !== null} onClick={() => attachmentInputRef.current?.click()}><Paperclip size={15} /></button>
              <button className="icon-button" type="button" aria-label={voiceCaptureStatus === 'recording' ? 'Stop and send voice note' : 'Record voice note'} title={voiceCaptureStatus === 'recording' ? 'Stop and send voice note' : 'Record voice note'} aria-pressed={voiceCaptureStatus === 'recording'} disabled={!selectedRoom || busy || voiceCaptureStatus === 'processing'} onClick={() => voiceCaptureStatus === 'recording' ? stopVoiceNoteRecording() : void startVoiceNoteRecording()}>{voiceCaptureStatus === 'recording' ? <Square size={15} /> : <Mic size={15} />}</button>
              <button className="icon-button" type="button" aria-label="Ask AI about the composer text" title={aiEnabled ? 'Ask AI about the composer text' : aiUnavailableTitle} disabled={!selectedRoom || !composeText.trim() || busy || !aiEnabled || Boolean(editTarget)} onClick={askAI}><Sparkles size={15} /></button>
              <button className="icon-button" type="button" aria-label="Summarize recent messages" title={aiEnabled ? 'Summarize recent messages' : aiUnavailableTitle} disabled={!selectedRoom || busy || !aiEnabled || messages.length === 0} onClick={summarizeRoom}><FileText size={15} /></button>
              <button className="icon-button" type="button" aria-label={replyTarget ? 'Schedule reply' : 'Schedule message'} title={replyTarget ? 'Schedule reply' : 'Schedule message'} disabled={!selectedRoom || !composeText.trim() || busy || voiceCaptureStatus !== null || Boolean(editTarget)} onClick={openScheduleMessage}><Clock3 size={15} /></button>
              <span>{voiceCaptureStatus === 'recording' ? 'Recording voice note · click stop to send' : voiceCaptureStatus === 'processing' ? 'Preparing voice note…' : selectedRoom ? busy ? 'Sending…' : preferenceBoolean(preferences, 'composer.ctrl_enter') ? 'Ctrl + Enter to send' : 'Enter to send · Shift + Enter for a new line' : ' '}</span>
            </div>
            <button className="send-button" type="submit" aria-label="Send message" disabled={!selectedRoom || !composeText.trim() || busy || voiceCaptureStatus !== null}>
              <Send size={17} />
            </button>
          </div>
        </form>
      </section>
      {scheduledNotice && <div className="scheduled-notice" role="status">
        <BellRing size={16} />
        <span>{scheduledNotice}</span>
        <button type="button" onClick={openScheduledJobs}>View reminders</button>
        <button className="icon-button" type="button" aria-label="Dismiss reminder notice" onClick={() => setScheduledNotice('')}><X size={15} /></button>
      </div>}
      {aiPanel && <div className="room-info-scrim" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setAIPanel(null) }}>
        <DialogSurface className="room-info-dialog ai-result-dialog" labelledBy="ai-result-title" onClose={() => setAIPanel(null)} onClick={(event) => event.stopPropagation()}>
          <header className="room-info-header">
            <div><span className="eyebrow">LOCAL AI</span><h2 id="ai-result-title">{aiPanel.title}</h2></div>
            <button className="icon-button" type="button" aria-label="Close AI result" onClick={() => setAIPanel(null)}><X size={16} /></button>
          </header>
          {aiPanel.busy && <p className="settings-note" role="status">Waiting for the local model…</p>}
          {aiPanel.error && <p className="settings-error" role="alert">{aiPanel.error}</p>}
          {aiPanel.result && <div className="ai-result-text">{aiPanel.result}</div>}
          <p className="settings-note">Nothing is sent. Review the result, then use it as a draft.</p>
          <footer className="room-info-footer">
            <button className="settings-command" type="button" disabled={!aiPanel.result || Boolean(editTarget)} onClick={useAIResultAsDraft}>Use as draft</button>
            <button className="settings-command" type="button" onClick={() => setAIPanel(null)}>Close</button>
          </footer>
        </DialogSurface>
      </div>}
      {accountManagerOpen && <div className="room-info-scrim" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) setAccountManagerOpen(false) }}>
        <DialogSurface className="room-info-dialog account-dialog" labelledBy="account-manager-title" onClose={() => { if (!busy) setAccountManagerOpen(false) }} onClick={(event) => event.stopPropagation()}>
          <header className="room-info-header">
            <div><span className="eyebrow">MATRIX</span><h2 id="account-manager-title">{addingAccount ? 'Add account' : 'Accounts'}</h2></div>
            <button className="icon-button" type="button" aria-label="Close account manager" disabled={busy} onClick={() => setAccountManagerOpen(false)}><X size={16} /></button>
          </header>
          {accountError && <p className="account-manager-error" role="alert">{accountError}</p>}
          {addingAccount ? <form className="account-add-form" onSubmit={submitAddAccount}>
            <label><span>Homeserver</span><input type="url" value={accountHomeserver} onChange={(event) => setAccountHomeserver(event.target.value)} autoComplete="url" required /></label>
            <label><span>Username</span><input value={accountUsername} onChange={(event) => setAccountUsername(event.target.value)} autoComplete="username" autoCapitalize="none" required /></label>
            <label><span>Password</span><input type="password" value={accountPassword} onChange={(event) => setAccountPassword(event.target.value)} autoComplete="new-password" required /></label>
            <div className="account-manager-actions">
              <button className="settings-command" type="button" disabled={busy} onClick={() => { setAddingAccount(false); setAccountError('') }}>Cancel</button>
              <button className="primary-button" type="submit" disabled={busy}>{busy ? 'Connecting…' : 'Add account'}{!busy && <Plus size={15} />}</button>
            </div>
          </form> : <div className="account-manager-content">
            <div className="account-option-list" aria-label="Connected Matrix accounts">
              {(snapshot.accounts ?? []).map((account) => {
                const label = account.profile.displayName || account.profile.userId
                const active = account.id === snapshot.activeAccountId
                return <button key={account.id} className="account-option" type="button" aria-current={active ? 'true' : undefined} disabled={busy} onClick={() => void switchAccount(account.id)}>
                  <Avatar uri={account.profile.avatarUri} label={label} className="profile-avatar" />
                  <span className="account-option-copy"><strong>{label}</strong><span>{account.profile.userId}</span></span>
                  {active ? <Check size={16} aria-label="Current account" /> : <span className="account-option-homeserver">{account.profile.homeserver}</span>}
                </button>
              })}
            </div>
            <button className="settings-command account-add-button" type="button" disabled={busy} onClick={() => { setAccountError(''); setAddingAccount(true) }}><Plus size={15} />Add account</button>
          </div>}
        </DialogSurface>
      </div>}
      {settingsOpen && (
        <SettingsDialog
          tab={settingsTab}
          profile={snapshot.profile}
          preferences={preferences}
          rooms={rooms}
          audioInputDevices={audioInputDevices}
          selectedAudioInputDeviceId={selectedAudioInputDeviceId}
          onArchiveInactiveRooms={archiveInactiveRooms}
          onTabChange={setSettingsTab}
          onClose={() => setSettingsOpen(false)}
          onLogout={async () => {
            await AppService.Logout()
            setSettingsOpen(false)
            try {
              resetAccountWorkspace(await AppService.Snapshot())
            } catch {
              // The app is closing after logout from the final account.
            }
          }}
          onPreferenceChange={(key, value) => setPreference(key, value)}
        />
      )}
      {scheduledJobsOpen && <div className="room-info-scrim" role="presentation" onClick={() => setScheduledJobsOpen(false)}>
        <DialogSurface className="room-info-dialog scheduled-dialog" labelledBy="scheduled-jobs-title" onClose={() => setScheduledJobsOpen(false)} onClick={(event) => event.stopPropagation()}>
          <header className="room-info-header">
            <div><span className="eyebrow">YOUR QUEUE</span><h2 id="scheduled-jobs-title">Scheduled messages and reminders</h2></div>
            <button className="icon-button" type="button" aria-label="Close scheduled jobs" onClick={() => setScheduledJobsOpen(false)}><X size={16} /></button>
          </header>
          <div className="room-info-content scheduled-job-list" aria-live="polite">
            {scheduledJobsError && <p className="room-info-state is-error" role="alert">{scheduledJobsError}</p>}
            {scheduledLoading && <div className="room-info-state"><span className="loading-mark" />Loading scheduled jobs</div>}
            {!scheduledLoading && scheduledJobs.length === 0 && <p className="room-info-state">No scheduled messages or reminders.</p>}
            {scheduledJobs.map((job) => {
              const roomName = rooms.find((room) => room.id === job.roomId)?.name || job.roomId
              const editable = job.status === 'pending' || job.status === 'failed'
              const dismissible = job.status === 'fired' || job.status === 'sent-late'
              const statusLabel = job.status === 'pending' ? 'Scheduled'
                : job.status === 'sending' ? 'Sending'
                  : job.status === 'failed' ? 'Failed'
                    : job.status === 'fired' ? 'Due now'
                      : job.status === 'sent-late' ? 'Sent late' : job.status
              return <article className="scheduled-job" key={job.id}>
                <div className="scheduled-job-heading">
                  <div><strong>{job.kind === 'message' ? 'Scheduled message' : 'Reminder'}</strong><span className={`scheduled-job-status is-${job.status}`}>{statusLabel}</span></div>
                  <time>{formatScheduledTime(job.dueAt)}</time>
                </div>
                <span className="scheduled-job-room">{roomName}</span>
                {job.replyToEventId && <span className="scheduled-job-room">Reply to {job.replyToEventId}</span>}
                <p>{job.text || (job.kind === 'reminder' ? 'Reminder for a message' : '')}</p>
                {editable && <label className="scheduled-job-time">New time
                  <input type="datetime-local" value={scheduledTimes[job.id] ?? localDateTimeInputValue(job.dueAt)} onChange={(event) => setScheduledTimes((current) => ({ ...current, [job.id]: event.target.value }))} />
                </label>}
                {job.error && <p className="scheduled-job-error" role="alert">{job.error}</p>}
                <div className="scheduled-job-actions">
                  {editable && <button className="settings-command" type="button" disabled={scheduledBusyID === job.id} onClick={() => void manageScheduledJob(job.id, 'reschedule')}><Check size={14} />Save time</button>}
                  {job.status === 'failed' && job.kind === 'message' && <button className="settings-command" type="button" disabled={scheduledBusyID === job.id} onClick={() => void manageScheduledJob(job.id, 'retry')}><RotateCcw size={14} />Retry</button>}
                  {editable && <button className="settings-command is-danger" type="button" disabled={scheduledBusyID === job.id} onClick={() => void manageScheduledJob(job.id, 'cancel')}><X size={14} />Cancel</button>}
                  {dismissible && <button className="settings-command" type="button" disabled={scheduledBusyID === job.id} onClick={() => void manageScheduledJob(job.id, 'dismiss')}><Check size={14} />Dismiss</button>}
                </div>
              </article>
            })}
          </div>
          <footer className="room-info-footer"><button className="settings-command" type="button" onClick={() => setScheduledJobsOpen(false)}>Close</button></footer>
        </DialogSurface>
      </div>}
      {scheduledDraft && <div className="room-info-scrim" role="presentation" onClick={() => setScheduledDraft(null)}>
        <DialogSurface className="room-info-dialog scheduled-editor" labelledBy="scheduled-editor-title" onClose={() => setScheduledDraft(null)} onClick={(event) => event.stopPropagation()}>
          <header className="room-info-header">
            <div><span className="eyebrow">{scheduledDraft.kind === 'message' ? scheduledDraft.replyToEventID ? 'SCHEDULE REPLY' : 'SCHEDULE SEND' : 'MESSAGE REMINDER'}</span><h2 id="scheduled-editor-title">{scheduledDraft.kind === 'message' ? scheduledDraft.replyToEventID ? 'Schedule reply' : 'Schedule message' : 'Set reminder'}</h2></div>
            <button className="icon-button" type="button" aria-label="Close schedule dialog" onClick={() => setScheduledDraft(null)}><X size={16} /></button>
          </header>
          <form className="scheduled-editor-form" onSubmit={submitScheduledDraft}>
            <div className="room-info-content scheduled-editor-content">
              <p className="scheduled-editor-room">To {rooms.find((room) => room.id === scheduledDraft.roomID)?.name || scheduledDraft.roomID}</p>
              {scheduledDraft.kind === 'message'
                ? <>
                  {scheduledDraft.replyToEventID && <div className="scheduled-reply-context"><strong>Replying to {scheduledDraft.replyToSender}</strong><span>{scheduledDraft.replyToPreview}</span></div>}
                  <p className="scheduled-draft-preview">{scheduledDraft.text}</p>
                </>
                : <>
                  <label>Reminder note <input type="text" value={scheduledDraft.text} onChange={(event) => setScheduledDraft((current) => current?.kind === 'reminder' ? { ...current, text: event.target.value } : current)} placeholder="Optional note" /></label>
                  <label className="scheduled-archive-toggle">
                    <input type="checkbox" aria-label="Archive room after setting reminder" checked={Boolean(scheduledDraft.archiveAfter)} disabled={scheduledSaving} onChange={(event) => setScheduledDraft((current) => current?.kind === 'reminder' ? { ...current, archiveAfter: event.target.checked } : current)} />
                    <span>Archive this room after setting the reminder</span>
                  </label>
                </>}
              <label>{scheduledDraft.kind === 'message' ? 'Send at' : 'Remind me at'}
                <input type="datetime-local" required value={scheduledDueAt} onChange={(event) => setScheduledDueAt(event.target.value)} />
              </label>
              <div className="scheduled-time-suggestions" role="group" aria-label="Time suggestions">
                {scheduleTimeSuggestions.map((minutes) => <button key={minutes} className="settings-command" type="button" onClick={() => setScheduledDueAt(localDateTimeInputValue(Date.now() + minutes * 60_000))}>{formatTimeSuggestion(minutes)}</button>)}
              </div>
              {scheduledDialogError && <p className="room-info-state is-error" role="alert">{scheduledDialogError}</p>}
            </div>
            <footer className="room-info-footer">
              <button className="settings-command" type="button" disabled={scheduledSaving} onClick={() => setScheduledDraft(null)}>Cancel</button>
              <button className="settings-command is-primary" type="submit" disabled={scheduledSaving}>{scheduledSaving ? 'Saving…' : scheduledDraft.kind === 'message' ? scheduledDraft.replyToEventID ? 'Schedule reply' : 'Schedule message' : 'Set reminder'}</button>
            </footer>
          </form>
        </DialogSurface>
      </div>}
      {roomInfoOpen && selectedRoom && <div className="room-info-scrim" role="presentation" onClick={() => setRoomInfoOpen(false)}>
        <DialogSurface className="room-info-dialog" labelledBy="room-info-title" onClose={() => setRoomInfoOpen(false)} onClick={(event) => event.stopPropagation()}>
          <header className="room-info-header">
            <div><span className="eyebrow">ROOM DETAILS</span><h2 id="room-info-title">{selectedRoom.name || selectedRoom.id}</h2></div>
            <button className="icon-button" type="button" aria-label="Close room details" onClick={() => setRoomInfoOpen(false)}><X size={16} /></button>
          </header>
          <div className="room-info-content">
            <div className="room-info-fact"><span>Room ID</span><strong>{selectedRoom.id}</strong></div>
            {selectedRoom.topic && <div className="room-info-fact"><span>Topic</span><strong>{selectedRoom.topic}</strong></div>}
            <div className="room-members-heading"><h3>Members</h3><span>{roomMembersLoading ? 'Loading' : roomMembers.length}</span></div>
            {roomMembersLoading ? <div className="room-info-state"><span className="loading-mark" />Loading members</div>
              : roomMembersError ? <p className="room-info-state is-error" role="alert">{roomMembersError}</p>
                : roomMembers.length === 0 ? <p className="room-info-state">No room members found.</p>
                  : <div className="room-member-list">{roomMembers.map((member) => <div className="room-member-row" key={member.id}>
                    <span className="room-member-avatar">{initials(member.displayName || member.id)}</span>
                    <span><strong>{member.displayName || member.id}</strong>{member.displayName && <small>{member.id}</small>}</span>
                  </div>)}</div>}
          </div>
          <footer className="room-info-footer">
            <button className="settings-command is-danger" type="button" onClick={() => { setLeaveRoomError(''); setLeaveConfirmOpen(true) }}><LogOut size={15} />Leave room</button>
          </footer>
        </DialogSurface>
      </div>}
      {leaveConfirmOpen && selectedRoom && <div className="confirm-scrim" role="presentation">
        <DialogSurface className="confirm-dialog" role="alertdialog" labelledBy="leave-room-title" onClose={() => setLeaveConfirmOpen(false)}>
          <h2 id="leave-room-title">Leave this room?</h2>
          <p>You will leave {selectedRoom.name || selectedRoom.id} and stop receiving its messages.</p>
          {leaveRoomError && <p className="room-info-state is-error" role="alert">{leaveRoomError}</p>}
          <div><button type="button" disabled={leavingRoom} onClick={() => setLeaveConfirmOpen(false)}>Cancel</button><button className="is-danger" type="button" disabled={leavingRoom} onClick={() => void leaveSelectedRoom()}>{leavingRoom ? 'Leaving…' : 'Leave room'}</button></div>
        </DialogSurface>
      </div>}
      {redactTarget && <div className="confirm-scrim" role="presentation">
        <DialogSurface className="confirm-dialog" role="alertdialog" labelledBy="redact-title" onClose={() => setRedactTarget(null)}>
          <h2 id="redact-title">Delete message?</h2>
          <p>This removes the message for everyone in the room.</p>
          <div><button type="button" onClick={() => setRedactTarget(null)}>Cancel</button><button className="is-danger" type="button" disabled={busy} onClick={() => void redactMessage()}>{busy ? 'Deleting…' : 'Delete'}</button></div>
        </DialogSurface>
      </div>}
      {undoRoomID && <div className="triage-toast" role="status">
        <span>Marked {rooms.find((room) => room.id === undoRoomID)?.name || 'room'} answered</span>
        <button type="button" onClick={() => void undoAnswered()}><RotateCcw size={14} />Undo</button>
      </div>}
      {quickSwitcherOpen && <div className="quick-switcher-scrim" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setQuickSwitcherOpen(false) }}>
        <DialogSurface className="quick-switcher" label="Quick switcher" onClose={() => setQuickSwitcherOpen(false)}>
          <div className="quick-switcher-input"><Search size={17} /><input ref={quickSwitcherInputRef} value={quickSwitcherQuery} onChange={(event) => setQuickSwitcherQuery(event.target.value)} onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault()
              event.stopPropagation()
              setQuickSwitcherOpen(false)
            } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault()
              event.stopPropagation()
              if (quickSwitcherRooms.length > 0) setQuickSwitcherIndex((current) => (current + (event.key === 'ArrowDown' ? 1 : -1) + quickSwitcherRooms.length) % quickSwitcherRooms.length)
            } else if (event.key === 'Enter') {
              event.preventDefault()
              event.stopPropagation()
              const room = quickSwitcherRooms[quickSwitcherIndex]
              if (room) {
                showRoom(room.id)
                setQuickSwitcherOpen(false)
              }
            }
          }} placeholder="Search rooms" aria-label="Search rooms" autoComplete="off" />
            <button className="icon-button" type="button" aria-label="Close quick switcher" onClick={() => setQuickSwitcherOpen(false)}><X size={16} /></button>
          </div>
          <div className="quick-switcher-results" role="listbox" aria-label="Matching rooms">
            {quickSwitcherRooms.map((room, index) => <button key={room.id} type="button" role="option" aria-selected={quickSwitcherIndex === index} className={`quick-switcher-option ${quickSwitcherIndex === index ? 'is-active' : ''}`} onMouseEnter={() => setQuickSwitcherIndex(index)} onClick={() => { showRoom(room.id); setQuickSwitcherOpen(false) }}>
              <Avatar uri={room.avatar} label={room.name || room.id} className="quick-switcher-avatar" />
              <span className="quick-switcher-copy"><strong>{room.name || room.id}</strong><small>{room.id}</small></span>
              {room.unread > 0 && <span className="quick-switcher-unread">{room.unread}</span>}
            </button>)}
            {quickSwitcherRooms.length === 0 && <div className="quick-switcher-empty">No matching rooms</div>}
          </div>
        </DialogSurface>
      </div>}
      {isDraggingFiles && <div className="drop-overlay" role="status"><div><Upload size={21} /><span>{selectedRoom ? `Add files to ${selectedRoom.name || selectedRoom.id}` : 'Select a room to upload files'}</span></div></div>}
    </main>
  )
}

function MessageMedia({ message, onAudioEnded, autoPlayNext }: { message: Message; onAudioEnded?: () => void; autoPlayNext?: boolean }) {
  const [source, setSource] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [viewerOpen, setViewerOpen] = useState(false)
  const [zoom, setZoom] = useState(1)
  const [copyFeedback, setCopyFeedback] = useState('')
  const mimeType = message.mediaMime || 'application/octet-stream'
  const isImage = mimeType.startsWith('image/')
  const isAudio = mimeType.startsWith('audio/')
  const isVideo = mimeType.startsWith('video/')
  const filename = message.body.trim() || 'matrix-attachment'
  const fileType = mimeType === 'application/pdf' ? 'PDF document' : mimeType.startsWith('text/') ? 'Text document' : 'File attachment'

  useEffect(() => () => {
    if (source) URL.revokeObjectURL(source)
  }, [source])

  useEffect(() => {
    if (!viewerOpen) return
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') setViewerOpen(false)
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [viewerOpen])

  async function prepareMedia() {
    if (source) return source
    if (loading) return ''
    setLoading(true)
    setError('')
    try {
      const encoded = await AppService.DownloadMedia(message.mediaUri)
      if (!encoded) throw new Error('No media data returned.')
      const objectURL = createMediaObjectURL(encoded, mimeType)
      setSource(objectURL)
      return objectURL
    } catch (mediaError) {
      setError(describeError(mediaError))
      return ''
    } finally {
      setLoading(false)
    }
  }

  async function openImage() {
    if (await prepareMedia()) {
      setZoom(1)
      setCopyFeedback('')
      setViewerOpen(true)
    }
  }

  async function downloadFile() {
    setError('')
    try {
      await AppService.SaveMedia(message.mediaUri, filename)
    } catch (downloadError) {
      setError(describeError(downloadError))
    }
  }

  async function copyImage() {
    setCopyFeedback('')
    try {
      if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
        throw new Error('Image copying is not supported by this webview.')
      }
      const image = new Image()
      image.src = source
      await image.decode()
      const canvas = document.createElement('canvas')
      canvas.width = image.naturalWidth
      canvas.height = image.naturalHeight
      const context = canvas.getContext('2d')
      if (!context) throw new Error('Could not prepare the image for copying.')
      context.drawImage(image, 0, 0)
      const png = await new Promise<Blob>((resolve, reject) => {
        canvas.toBlob((blob) => blob ? resolve(blob) : reject(new Error('Could not encode the image.')), 'image/png')
      })
      await navigator.clipboard.write([new ClipboardItem({ 'image/png': png })])
      setCopyFeedback('Image copied')
    } catch (copyError) {
      setCopyFeedback(describeError(copyError))
    }
  }

  return (
    <div className="message-attachment">
      {!source && (isImage || isAudio || isVideo) && <button className="media-load" type="button" disabled={loading} onClick={() => void prepareMedia()}>
        <Download size={14} />{loading ? 'Loading media…' : isImage ? `Preview ${filename}` : isAudio ? 'Load audio' : isVideo ? 'Load video' : `Prepare ${filename}`}
        {message.mediaSize > 0 && <span>{formatBytes(message.mediaSize)}</span>}
      </button>}
      {error && <span className="media-error" role="status">{error}</span>}
      {source && isImage && <>
        <button className="media-preview-button" type="button" aria-label={`Open image ${filename}`} onClick={() => void openImage()}>
          <img className="media-preview" src={source} alt={filename} />
        </button>
        <div className="media-toolbar"><span>{filename}</span><a className="media-download" href={source} download={filename} aria-label={`Download ${filename}`}><Download size={13} />Download</a></div>
      </>}
      {source && isAudio && <>
        <audio className="media-player" controls preload="metadata" src={source} aria-label={filename} autoPlay={autoPlayNext} onEnded={() => onAudioEnded?.()} />
        <a className="media-download" href={source} download={filename}><Download size={13} />Download {filename}</a>
      </>}
      {source && isVideo && <>
        <video className="media-player" controls preload="metadata" src={source} aria-label={filename} />
        <a className="media-download" href={source} download={filename}><Download size={13} />Download {filename}</a>
      </>}
      {!isImage && !isAudio && !isVideo && <div className="file-card">
        <span className="file-card-icon"><FileText size={17} /></span>
        <span className="file-card-copy"><strong title={filename}>{filename}</strong><small>{fileType}{message.mediaSize > 0 && ` · ${formatBytes(message.mediaSize)}`}</small></span>
        <button className="icon-button" type="button" title={`Download ${filename}`} aria-label={`Download ${filename}`} disabled={loading} onClick={() => void downloadFile()}><Download size={15} /></button>
      </div>}
      {viewerOpen && source && createPortal(
        <div className="media-viewer-scrim" role="presentation" onClick={() => setViewerOpen(false)}>
          <DialogSurface className="media-viewer" label={`Preview ${filename}`} onClose={() => setViewerOpen(false)} onClick={(event) => event.stopPropagation()}>
            <header className="media-viewer-toolbar">
              <strong>{filename}</strong>
              <div>
                <button className="icon-button" type="button" title="Zoom out" aria-label="Zoom out" disabled={zoom <= 1} onClick={() => setZoom((current) => Math.max(1, current / 1.25))}><ZoomOut size={15} /></button>
                <button className="icon-button" type="button" title="Zoom in" aria-label="Zoom in" disabled={zoom >= 4} onClick={() => setZoom((current) => Math.min(4, current * 1.25))}><ZoomIn size={15} /></button>
                <button className="icon-button" type="button" title="Copy image" aria-label="Copy image" onClick={() => void copyImage()}><Copy size={14} /></button>
                <a className="media-download" href={source} download={filename}><Download size={14} />Download</a>
                <button className="icon-button" type="button" aria-label="Close image preview" onClick={() => setViewerOpen(false)}><X size={15} /></button>
              </div>
            </header>
            {copyFeedback && <span className="media-viewer-feedback" role="status">{copyFeedback}</span>}
            <div className="media-viewer-image-wrap">
              <img className="media-viewer-image" src={source} alt={filename} style={{ maxWidth: zoom === 1 ? '100%' : 'none', maxHeight: zoom === 1 ? 'calc(100vh - 100px)' : 'none', transform: `scale(${zoom})` }} />
            </div>
          </DialogSurface>
        </div>,
        document.body,
      )}
    </div>
  )
}

function RoomButton({ room, active, lastMessageByMe, use24Hour, showNetwork, onSelect, onKeyDown, onContextMenu }: { room: Room; active: boolean; lastMessageByMe: boolean; use24Hour: boolean; showNetwork: boolean; onSelect: () => void; onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void; onContextMenu: (event: React.MouseEvent<HTMLButtonElement>) => void }) {
  const hasUnread = room.unread > 0 || room.markedUnread
  return (
    <button className={`room-button ${active ? 'is-active' : ''} ${room.muted ? 'is-muted' : ''} ${hasUnread ? 'has-unread' : ''}`} onClick={onSelect} onKeyDown={onKeyDown} onContextMenu={onContextMenu} aria-current={active ? 'page' : undefined} title={room.name || room.id}>
      <Avatar uri={room.avatar} label={room.name || room.id} className="room-avatar" style={avatarStyle(room.name || room.id)} deferLoad />
      <span className="room-copy">
        <span className="room-name">{room.favorite && <Star size={10} fill="currentColor" aria-label="Favorite" />}<span className="room-name-text">{room.name || room.id}</span>{room.muted && <span className="room-muted-mark" title="Muted">· muted</span>}{showNetwork && room.bridge && <span className="room-network" style={{ '--network-hue': networkHue(room.bridge) } as React.CSSProperties}>{room.bridge}</span>}</span>
        <span className={`room-preview ${lastMessageByMe ? 'is-own' : ''}`}>
          {lastMessageByMe && <span className="room-preview-author">You: </span>}
          {room.preview || room.topic || room.id}
        </span>
      </span>
      <span className="room-meta">
        {hasUnread && (room.unread > 1 ? <span className="unread-count">{room.unread > 99 ? '99+' : room.unread}</span> : <span className="unread-dot" aria-label="Unread" />)}
        {room.lastEventAt > 0 && <time>{formatTime(room.lastEventAt, use24Hour)}</time>}
      </span>
    </button>
  )
}

const storageMediaOptions: Array<{ value: string; label: string }> = [
  { value: '', label: 'All media' },
  { value: 'image', label: 'Images' },
  { value: 'video', label: 'Videos' },
  { value: 'audio', label: 'Audio' },
  { value: 'file', label: 'Files' },
  { value: 'other', label: 'Other' },
]

const storageAgeOptions: Array<{ value: string; label: string }> = [
  { value: '', label: 'Any age' },
  { value: '30', label: 'Older than 30 days' },
  { value: '90', label: 'Older than 90 days' },
  { value: '365', label: 'Older than 1 year' },
]

const chatNetworks: Array<{ code: string; name: string }> = [
  { code: 'WA', name: 'WhatsApp' },
  { code: 'TG', name: 'Telegram' },
  { code: 'IG', name: 'Instagram' },
  { code: 'SG', name: 'Signal' },
  { code: 'iM', name: 'iMessage' },
  { code: 'DC', name: 'Discord' },
  { code: 'SL', name: 'Slack' },
  { code: 'SMS', name: 'SMS' },
  { code: 'BR', name: 'Other bridged chats' },
]

function ChatStorageSettings() {
  const [rows, setRows] = useState<RoomStorage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [chatID, setChatID] = useState('')
  const [age, setAge] = useState('')
  const [mediaType, setMediaType] = useState('')
  const [busy, setBusy] = useState(false)
  const [cleanupResult, setCleanupResult] = useState<MediaCleanupResult | null>(null)

  useEffect(() => {
    let active = true
    AppService.RoomStorageUsage()
      .then((usage) => { if (active) setRows(usage ?? []) })
      .catch((loadError: unknown) => { if (active) setError(describeError(loadError)) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  async function reloadRows() {
    setLoading(true)
    try {
      setRows((await AppService.RoomStorageUsage()) ?? [])
    } catch (loadError) {
      setError(describeError(loadError))
    } finally {
      setLoading(false)
    }
  }

  async function clearMedia(target: { roomID: string; name: string }) {
    const typeLabel = storageMediaOptions.find((option) => option.value === mediaType)?.label.toLowerCase() ?? 'all media'
    const ageLabel = storageAgeOptions.find((option) => option.value === age)?.label.toLowerCase() ?? 'any age'
    const scope = target.roomID ? target.name : 'all chats'
    if (!window.confirm(`Remove cached media (${typeLabel}, ${scope}, ${ageLabel})? Messages stay. Media downloads again when opened.`)) return
    setBusy(true)
    setError('')
    setCleanupResult(null)
    try {
      setCleanupResult(await AppService.ClearCachedMedia({ roomId: target.roomID, olderThanDays: Number(age) || 0, mediaType }))
      await reloadRows()
    } catch (cleanupError) {
      setError(describeError(cleanupError))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="chat-storage">
      <h4>Storage by chat</h4>
      <p className="settings-note">Text sizes are approximate. Cleanup removes cached media files only; messages are not removed.</p>
      {loading && <p className="settings-note">Reading chat storage…</p>}
      {error && <p className="settings-error" role="alert">{error}</p>}
      {!loading && !error && rows.length === 0 && <p className="settings-note">No chats have stored messages on this device yet.</p>}
      {rows.length > 0 && (
        <table className="storage-table" aria-label="Storage used by each chat">
          <thead>
            <tr><th scope="col">Chat</th><th scope="col">Messages</th><th scope="col">Text</th><th scope="col">Media</th><th scope="col">Files</th><th scope="col">Cleanup</th></tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const name = row.name || row.roomId
              return (
                <tr key={row.roomId}>
                  <th scope="row">{name}</th>
                  <td>{row.messages.toLocaleString()}</td>
                  <td>{formatBytes(row.textBytes)}</td>
                  <td>{formatBytes(row.mediaBytes)}</td>
                  <td>{row.mediaFiles.toLocaleString()}</td>
                  <td><button className="settings-command" type="button" disabled={busy || row.mediaFiles === 0} aria-label={`Clear media in ${name}`} onClick={() => void clearMedia({ roomID: row.roomId, name })}>Clear media</button></td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
      <form className="storage-cleanup" aria-label="Clear cached media" onSubmit={(event) => {
        event.preventDefault()
        void clearMedia({ roomID: chatID, name: rows.find((row) => row.roomId === chatID)?.name || chatID })
      }}>
        <label><span>Chat</span><select aria-label="Cleanup chat" value={chatID} onChange={(event) => setChatID(event.target.value)}>
          <option value="">All chats</option>
          {rows.map((row) => <option key={row.roomId} value={row.roomId}>{row.name || row.roomId}</option>)}
        </select></label>
        <label><span>Age</span><select aria-label="Cleanup age" value={age} onChange={(event) => setAge(event.target.value)}>
          {storageAgeOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select></label>
        <label><span>Media type</span><select aria-label="Cleanup media type" value={mediaType} onChange={(event) => setMediaType(event.target.value)}>
          {storageMediaOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
        </select></label>
        <button className="settings-command is-danger" type="submit" disabled={busy || loading}>{busy ? 'Clearing…' : 'Clear matching media'}</button>
      </form>
      {cleanupResult && <p className="settings-note" role="status">{`Removed ${cleanupResult.files.toLocaleString()} cached files (${formatBytes(cleanupResult.bytes)}).`}</p>}
    </div>
  )
}

function AboutPinger() {
  const [version, setVersion] = useState('')
  const [update, setUpdate] = useState<UpdateStatus | null>(null)
  const [checking, setChecking] = useState(false)
  const [updateError, setUpdateError] = useState('')

  useEffect(() => {
    let active = true
    AppService.AppVersion()
      .then((value) => { if (active) setVersion(value) })
      .catch(() => { if (active) setVersion('unknown') })
    return () => { active = false }
  }, [])

  async function checkForUpdates() {
    setChecking(true)
    setUpdateError('')
    try {
      setUpdate(await AppService.CheckForUpdates())
    } catch (checkError) {
      setUpdate(null)
      setUpdateError(describeError(checkError))
    } finally {
      setChecking(false)
    }
  }

  const published = update?.publishedAt ? Date.parse(update.publishedAt) : NaN
  return (
    <>
      <div className="account-details"><span>Application</span><strong>Pinger</strong><span>Version</span><strong>{version || 'Loading…'}</strong><span>Matrix engine</span><strong>gomuks</strong><span>License</span><strong>AGPL-3.0-or-later</strong></div>
      <button className="settings-command" type="button" disabled={checking} onClick={() => void checkForUpdates()}>{checking ? 'Checking…' : 'Check for updates'}</button>
      {updateError && <p className="settings-error" role="alert">{updateError}</p>}
      {update && (
        <div className="update-status" role="status">
          <p>{update.message}</p>
          {update.latestVersion && <p className="settings-note">{`Latest release ${update.latestVersion}${Number.isNaN(published) ? '' : ` · published ${new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(published))}`}`}</p>}
          {update.updateAvailable && <p className="settings-note">Pinger does not install updates itself. Download the new version from the release page.</p>}
          {update.releaseNotes && <details><summary>Release notes</summary><pre className="release-notes">{update.releaseNotes}</pre></details>}
          <a className="settings-link" href={update.releaseUrl} target="_blank" rel="noreferrer">View release</a>
        </div>
      )}
      <a className="settings-link" href="https://github.com/cataWalter/pinger" target="_blank" rel="noreferrer">Project repository</a>
      <a className="settings-link" href="https://github.com/cataWalter/pinger/issues" target="_blank" rel="noreferrer">Send feedback or report an issue</a>
      <a className="settings-link" href="https://github.com/cataWalter/pinger/blob/main/LICENSE" target="_blank" rel="noreferrer">License (AGPL-3.0-or-later)</a>
    </>
  )
}

function LocalAISettings({ endpoint, model, translateLanguage, onSave }: {
  endpoint: string
  model: string
  translateLanguage: string
  onSave: (key: string, value: string) => Promise<boolean>
}) {
  const [endpointDraft, setEndpointDraft] = useState(endpoint)
  const [modelDraft, setModelDraft] = useState(model)
  const [languageDraft, setLanguageDraft] = useState(translateLanguage)
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  const endpointUnsaved = endpointDraft.trim() !== endpoint

  async function saveSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setStatus('')
    let saved = await onSave('ai.endpoint', endpointDraft.trim())
    if (saved) saved = await onSave('ai.model', modelDraft.trim())
    if (saved) saved = await onSave('ai.translate_language', languageDraft.trim())
    setStatus(saved ? 'AI settings saved.' : 'Not saved. The endpoint must be an http or https URL.')
    setBusy(false)
  }

  async function testEndpoint() {
    setBusy(true)
    setStatus('Checking the endpoint…')
    try {
      await AppService.TestAIEndpoint()
      setStatus('Model is loaded and ready.')
    } catch (testError) {
      setStatus(describeError(testError))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="ai-settings-form" onSubmit={(event) => void saveSettings(event)}>
      <h4>Local AI (llama.cpp)</h4>
      <p className="settings-note">Point Pinger at a llama-server you run yourself. Replies are drafts only and are never sent automatically.</p>
      <label><span>Endpoint</span><input type="url" value={endpointDraft} placeholder="http://localhost:8080" onChange={(event) => setEndpointDraft(event.target.value)} /></label>
      <label><span>Model name (optional)</span><input type="text" value={modelDraft} placeholder="Uses the loaded model" onChange={(event) => setModelDraft(event.target.value)} /></label>
      <label><span>Translate into</span><input type="text" value={languageDraft} placeholder="English" maxLength={40} onChange={(event) => setLanguageDraft(event.target.value)} /></label>
      <div className="ai-settings-actions">
        <button className="settings-command" type="submit" disabled={busy}>Save</button>
        <button
          className="settings-command"
          type="button"
          disabled={busy || !endpoint || endpointUnsaved}
          title={endpointUnsaved ? 'Save the endpoint before testing' : undefined}
          onClick={() => void testEndpoint()}
        >Test endpoint</button>
      </div>
      {status && <p className="settings-note" role="status">{status}</p>}
    </form>
  )
}

function SettingsDialog({
  tab,
  profile,
  preferences,
  rooms,
  audioInputDevices,
  selectedAudioInputDeviceId,
  onArchiveInactiveRooms,
  onTabChange,
  onClose,
  onLogout,
  onPreferenceChange,
}: {
  tab: SettingsTab
  profile: Snapshot['profile']
  preferences: PreferenceValues
  rooms: Room[]
  audioInputDevices: MediaDeviceInfo[]
  selectedAudioInputDeviceId: string
  onArchiveInactiveRooms: () => Promise<void>
  onTabChange: (tab: SettingsTab) => void
  onClose: () => void
  onLogout: () => Promise<void>
  onPreferenceChange: (key: string, value: boolean | string) => Promise<boolean>
}) {
  const tabs: SettingsTab[] = ['General', 'Account', 'Appearance', 'Notifications', 'Keyboard', 'History', 'Devices', 'Storage', 'Security', 'Help', 'About', 'Integrations']
  const theme = preferenceString(preferences, 'appearance.theme', 'System default')
  const fontSize = preferenceString(preferences, 'appearance.font_size', 'Medium')
  const bubbleColor = preferenceString(preferences, 'appearance.bubble_color', '#236b52')
  const activeAccentPalette = accentPalettes.find((palette) => palette.name === preferenceString(preferences, 'appearance.accent_color', 'Forest')) ?? accentPalettes[0]
  const activeWallpaper = chatWallpapers.find((wallpaper) => wallpaper.name === preferenceString(preferences, 'appearance.chat_wallpaper', 'None')) ?? chatWallpapers[0]
  const answeredShortcut = preferenceString(preferences, 'shortcuts.mark_answered', 'E').trim().toUpperCase() || 'E'
  const safeBubbleColor = /^#[0-9a-fA-F]{6}$/.test(bubbleColor) ? bubbleColor : '#236b52'
  const downloadFolder = preferenceString(preferences, 'downloads.folder', '')
  const [downloadError, setDownloadError] = useState('')
  async function chooseDownloadFolder() {
    setDownloadError('')
    try {
      const folder = await AppService.ChooseDownloadFolder()
      if (folder) onPreferenceChange('downloads.folder', folder)
    } catch (folderError) {
      setDownloadError(describeError(folderError))
    }
  }
  const [devices, setDevices] = useState<Device[]>([])
  const [devicesLoading, setDevicesLoading] = useState(false)
  const [devicesError, setDevicesError] = useState('')
  const [scheduleSuggestionsDraft, setScheduleSuggestionsDraft] = useState(preferenceString(preferences, 'schedule.suggestions_minutes', '5,15,60'))
  const [scheduleSuggestionsError, setScheduleSuggestionsError] = useState('')
  const [scheduleSendDelayDraft, setScheduleSendDelayDraft] = useState(preferenceString(preferences, 'schedule.default_send_minutes', '5'))
  const [scheduleReminderDelayDraft, setScheduleReminderDelayDraft] = useState(preferenceString(preferences, 'schedule.default_reminder_minutes', '60'))
  const [scheduleSendDelayError, setScheduleSendDelayError] = useState('')
  const [scheduleReminderDelayError, setScheduleReminderDelayError] = useState('')
  const [storage, setStorage] = useState<{ database: StorageUsage; media: MediaCacheUsage } | null>(null)
  const [storageLoading, setStorageLoading] = useState(false)
  const [storageError, setStorageError] = useState('')
  const [storageAction, setStorageAction] = useState('')
  const [backupPassphrase, setBackupPassphrase] = useState('')
  const [backupConfirmation, setBackupConfirmation] = useState('')
  const [importPassphrase, setImportPassphrase] = useState('')
  const [keyStatus, setKeyStatus] = useState('')
  const [keyBusy, setKeyBusy] = useState(false)
  const [logoutConfirm, setLogoutConfirm] = useState(false)
  const [logoutBusy, setLogoutBusy] = useState(false)
  const [notificationPermission, setNotificationPermission] = useState(() => typeof Notification === 'undefined' ? 'unsupported' : Notification.permission)

  function saveScheduleDefaultDelay(key: string, draft: string, setDraft: (value: string) => void, setError: (value: string) => void) {
    const minutes = Number(draft)
    if (!Number.isInteger(minutes) || minutes < 1 || minutes > 10080) {
      setError('Use a whole number from 1 to 10080 minutes.')
      return
    }
    const value = String(minutes)
    setDraft(value)
    setError('')
    void onPreferenceChange(key, value)
  }

  useEffect(() => {
    let active = true
    void AppService.NativeNotificationsSupported()
      .then((supported) => supported ? AppService.NotificationPermission() : typeof Notification === 'undefined' ? 'unsupported' : Notification.permission)
      .then((permission) => { if (active) setNotificationPermission(permission) })
      .catch(() => { if (active) setNotificationPermission('unsupported') })
    return () => { active = false }
  }, [])

  async function enableSystemNotifications() {
    if (await AppService.NativeNotificationsSupported()) {
      try {
        setNotificationPermission(await AppService.RequestNotificationPermission())
      } catch {
        setNotificationPermission('unsupported')
      }
      return
    }
    if (typeof Notification === 'undefined') {
      setNotificationPermission('unsupported')
      return
    }
    try {
      setNotificationPermission(await Notification.requestPermission())
    } catch {
      setNotificationPermission('unsupported')
    }
  }

  useEffect(() => {
    if (tab !== 'Devices') return
    let active = true
    setDevicesLoading(true)
    setDevicesError('')
    AppService.Devices()
      .then((result) => { if (active) setDevices(result ?? []) })
      .catch((loadError: unknown) => { if (active) setDevicesError(describeError(loadError)) })
      .finally(() => { if (active) setDevicesLoading(false) })
    return () => { active = false }
  }, [tab])

  useEffect(() => {
    if (tab !== 'Storage') return
    let active = true
    setStorageLoading(true)
    setStorageError('')
    Promise.all([AppService.StorageUsage(), AppService.MediaCacheUsage()])
      .then(([database, media]) => { if (active) setStorage({ database, media }) })
      .catch((loadError: unknown) => { if (active) setStorageError(describeError(loadError)) })
      .finally(() => { if (active) setStorageLoading(false) })
    return () => { active = false }
  }, [tab])

  async function revokeDevice(device: Device) {
    if (!window.confirm(`Revoke ${device.displayName || device.id}?`)) return
    try {
      await AppService.RevokeDevice(device.id)
      setDevices((current) => current.filter((item) => item.id !== device.id))
    } catch (revokeError) {
      setDevicesError(describeError(revokeError))
    }
  }

  async function refreshStorage() {
    setStorageLoading(true)
    setStorageError('')
    try {
      const [database, media] = await Promise.all([AppService.StorageUsage(), AppService.MediaCacheUsage()])
      setStorage({ database, media })
    } catch (loadError) {
      setStorageError(describeError(loadError))
    } finally {
      setStorageLoading(false)
    }
  }

  async function runStorageAction(action: 'clear' | 'optimize') {
    setStorageAction(action)
    setStorageError('')
    try {
      if (action === 'clear') await AppService.ClearMediaCache()
      else await AppService.OptimizeDatabase()
      await refreshStorage()
    } catch (actionError) {
      setStorageError(describeError(actionError))
    } finally {
      setStorageAction('')
    }
  }

  async function exportRoomKeys() {
    if (!backupPassphrase || backupPassphrase !== backupConfirmation) {
      setKeyStatus('Enter and confirm the same non-empty passphrase.')
      return
    }
    setKeyBusy(true)
    setKeyStatus('')
    try {
      const backup = await AppService.ExportRoomKeys(backupPassphrase)
      if (!backup) throw new Error('The homeserver returned an empty room key backup.')
      const link = document.createElement('a')
      link.href = `data:application/octet-stream;base64,${backup}`
      link.download = 'matrix-room-keys.txt'
      link.click()
      setKeyStatus('Encrypted room key backup exported.')
      setBackupPassphrase('')
      setBackupConfirmation('')
    } catch (exportError) {
      setKeyStatus(describeError(exportError))
    } finally {
      setKeyBusy(false)
    }
  }

  async function importRoomKeys(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    if (!importPassphrase) {
      setKeyStatus('Enter the backup passphrase before choosing a file.')
      return
    }
    if (file.size > 100 * 1024 * 1024) {
      setKeyStatus('Room key backup exceeds 100 MiB.')
      return
    }
    setKeyBusy(true)
    setKeyStatus('')
    try {
      const bytes = new Uint8Array(await file.arrayBuffer())
      let binary = ''
      for (let offset = 0; offset < bytes.length; offset += 0x8000) {
        binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000))
      }
      const result = await AppService.ImportRoomKeys(importPassphrase, btoa(binary))
      setKeyStatus(`Imported ${result.imported} of ${result.total} room keys.`)
      setImportPassphrase('')
    } catch (importError) {
      setKeyStatus(describeError(importError))
    } finally {
      setKeyBusy(false)
    }
  }

  async function confirmLogout() {
    setLogoutBusy(true)
    try {
      await onLogout()
    } catch {
      setLogoutBusy(false)
      setLogoutConfirm(false)
    }
  }

  return (
    <div className="settings-scrim" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <DialogSurface className="settings-dialog" labelledBy="settings-title" onClose={onClose}>
        <header className="settings-header">
          <div><span className="eyebrow">PINGER</span><h2 id="settings-title">Settings</h2></div>
          <button className="icon-button" type="button" aria-label="Close settings" title="Close settings" onClick={onClose}><X size={17} /></button>
        </header>
        <nav className="settings-tabs" role="tablist" aria-label="Settings sections">
          {tabs.map((item) => (
            <button
              key={item}
              id={`settings-tab-${item.toLowerCase().replace(/\s+/g, '-')}`}
              type="button"
              role="tab"
              aria-selected={tab === item}
              aria-controls="settings-panel"
              tabIndex={tab === item ? 0 : -1}
              className={tab === item ? 'is-active' : ''}
              onClick={() => onTabChange(item)}
              onKeyDown={(event) => {
                const currentIndex = tabs.indexOf(item)
                let nextIndex = currentIndex
                if (event.key === 'ArrowRight' || event.key === 'ArrowDown') nextIndex = (currentIndex + 1) % tabs.length
                else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') nextIndex = (currentIndex - 1 + tabs.length) % tabs.length
                else if (event.key === 'Home') nextIndex = 0
                else if (event.key === 'End') nextIndex = tabs.length - 1
                else return
                event.preventDefault()
                onTabChange(tabs[nextIndex])
                event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[nextIndex]?.focus()
              }}
            >{item}</button>
          ))}
        </nav>
        <div id="settings-panel" className="settings-content" role="tabpanel" aria-labelledby={`settings-tab-${tab.toLowerCase().replace(/\s+/g, '-')}`} tabIndex={0}>
          {tab === 'General' && (
            <div className="settings-section">
              <h3>General</h3>
              <PreferenceToggle label="Launch Pinger at login" checked={preferenceBoolean(preferences, 'system.launch_at_startup')} onChange={(value) => onPreferenceChange('system.launch_at_startup', value)} />
              <PreferenceToggle label="Minimize to tray when closing the window" checked={preferenceBoolean(preferences, 'window.minimize_to_tray')} onChange={(value) => onPreferenceChange('window.minimize_to_tray', value)} />
              <PreferenceToggle label="Start minimized" checked={preferenceBoolean(preferences, 'window.start_minimized')} onChange={(value) => onPreferenceChange('window.start_minimized', value)} />
              <PreferenceToggle label="Quit Pinger when the window closes" checked={preferenceBoolean(preferences, 'window.quit_on_close')} onChange={(value) => onPreferenceChange('window.quit_on_close', value)} />
              <PreferenceToggle label="Show menu bar icon" checked={preferenceBoolean(preferences, 'window.menu_bar_icon', true)} onChange={(value) => onPreferenceChange('window.menu_bar_icon', value)} />
              <div className="setting-row"><span><strong>Download folder</strong><small>{downloadFolder || 'Downloads folder in your home directory'}</small></span>
                <span>
                  <button type="button" className="settings-command" onClick={() => void chooseDownloadFolder()}>Choose…</button>
                  {downloadFolder && <button type="button" className="settings-command" onClick={() => onPreferenceChange('downloads.folder', '')}>Use default</button>}
                </span>
              </div>
              {downloadError && <p className="settings-error" role="alert">{downloadError}</p>}
              <PreferenceToggle label="Ask where to save each download" checked={preferenceBoolean(preferences, 'downloads.ask_each_time')} onChange={(value) => onPreferenceChange('downloads.ask_each_time', value)} />
              <label className="setting-row"><span><strong>Double-click a message</strong><small>Choose what a double-click on a message does.</small></span>
                <select aria-label="Double-click a message" value={preferenceString(preferences, 'messages.double_click', 'None')} onChange={(event) => onPreferenceChange('messages.double_click', event.target.value)}>
                  <option value="None">Nothing</option>
                  <option value="Reply">Reply</option>
                  <option value="React">Add reaction</option>
                </select>
              </label>
              <PreferenceToggle label="Show quick reaction picker" checked={preferenceBoolean(preferences, 'reactions.quick_enabled', true)} onChange={(value) => onPreferenceChange('reactions.quick_enabled', value)} />
              <label className="setting-row"><span><strong>Default reaction</strong><small>Sent directly when the quick picker is turned off.</small></span>
                <select aria-label="Default reaction" value={preferenceString(preferences, 'reactions.default_emoji', '👍')} onChange={(event) => onPreferenceChange('reactions.default_emoji', event.target.value)}>
                  {['👍', '❤️', '😂', '🎉', '👀', '🙏'].map((emoji) => <option key={emoji} value={emoji}>{emoji}</option>)}
                </select>
              </label>
              <label className="setting-row"><span><strong>Quick reaction set</strong><small>Use the built-in frequent set or your own emoji.</small></span>
                <select aria-label="Quick reaction set" value={preferenceString(preferences, 'reactions.quick_source', 'Frequent')} onChange={(event) => onPreferenceChange('reactions.quick_source', event.target.value)}>
                  <option value="Frequent">Frequent</option>
                  <option value="Custom">Custom</option>
                </select>
              </label>
              <label className="setting-row"><span><strong>Custom quick reactions</strong><small>Up to 6 emoji, separated by spaces.</small></span>
                <input type="text" aria-label="Custom quick reactions" defaultValue={preferenceString(preferences, 'reactions.custom_emojis', '')} onBlur={(event) => onPreferenceChange('reactions.custom_emojis', event.target.value.trim())} />
              </label>
              <label className="setting-row"><span><strong>After changing a room's read state</strong><small>Choose where to go after marking a room read or unread.</small></span>
                <select aria-label="After changing a room's read state" value={preferenceString(preferences, 'inbox.after_read', 'stay')} onChange={(event) => onPreferenceChange('inbox.after_read', event.target.value)}>
                  <option value="stay">Stay in the current room</option>
                  <option value="next_visible">Next visible room</option>
                  <option value="next_unread">Next unread room</option>
                  <option value="none">Clear selection</option>
                </select>
              </label>
              <label className="setting-row"><span><strong>After archiving a room</strong><small>Choose what opens after the selected room is archived.</small></span>
                <select aria-label="After archiving a room" value={preferenceString(preferences, 'archive.next_room', 'next_visible')} onChange={(event) => onPreferenceChange('archive.next_room', event.target.value)}>
                  <option value="next_visible">Next visible room</option>
                  <option value="next_unread">Next unread room</option>
                  <option value="none">Clear selection</option>
                </select>
              </label>
              <label className="setting-row"><span><strong>Inactive room age</strong><small>Archive rooms without activity for this long.</small></span>
                <select aria-label="Inactive room age" value={preferenceString(preferences, 'archive.older_than_days', '90')} onChange={(event) => onPreferenceChange('archive.older_than_days', event.target.value)}>
                  {['30', '60', '90', '180', '365'].map((days) => <option key={days} value={days}>{days} days</option>)}
                </select>
              </label>
              <button className="settings-command" type="button" onClick={() => void onArchiveInactiveRooms()}><Archive size={15} />Archive inactive rooms</button>
              <label className="setting-row"><span><strong>Voice-note microphone</strong><small>Choose the input used for recording.</small></span>
                <select value={selectedAudioInputDeviceId} aria-label="Voice-note microphone" onChange={(event) => onPreferenceChange('audio.input_device', event.target.value)}>
                  <option value="">System default</option>
                  {audioInputDevices.map((device, index) => <option key={device.deviceId || index} value={device.deviceId}>{device.label || `Microphone ${index + 1}`}</option>)}
                </select>
              </label>
              <PreferenceToggle label="Auto-play next voice note" checked={preferenceBoolean(preferences, 'voice.autoplay_next')} onChange={(value) => onPreferenceChange('voice.autoplay_next', value)} />
              <label className="setting-row"><span><strong>Default Send Later delay</strong><small>Initial scheduled-send time, in minutes (1–10080).</small></span>
                <input type="number" min="1" max="10080" step="1" value={scheduleSendDelayDraft} aria-label="Default Send Later delay in minutes" aria-invalid={Boolean(scheduleSendDelayError)} onChange={(event) => { setScheduleSendDelayDraft(event.target.value); setScheduleSendDelayError('') }} onBlur={() => saveScheduleDefaultDelay('schedule.default_send_minutes', scheduleSendDelayDraft, setScheduleSendDelayDraft, setScheduleSendDelayError)} />
              </label>
              {scheduleSendDelayError && <p className="form-error" role="alert">{scheduleSendDelayError}</p>}
              <label className="setting-row"><span><strong>Default Remind Later delay</strong><small>Initial reminder time, in minutes (1–10080).</small></span>
                <input type="number" min="1" max="10080" step="1" value={scheduleReminderDelayDraft} aria-label="Default Remind Later delay in minutes" aria-invalid={Boolean(scheduleReminderDelayError)} onChange={(event) => { setScheduleReminderDelayDraft(event.target.value); setScheduleReminderDelayError('') }} onBlur={() => saveScheduleDefaultDelay('schedule.default_reminder_minutes', scheduleReminderDelayDraft, setScheduleReminderDelayDraft, setScheduleReminderDelayError)} />
              </label>
              {scheduleReminderDelayError && <p className="form-error" role="alert">{scheduleReminderDelayError}</p>}
              <label className="setting-row"><span><strong>Schedule time suggestions</strong><small>Enter 1–6 comma-separated delays in minutes.</small></span>
                <input className="schedule-suggestions-input" type="text" inputMode="numeric" value={scheduleSuggestionsDraft} aria-label="Schedule time suggestions" aria-invalid={Boolean(scheduleSuggestionsError)} onChange={(event) => { setScheduleSuggestionsDraft(event.target.value); setScheduleSuggestionsError('') }} onBlur={() => {
                  const minutes = parseTimeSuggestionMinutes(scheduleSuggestionsDraft)
                  if (minutes.length === 0) {
                    setScheduleSuggestionsError('Use 1–6 unique values from 1 to 10080.')
                    return
                  }
                  const value = minutes.join(',')
                  setScheduleSuggestionsDraft(value)
                  setScheduleSuggestionsError('')
                  onPreferenceChange('schedule.suggestions_minutes', value)
                }} />
              </label>
              {scheduleSuggestionsError && <p className="form-error" role="alert">{scheduleSuggestionsError}</p>}
            </div>
          )}
          {tab === 'Appearance' && (
            <div className="settings-section">
              <h3>Appearance</h3>
              <label className="setting-row"><span><strong>Theme</strong><small>Choose the conversation surface.</small></span>
                <select value={theme} onChange={(event) => onPreferenceChange('appearance.theme', event.target.value)}>
                  {['System default', 'Light', 'Dark', 'OLED black', 'Bosco', 'Rosa'].map((value) => <option key={value}>{value}</option>)}
                </select>
              </label>
              <label className="setting-row"><span><strong>Font size</strong><small>Adjust room and message text.</small></span>
                <select value={fontSize} onChange={(event) => onPreferenceChange('appearance.font_size', event.target.value)}>
                  {['Small', 'Medium', 'Large'].map((value) => <option key={value}>{value}</option>)}
                </select>
              </label>
              <label className="setting-row"><span><strong>App zoom</strong><small>Scale the full interface.</small></span>
                <div className="setting-range"><input type="range" min="75" max="125" step="5" value={preferenceNumber(preferences, 'appearance.zoom', 100, 75, 125)} aria-label="App zoom" onChange={(event) => onPreferenceChange('appearance.zoom', event.target.value)} /><output>{preferenceNumber(preferences, 'appearance.zoom', 100, 75, 125)}%</output></div>
              </label>
              <label className="setting-row"><span><strong>Outgoing message color</strong><small>Choose a color for your messages.</small></span>
                <input className="color-input" type="color" value={safeBubbleColor} onChange={(event) => onPreferenceChange('appearance.bubble_color', event.target.value)} aria-label="Outgoing message color" />
              </label>
              <div className="setting-row accent-setting-row"><span><strong>Theme accent</strong><small>Choose an accent independent of message color.</small></span>
                <div className="accent-palette" role="group" aria-label="Theme accent color">
                  {accentPalettes.map((palette) => <button key={palette.name} className="accent-swatch" type="button" aria-label={`${palette.name} accent color`} aria-pressed={activeAccentPalette.name === palette.name} title={palette.name} style={{ '--swatch': palette.base } as React.CSSProperties} onClick={() => onPreferenceChange('appearance.accent_color', palette.name)}>
                    {activeAccentPalette.name === palette.name && <Check size={14} />}
                  </button>)}
                </div>
              </div>
              <div className="setting-row accent-setting-row"><span><strong>Chat wallpaper</strong><small>Add a subtle background to conversations.</small></span>
                <div className="wallpaper-picker" role="group" aria-label="Chat wallpaper">
                  {chatWallpapers.map((wallpaper) => <button key={wallpaper.name} className="wallpaper-swatch" type="button" aria-label={`${wallpaper.name} wallpaper`} aria-pressed={activeWallpaper.name === wallpaper.name} title={wallpaper.name} style={{ '--wallpaper-swatch': wallpaper.swatch } as React.CSSProperties} onClick={() => onPreferenceChange('appearance.chat_wallpaper', wallpaper.name)}>
                    {activeWallpaper.name === wallpaper.name && <Check size={13} />}
                  </button>)}
                </div>
              </div>
              <PreferenceToggle label="Show message avatars" checked={preferenceBoolean(preferences, 'appearance.show_avatars', true)} onChange={(value) => onPreferenceChange('appearance.show_avatars', value)} />
              <PreferenceToggle label="Use pointer cursor on clickable controls" checked={preferenceBoolean(preferences, 'appearance.pointer_cursor', true)} onChange={(value) => onPreferenceChange('appearance.pointer_cursor', value)} />
              <PreferenceToggle label="Compact message layout" checked={preferenceBoolean(preferences, 'appearance.compact')} onChange={(value) => onPreferenceChange('appearance.compact', value)} />
              <PreferenceToggle label="Show room previews" checked={preferenceBoolean(preferences, 'sidebar.show_previews', true)} onChange={(value) => onPreferenceChange('sidebar.show_previews', value)} />
              <PreferenceToggle label="Show unread counts" checked={preferenceBoolean(preferences, 'sidebar.show_unread_counts', true)} onChange={(value) => onPreferenceChange('sidebar.show_unread_counts', value)} />
              <PreferenceToggle label="Compact chat list" checked={preferenceBoolean(preferences, 'sidebar.compact_chat_list')} onChange={(value) => onPreferenceChange('sidebar.compact_chat_list', value)} />
              <PreferenceToggle label="Transparent sidebar" checked={preferenceBoolean(preferences, 'sidebar.transparent')} onChange={(value) => onPreferenceChange('sidebar.transparent', value)} />
              <PreferenceToggle label="Show network labels in the chat list" checked={preferenceBoolean(preferences, 'sidebar.network_icons')} onChange={(value) => onPreferenceChange('sidebar.network_icons', value)} />
              <label className="setting-row"><span><strong>Pinned chats</strong><small>List pinned rooms first, or only mark them with a star.</small></span>
                <select aria-label="Pinned chats" value={preferenceString(preferences, 'sidebar.pinned_style', 'Top of list')} onChange={(event) => onPreferenceChange('sidebar.pinned_style', event.target.value)}>
                  {['Top of list', 'Star only'].map((value) => <option key={value}>{value}</option>)}
                </select>
              </label>
              <PreferenceToggle label="Animate new messages" checked={preferenceBoolean(preferences, 'appearance.message_animations', true)} onChange={(value) => onPreferenceChange('appearance.message_animations', value)} />
              <PreferenceToggle label="Show message timestamps" checked={preferenceBoolean(preferences, 'appearance.show_timestamps', true)} onChange={(value) => onPreferenceChange('appearance.show_timestamps', value)} />
              <PreferenceToggle label="Hide deleted messages" checked={preferenceBoolean(preferences, 'appearance.hide_deleted_messages')} onChange={(value) => onPreferenceChange('appearance.hide_deleted_messages', value)} />
              <PreferenceToggle label="Balance message line wrapping" checked={preferenceBoolean(preferences, 'appearance.balanced_text')} onChange={(value) => onPreferenceChange('appearance.balanced_text', value)} />
              <PreferenceToggle label="Use a 24-hour clock" checked={preferenceBoolean(preferences, 'appearance.time_24_hour')} onChange={(value) => onPreferenceChange('appearance.time_24_hour', value)} />
              <PreferenceToggle label="Use relative timestamps" checked={preferenceBoolean(preferences, 'appearance.relative_timestamps')} onChange={(value) => onPreferenceChange('appearance.relative_timestamps', value)} />
            </div>
          )}
          {tab === 'Notifications' && (
            <div className="settings-section">
              <h3>Notifications</h3>
              <div className="setting-row notification-permission"><span><strong>System permission</strong><small>{notificationPermission === 'granted' ? 'Allowed' : notificationPermission === 'denied' ? 'Blocked by the system' : notificationPermission === 'unsupported' ? 'Unavailable for this launch' : 'Permission needed for desktop alerts.'}</small></span>
                <button className="settings-command" type="button" disabled={notificationPermission === 'granted' || notificationPermission === 'unsupported'} onClick={() => void enableSystemNotifications()}><Bell size={14} />{notificationPermission === 'granted' ? 'Allowed' : 'Enable'}</button>
              </div>
              <PreferenceToggle label="Enable notifications" checked={preferenceBoolean(preferences, 'notifications.enabled', true)} onChange={(value) => onPreferenceChange('notifications.enabled', value)} />
              <PreferenceToggle label="Notify while Pinger is focused" checked={!preferenceBoolean(preferences, 'notifications.only_when_unfocused', true)} onChange={(value) => onPreferenceChange('notifications.only_when_unfocused', !value)} />
              <PreferenceToggle label="Play a notification sound" checked={preferenceBoolean(preferences, 'notifications.sound')} onChange={(value) => onPreferenceChange('notifications.sound', value)} />
              <label className="setting-row"><span><strong>Notification volume</strong><small>Adjust the alert tone level.</small></span>
                <div className="setting-range"><input type="range" min="0" max="100" step="5" value={preferenceNumber(preferences, 'notifications.volume', 50)} aria-label="Notification volume" onChange={(event) => onPreferenceChange('notifications.volume', event.target.value)} /><output>{preferenceNumber(preferences, 'notifications.volume', 50)}%</output></div>
              </label>
              <label className="setting-row"><span><strong>Notification content</strong><small>Control what appears outside the app.</small></span>
                <select value={preferenceString(preferences, 'notifications.privacy', 'Sender and message preview')} onChange={(event) => onPreferenceChange('notifications.privacy', event.target.value)}>
                  {['Sender and message preview', 'Sender only', 'Hidden content'].map((value) => <option key={value}>{value}</option>)}
                </select>
              </label>
              <PreferenceToggle label="Direct message alerts" checked={preferenceBoolean(preferences, 'notifications.direct', true)} onChange={(value) => onPreferenceChange('notifications.direct', value)} />
              <PreferenceToggle label="Mention alerts" checked={preferenceBoolean(preferences, 'notifications.mentions', true)} onChange={(value) => onPreferenceChange('notifications.mentions', value)} />
              <PreferenceToggle label="Group message alerts" checked={preferenceBoolean(preferences, 'notifications.group')} onChange={(value) => onPreferenceChange('notifications.group', value)} />
              {[['Direct message tone', 'notifications.sound_direct'], ['Mention tone', 'notifications.sound_mention'], ['Group message tone', 'notifications.sound_group']].map(([label, key]) => (
                <label key={key} className="setting-row"><span><strong>{label}</strong><small>Sound played for this kind of alert.</small></span>
                  <select aria-label={label} value={preferenceString(preferences, key, 'Chime')} onChange={(event) => onPreferenceChange(key, event.target.value)}>
                    {['Chime', 'Pulse', 'Soft', 'Silent'].map((value) => <option key={value}>{value}</option>)}
                  </select>
                </label>
              ))}
              <label className="setting-row"><span><strong>Batch new messages</strong><small>Combine messages from the same room into one alert.</small></span>
                <select aria-label="Batch new messages" value={preferenceString(preferences, 'notifications.batch_seconds', '0')} onChange={(event) => onPreferenceChange('notifications.batch_seconds', event.target.value)}>
                  <option value="0">Off</option>
                  <option value="3">3 seconds</option>
                  <option value="10">10 seconds</option>
                  <option value="30">30 seconds</option>
                </select>
              </label>
              <label className="setting-row"><span><strong>Repeat unread reminder</strong><small>Remind you once about a room that is still unread.</small></span>
                <select aria-label="Repeat unread reminder" value={preferenceString(preferences, 'notifications.repeat_minutes', '0')} onChange={(event) => onPreferenceChange('notifications.repeat_minutes', event.target.value)}>
                  <option value="0">Off</option>
                  <option value="5">After 5 minutes</option>
                  <option value="15">After 15 minutes</option>
                  <option value="60">After 60 minutes</option>
                </select>
              </label>
              <PreferenceToggle label="Notify one-time codes immediately" checked={preferenceBoolean(preferences, 'notifications.otp_immediate', true)} onChange={(value) => onPreferenceChange('notifications.otp_immediate', value)} />
              <PreferenceToggle label="Copy one-time codes automatically" checked={preferenceBoolean(preferences, 'notifications.otp_copy')} onChange={(value) => onPreferenceChange('notifications.otp_copy', value)} />
              <label className="setting-row"><span><strong>Sound effects</strong><small>Play a short sound when a message is sent.</small></span>
                <select aria-label="Sound effects" value={preferenceString(preferences, 'sounds.effects', 'Off')} onChange={(event) => onPreferenceChange('sounds.effects', event.target.value)}>
                  <option value="Off">Off</option>
                  <option value="Sent messages">Sent messages</option>
                </select>
              </label>
            </div>
          )}
          {tab === 'Keyboard' && (
            <div className="settings-section">
              <h3>Keyboard</h3>
              <PreferenceToggle label="Use Ctrl + Enter to send" checked={preferenceBoolean(preferences, 'composer.ctrl_enter')} onChange={(value) => onPreferenceChange('composer.ctrl_enter', value)} />
              <label className="setting-row"><span><strong>Undo send window</strong><small>Hold a sent message briefly so it can be undone.</small></span>
                <select aria-label="Undo send window" value={preferenceString(preferences, 'composer.undo_send_seconds', '0')} onChange={(event) => onPreferenceChange('composer.undo_send_seconds', event.target.value)}>
                  <option value="0">Off</option>
                  {['5', '10', '20', '30'].map((seconds) => <option key={seconds} value={seconds}>{seconds} seconds</option>)}
                </select>
              </label>
              <PreferenceToggle label="Spellcheck messages" checked={preferenceBoolean(preferences, 'composer.spellcheck', true)} onChange={(value) => onPreferenceChange('composer.spellcheck', value)} />
              <PreferenceToggle label="Show formatting toolbar" checked={preferenceBoolean(preferences, 'composer.formatting_toolbar')} onChange={(value) => onPreferenceChange('composer.formatting_toolbar', value)} />
              <PreferenceToggle label="Suggest emoji for :shortcodes:" checked={preferenceBoolean(preferences, 'emoji.autocomplete')} onChange={(value) => onPreferenceChange('emoji.autocomplete', value)} />
              <PreferenceToggle label="Replace emoticons when sending" checked={preferenceBoolean(preferences, 'emoji.replace_emoticons')} onChange={(value) => onPreferenceChange('emoji.replace_emoticons', value)} />
              <PreferenceToggle label="Include timestamps when copying messages" checked={preferenceBoolean(preferences, 'clipboard.copy_timestamp')} onChange={(value) => onPreferenceChange('clipboard.copy_timestamp', value)} />
              <PreferenceToggle label="Use Up to focus the message list when the composer is empty" checked={preferenceBoolean(preferences, 'composer.arrow_up_focus_timeline')} onChange={(value) => onPreferenceChange('composer.arrow_up_focus_timeline', value)} />
              <PreferenceToggle label="Enable single-key shortcuts (R, J, K and the answered key)" checked={preferenceBoolean(preferences, 'shortcuts.single_keys', true)} onChange={(value) => onPreferenceChange('shortcuts.single_keys', value)} />
              {!preferenceBoolean(preferences, 'shortcuts.single_keys', true) && <p className="settings-note" role="status">Single-key shortcuts are off. Use the message and room menus instead. Ctrl / ⌘ shortcuts still work.</p>}
              <label className="setting-row"><span><strong>Mark answered shortcut</strong><small>Use one letter to mark the selected room answered.</small></span>
                <input className="shortcut-input" type="text" maxLength={1} value={preferenceString(preferences, 'shortcuts.mark_answered', 'E')} aria-label="Mark answered shortcut" onChange={(event) => { const value = event.target.value.toUpperCase(); if (value) onPreferenceChange('shortcuts.mark_answered', value) }} />
              </label>
              <dl className="keyboard-shortcut-list" aria-label="Keyboard shortcuts">
                {[
                  ['Ctrl / ⌘ + K', 'Quick switch rooms'],
                  ['Ctrl / ⌘ + F', 'Search this room'],
                  ['Ctrl / ⌘ + ,', 'Open settings'],
                  ['/ or Ctrl / ⌘ + Shift + S', 'Focus room search'],
                  ['Alt + ↑ / ↓', 'Switch rooms'],
                  ['Ctrl / ⌘ + Shift + ↑ / ↓', 'Switch needs-reply rooms'],
                  ['Ctrl / ⌘ + 1–9', 'Switch to a visible room'],
                  ['Ctrl / ⌘ + Alt + 1–9', 'Switch Matrix accounts'],
                  ['Ctrl / ⌘ + + / -', 'Adjust app zoom'],
                  ['Ctrl / ⌘ + 0', 'Reset app zoom'],
                  ['Ctrl / ⌘ + Shift + A', 'Show all rooms'],
                  ['Ctrl / ⌘ + Shift + U', 'Show needs-reply rooms'],
                  ['Ctrl / ⌘ + Shift + L', 'Jump to latest messages'],
                  ['↑ / ↓ / Home / End', 'Navigate the room list'],
                  ['Shift + F10', 'Open room actions'],
                  ['↑ / ↓ or J / K', 'Navigate the focused timeline'],
                  ['Home / End', 'Focus first / latest message'],
                  ['R', 'Reply to the focused message'],
                  [answeredShortcut, 'Mark room answered'],
                  ['Ctrl / ⌘ + Z', 'Undo mark answered'],
                  ['Escape', 'Close search or dialog'],
                  ['?', 'Open keyboard shortcuts'],
                  ['Tab / Shift + Tab', 'Move between controls'],
                  [preferenceBoolean(preferences, 'composer.ctrl_enter') ? 'Ctrl + Enter' : 'Enter', 'Send message'],
                  ['Shift + Enter', 'Add a line in the composer'],
                ].map(([keys, description]) => (
                  <div className="keyboard-shortcut-row" key={description}>
                    <dt>{description}</dt>
                    <dd><kbd>{keys}</kbd></dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
          {tab === 'History' && (
            <div className="settings-section">
              <h3>Message history</h3>
              <label className="setting-row"><span><strong>Messages to load</strong><small>Load this many recent messages when opening a room.</small></span>
                <select value={preferenceString(preferences, 'history.initial_messages', '100')} onChange={(event) => onPreferenceChange('history.initial_messages', event.target.value)}>
                  {['25', '50', '100', '250', '500', '1000'].map((value) => <option key={value} value={value}>{Number(value).toLocaleString()}</option>)}
                </select>
              </label>
            </div>
          )}
          {tab === 'Account' && (
            <div className="settings-section">
              <h3>Account</h3>
              <div className="account-summary"><Avatar uri={profile.avatarUri} label={profile.displayName || profile.userId} className="profile-avatar" /><div><strong>{profile.displayName || profile.userId}</strong><span>{profile.userId}</span></div></div>
              <div className="account-details"><span>Homeserver</span><strong>{profile.homeserver}</strong><span>This device</span><strong>{profile.deviceId}</strong><span>Encryption</span><strong>{profile.verified ? 'Verified' : 'Needs verification'}</strong></div>
              <PreferenceToggle label="Send typing indicators" checked={preferenceBoolean(preferences, 'privacy.send_typing', true)} onChange={(value) => onPreferenceChange('privacy.send_typing', value)} />
              <PreferenceToggle label="Incognito mode" checked={preferenceBoolean(preferences, 'privacy.incognito')} onChange={(value) => onPreferenceChange('privacy.incognito', value)} />
              {preferenceBoolean(preferences, 'privacy.incognito') && <p className="settings-note" role="status">Incognito mode is active.</p>}
              <p className="settings-note">Session data and encryption keys are stored locally on this device.</p>
              {!logoutConfirm ? (
                <button className="settings-command is-danger" type="button" onClick={() => setLogoutConfirm(true)}><LogOut size={15} />Log out</button>
              ) : (
                <div className="logout-confirm" role="group" aria-label="Confirm logout">
                  <span>Log out and clear local session data?</span>
                  <button type="button" onClick={() => setLogoutConfirm(false)}>Cancel</button>
                  <button className="is-danger" type="button" disabled={logoutBusy} onClick={() => void confirmLogout()}>{logoutBusy ? 'Logging out…' : 'Log out'}</button>
                </div>
              )}
            </div>
          )}
          {tab === 'Devices' && (
            <div className="settings-section">
              <h3>Active devices</h3>
              {devicesLoading && <p className="settings-note">Loading devices…</p>}
              {devicesError && <p className="settings-error" role="alert">{devicesError}</p>}
              {!devicesLoading && !devicesError && devices.length === 0 && <p className="settings-note">No devices were returned by the homeserver.</p>}
              {devices.map((device) => (
                <article className="device-row" key={device.id}>
                  <div><strong>{device.displayName || '(unnamed device)'}</strong><span>{device.id}{device.current ? ' · this device' : ''}</span><small>{device.lastSeenAt > 0 ? `Last active ${new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(device.lastSeenAt))}` : device.lastSeenIp || 'Last active time unavailable'}</small></div>
                  {!device.current && <button className="settings-command is-danger" type="button" onClick={() => void revokeDevice(device)}>Revoke</button>}
                </article>
              ))}
            </div>
          )}
          {tab === 'Storage' && (
            <div className="settings-section">
              <h3>Local storage</h3>
              {storageLoading && <p className="settings-note">Reading storage usage…</p>}
              {storageError && <p className="settings-error" role="alert">{storageError}</p>}
              {storage && <div className="storage-summary">
                <div><HardDrive size={16} /><span>Matrix database</span><strong>{formatBytes(storage.database.databaseBytes)}</strong></div>
                <div><HardDrive size={16} /><span>Database WAL</span><strong>{formatBytes(storage.database.walBytes)}</strong></div>
                <div><HardDrive size={16} /><span>Media cache</span><strong>{formatBytes(storage.media.bytes)} · {storage.media.files} files</strong></div>
              </div>}
              <button className="settings-command" type="button" disabled={storageLoading || storageAction !== ''} onClick={() => void runStorageAction('clear')}><HardDrive size={15} />{storageAction === 'clear' ? 'Clearing cache…' : 'Clear media cache'}</button>
              <button className="settings-command" type="button" disabled={storageLoading || storageAction !== ''} onClick={() => void runStorageAction('optimize')}><HardDrive size={15} />{storageAction === 'optimize' ? 'Optimizing database…' : 'Optimize database'}</button>
              <p className="settings-note">Clearing the media cache does not remove messages or encryption keys.</p>
              <ChatStorageSettings />
            </div>
          )}
          {tab === 'Security' && (
            <div className="settings-section">
              <h3>Encryption and room keys</h3>
              <p className="settings-note">Device verification: {profile.verified ? 'Verified' : 'This device needs verification.'}</p>
              <label className="security-field"><span>Backup passphrase</span><input type="password" autoComplete="new-password" value={backupPassphrase} onChange={(event) => setBackupPassphrase(event.target.value)} /></label>
              <label className="security-field"><span>Confirm passphrase</span><input type="password" autoComplete="new-password" value={backupConfirmation} onChange={(event) => setBackupConfirmation(event.target.value)} /></label>
              <button className="settings-command" type="button" disabled={keyBusy || !backupPassphrase || backupPassphrase !== backupConfirmation} onClick={() => void exportRoomKeys()}><Download size={15} />{keyBusy ? 'Working…' : 'Export room keys'}</button>
              <label className="security-field"><span>Import passphrase</span><input type="password" autoComplete="off" value={importPassphrase} onChange={(event) => setImportPassphrase(event.target.value)} /></label>
              <label className="settings-command file-command"><Upload size={15} />Import room key file<input type="file" accept=".txt,.json,application/octet-stream" disabled={keyBusy || !importPassphrase} onChange={(event) => void importRoomKeys(event)} /></label>
              {keyStatus && <p className="settings-note" role="status">{keyStatus}</p>}
            </div>
          )}
          {tab === 'Integrations' && (
            <div className="settings-section">
              <h3>Integrations</h3>
              <p className="settings-note">Chat networks are detected from the active account's chats, using room names and DM users. This is a best-effort view. Connecting a network requires a bridge set up on your homeserver; Pinger cannot create bridge logins.</p>
              <LocalAISettings
                endpoint={preferenceString(preferences, 'ai.endpoint', '')}
                model={preferenceString(preferences, 'ai.model', '')}
                translateLanguage={preferenceString(preferences, 'ai.translate_language', 'English')}
                onSave={onPreferenceChange}
              />
              <div className="integration-status" aria-label="Detected chat networks">
                {chatNetworks.map(({ code, name }) => {
                  const count = rooms.filter((room) => room.bridge === code).length
                  return (
                    <div key={code} className="account-details"><span>{name}</span><strong>{count > 0 ? `${count.toLocaleString()} ${count === 1 ? 'chat' : 'chats'} detected` : 'No chats detected'}</strong></div>
                  )
                })}
              </div>
            </div>
          )}
          {tab === 'Help' && (
            <div className="settings-section">
              <h3>Help center</h3>
              <p className="settings-note">Answers to common questions about Pinger.</p>
              <article className="help-item"><h4>Keyboard shortcuts</h4><p>The Keyboard tab lists every shortcut. Single-key shortcuts can be turned off there.</p><button className="settings-command" type="button" onClick={() => onTabChange('Keyboard')}>Open keyboard shortcuts</button></article>
              <article className="help-item"><h4>Storage and media</h4><p>Clearing media removes cached files only. Messages and encryption keys stay, and media downloads again when you open it.</p><button className="settings-command" type="button" onClick={() => onTabChange('Storage')}>Open storage</button></article>
              <article className="help-item"><h4>Local AI</h4><p>Pinger connects to a llama-server you run yourself. Set its address in Integrations. AI replies are drafts and are never sent automatically.</p></article>
              <article className="help-item"><h4>Chat networks</h4><p>WhatsApp, Telegram and other networks appear when your homeserver has bridges set up. Pinger cannot create bridge logins.</p></article>
              <article className="help-item"><h4>Encryption and room keys</h4><p>Export room keys from Security and protect the file with a passphrase. Keep the backup somewhere safe.</p></article>
              <a className="settings-link" href="https://github.com/cataWalter/pinger/issues" target="_blank" rel="noreferrer">Report an issue</a>
            </div>
          )}
          {tab === 'About' && (
            <div className="settings-section">
              <h3>Pinger</h3>
              <AboutPinger />
            </div>
          )}
        </div>
      </DialogSurface>
    </div>
  )
}

function PreferenceToggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="setting-toggle">
      <span>{label}</span>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
    </label>
  )
}

export default App
