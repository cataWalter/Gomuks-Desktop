import { Fragment, lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type ClipboardEvent, type DragEvent, type FormEvent, type KeyboardEvent, type ReactElement } from 'react'
import { createPortal } from 'react-dom'
import { Events } from '@wailsio/runtime'
import {
  ArrowDown,
  ArrowLeft,
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
  Smile,
  Star,
  Trash2,
  UsersRound,
  X,
  Download,
  HardDrive,
  LogOut,
  Upload,
  ZoomIn,
  ZoomOut,
} from 'lucide-react'
import { AppService } from '../bindings/mychat/desktop'
import type { Device, MediaCacheUsage, Member, Message, MessageSearchRequest, MessageSearchResult, Room, ScheduledJob, Snapshot, StorageUsage } from '../bindings/mychat/desktop/models.js'
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
}

type PreferenceValues = NonNullable<Snapshot['preferences']>
type RoomScope = 'all' | 'direct' | 'group'
type RoomRefinements = { unread: boolean; unanswered: boolean; favorite: boolean }
type MessageMenuItem = { key: string; label: string; ariaLabel: string; icon: ReactElement; danger?: boolean; disabled?: boolean; run: () => void }
type SettingsTab = 'Appearance' | 'Notifications' | 'Keyboard' | 'History' | 'Account' | 'Devices' | 'Storage' | 'Security' | 'About'
type ScheduledDraft =
  | { kind: 'message'; roomID: string; text: string; replyToEventID?: string; replyToSender?: string; replyToPreview?: string }
  | { kind: 'reminder'; roomID: string; eventID: string; text: string }

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

function preferenceNumber(preferences: PreferenceValues, key: string, fallback: number) {
  const value = preferences[key]
  const number = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN
  return Number.isFinite(number) ? Math.min(100, Math.max(0, number)) : fallback
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
      ; (firstFocusable ?? dialog).focus()

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

function playNotificationSound(volume: number) {
  if (volume <= 0) return
  try {
    const audio = new AudioContext()
    const oscillator = audio.createOscillator()
    const gain = audio.createGain()
    oscillator.type = 'sine'
    oscillator.frequency.setValueAtTime(720, audio.currentTime)
    gain.gain.setValueAtTime(0.035 * volume / 100, audio.currentTime)
    gain.gain.exponentialRampToValueAtTime(0.0001, audio.currentTime + 0.12)
    oscillator.connect(gain)
    gain.connect(audio.destination)
    oscillator.onended = () => { void audio.close() }
    oscillator.start()
    oscillator.stop(audio.currentTime + 0.12)
  } catch {
    return
  }
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
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('Appearance')
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
  const lastReadEventByRoom = useRef(new Map<string, string>())
  const draftsRef = useRef(new Map<string, string>())
  const triageBusyRef = useRef(false)
  const dragDepthRef = useRef(0)
  const timelineRef = useRef<HTMLDivElement>(null)
  const timelineHeightBeforePrepend = useRef<number | null>(null)
  const stickToBottomRef = useRef(true)
  const roomSwitchRef = useRef(false)
  const attachmentInputRef = useRef<HTMLInputElement>(null)
  const roomSearchInputRef = useRef<HTMLInputElement>(null)
  const composerInputRef = useRef<HTMLTextAreaElement>(null)
  const quickSwitcherInputRef = useRef<HTMLInputElement>(null)
  const pendingSearchJumpRef = useRef<{ roomID: string; eventID: string; timelineCursor: number } | null>(null)

  const selectedRoom = (snapshot?.rooms ?? []).find((room) => room.id === selectedRoomID)
  const selectedRoomAvatar = selectedRoom?.avatar || (
    historyRoomIDRef.current === selectedRoomID
      ? [...messages].reverse().find((message) => !message.own && message.avatar)?.avatar ?? ''
      : ''
  )
  const preferences = snapshot?.preferences ?? {}
  const answeredShortcut = preferenceString(preferences, 'shortcuts.mark_answered', 'E').trim().toUpperCase() || 'E'
  const rooms = snapshot?.rooms ?? []
  preferencesRef.current = preferences
  roomsRef.current = rooms
  messagesRef.current = messages
  hasOlderRef.current = hasOlder
  editTargetRef.current = editTarget
  const filteredRooms = rooms.filter((room) => {
    const query = search.trim().toLowerCase()
    const matchesQuery = !query || `${room.name} ${room.id} ${room.preview}`.toLowerCase().includes(query)
    const matchesScope = roomScope === 'all' || (roomScope === 'direct' ? room.direct : !room.direct)
    const matchesRefinements = (!roomRefinements.unread || room.unread > 0)
      && (!roomRefinements.unanswered || room.unanswered)
      && (!roomRefinements.favorite || room.favorite)
    return matchesQuery && matchesScope && matchesRefinements
  }).sort((left, right) => Number(right.favorite) - Number(left.favorite))
  const quickSwitcherRooms = rankQuickSwitcherRooms(rooms, quickSwitcherQuery)
  const unansweredCount = rooms.filter((room) => room.unanswered).length
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
    setScheduledDueAt(localDateTimeInputValue(Date.now() + 5 * 60_000))
    setScheduledDialogError('')
  }

  function openReminder(message: Message) {
    if (!selectedRoomID || !message.id || message.id.startsWith('~') || message.redacted) return
    setScheduledDraft({ kind: 'reminder', roomID: selectedRoomID, eventID: message.id, text: '' })
    setScheduledDueAt(localDateTimeInputValue(Date.now() + 60 * 60_000))
    setScheduledDialogError('')
  }

  useEffect(() => {
    selectedRoomRef.current = selectedRoomID
    void AppService.SetActiveRoom(selectedRoomID)
  }, [selectedRoomID])

  useEffect(() => {
    const handleGlobalAnsweredShortcut = (event: globalThis.KeyboardEvent) => {
      if (event.repeat || event.altKey || event.metaKey || event.ctrlKey || event.key.toUpperCase() !== answeredShortcut) return
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
  }, [answeredShortcut, leaveConfirmOpen, quickSwitcherOpen, roomContextMenu, roomInfoOpen, redactTarget, selectedRoomID, settingsOpen])

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
    if (!selectedRoomID) return
    const timeout = window.setTimeout(() => {
      const timeoutMilliseconds = composeText.trim() ? 5000 : 0
      void AppService.SetTyping(selectedRoomID, timeoutMilliseconds).catch((typingError: unknown) => setError(describeError(typingError)))
    }, 350)
    return () => window.clearTimeout(timeout)
  }, [composeText, selectedRoomID])

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
        if (!preferenceBoolean(currentPreferences, 'notifications.enabled', true)) continue
        if (preferenceBoolean(currentPreferences, `room.muted.${notification.roomId}`)) continue
        if (notification.roomId === selectedRoomRef.current) continue
        if (preferenceBoolean(currentPreferences, 'notifications.only_when_unfocused', true) && document.hasFocus()) continue
        if (typeof Notification === 'undefined' || Notification.permission !== 'granted') continue
        const roomName = notification.room || roomsRef.current.find((room) => room.id === notification.roomId)?.name || notification.roomId
        let title = notification.sender ? `${notification.sender} · ${roomName}` : roomName
        let body = notification.body
        switch (preferenceString(currentPreferences, 'notifications.privacy', 'Sender and message preview')) {
          case 'Sender only':
            body = 'New message'
            break
          case 'Hidden content':
            title = 'Matrix'
            body = 'New activity'
            break
        }
        try {
          const systemNotification = new Notification(title, { body })
          systemNotification.onclick = () => {
            window.focus()
            showRoom(notification.roomId)
          }
          if (preferenceBoolean(currentPreferences, 'notifications.sound')) playNotificationSound(preferenceNumber(currentPreferences, 'notifications.volume', 50))
        } catch (notificationError) {
          setError(describeError(notificationError))
        }
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
      if (typeof Notification !== 'undefined' && Notification.permission === 'granted') {
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
    if (!selectedRoomID || loadingHistory || historyRoomIDRef.current !== selectedRoomID) return
    const newestEvent = [...messages].reverse().find((message) => message.id && !message.id.startsWith('~'))
    if (!newestEvent || lastReadEventByRoom.current.get(selectedRoomID) === newestEvent.id) return
    lastReadEventByRoom.current.set(selectedRoomID, newestEvent.id)
    void AppService.MarkRead(selectedRoomID, newestEvent.id).catch((readError: unknown) => {
      if (lastReadEventByRoom.current.get(selectedRoomID) === newestEvent.id) lastReadEventByRoom.current.delete(selectedRoomID)
      setError(describeError(readError))
    })
  }, [loadingHistory, messages, selectedRoomID])

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
    }
  }

  function updateRoomUnanswered(roomID: string, unanswered: boolean) {
    setSnapshot((current) => current ? {
      ...current,
      rooms: (current.rooms ?? []).map((room) => room.id === roomID ? { ...room, unanswered } : room),
    } : current)
  }

  function openRoomContextMenu(roomID: string, x: number, y: number) {
    if (!roomsRef.current.find((room) => room.id === roomID)?.unanswered) return
    const menuWidth = 232
    const menuHeight = 48
    setMessageContextMenu(null)
    setRoomContextMenu({
      roomID,
      x: Math.max(8, Math.min(x, window.innerWidth - menuWidth - 8)),
      y: Math.max(8, Math.min(y, window.innerHeight - menuHeight - 8)),
    })
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
      { key: 'reply', label: 'Reply', ariaLabel: `Reply to ${message.senderName || message.sender}`, icon: <Reply size={14} />, disabled: unsent, run: () => { setEditTarget(null); setReplyTarget(message) } },
      { key: 'remind', label: 'Set reminder', ariaLabel: 'Set reminder for message', icon: <BellRing size={14} />, disabled: !manageable, run: () => openReminder(message) },
      { key: 'react', label: 'React', ariaLabel: 'Add reaction', icon: <Smile size={14} />, disabled: unsent, run: () => setReactingMessageID(message.id || '') },
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
      if (wasSelected && nextRoom) showRoom(nextRoom.id)
    } catch (triageError) {
      setError(describeError(triageError))
    } finally {
      triageBusyRef.current = false
    }
  }
  markRoomAnsweredRef.current = markRoomAnswered

  function handleMarkAnsweredKeyDown(event: KeyboardEvent<HTMLElement>, roomID: string) {
    if (event.altKey || event.metaKey || event.ctrlKey || event.key.toUpperCase() !== answeredShortcut) return
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

  function selectRoomByOffset(direction: number, unansweredOnly = false) {
    const availableRooms = filteredRooms.filter((room) => !unansweredOnly || room.unanswered)
    if (availableRooms.length === 0) return
    const currentIndex = availableRooms.findIndex((room) => room.id === selectedRoomRef.current)
    const nextIndex = currentIndex < 0
      ? direction > 0 ? 0 : availableRooms.length - 1
      : (currentIndex + direction + availableRooms.length) % availableRooms.length
    showRoom(availableRooms[nextIndex].id)
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
          showRoom(room.id)
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
    handleMarkAnsweredKeyDown(event, room.id)
    if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey) return
    if ((event.key === 'F10' && event.shiftKey || event.key === 'ContextMenu') && room.unanswered) {
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

  async function submitMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const roomID = selectedRoomID
    const text = composeText.trim()
    if (!roomID || !text || busy) return
    setBusy(true)
    setError('')
    try {
      const mentionUserIDs = [...new Set(mentionBindings.filter(({ token }) => text.includes(token)).map(({ userID }) => userID))]
      if (editTarget) {
        await AppService.EditMessage(roomID, text, editTarget.id)
      } else if (replyTarget) {
        await AppService.SendReply(roomID, text, replyTarget.id)
      } else if (mentionUserIDs.length > 0) {
        await AppService.SendMessageWithMentions(roomID, text, mentionUserIDs)
      } else {
        await AppService.SendMessage(roomID, text)
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

  async function sendAttachmentFiles(files: File[], roomID = selectedRoomID) {
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
        await AppService.SendAttachment(roomID, file.name, encodeBase64(bytes))
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
        <span>Gomuks Desktop</span>
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
            <span>gomuks</span>
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
          <div className="auth-side-mark">G.</div>
          <div className="auth-side-grid" />
          <div className="auth-side-bottom"><span>GOMUKS DESKTOP</span><span>01 / 01</span></div>
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
            <span>gomuks</span>
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
          <div className="auth-side-mark">G.</div>
          <div className="auth-side-grid" />
          <div className="auth-side-bottom"><span>GOMUKS DESKTOP</span><span>01 / 01</span></div>
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
      data-show-avatars={preferenceBoolean(preferences, 'appearance.show_avatars', true)}
      data-font-size={preferenceString(preferences, 'appearance.font_size', 'Medium').toLowerCase()}
      style={{ '--own-bubble': safeBubbleColor } as React.CSSProperties}
    >
      <aside className="sidebar">
        <div className="sidebar-brand">
          <span className="brand-glyph"><MessageCircleMore size={19} strokeWidth={2.1} /></span>
          <span>gomuks</span>
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
          <div className="room-scope-filters" role="group" aria-label="Room type">
            <button type="button" className={roomScope === 'all' ? 'is-active' : ''} aria-pressed={roomScope === 'all'} onClick={() => setRoomScope('all')}>All</button>
            <button type="button" className={roomScope === 'direct' ? 'is-active' : ''} aria-pressed={roomScope === 'direct'} onClick={() => setRoomScope('direct')}>DMs</button>
            <button type="button" className={roomScope === 'group' ? 'is-active' : ''} aria-pressed={roomScope === 'group'} onClick={() => setRoomScope('group')}>Groups</button>
          </div>
          <div className="room-refinement-filters" role="group" aria-label="Room refinements">
            <button type="button" className={roomRefinements.unread ? 'is-active' : ''} aria-label="Unread rooms" aria-pressed={roomRefinements.unread} title="Unread" onClick={() => setRoomRefinements((current) => ({ ...current, unread: !current.unread }))}><CircleDot size={14} /></button>
            <button type="button" className={roomRefinements.unanswered ? 'is-active' : ''} aria-label="Needs reply" aria-pressed={roomRefinements.unanswered} aria-keyshortcuts={answeredShortcut} title={`Needs reply (press ${answeredShortcut} to mark answered)`} onClick={() => setRoomRefinements((current) => ({ ...current, unanswered: !current.unanswered }))} onKeyDown={(event) => handleMarkAnsweredKeyDown(event, selectedRoomID)}>
              <Reply size={14} />{unansweredCount > 0 && <span>{unansweredCount}</span>}
            </button>
            <button type="button" className={roomRefinements.favorite ? 'is-active' : ''} aria-label="Pinned rooms" aria-pressed={roomRefinements.favorite} title="Pinned" onClick={() => setRoomRefinements((current) => ({ ...current, favorite: !current.favorite }))}><Star size={14} /></button>
          </div>
        </div>
        <div className="room-section-label"><span>ROOMS</span><span>{filteredRooms.length.toString().padStart(2, '0')}</span></div>
        <nav className="room-list" aria-label="Rooms">
          {filteredRooms.map((room, index) => (
            <RoomButton
              key={room.id}
              room={room}
              active={room.id === selectedRoomID}
              lastMessageByMe={Boolean(room.lastEventSender && room.lastEventSender === snapshot.profile.userId)}
              use24Hour={preferenceBoolean(preferences, 'appearance.time_24_hour')}
              onSelect={() => showRoom(room.id)}
              onKeyDown={(event) => handleRoomKeyDown(event, room, index)}
              onContextMenu={(event) => {
                if (!room.unanswered) return
                event.preventDefault()
                openRoomContextMenu(room.id, event.clientX, event.clientY)
              }}
            />
          ))}
          {filteredRooms.length === 0 && (
            <div className="room-empty">{rooms.length === 0 ? 'No rooms yet' : search ? 'No matching rooms' : 'No rooms match these filters'}</div>
          )}
        </nav>
        {roomContextMenu && <div className="room-context-menu" role="menu" style={{ left: roomContextMenu.x, top: roomContextMenu.y }}>
          <button type="button" role="menuitem" aria-keyshortcuts={answeredShortcut} onClick={() => void markRoomAnswered(roomContextMenu.roomID)} onKeyDown={(event) => handleMarkAnsweredKeyDown(event, roomContextMenu.roomID)}>
            <Check size={15} /><span>Remove from Needs reply</span><kbd>{answeredShortcut}</kbd>
          </button>
        </div>}
        <div className="sidebar-profile">
          <Avatar uri={snapshot.profile.avatarUri} label={snapshot.profile.displayName || snapshot.profile.userId} className="profile-avatar" />
          <div className="profile-copy">
            <strong>{snapshot.profile.displayName || snapshot.profile.userId}</strong>
            <span>{snapshot.profile.userId}</span>
          </div>
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
          {selectedRoom && !loadingHistory && messages.length === 0 && <div className="timeline-empty"><MessageCircleMore size={25} /><span>{hasOlder ? 'No messages in the past 24 hours' : 'No messages yet'}</span></div>}
          {selectedRoom && hasOlder && (
            <button className="load-older" type="button" onClick={loadOlderMessages} disabled={loadingOlder}>
              {loadingOlder ? <><span className="loading-mark" />Loading older messages</> : 'Load older messages'}
            </button>
          )}
          {messages.map((message, index) => {
            const previous = index > 0 ? messages[index - 1] : null
            const grouped = Boolean(previous && !previous.redacted && previous.sender === message.sender && message.timestamp - previous.timestamp < 5 * 60_000 && isSameDay(previous.timestamp, message.timestamp))
            const showDayDivider = !previous || !isSameDay(previous?.timestamp ?? 0, message.timestamp)
            const displayBody = displayMessageBody(message)
            return (
              <Fragment key={message.id || message.transactionId}>
                {showDayDivider && message.timestamp > 0 && <div className="day-divider" aria-label={formatDayLabel(message.timestamp)}><span>{formatDayLabel(message.timestamp)}</span></div>}
                <article className={`message-row ${message.own ? 'is-own' : ''} ${grouped ? 'is-grouped' : 'is-first'}`} data-event-id={message.id || message.transactionId} tabIndex={message.id || message.transactionId ? -1 : undefined} style={{ '--message-index': Math.min(index, 12) } as React.CSSProperties} onContextMenu={(event) => {
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
                      {message.timestamp > 0 && <time>{formatTimelineTime(message.timestamp, preferences)}</time>}
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
                    {message.mediaUri && !message.redacted && !message.decryptionError && <MessageMedia message={message} />}
                    {Object.keys(message.reactions ?? {}).length > 0 && <div className="message-reactions">
                      {Object.entries(message.reactions ?? {}).map(([emoji, count]) => (
                        <button key={emoji} type="button" className={`reaction-chip ${message.reacted?.[emoji] ? 'is-reacted' : ''}`} aria-pressed={message.reacted?.[emoji] ?? false} onClick={() => void sendReaction(message, emoji)}>{emoji} {typeof count === 'number' ? count : 0}</button>
                      ))}
                    </div>}
                    {message.sendError && message.sendError !== 'not sent' && <span className="message-error">{message.sendError}</span>}
                    {message.sending && !message.sendError && <span className="message-pending">Sending…</span>}
                    {reactingMessageID === message.id && <div className="reaction-picker" aria-label="Quick reactions">
                      {['👍', '❤️', '😂', '🎉', '👀', '🙏'].map((emoji) => <button key={emoji} type="button" aria-label={`React with ${emoji}`} onClick={() => void sendReaction(message, emoji)}>{emoji}</button>)}
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
          <textarea
            ref={composerInputRef}
            value={composeText}
            onChange={(event) => {
              const value = event.currentTarget.value
              setComposeText(value)
              if (!editTarget && selectedRoomID) {
                if (value) draftsRef.current.set(selectedRoomID, value)
                else draftsRef.current.delete(selectedRoomID)
              }
              setMentionBindings((current) => current.filter(({ token }) => value.includes(token)))
              updateMentionQuery(value, event.currentTarget.selectionStart)
            }}
            onKeyDown={handleComposerKeyDown}
            onPaste={handleComposerPaste}
            placeholder={selectedRoom ? `Message ${selectedRoom.name || selectedRoom.id}` : 'Select a room to start'}
            aria-label="Message"
            disabled={!selectedRoom || busy}
            rows={1}
          />
          <div className="composer-footer">
            <div className="composer-tools">
              <button className="icon-button" type="button" aria-label="Attach file" title="Attach file" disabled={!selectedRoom || busy} onClick={() => attachmentInputRef.current?.click()}><Paperclip size={15} /></button>
              <button className="icon-button" type="button" aria-label={replyTarget ? 'Schedule reply' : 'Schedule message'} title={replyTarget ? 'Schedule reply' : 'Schedule message'} disabled={!selectedRoom || !composeText.trim() || busy || Boolean(editTarget)} onClick={openScheduleMessage}><Clock3 size={15} /></button>
              <span>{selectedRoom ? busy ? 'Sending…' : preferenceBoolean(preferences, 'composer.ctrl_enter') ? 'Ctrl + Enter to send' : 'Enter to send · Shift + Enter for a new line' : ' '}</span>
            </div>
            <button className="send-button" type="submit" aria-label="Send message" disabled={!selectedRoom || !composeText.trim() || busy}>
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
      {settingsOpen && (
        <SettingsDialog
          tab={settingsTab}
          profile={snapshot.profile}
          preferences={preferences}
          onTabChange={setSettingsTab}
          onClose={() => setSettingsOpen(false)}
          onLogout={async () => { await AppService.Logout() }}
          onPreferenceChange={(key, value) => void setPreference(key, value)}
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
                : <label>Reminder note <input type="text" value={scheduledDraft.text} onChange={(event) => setScheduledDraft((current) => current?.kind === 'reminder' ? { ...current, text: event.target.value } : current)} placeholder="Optional note" /></label>}
              <label>{scheduledDraft.kind === 'message' ? 'Send at' : 'Remind me at'}
                <input type="datetime-local" required value={scheduledDueAt} onChange={(event) => setScheduledDueAt(event.target.value)} />
              </label>
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

function MessageMedia({ message }: { message: Message }) {
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
    const objectURL = await prepareMedia()
    if (!objectURL) return
    const anchor = document.createElement('a')
    anchor.href = objectURL
    anchor.download = filename
    document.body.append(anchor)
    anchor.click()
    anchor.remove()
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
        <audio className="media-player" controls preload="metadata" src={source} aria-label={filename} />
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

function RoomButton({ room, active, lastMessageByMe, use24Hour, onSelect, onKeyDown, onContextMenu }: { room: Room; active: boolean; lastMessageByMe: boolean; use24Hour: boolean; onSelect: () => void; onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void; onContextMenu: (event: React.MouseEvent<HTMLButtonElement>) => void }) {
  const hasUnread = room.unread > 0
  return (
    <button className={`room-button ${active ? 'is-active' : ''} ${room.muted ? 'is-muted' : ''} ${hasUnread ? 'has-unread' : ''}`} onClick={onSelect} onKeyDown={onKeyDown} onContextMenu={onContextMenu} aria-current={active ? 'page' : undefined} title={room.name || room.id}>
      <Avatar uri={room.avatar} label={room.name || room.id} className="room-avatar" style={avatarStyle(room.name || room.id)} deferLoad />
      <span className="room-copy">
        <span className="room-name">{room.favorite && <Star size={10} fill="currentColor" aria-label="Favorite" />}<span className="room-name-text">{room.name || room.id}</span>{room.muted && <span className="room-muted-mark" title="Muted">· muted</span>}</span>
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

function SettingsDialog({
  tab,
  profile,
  preferences,
  onTabChange,
  onClose,
  onLogout,
  onPreferenceChange,
}: {
  tab: SettingsTab
  profile: Snapshot['profile']
  preferences: PreferenceValues
  onTabChange: (tab: SettingsTab) => void
  onClose: () => void
  onLogout: () => Promise<void>
  onPreferenceChange: (key: string, value: boolean | string) => void
}) {
  const tabs: SettingsTab[] = ['Account', 'Appearance', 'Notifications', 'Keyboard', 'History', 'Devices', 'Storage', 'Security', 'About']
  const theme = preferenceString(preferences, 'appearance.theme', 'System default')
  const fontSize = preferenceString(preferences, 'appearance.font_size', 'Medium')
  const bubbleColor = preferenceString(preferences, 'appearance.bubble_color', '#236b52')
  const answeredShortcut = preferenceString(preferences, 'shortcuts.mark_answered', 'E').trim().toUpperCase() || 'E'
  const safeBubbleColor = /^#[0-9a-fA-F]{6}$/.test(bubbleColor) ? bubbleColor : '#236b52'
  const [devices, setDevices] = useState<Device[]>([])
  const [devicesLoading, setDevicesLoading] = useState(false)
  const [devicesError, setDevicesError] = useState('')
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

  async function enableSystemNotifications() {
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
          <div><span className="eyebrow">GOMUKS DESKTOP</span><h2 id="settings-title">Settings</h2></div>
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
              <label className="setting-row"><span><strong>Outgoing message color</strong><small>Choose a color for your messages.</small></span>
                <input className="color-input" type="color" value={safeBubbleColor} onChange={(event) => onPreferenceChange('appearance.bubble_color', event.target.value)} aria-label="Outgoing message color" />
              </label>
              <PreferenceToggle label="Show message avatars" checked={preferenceBoolean(preferences, 'appearance.show_avatars', true)} onChange={(value) => onPreferenceChange('appearance.show_avatars', value)} />
              <PreferenceToggle label="Compact message layout" checked={preferenceBoolean(preferences, 'appearance.compact')} onChange={(value) => onPreferenceChange('appearance.compact', value)} />
              <PreferenceToggle label="Use a 24-hour clock" checked={preferenceBoolean(preferences, 'appearance.time_24_hour')} onChange={(value) => onPreferenceChange('appearance.time_24_hour', value)} />
              <PreferenceToggle label="Use relative timestamps" checked={preferenceBoolean(preferences, 'appearance.relative_timestamps')} onChange={(value) => onPreferenceChange('appearance.relative_timestamps', value)} />
            </div>
          )}
          {tab === 'Notifications' && (
            <div className="settings-section">
              <h3>Notifications</h3>
              <div className="setting-row notification-permission"><span><strong>System permission</strong><small>{notificationPermission === 'granted' ? 'Allowed' : notificationPermission === 'denied' ? 'Blocked by the system' : notificationPermission === 'unsupported' ? 'Unavailable in this WebView' : 'Permission needed for desktop alerts.'}</small></span>
                <button className="settings-command" type="button" disabled={notificationPermission === 'granted' || notificationPermission === 'unsupported'} onClick={() => void enableSystemNotifications()}><Bell size={14} />{notificationPermission === 'granted' ? 'Allowed' : 'Enable'}</button>
              </div>
              <PreferenceToggle label="Enable notifications" checked={preferenceBoolean(preferences, 'notifications.enabled', true)} onChange={(value) => onPreferenceChange('notifications.enabled', value)} />
              <PreferenceToggle label="Only notify when the window is unfocused" checked={preferenceBoolean(preferences, 'notifications.only_when_unfocused', true)} onChange={(value) => onPreferenceChange('notifications.only_when_unfocused', value)} />
              <PreferenceToggle label="Play a notification sound" checked={preferenceBoolean(preferences, 'notifications.sound')} onChange={(value) => onPreferenceChange('notifications.sound', value)} />
              <label className="setting-row"><span><strong>Notification volume</strong><small>Adjust the alert tone level.</small></span>
                <div className="setting-range"><input type="range" min="0" max="100" step="5" value={preferenceNumber(preferences, 'notifications.volume', 50)} aria-label="Notification volume" onChange={(event) => onPreferenceChange('notifications.volume', event.target.value)} /><output>{preferenceNumber(preferences, 'notifications.volume', 50)}%</output></div>
              </label>
              <label className="setting-row"><span><strong>Notification content</strong><small>Control what appears outside the app.</small></span>
                <select value={preferenceString(preferences, 'notifications.privacy', 'Sender and message preview')} onChange={(event) => onPreferenceChange('notifications.privacy', event.target.value)}>
                  {['Sender and message preview', 'Sender only', 'Hidden content'].map((value) => <option key={value}>{value}</option>)}
                </select>
              </label>
            </div>
          )}
          {tab === 'Keyboard' && (
            <div className="settings-section">
              <h3>Keyboard</h3>
              <PreferenceToggle label="Use Ctrl + Enter to send" checked={preferenceBoolean(preferences, 'composer.ctrl_enter')} onChange={(value) => onPreferenceChange('composer.ctrl_enter', value)} />
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
          {tab === 'About' && (
            <div className="settings-section">
              <h3>Gomuks Desktop</h3>
              <div className="account-details"><span>Application</span><strong>Gomuks Desktop</strong><span>Matrix engine</span><strong>gomuks</strong><span>License</span><strong>AGPL-3.0-or-later</strong></div>
              <a className="settings-link" href="https://github.com/cataWalter/Gomuks-Desktop" target="_blank" rel="noreferrer">Project repository</a>
              <a className="settings-link" href="https://github.com/cataWalter/Gomuks-Desktop/issues" target="_blank" rel="noreferrer">Report an issue</a>
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
