import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AccountSummary, Message, Room, Snapshot } from '../bindings/mychat/desktop/models.js'
import App from './App'

const harness = vi.hoisted(() => ({
  eventHandlers: new Map<string, (event: { data: any }) => void>(),
  service: {
    Snapshot: vi.fn(),
    AddAccount: vi.fn(),
    SwitchAccount: vi.fn(),
    AccountRooms: vi.fn(),
    History: vi.fn(),
    SearchMessages: vi.fn(),
    ScheduledJobs: vi.fn(),
    ScheduleMessage: vi.fn(),
    ScheduleReply: vi.fn(),
    ScheduleReminder: vi.fn(),
    UpdateScheduledJob: vi.fn(),
    CancelScheduledJob: vi.fn(),
    RetryScheduledMessage: vi.fn(),
    DismissScheduledJob: vi.fn(),
    RoomMembers: vi.fn(),
    SetActiveRoom: vi.fn(),
    SetTyping: vi.fn(),
    MarkRead: vi.fn(),
    MarkUnread: vi.fn(),
    SetBooleanPreference: vi.fn(),
    SetStringPreference: vi.fn(),
    NativeNotificationsSupported: vi.fn(),
    NotificationPermission: vi.fn(),
    RequestNotificationPermission: vi.fn(),
    SendNotification: vi.fn(),
    SendMessage: vi.fn(),
    SendReply: vi.fn(),
    SendMessageWithMentions: vi.fn(),
    React: vi.fn(),
    AIComplete: vi.fn(),
    TestAIEndpoint: vi.fn(),
    StorageUsage: vi.fn(),
    MediaCacheUsage: vi.fn(),
    ClearMediaCache: vi.fn(),
    OptimizeDatabase: vi.fn(),
    RoomStorageUsage: vi.fn(),
    ClearCachedMedia: vi.fn(),
    AppVersion: vi.fn(),
    CheckForUpdates: vi.fn(),
  },
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, handler: (event: { data: any }) => void) => {
      harness.eventHandlers.set(name, handler)
      return () => harness.eventHandlers.delete(name)
    }),
  },
}))

vi.mock('../bindings/mychat/desktop', () => ({ AppService: harness.service }))

vi.mock('./MarkdownMessage', () => ({
  default: ({ body }: { body: string }) => <div className="message-body">{body}</div>,
}))

const room: Room = {
  id: '!room:example.org',
  name: 'Test room',
  topic: '',
  avatar: '',
  preview: '',
  lastSeen: 0,
  direct: true,
  favorite: false,
  muted: false,
  unread: 0,
  markedUnread: false,
  mentions: 0,
  unanswered: false,
  lastEventId: '',
  lastEventSender: '',
  lastEventAt: 0,
  accountId: 'primary',
  bridge: '',
}

const secondRoom: Room = { ...room, id: '!second:example.org', name: 'Second room', unanswered: true }

function message(id: string, body: string, timestamp: number): Message {
  return {
    id,
    transactionId: '',
    sender: '@alice:example.org',
    senderName: 'Alice',
    avatar: '',
    body,
    formattedBody: '',
    replyToEventId: '',
    replyToSender: '',
    replyToPreview: '',
    timestamp,
    timelineCursor: timestamp,
    sending: false,
    sendError: '',
    own: false,
    reactions: {},
    reacted: {},
    mediaUri: '',
    mediaType: '',
    mediaMime: '',
    mediaSize: 0,
    mediaWidth: 0,
    mediaHeight: 0,
    mediaDurationMs: 0,
    encrypted: false,
    decryptionError: '',
    edited: false,
    editHistory: [],
    redacted: false,
  }
}

// Message actions live in a right-click menu now, so tests open it the same way
// a user would.
function openMessageMenu(container: HTMLElement, eventID = '$latest') {
  const row = container.querySelector<HTMLElement>(`[data-event-id="${eventID}"]`)
  if (!row) throw new Error(`Missing timeline row for ${eventID}`)
  fireEvent.contextMenu(row, { clientX: 48, clientY: 48 })
}

const snapshot: Snapshot = {
  loggedIn: true,
  needsVerification: false,
  syncReady: true,
  connectionState: 'connected',
  profile: {
    userId: '@me:example.org',
    displayName: 'Me',
    homeserver: 'https://example.org',
    deviceId: 'DEVICE',
    avatarUri: '',
    verified: true,
  },
  accounts: [],
  activeAccountId: 'primary',
  rooms: [room],
  preferences: {},
}

describe('settings storage, integrations, help, and about', () => {
  let confirmSpy: { mockRestore: () => void; } | undefined

  afterEach(() => {
    confirmSpy?.mockRestore()
    confirmSpy = undefined
    cleanup()
  })

  beforeEach(() => {
    harness.eventHandlers.clear()
    harness.service.Snapshot.mockResolvedValue(snapshot)
    harness.service.AccountRooms.mockResolvedValue([])
    harness.service.History.mockResolvedValue({ messages: [], before: 0, hasMore: false })
    harness.service.ScheduledJobs.mockResolvedValue([])
    harness.service.SetActiveRoom.mockResolvedValue(undefined)
    harness.service.MarkRead.mockResolvedValue(undefined)
    harness.service.RoomMembers.mockResolvedValue([])
    harness.service.NativeNotificationsSupported.mockResolvedValue(false)
    harness.service.NotificationPermission.mockResolvedValue('default')
    harness.service.SetBooleanPreference.mockResolvedValue(undefined)
    harness.service.SetStringPreference.mockResolvedValue(undefined)
    harness.service.StorageUsage.mockResolvedValue({ databaseBytes: 2048, walBytes: 0 })
    harness.service.MediaCacheUsage.mockResolvedValue({ bytes: 4096, files: 3 })
    harness.service.RoomStorageUsage.mockResolvedValue([])
    harness.service.ClearCachedMedia.mockResolvedValue({ files: 0, bytes: 0 })
    harness.service.AppVersion.mockResolvedValue('1.0.0')
    harness.service.CheckForUpdates.mockResolvedValue({ currentVersion: '1.0.0', latestVersion: '', releaseUrl: 'https://github.com/cataWalter/pinger/releases', publishedAt: '', releaseNotes: '', updateAvailable: false, message: 'Pinger is up to date.' })
  })

  async function openSettingsTab(name: string) {
    const user = userEvent.setup()
    render(<App />)
    await user.click(await screen.findByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name }))
    return user
  }

  it('shows chat networks detected from bridged rooms instead of placeholder statuses', async () => {
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      rooms: [room, { ...room, id: '!wa1:example.org', name: 'Alice', bridge: 'WA' }, { ...room, id: '!wa2:example.org', name: 'Bob', bridge: 'WA' }],
    })
    await openSettingsTab('Integrations')

    const status = screen.getByLabelText('Detected chat networks')
    expect(within(status).getByText('WhatsApp').nextElementSibling?.textContent).toBe('2 chats detected')
    expect(within(status).getByText('Telegram').nextElementSibling?.textContent).toBe('No chats detected')
    expect(screen.queryByText('Not connected')).toBeNull()
    expect(screen.getByText(/Connecting a network requires a bridge set up on your homeserver/)).toBeTruthy()
  })

  it('lists per-chat storage and clears matching cached media after confirmation', async () => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    harness.service.RoomStorageUsage
      .mockResolvedValueOnce([{ roomId: '!room:example.org', name: 'Test room', messages: 12, textBytes: 1024, mediaBytes: 4096, mediaFiles: 3 }])
      .mockResolvedValue([{ roomId: '!room:example.org', name: 'Test room', messages: 12, textBytes: 1024, mediaBytes: 0, mediaFiles: 0 }])
    harness.service.ClearCachedMedia.mockResolvedValue({ files: 3, bytes: 4096 })
    const user = await openSettingsTab('Storage')

    const table = await screen.findByRole('table', { name: 'Storage used by each chat' })
    expect(within(table).getByText('Test room')).toBeTruthy()
    await user.selectOptions(screen.getByRole('combobox', { name: 'Cleanup age' }), '30')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Cleanup media type' }), 'image')
    await user.click(within(table).getByRole('button', { name: 'Clear media in Test room' }))

    await waitFor(() => expect(harness.service.ClearCachedMedia).toHaveBeenCalledWith({ roomId: '!room:example.org', olderThanDays: 30, mediaType: 'image' }))
    expect(confirmSpy).toHaveBeenCalled()
    expect(await screen.findByText(/Removed 3 cached files/)).toBeTruthy()
  })

  it('does not clear media when the cleanup confirmation is declined', async () => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    harness.service.RoomStorageUsage.mockResolvedValue([{ roomId: '!room:example.org', name: 'Test room', messages: 1, textBytes: 10, mediaBytes: 20, mediaFiles: 1 }])
    const user = await openSettingsTab('Storage')

    await user.click(await screen.findByRole('button', { name: 'Clear matching media' }))
    expect(confirmSpy).toHaveBeenCalled()
    expect(harness.service.ClearCachedMedia).not.toHaveBeenCalled()
  })

  it('checks for updates only when requested and shows the release', async () => {
    harness.service.CheckForUpdates.mockResolvedValueOnce({
      currentVersion: '1.0.0',
      latestVersion: 'v1.1.0',
      releaseUrl: 'https://github.com/cataWalter/pinger/releases/tag/v1.1.0',
      publishedAt: '2026-03-01T00:00:00Z',
      releaseNotes: 'Adds storage cleanup.',
      updateAvailable: true,
      message: 'Version v1.1.0 is available.',
    })
    const user = await openSettingsTab('About')

    expect(await screen.findByText('1.0.0')).toBeTruthy()
    expect(harness.service.CheckForUpdates).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Check for updates' }))

    expect(await screen.findByText('Version v1.1.0 is available.')).toBeTruthy()
    expect(screen.getByText('Adds storage cleanup.')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'View release' }).getAttribute('href')).toBe('https://github.com/cataWalter/pinger/releases/tag/v1.1.0')
  })

  it('reports update check failures without changing the About page', async () => {
    harness.service.CheckForUpdates.mockRejectedValueOnce(new Error('Network unavailable'))
    const user = await openSettingsTab('About')

    await user.click(await screen.findByRole('button', { name: 'Check for updates' }))
    expect(await screen.findByText('Network unavailable')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'View release' })).toBeNull()
  })

  it('links the help center to the keyboard and storage settings', async () => {
    const user = await openSettingsTab('Help')

    await user.click(screen.getByRole('button', { name: 'Open storage' }))
    expect((await screen.findByRole('tab', { name: 'Storage' })).getAttribute('aria-selected')).toBe('true')
  })

  it('turns single-key shortcuts off from the keyboard settings', async () => {
    const user = await openSettingsTab('Keyboard')

    await user.click(screen.getByLabelText('Enable single-key shortcuts (R, J, K and the answered key)'))
    await waitFor(() => expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith('shortcuts.single_keys', false))
    expect(await screen.findByText(/Single-key shortcuts are off/)).toBeTruthy()
  })
})

describe('room history updates', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  beforeEach(() => {
    harness.eventHandlers.clear()
    harness.service.Snapshot.mockResolvedValue(snapshot)
    harness.service.AccountRooms.mockResolvedValue([])
    harness.service.AddAccount.mockResolvedValue(snapshot)
    harness.service.SwitchAccount.mockResolvedValue(snapshot)
    harness.service.RoomMembers.mockResolvedValue([])
    harness.service.SearchMessages.mockResolvedValue({ results: [], nextBatch: '' })
    harness.service.ScheduledJobs.mockResolvedValue([])
    harness.service.ScheduleMessage.mockResolvedValue({})
      harness.service.ScheduleReply.mockResolvedValue({})
    harness.service.ScheduleReminder.mockResolvedValue({})
    harness.service.UpdateScheduledJob.mockResolvedValue(undefined)
    harness.service.CancelScheduledJob.mockResolvedValue(undefined)
    harness.service.RetryScheduledMessage.mockResolvedValue(undefined)
    harness.service.DismissScheduledJob.mockResolvedValue(undefined)
    harness.service.SetActiveRoom.mockResolvedValue(undefined)
    harness.service.SetTyping.mockResolvedValue(undefined)
    harness.service.MarkRead.mockResolvedValue(undefined)
    harness.service.SetBooleanPreference.mockResolvedValue(undefined)
    harness.service.SetStringPreference.mockResolvedValue(undefined)
    harness.service.SendMessage.mockResolvedValue(undefined)
    harness.service.SendReply.mockResolvedValue(undefined)
    harness.service.SendMessageWithMentions.mockResolvedValue(undefined)
    harness.service.React.mockResolvedValue(undefined)
    harness.service.NativeNotificationsSupported.mockResolvedValue(false)
    harness.service.NotificationPermission.mockResolvedValue('default')
    harness.service.RequestNotificationPermission.mockResolvedValue('granted')
    harness.service.SendNotification.mockResolvedValue(false)
    harness.service.AIComplete.mockResolvedValue('')
    harness.service.TestAIEndpoint.mockResolvedValue(undefined)

    let firstPageRequest = true
    harness.service.History.mockImplementation(async (_roomID: string, before: number) => {
      if (before === 0 && firstPageRequest) {
        firstPageRequest = false
        return { messages: [message('$latest', 'latest message', 300)], before: 50, hasMore: true }
      }
      if (before === 50) {
        return { messages: [message('$older', 'older history', 100)], before: 20, hasMore: true }
      }
      if (before === 20) {
        return { messages: [message('$oldest', 'oldest history', 50)], before: 10, hasMore: false }
      }
      return {
        messages: [message('$latest', 'latest message updated', 300), message('$new', 'new live message', 400)],
        before: 75,
        hasMore: true,
      }
    })
  })

  it('keeps AI actions disabled until an AI endpoint is set', async () => {
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Summarize recent messages' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: 'Ask AI about the composer text' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('sends an Ask AI question to the local endpoint and only drafts the reply', async () => {
    const user = userEvent.setup()
    harness.service.AIComplete.mockResolvedValue('Here is a draft reply.')
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'ai.endpoint': 'http://localhost:8080' } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    const composer = screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement
    await user.type(composer, 'What did Alice ask?')
    await user.click(screen.getByRole('button', { name: 'Ask AI about the composer text' }))

    await waitFor(() => expect(harness.service.AIComplete).toHaveBeenCalledTimes(1))
    const request = harness.service.AIComplete.mock.calls[0][0]
    expect(request[request.length - 1]).toEqual({ role: 'user', content: 'What did Alice ask?' })
    expect(request[1].content).toContain('Alice: latest message')

    const dialog = await screen.findByRole('dialog', { name: 'Ask AI' })
    await within(dialog).findByText('Here is a draft reply.')
    await user.click(within(dialog).getByRole('button', { name: 'Use as draft' }))

    expect(composer.value).toBe('Here is a draft reply.')
    expect(screen.queryByRole('dialog', { name: 'Ask AI' })).toBeNull()
    expect(harness.service.SendMessage).not.toHaveBeenCalled()
    expect(harness.service.SendMessageWithMentions).not.toHaveBeenCalled()
  })

  it('translates a message into the composer using the configured language', async () => {
    const user = userEvent.setup()
    harness.service.AIComplete.mockResolvedValue('Hola')
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      preferences: { 'ai.endpoint': 'http://localhost:8080', 'ai.translate_language': 'Spanish' },
    })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Translate message with local AI' }))

    await waitFor(() => expect(harness.service.AIComplete).toHaveBeenCalledTimes(1))
    const request = harness.service.AIComplete.mock.calls[0][0]
    expect(request[0].content).toContain('Spanish')
    expect(request[1]).toEqual({ role: 'user', content: 'latest message' })

    const dialog = await screen.findByRole('dialog', { name: 'Translate message' })
    await within(dialog).findByText('Hola')
    await user.click(within(dialog).getByRole('button', { name: 'Use as draft' }))
    expect((screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement).value).toBe('Hola')
    expect(harness.service.SendMessage).not.toHaveBeenCalled()
  })

  it('summarizes recent messages in a read-only panel', async () => {
    const user = userEvent.setup()
    harness.service.AIComplete.mockResolvedValue('- Alice shared an update')
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'ai.endpoint': 'http://localhost:8080' } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Summarize recent messages' }))
    const dialog = await screen.findByRole('dialog', { name: 'Summarize room' })
    await within(dialog).findByText('- Alice shared an update')
    expect((screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement).value).toBe('')

    await user.click(within(dialog).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Summarize room' })).toBeNull())
    expect(harness.service.SendMessage).not.toHaveBeenCalled()
  })

  it('saves the AI endpoint and reports whether the model is ready', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'ai.endpoint': 'http://localhost:8080' } })
    harness.service.TestAIEndpoint.mockRejectedValueOnce(new Error('AI model is still loading'))
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Integrations' }))

    await user.click(screen.getByRole('button', { name: 'Test endpoint' }))
    expect(await screen.findByText('AI model is still loading')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Test endpoint' }))
    expect(await screen.findByText('Model is loaded and ready.')).toBeTruthy()

    const endpoint = screen.getByLabelText('Endpoint')
    await user.clear(endpoint)
    await user.type(endpoint, 'http://localhost:9090')
    expect((screen.getByRole('button', { name: 'Test endpoint' }) as HTMLButtonElement).disabled).toBe(true)

    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('ai.endpoint', 'http://localhost:9090'))
  })

  it('keeps loaded older messages when the selected room receives a live update', async () => {
    const user = userEvent.setup()
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Load older messages' }))
    expect(await screen.findByText('older history')).toBeTruthy()

    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          rooms: [room],
          timelineChangedRoomId: [room.id],
          initialSyncComplete: true,
          needsVerification: false,
          connectionState: 'connected',
        },
      })
    })

    expect(await screen.findByText('new live message')).toBeTruthy()
    await waitFor(() => expect(screen.getByText('older history')).toBeTruthy())
    expect(harness.service.History).toHaveBeenCalledTimes(3)

    await user.click(screen.getByRole('button', { name: 'Load older messages' }))
    expect(await screen.findByText('oldest history')).toBeTruthy()
    expect(screen.getByText('older history')).toBeTruthy()
    expect(harness.service.History).toHaveBeenLastCalledWith(room.id, 20)
  })

  it('switches the active Matrix account and loads its rooms', async () => {
    const user = userEvent.setup()
    harness.service.ScheduledJobs.mockResolvedValueOnce([{
      id: 'primary-scheduled',
      kind: 'message',
      roomId: room.id,
      text: 'primary job',
      eventId: '',
      dueAt: Date.now() + 60_000,
      createdAt: Date.now(),
      status: 'pending',
      error: '',
    }]).mockResolvedValueOnce([])
    const secondProfile = {
      userId: '@two:example.net',
      displayName: 'Two',
      homeserver: 'https://example.net',
      deviceId: 'DEVICE_TWO',
      avatarUri: '',
      verified: true,
    }
    const secondAccount: AccountSummary = { id: 'second', profile: secondProfile }
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      accounts: [{ id: 'primary', profile: snapshot.profile }, secondAccount],
    })
    harness.service.SwitchAccount.mockResolvedValue({
      ...snapshot,
      activeAccountId: secondAccount.id,
      profile: secondProfile,
      rooms: [secondRoom],
    })
    const { container } = render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    await waitFor(() => expect(container.querySelector('.scheduled-job-count')).toBeTruthy())
    await user.click(screen.getByRole('button', { name: 'Manage Matrix accounts' }))
    await user.click(screen.getByRole('button', { name: /@two:example\.net/ }))

    await waitFor(() => expect(harness.service.SwitchAccount).toHaveBeenCalledWith(secondAccount.id))
    await waitFor(() => expect(container.querySelector('.scheduled-job-count')).toBeNull())
    expect(await screen.findByText('@two:example.net')).toBeTruthy()
    await waitFor(() => expect(harness.service.History).toHaveBeenCalledWith(secondRoom.id, 0))
  })

  it('filters the inbox by account and switches accounts when opening another account room', async () => {
    const user = userEvent.setup()
    const secondProfile = {
      userId: '@two:example.net',
      displayName: 'Two',
      homeserver: 'https://example.net',
      deviceId: 'DEVICE_TWO',
      avatarUri: '',
      verified: true,
    }
    const secondAccount: AccountSummary = { id: 'second', profile: secondProfile }
    const otherRoom: Room = { ...room, id: '!other:example.net', name: 'Other account room', accountId: 'second' }
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      accounts: [{ id: 'primary', profile: snapshot.profile }, secondAccount],
    })
    harness.service.AccountRooms.mockResolvedValue([room, otherRoom])
    harness.service.SwitchAccount.mockResolvedValue({
      ...snapshot,
      activeAccountId: secondAccount.id,
      profile: secondProfile,
      rooms: [otherRoom],
    })
    render(<App />)

    expect(await screen.findByText('Other account room')).toBeTruthy()
    const accountFilter = screen.getByRole('combobox', { name: 'Filter rooms by account' })
    await user.selectOptions(accountFilter, 'second')
    const roomList = screen.getByRole('navigation', { name: 'Rooms' })
    expect(within(roomList).queryByText('Test room')).toBeNull()
    expect(within(roomList).getByText('Other account room')).toBeTruthy()

    await user.click(within(roomList).getByText('Other account room'))
    await waitFor(() => expect(harness.service.SwitchAccount).toHaveBeenCalledWith(secondAccount.id))
  })

  it('switches Matrix accounts with the account number shortcut', async () => {
    const secondProfile = {
      userId: '@two:example.net',
      displayName: 'Two',
      homeserver: 'https://example.net',
      deviceId: 'DEVICE_TWO',
      avatarUri: '',
      verified: true,
    }
    const secondAccount: AccountSummary = { id: 'second', profile: secondProfile }
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      accounts: [{ id: 'primary', profile: snapshot.profile }, secondAccount],
    })
    harness.service.SwitchAccount.mockResolvedValue({
      ...snapshot,
      activeAccountId: secondAccount.id,
      profile: secondProfile,
      rooms: [secondRoom],
    })
    const { container } = render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    fireEvent.keyDown(container.querySelector('.app-shell')!, { key: '2', code: 'Digit2', ctrlKey: true, altKey: true })

    await waitFor(() => expect(harness.service.SwitchAccount).toHaveBeenCalledWith(secondAccount.id))
    expect(await screen.findByText('@two:example.net')).toBeTruthy()
  })

  it('adds another Matrix account from the account manager', async () => {
    const user = userEvent.setup()
    const secondProfile = {
      userId: '@two:example.net',
      displayName: 'Two',
      homeserver: 'https://example.net',
      deviceId: 'DEVICE_TWO',
      avatarUri: '',
      verified: true,
    }
    const secondAccount: AccountSummary = { id: 'second', profile: secondProfile }
    harness.service.AddAccount.mockResolvedValue({
      ...snapshot,
      activeAccountId: secondAccount.id,
      accounts: [{ id: 'primary', profile: snapshot.profile }, secondAccount],
      profile: secondProfile,
      rooms: [secondRoom],
    })
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Manage Matrix accounts' }))
    await user.click(screen.getByRole('button', { name: 'Add account' }))
    await user.clear(screen.getByLabelText('Homeserver'))
    await user.type(screen.getByLabelText('Homeserver'), 'https://example.net')
    await user.type(screen.getByLabelText('Username'), '@two:example.net')
    await user.type(screen.getByLabelText('Password'), 'secret')
    await user.click(screen.getByRole('button', { name: 'Add account' }))

    await waitFor(() => expect(harness.service.AddAccount).toHaveBeenCalledWith('https://example.net', '@two:example.net', 'secret'))
    expect(await screen.findByText('@two:example.net')).toBeTruthy()
  })

  it('sends incoming alerts through the native notification service when available', async () => {
    harness.service.NativeNotificationsSupported.mockResolvedValue(true)
    harness.service.SendNotification.mockResolvedValue(true)
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      rooms: [room, secondRoom],
      preferences: { 'notifications.only_when_unfocused': false },
    })
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          notifications: [{ roomId: secondRoom.id, room: secondRoom.name, sender: 'Alice', body: 'A native alert' }],
        },
      })
    })

    await waitFor(() => expect(harness.service.SendNotification).toHaveBeenCalledWith('Alice · Second room', 'A native alert', secondRoom.id))
    act(() => {
      harness.eventHandlers.get('notification:open-room')?.({ data: secondRoom.id })
    })
    await waitFor(() => expect(harness.service.History).toHaveBeenCalledWith(secondRoom.id, 0))
  })

  it('keeps group alerts off by default and sends them when enabled', async () => {
    harness.service.NativeNotificationsSupported.mockResolvedValue(true)
    harness.service.SendNotification.mockClear()
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      rooms: [room, secondRoom],
      preferences: { 'notifications.only_when_unfocused': false },
    })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          notifications: [{ roomId: secondRoom.id, room: secondRoom.name, sender: 'Bob', body: 'group chatter', category: 'group' }],
        },
      })
    })
    expect(harness.service.SendNotification).not.toHaveBeenCalled()
  })

  it('sends group alerts when the group setting is on', async () => {
    harness.service.NativeNotificationsSupported.mockResolvedValue(true)
    harness.service.SendNotification.mockClear()
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      rooms: [room, secondRoom],
      preferences: { 'notifications.only_when_unfocused': false, 'notifications.group': true },
    })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          notifications: [{ roomId: secondRoom.id, room: secondRoom.name, sender: 'Bob', body: 'group chatter', category: 'group' }],
        },
      })
    })
    await waitFor(() => expect(harness.service.SendNotification).toHaveBeenCalledWith('Bob · Second room', 'group chatter', secondRoom.id))
  })

  it('combines batched alerts from one room into a single notification', async () => {
    harness.service.NativeNotificationsSupported.mockResolvedValue(true)
    harness.service.SendNotification.mockClear()
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      rooms: [room, secondRoom],
      preferences: { 'notifications.only_when_unfocused': false, 'notifications.batch_seconds': '3' },
    })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    vi.useFakeTimers()
    try {
      act(() => {
        harness.eventHandlers.get('matrix:update')?.({
          data: {
            notifications: [
              { roomId: secondRoom.id, room: secondRoom.name, sender: 'Alice', body: 'one', category: 'direct' },
              { roomId: secondRoom.id, room: secondRoom.name, sender: 'Bob', body: 'two', category: 'direct' },
            ],
          },
        })
      })
      expect(harness.service.SendNotification).not.toHaveBeenCalled()
      await act(async () => {
        vi.advanceTimersByTime(3000)
      })
      expect(harness.service.SendNotification).toHaveBeenCalledTimes(1)
      expect(harness.service.SendNotification).toHaveBeenCalledWith('Second room', '2 new messages from Alice, Bob', secondRoom.id)
    } finally {
      vi.useRealTimers()
    }
  })

  it('sends the default reaction directly when the quick picker is off', async () => {
    const user = userEvent.setup()
    harness.service.React.mockClear()
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      preferences: { 'reactions.quick_enabled': false, 'reactions.default_emoji': '🎉' },
    })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Add reaction' }))
    await waitFor(() => expect(harness.service.React).toHaveBeenCalledWith(room.id, '$latest', '🎉'))
  })

  it('starts a reply on double-click when configured', async () => {
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      preferences: { 'messages.double_click': 'Reply' },
    })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    const row = container.querySelector<HTMLElement>('[data-event-id="$latest"]')
    if (!row) throw new Error('Missing timeline row for $latest')
    fireEvent.doubleClick(row)
    expect(await screen.findByText('Replying to Alice')).toBeTruthy()
  })

  it('shows the bridge network label only when the setting is on', async () => {
    harness.service.Snapshot.mockResolvedValue({
      ...snapshot,
      rooms: [{ ...room, bridge: 'WhatsApp' }],
      preferences: { 'sidebar.network_icons': true },
    })
    render(<App />)
    expect(await screen.findByText('WhatsApp')).toBeTruthy()
  })

  it('loads older messages automatically when the timeline is scrolled to the top', async () => {
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    const timeline = screen.getByRole('region', { name: 'Message timeline' })

    fireEvent.scroll(timeline)

    expect(await screen.findByText('older history')).toBeTruthy()
    expect(harness.service.History).toHaveBeenLastCalledWith(room.id, 50)
  })

  it('composes room type, unread, needs reply, pinned, and search filters', async () => {
    const user = userEvent.setup()
    const pinnedDirect = { ...secondRoom, name: 'Pinned direct', unread: 2, favorite: true }
    const pinnedGroup: Room = { ...room, id: '!group:example.org', name: 'Pinned project', direct: false, favorite: true, unread: 1, unanswered: true }
    const unreadDirect: Room = { ...room, id: '!unread:example.org', name: 'Unread direct', unread: 1 }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, pinnedDirect, pinnedGroup, unreadDirect] })
    render(<App />)

    await screen.findByText('latest message')
    const roomList = screen.getByRole('navigation', { name: 'Rooms' })
    const groupsFilter = screen.getByRole('button', { name: /^Groups$/ })
    await user.click(groupsFilter)
    expect(groupsFilter.getAttribute('aria-pressed')).toBe('true')
    expect(within(roomList).getByRole('button', { name: /Pinned project/ })).toBeTruthy()
    expect(within(roomList).queryByRole('button', { name: /Pinned direct/ })).toBeNull()

    await user.click(screen.getByRole('button', { name: 'Unread rooms' }))
    await user.click(screen.getByRole('button', { name: 'Needs reply' }))
    await user.click(screen.getByRole('button', { name: 'Pinned rooms' }))
    await user.type(screen.getByRole('textbox', { name: 'Find a room' }), 'Pinned')
    expect(within(roomList).getByRole('button', { name: /Pinned project/ })).toBeTruthy()
    expect(within(roomList).queryByRole('button', { name: /Unread direct/ })).toBeNull()

    await user.click(screen.getByRole('button', { name: /^DMs$/ }))
    expect(within(roomList).getByRole('button', { name: /Pinned direct/ })).toBeTruthy()
    expect(within(roomList).queryByRole('button', { name: /Pinned project/ })).toBeNull()
    await user.click(screen.getByRole('button', { name: /^All$/ }))
    expect(within(roomList).getByRole('button', { name: /Pinned direct/ })).toBeTruthy()
    expect(within(roomList).getByRole('button', { name: /Pinned project/ })).toBeTruthy()
  })

  it('explains when active inbox filters hide every room', async () => {
    const user = userEvent.setup()
    render(<App />)
    await screen.findByText('latest message')

    await user.click(screen.getByRole('button', { name: 'Unread rooms' }))

    expect(screen.getByText('No rooms match these filters')).toBeTruthy()
  })

  it('follows the operating system color scheme for System default', async () => {
    let changeHandler: ((event: MediaQueryListEvent) => void) | undefined
    vi.stubGlobal('matchMedia', vi.fn(() => ({
      matches: true,
      addEventListener: (_type: string, listener: (event: MediaQueryListEvent) => void) => { changeHandler = listener },
      removeEventListener: vi.fn(),
    })))
    const { container } = render(<App />)

    await waitFor(() => expect(container.querySelector('.app-shell')?.getAttribute('data-theme')).toBe('dark'))
    act(() => changeHandler?.({ matches: false } as MediaQueryListEvent))
    await waitFor(() => expect(container.querySelector('.app-shell')?.getAttribute('data-theme')).toBe('light'))
  })

  it('supports keyboard settings tabs and restores focus when dismissed', async () => {
    const user = userEvent.setup()
    render(<App />)
    await screen.findByText('latest message')

    const settingsButton = screen.getByRole('button', { name: 'Settings' })
    await user.click(settingsButton)
    const closeSettings = screen.getByRole('button', { name: 'Close settings' })
    const settingsDialog = screen.getByRole('dialog', { name: 'Settings' })
    expect(document.activeElement).toBe(closeSettings)
    await user.tab({ shift: true })
    expect(settingsDialog.contains(document.activeElement)).toBe(true)
    await user.tab()
    expect(document.activeElement).toBe(closeSettings)

    await user.click(screen.getByRole('tab', { name: 'General' }))
    expect(screen.getByRole('checkbox', { name: 'Launch Pinger at login' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Minimize to tray when closing the window' })).toBeTruthy()

    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    fireEvent.keyDown(settingsDialog, { key: 'k', metaKey: true })
    expect(screen.queryByRole('dialog', { name: 'Quick switcher' })).toBeNull()
    await user.keyboard('{ArrowRight}')
    const notificationsTab = screen.getByRole('tab', { name: 'Notifications' })
    expect(notificationsTab.getAttribute('aria-selected')).toBe('true')
    expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe('settings-tab-notifications')

    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Settings' })).toBeNull())
    expect(document.activeElement).toBe(settingsButton)
  })

  it('shows the incognito indicator while read receipts are suppressed', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'privacy.incognito': true } })
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(screen.getByRole('status', { name: 'Incognito mode is on' })).toBeTruthy()
  })

  it('honors the spellcheck preference in the message composer', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'composer.spellcheck': false } })
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(screen.getByRole('textbox', { name: 'Message' }).getAttribute('spellcheck')).toBe('false')
  })

  it('moves focus from the empty composer to the message list when configured', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'composer.arrow_up_focus_timeline': true } })
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    const composer = screen.getByRole('textbox', { name: 'Message' })
    const timeline = screen.getByRole('region', { name: 'Message timeline' })
    composer.focus()
    fireEvent.keyDown(composer, { key: 'ArrowUp' })

    expect(document.activeElement).toBe(timeline)
  })

  it('hides deleted messages when the appearance preference is enabled', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'appearance.hide_deleted_messages': true } })
    harness.service.History.mockResolvedValueOnce({
      messages: [
        { ...message('$removed', 'removed body', 200), redacted: true },
        message('$latest', 'latest message', 300),
      ],
      before: 50,
      hasMore: true,
    })
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(screen.queryByText('Message removed')).toBeNull()
  })

  it('hides message timestamps when configured', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'appearance.show_timestamps': false } })
    const { container } = render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(container.querySelector('[data-event-id="$latest"] .message-meta time')).toBeNull()
  })

  it('disables message entrance animations when configured', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'appearance.message_animations': false } })
    const { container } = render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(container.querySelector('.app-shell')?.getAttribute('data-message-animations')).toBe('false')
  })

  it('reloads the initial page when the history count preference changes', async () => {
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'History' }))
    await user.selectOptions(screen.getByRole('combobox'), '250')

    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('history.initial_messages', '250'))
    await waitFor(() => expect(harness.service.History).toHaveBeenCalledTimes(2))
    expect(await screen.findByText('new live message')).toBeTruthy()
  })

  it('enables older paging if live sync grows an initially complete page', async () => {
    const user = userEvent.setup()
    let firstPageRequest = true
    harness.service.History.mockImplementation(async (_roomID: string, before: number) => {
      if (before === 0 && firstPageRequest) {
        firstPageRequest = false
        return { messages: [message('$latest', 'latest message', 300)], before: 10, hasMore: false }
      }
      if (before === 20) {
        return { messages: [message('$older', 'older after growth', 100)], before: 5, hasMore: false }
      }
      return {
        messages: [message('$latest', 'latest message updated', 300), message('$new', 'new live message', 400)],
        before: 20,
        hasMore: true,
      }
    })

    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Load older messages' })).toBeNull()

    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          rooms: [room],
          timelineChangedRoomId: [room.id],
          initialSyncComplete: true,
          needsVerification: false,
          connectionState: 'connected',
        },
      })
    })

    expect(await screen.findByText('new live message')).toBeTruthy()
    await user.click(await screen.findByRole('button', { name: 'Load older messages' }))
    expect(await screen.findByText('older after growth')).toBeTruthy()
    expect(harness.service.History).toHaveBeenLastCalledWith(room.id, 20)
  })

  it('moves room focus with arrows and switches rooms with the primary modifier', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, secondRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    const roomButtons = container.querySelectorAll<HTMLButtonElement>('.room-list .room-button')
    fireEvent.keyDown(roomButtons[0], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(roomButtons[1])
    await user.keyboard('{Enter}')
    await waitFor(() => expect(harness.service.SetActiveRoom).toHaveBeenLastCalledWith(secondRoom.id))

    fireEvent.keyDown(container.querySelector('.app-shell')!, { key: '1', ctrlKey: true })
    await waitFor(() => expect(harness.service.SetActiveRoom).toHaveBeenLastCalledWith(room.id))
  })

  it('archives unread rooms after marking them read and restores them from Archived', async () => {
    const user = userEvent.setup()
    const unreadRoom = { ...room, unread: 2, lastEventId: '$latest' }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [unreadRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    fireEvent.contextMenu(container.querySelector('.room-button')!, { clientX: 48, clientY: 48 })
    await user.click(screen.getByRole('menuitem', { name: 'Archive room' }))
    await waitFor(() => {
      expect(harness.service.MarkRead).toHaveBeenCalledWith(room.id, '$latest')
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith(`room.archived.primary.${room.id}`, true)
    })
    expect(container.querySelector('.room-list .room-button')).toBeNull()

    await user.click(screen.getByRole('button', { name: 'Archived rooms' }))
    expect(container.querySelector('.room-list .room-button')).toBeTruthy()
    fireEvent.contextMenu(container.querySelector('.room-list .room-button')!, { clientX: 48, clientY: 48 })
    await user.click(screen.getByRole('menuitem', { name: 'Restore room' }))
    await waitFor(() => expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith(`room.archived.primary.${room.id}`, false))
    await user.click(screen.getByRole('button', { name: 'All' }))
    expect(container.querySelector('.room-list .room-button')).toBeTruthy()
  })

  it('archives all currently read rooms', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Archive all read rooms' }))

    await waitFor(() => expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith(`room.archived.primary.${room.id}`, true))
    expect(container.querySelector('.room-list .room-button')).toBeNull()
  })

  it('uses the configured next-unread room after archiving the selected room', async () => {
    const user = userEvent.setup()
    const unreadDestination = { ...secondRoom, unread: 1 }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, unreadDestination] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.selectOptions(screen.getByRole('combobox', { name: 'After archiving a room' }), 'next_unread')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('archive.next_room', 'next_unread'))
    await user.keyboard('{Escape}')

    fireEvent.contextMenu(container.querySelector('.room-list .room-button')!, { clientX: 48, clientY: 48 })
    await user.click(screen.getByRole('menuitem', { name: 'Archive room' }))
    await waitFor(() => expect(container.querySelector('.room-button.is-active')?.textContent).toContain(unreadDestination.name))
  })

  it('archives rooms older than the selected age after marking unread rooms read', async () => {
    const user = userEvent.setup()
    const staleRoom = { ...room, unread: 1, lastEventId: '$stale', lastEventAt: Date.now() - 100 * 24 * 60 * 60 * 1000 }
    const currentRoom = { ...secondRoom, lastEventAt: Date.now() }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [staleRoom, currentRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.selectOptions(screen.getByRole('combobox', { name: 'Inactive room age' }), '30')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('archive.older_than_days', '30'))
    await user.click(screen.getByRole('button', { name: 'Archive inactive rooms' }))

    await waitFor(() => {
      expect(harness.service.MarkRead).toHaveBeenCalledWith(staleRoom.id, '$stale')
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith(`room.archived.primary.${staleRoom.id}`, true)
    })
    expect(container.querySelector('.room-list .room-button')?.textContent).toContain(currentRoom.name)
  })

  it('navigates to the next unread room after an explicit mark-read action', async () => {
    const user = userEvent.setup()
    const markedRoom = { ...secondRoom, unread: 1, lastEventId: '$second' }
    const nextUnreadRoom = { ...secondRoom, id: '!third:example.org', name: 'Third room', unread: 1, lastEventId: '$third' }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, markedRoom, nextUnreadRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'General' }))
    await user.selectOptions(screen.getByRole('combobox', { name: "After changing a room's read state" }), 'next_unread')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('inbox.after_read', 'next_unread'))
    await user.keyboard('{Escape}')

    const roomButtons = container.querySelectorAll<HTMLButtonElement>('.room-list .room-button')
    fireEvent.contextMenu(roomButtons[1], { clientX: 48, clientY: 48 })
    await user.click(screen.getByRole('menuitem', { name: 'Mark room read' }))

    await waitFor(() => {
      expect(harness.service.MarkRead).toHaveBeenCalledWith(markedRoom.id, '$second')
      expect(container.querySelector('.room-button.is-active')?.textContent).toContain(nextUnreadRoom.name)
    })
  })

  it('marks a room unread and navigates to the next unread room', async () => {
    const user = userEvent.setup()
    const nextUnreadRoom = { ...secondRoom, id: '!third:example.org', name: 'Third room', unread: 1, lastEventId: '$third' }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, nextUnreadRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'General' }))
    await user.selectOptions(screen.getByRole('combobox', { name: "After changing a room's read state" }), 'next_unread')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('inbox.after_read', 'next_unread'))
    await user.keyboard('{Escape}')

    const roomButton = container.querySelector<HTMLButtonElement>('.room-list .room-button')!
    fireEvent.contextMenu(roomButton, { clientX: 48, clientY: 48 })
    await user.click(screen.getByRole('menuitem', { name: 'Mark room unread' }))

    await waitFor(() => {
      expect(harness.service.MarkUnread).toHaveBeenCalledWith(room.id)
      expect(container.querySelector('.room-button.is-active')?.textContent).toContain(nextUnreadRoom.name)
    })
  })

  it('switches between the room list and conversation on mobile', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    const appShell = container.querySelector('.app-shell')!
    expect(appShell.getAttribute('data-mobile-pane')).toBe('rooms')

    await user.click(screen.getByRole('button', { name: /Test room/ }))
    expect(appShell.getAttribute('data-mobile-pane')).toBe('conversation')
    await user.click(screen.getByRole('button', { name: 'Back to rooms' }))
    expect(appShell.getAttribute('data-mobile-pane')).toBe('rooms')

    fireEvent.keyDown(appShell, { key: 'k', metaKey: true })
    const quickSwitcher = await screen.findByRole('textbox', { name: 'Search rooms' })
    fireEvent.keyDown(quickSwitcher, { key: 'Enter' })
    expect(appShell.getAttribute('data-mobile-pane')).toBe('conversation')
  })

  it('marks the preview when the latest message was sent by the current user', async () => {
    const ownMessageRoom: Room = {
      ...room,
      id: '!own-message:example.org',
      name: 'Sent by me',
      preview: 'My latest message',
      lastEventSender: snapshot.profile.userId,
    }
    const incomingMessageRoom: Room = {
      ...room,
      id: '!incoming-message:example.org',
      name: 'Sent by them',
      preview: 'Their latest message',
      lastEventSender: '@alice:example.org',
    }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [ownMessageRoom, incomingMessageRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    const previews = container.querySelectorAll('.room-preview')
    expect(previews[0].textContent).toBe('You: My latest message')
    expect(previews[1].textContent).toBe('Their latest message')
  })

  it('waits for initial sync before showing a recovery key form', async () => {
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, needsVerification: true, syncReady: false })
    render(<App />)

    expect(await screen.findByText('Checking device verification…')).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Verify this device' })).toBeNull()

    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          rooms: [room],
          timelineChangedRoomId: [],
          initialSyncComplete: true,
          needsVerification: false,
          connectionState: 'connected',
        },
      })
    })

    expect(await screen.findByText('latest message')).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Verify this device' })).toBeNull()

    act(() => {
      harness.eventHandlers.get('matrix:update')?.({
        data: {
          rooms: [room],
          timelineChangedRoomId: [],
          initialSyncComplete: true,
          needsVerification: true,
          connectionState: 'connected',
        },
      })
    })

    expect(await screen.findByRole('heading', { name: 'Verify this device' })).toBeTruthy()
  })

  it('applies and persists the additional appearance themes', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    const themeSelect = container.querySelector<HTMLSelectElement>('.settings-content select')!

    await user.selectOptions(themeSelect, 'Bosco')
    expect(container.querySelector('.app-shell')?.getAttribute('data-theme')).toBe('bosco')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.theme', 'Bosco'))

    await user.selectOptions(themeSelect, 'Rosa')
    expect(container.querySelector('.app-shell')?.getAttribute('data-theme')).toBe('rosa')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.theme', 'Rosa'))
  })

  it('applies a theme accent independently from the outgoing message color', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    await user.click(screen.getByRole('button', { name: 'Ocean accent color' }))

    expect(container.querySelector('.app-shell')?.getAttribute('style')).toContain('--accent: #28718f')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.accent_color', 'Ocean'))
  })

  it('applies and persists a chat wallpaper', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    await user.click(screen.getByRole('button', { name: 'Meadow wallpaper' }))

    expect(container.querySelector('.app-shell')?.getAttribute('data-wallpaper')).toBe('meadow')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.chat_wallpaper', 'Meadow'))
  })

  it('controls room previews, unread counts, and chat-list density independently', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    await user.click(screen.getByLabelText('Show room previews'))
    await user.click(screen.getByLabelText('Show unread counts'))
    await user.click(screen.getByLabelText('Compact chat list'))

    const appShell = container.querySelector('.app-shell')
    await waitFor(() => {
      expect(appShell?.getAttribute('data-room-previews')).toBe('false')
      expect(appShell?.getAttribute('data-unread-counts')).toBe('false')
      expect(appShell?.getAttribute('data-compact-chat-list')).toBe('true')
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith('sidebar.show_previews', false)
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith('sidebar.show_unread_counts', false)
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith('sidebar.compact_chat_list', true)
    })
  })

  it('allows disabling the pointer cursor on clickable controls', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    await user.click(screen.getByLabelText('Use pointer cursor on clickable controls'))

    await waitFor(() => {
      expect(container.querySelector('.app-shell')?.getAttribute('data-pointer-cursor')).toBe('false')
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith('appearance.pointer_cursor', false)
    })
  })

  it('sets app zoom from the Appearance slider', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Appearance' }))
    fireEvent.change(screen.getByRole('slider', { name: 'App zoom' }), { target: { value: '110' } })

    await waitFor(() => {
      expect(container.querySelector('.app-shell')?.getAttribute('style')).toContain('--app-zoom: 110%')
      expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.zoom', '110')
    })
  })

  it('adjusts and resets app zoom with primary-modifier shortcuts', async () => {
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    const appShell = container.querySelector('.app-shell')!
    fireEvent.keyDown(appShell, { key: '=', metaKey: true })
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.zoom', '105'))

    fireEvent.keyDown(appShell, { key: '0', metaKey: true })
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.zoom', '100'))
  })

  it('opens keyboard help and focuses room search with unmodified shortcuts', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    const appShell = container.querySelector('.app-shell')!

    fireEvent.keyDown(appShell, { key: '/' })
    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: 'Find a room' }))
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Find a room' }), { key: 'k', ctrlKey: true })
    expect(screen.queryByRole('dialog', { name: 'Quick switcher' })).toBeNull()

    fireEvent.keyDown(appShell, { key: 'f', ctrlKey: true })
    const messageSearch = await screen.findByRole('textbox', { name: 'Search local message history' })
    fireEvent.keyDown(messageSearch, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Search local message history' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Search messages' })))

    fireEvent.keyDown(appShell, { key: 'f', ctrlKey: true })
    await screen.findByRole('textbox', { name: 'Search local message history' })
    await user.click(screen.getByRole('button', { name: 'Close message search' }))
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Search messages' })))

    fireEvent.keyDown(appShell, { key: '?' })
    expect((await screen.findByRole('tab', { name: 'Keyboard' })).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByText('Navigate the focused timeline')).toBeTruthy()
    expect(screen.getByText('Reply to the focused message')).toBeTruthy()
  })

  it('searches all chats, paginates results, and opens the matching message', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, secondRoom] })
    harness.service.SearchMessages
      .mockResolvedValueOnce({
        results: [{ roomID: secondRoom.id, message: message('$new', 'needle from another chat', 400) }],
        nextBatch: 'local_offset:50',
      })
      .mockResolvedValueOnce({ results: [], nextBatch: '' })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    fireEvent.keyDown(container.querySelector('.app-shell')!, { key: 'f', ctrlKey: true })
    const query = screen.getByRole('textbox', { name: 'Search local message history' })
    await user.type(query, 'needle')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    expect(await screen.findByRole('button', { name: 'Open message in Second room' })).toBeTruthy()
    expect(harness.service.SearchMessages).toHaveBeenNthCalledWith(1, {
      query: 'needle',
      roomID: '',
      sender: '',
      minTimestamp: 0,
      maxTimestamp: 0,
      limit: 50,
      nextBatch: '',
    })

    await user.click(screen.getByRole('button', { name: 'Load more results' }))
    await waitFor(() => expect(harness.service.SearchMessages).toHaveBeenCalledTimes(2))
    expect(harness.service.SearchMessages).toHaveBeenNthCalledWith(2, expect.objectContaining({ nextBatch: 'local_offset:50' }))

    await user.click(screen.getByRole('button', { name: 'Open message in Second room' }))
    expect(await screen.findByText('new live message')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Second room' })).toBeTruthy()
  })

  it('opens global search with no selected room', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [] })
    render(<App />)
    expect(await screen.findByRole('heading', { name: 'Matrix' })).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Search messages' }))
    await user.type(screen.getByRole('textbox', { name: 'Search local message history' }), 'invoice')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await waitFor(() => expect(harness.service.SearchMessages).toHaveBeenCalledWith(expect.objectContaining({
      query: 'invoice',
      roomID: '',
      limit: 50,
    })))
  })

  it('passes room, sender, and date filters to local message search', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [room, secondRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    fireEvent.keyDown(container.querySelector('.app-shell')!, { key: 'f', ctrlKey: true })
    await user.type(screen.getByRole('textbox', { name: 'Search local message history' }), 'report')
    await user.click(screen.getByText('Filters'))
    await user.selectOptions(screen.getByRole('combobox', { name: 'Filter by room' }), secondRoom.id)
    await user.type(screen.getByRole('textbox', { name: 'Filter by sender' }), '@alice:example.org')
    fireEvent.change(screen.getByLabelText('Search from date'), { target: { value: '2026-10-01' } })
    fireEvent.change(screen.getByLabelText('Search to date'), { target: { value: '2026-10-17' } })
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await waitFor(() => expect(harness.service.SearchMessages).toHaveBeenCalledWith({
      query: 'report',
      roomID: secondRoom.id,
      sender: '@alice:example.org',
      minTimestamp: new Date('2026-10-01T00:00:00').getTime(),
      maxTimestamp: new Date('2026-10-17T23:59:59.999').getTime(),
      limit: 50,
      nextBatch: '',
    }))
  })

  it('navigates the timeline and replies from a focused message', async () => {
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    const timeline = screen.getByRole('region', { name: 'Message timeline' })
    fireEvent.keyDown(timeline, { key: 'ArrowDown' })

    const messageRow = container.querySelector<HTMLElement>('.message-row')!
    expect(document.activeElement).toBe(messageRow)
    fireEvent.keyDown(messageRow, { key: 'r' })

    expect(await screen.findByText('Replying to Alice')).toBeTruthy()
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('textbox', { name: 'Message' })))
  })

  it('schedules a composer draft for a future time', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'schedule.default_send_minutes': '30' } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'send this later')
    await user.click(screen.getByRole('button', { name: 'Schedule message' }))
    const dialog = await screen.findByRole('dialog', { name: 'Schedule message' })
    const sendAt = within(dialog).getByLabelText('Send at') as HTMLInputElement
    const defaultSendAt = new Date(sendAt.value).getTime()
    expect(defaultSendAt).toBeGreaterThanOrEqual(Date.now() + 29 * 60_000)
    expect(defaultSendAt).toBeLessThanOrEqual(Date.now() + 31 * 60_000)
    await user.click(within(dialog).getByRole('button', { name: 'Schedule message' }))

    await waitFor(() => expect(harness.service.ScheduleMessage).toHaveBeenCalledWith(room.id, 'send this later', expect.any(Number)))
    expect(harness.service.ScheduleMessage.mock.calls[0][2]).toBeGreaterThan(Date.now())
  })

  it('holds a sent message until the undo window is cancelled', async () => {
    const user = userEvent.setup()
    harness.service.SendMessage.mockClear()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'composer.undo_send_seconds': '5' } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    const composer = screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement
    await user.type(composer, 'undo this')
    await user.click(screen.getByRole('button', { name: 'Send message' }))

    expect(await screen.findByText('Message queued')).toBeTruthy()
    expect(composer.value).toBe('')
    await user.click(screen.getByRole('button', { name: 'Undo send' }))

    expect(composer.value).toBe('undo this')
    expect(screen.queryByText('Message queued')).toBeNull()
    expect(harness.service.SendMessage).not.toHaveBeenCalled()
  })

  it('sends a queued message when the undo window expires', async () => {
    harness.service.SendMessage.mockClear()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'composer.undo_send_seconds': '5' } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    fireEvent.change(screen.getByRole('textbox', { name: 'Message' }), { target: { value: 'send after window' } })
    vi.useFakeTimers()
    try {
      fireEvent.click(screen.getByRole('button', { name: 'Send message' }))
      expect(harness.service.SendMessage).not.toHaveBeenCalled()
      await act(async () => {
        vi.advanceTimersByTime(5000)
      })
      expect(harness.service.SendMessage).toHaveBeenCalledWith(room.id, 'send after window')
    } finally {
      vi.useRealTimers()
    }
  })

  it('uses the configured time suggestions in the schedule dialog', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'schedule.suggestions_minutes': '10,30,180' } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'send this later')
    await user.click(screen.getByRole('button', { name: 'Schedule message' }))
    const sendAt = screen.getByLabelText('Send at') as HTMLInputElement
    const previousDueAt = new Date(sendAt.value).getTime()
    await user.click(screen.getByRole('button', { name: '30 min' }))

    const updatedDueAt = new Date(sendAt.value).getTime()
    expect(updatedDueAt - previousDueAt).toBeGreaterThanOrEqual(24 * 60_000)
    expect(updatedDueAt - previousDueAt).toBeLessThan(26 * 60_000)
  })

  it('persists the system-default voice-note microphone selection', async () => {
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.selectOptions(screen.getByRole('combobox', { name: 'Voice-note microphone' }), '')

    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('audio.input_device', ''))
  })

  it('lists available microphones and persists the selected input', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('navigator', {
      mediaDevices: {
        enumerateDevices: vi.fn().mockResolvedValue([
          { kind: 'audioinput', deviceId: 'mic-one', label: 'Desk microphone' },
          { kind: 'audiooutput', deviceId: 'speaker-one', label: 'Speakers' },
        ]),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      },
    })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    const microphone = await screen.findByRole('combobox', { name: 'Voice-note microphone' })
    expect(within(microphone).getByRole('option', { name: 'Desk microphone' })).toBeTruthy()
    await user.selectOptions(microphone, 'mic-one')

    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('audio.input_device', 'mic-one'))
  })

  it('saves a validated editable schedule suggestion list', async () => {
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    const suggestions = screen.getByRole('textbox', { name: 'Schedule time suggestions' })
    await user.clear(suggestions)
    await user.type(suggestions, '10, 30, 180')
    await user.tab()

    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('schedule.suggestions_minutes', '10,30,180'))
  })

  it('saves configurable default delays for reminders and scheduled sends', async () => {
    const user = userEvent.setup()
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'General' }))

    const sendDelay = screen.getByRole('spinbutton', { name: 'Default Send Later delay in minutes' })
    await user.clear(sendDelay)
    await user.type(sendDelay, '25')
    fireEvent.blur(sendDelay)
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('schedule.default_send_minutes', '25'))

    const reminderDelay = screen.getByRole('spinbutton', { name: 'Default Remind Later delay in minutes' })
    await user.clear(reminderDelay)
    await user.type(reminderDelay, '120')
    fireEvent.blur(reminderDelay)
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('schedule.default_reminder_minutes', '120'))
  })

  it('enables focused notifications through a positively worded setting', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'notifications.only_when_unfocused': true } })
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    await user.click(screen.getByRole('tab', { name: 'Notifications' }))

    const notifyWhileFocused = screen.getByRole('checkbox', { name: 'Notify while Pinger is focused' }) as HTMLInputElement
    expect(notifyWhileFocused.checked).toBe(false)
    await user.click(notifyWhileFocused)

    await waitFor(() => expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith('notifications.only_when_unfocused', false))
  })

  it('schedules a reminder for a specific Matrix message', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'schedule.default_reminder_minutes': '120' } })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Set reminder for message' }))
    const dialog = await screen.findByRole('dialog', { name: 'Set reminder' })
    const remindAt = within(dialog).getByLabelText('Remind me at') as HTMLInputElement
    const defaultRemindAt = new Date(remindAt.value).getTime()
    expect(defaultRemindAt).toBeGreaterThanOrEqual(Date.now() + 119 * 60_000)
    expect(defaultRemindAt).toBeLessThanOrEqual(Date.now() + 121 * 60_000)
    expect(within(dialog).getByRole('checkbox', { name: 'Archive room after setting reminder' }).getAttribute('checked')).toBeNull()
    await user.click(within(dialog).getByRole('button', { name: 'Set reminder' }))

    await waitFor(() => expect(harness.service.ScheduleReminder).toHaveBeenCalledWith(room.id, '$latest', '', expect.any(Number)))
    expect(harness.service.SetBooleanPreference).not.toHaveBeenCalledWith(`room.archived.primary.${room.id}`, true)
  })

  it('archives the room after successfully setting a reminder when requested', async () => {
    const user = userEvent.setup()
    const unreadRoom = { ...room, unread: 1, lastEventId: '$latest' }
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, rooms: [unreadRoom] })
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Set reminder for message' }))
    const dialog = await screen.findByRole('dialog', { name: 'Set reminder' })
    await user.click(within(dialog).getByRole('checkbox', { name: 'Archive room after setting reminder' }))
    await user.click(within(dialog).getByRole('button', { name: 'Set reminder' }))

    await waitFor(() => {
      expect(harness.service.ScheduleReminder).toHaveBeenCalledWith(room.id, '$latest', '', expect.any(Number))
      expect(harness.service.MarkRead).toHaveBeenCalledWith(room.id, '$latest')
      expect(harness.service.SetBooleanPreference).toHaveBeenCalledWith(`room.archived.primary.${room.id}`, true)
    })
    expect(container.querySelector('.room-list .room-button')).toBeNull()
  })

  it('does not archive the room when setting a reminder fails', async () => {
    const user = userEvent.setup()
    harness.service.ScheduleReminder.mockRejectedValueOnce(new Error('schedule failed'))
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Set reminder for message' }))
    const dialog = await screen.findByRole('dialog', { name: 'Set reminder' })
    await user.click(within(dialog).getByRole('checkbox', { name: 'Archive room after setting reminder' }))
    await user.click(within(dialog).getByRole('button', { name: 'Set reminder' }))

    const alert = await within(dialog).findByRole('alert')
    expect(alert.textContent).toContain('schedule failed')
    expect(harness.service.SetBooleanPreference).not.toHaveBeenCalledWith(`room.archived.primary.${room.id}`, true)
  })

  it('cancels a pending job from the scheduled queue', async () => {
    const user = userEvent.setup()
    harness.service.ScheduledJobs.mockResolvedValue([{
      id: 'scheduled-1',
      kind: 'message',
      roomId: room.id,
      text: 'scheduled text',
      eventId: '',
      dueAt: Date.now() + 60_000,
      createdAt: Date.now(),
      status: 'pending',
      error: '',
    }])
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Scheduled messages and reminders' }))
    const dialog = await screen.findByRole('dialog', { name: 'Scheduled messages and reminders' })
    expect(within(dialog).getByText('scheduled text')).toBeTruthy()
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    await waitFor(() => expect(harness.service.CancelScheduledJob).toHaveBeenCalledWith('scheduled-1'))
  })

  it('surfaces a due reminder and keeps it available in the queue', async () => {
    const user = userEvent.setup()
    const reminder = {
      id: 'reminder-1',
      kind: 'reminder',
      roomId: room.id,
      text: 'Stand up',
      eventId: '$latest',
      dueAt: Date.now() - 1_000,
      createdAt: Date.now() - 3_600_000,
      status: 'fired',
      error: '',
    }
    harness.service.ScheduledJobs.mockResolvedValue([reminder])
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    act(() => harness.eventHandlers.get('scheduler:reminder')?.({ data: reminder }))
    expect(await screen.findByText('Test room: Stand up')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'View reminders' }))
    const dialog = await screen.findByRole('dialog', { name: 'Scheduled messages and reminders' })

    expect(within(dialog).getByText('Stand up')).toBeTruthy()
    expect(within(dialog).getByText('Due now')).toBeTruthy()
  })

  it('schedules a reply with its original Matrix event target', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Reply to Alice' }))
    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'Answer later')
    await user.click(screen.getByRole('button', { name: 'Schedule reply' }))
    const dialog = await screen.findByRole('dialog', { name: 'Schedule reply' })
    expect(within(dialog).getByText('Replying to Alice')).toBeTruthy()
    await user.click(within(dialog).getByRole('button', { name: 'Schedule reply' }))

    await waitFor(() => expect(harness.service.ScheduleReply).toHaveBeenCalledWith(room.id, 'Answer later', '$latest', expect.any(Number)))
    expect(harness.service.ScheduleMessage).not.toHaveBeenCalled()
  })

  it('keeps message actions out of sight until the message is right-clicked', async () => {
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    expect(container.querySelector('.message-actions')).toBeNull()

    openMessageMenu(container)
    const menu = await screen.findByRole('menu', { name: 'Message actions' })
    expect(within(menu).getByRole('menuitem', { name: 'Reply to Alice' })).toBeTruthy()
    expect(within(menu).getByRole('menuitem', { name: 'Set reminder for message' })).toBeTruthy()
    expect(within(menu).getByRole('menuitem', { name: 'Add reaction' })).toBeTruthy()
    expect(within(menu).getByRole('menuitem', { name: 'Copy message' })).toBeTruthy()

    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu', { name: 'Message actions' })).toBeNull())
  })

  it('copies a message with its timestamp when configured', async () => {
    const user = userEvent.setup()
    harness.service.Snapshot.mockResolvedValue({ ...snapshot, preferences: { 'clipboard.copy_timestamp': true } })
    const writeText = vi.fn().mockResolvedValue(undefined)
    render(<App />)

    expect(await screen.findByText('latest message')).toBeTruthy()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    openMessageMenu(document.body)
    await user.click(screen.getByRole('menuitem', { name: 'Copy message' }))

    const timestamp = new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(300))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(`[${timestamp}] Alice: latest message`))
  })
})
