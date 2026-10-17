import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Message, Room, Snapshot } from '../bindings/mychat/desktop/models.js'
import App from './App'

const harness = vi.hoisted(() => ({
  eventHandlers: new Map<string, (event: { data: any }) => void>(),
  service: {
    Snapshot: vi.fn(),
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
    SetStringPreference: vi.fn(),
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
  mentions: 0,
  unanswered: false,
  lastEventId: '',
  lastEventSender: '',
  lastEventAt: 0,
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
  rooms: [room],
  preferences: {},
}

describe('room history updates', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  beforeEach(() => {
    harness.eventHandlers.clear()
    harness.service.Snapshot.mockResolvedValue(snapshot)
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
    harness.service.SetStringPreference.mockResolvedValue(undefined)

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
    const themeSelect = container.querySelector<HTMLSelectElement>('.settings-content select')!

    await user.selectOptions(themeSelect, 'Bosco')
    expect(container.querySelector('.app-shell')?.getAttribute('data-theme')).toBe('bosco')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.theme', 'Bosco'))

    await user.selectOptions(themeSelect, 'Rosa')
    expect(container.querySelector('.app-shell')?.getAttribute('data-theme')).toBe('rosa')
    await waitFor(() => expect(harness.service.SetStringPreference).toHaveBeenCalledWith('appearance.theme', 'Rosa'))
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
    render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    await user.type(screen.getByRole('textbox', { name: 'Message' }), 'send this later')
    await user.click(screen.getByRole('button', { name: 'Schedule message' }))
    const dialog = await screen.findByRole('dialog', { name: 'Schedule message' })
    await user.click(within(dialog).getByRole('button', { name: 'Schedule message' }))

    await waitFor(() => expect(harness.service.ScheduleMessage).toHaveBeenCalledWith(room.id, 'send this later', expect.any(Number)))
    expect(harness.service.ScheduleMessage.mock.calls[0][2]).toBeGreaterThan(Date.now())
  })

  it('schedules a reminder for a specific Matrix message', async () => {
    const user = userEvent.setup()
    const { container } = render(<App />)
    expect(await screen.findByText('latest message')).toBeTruthy()

    openMessageMenu(container)
    await user.click(await screen.findByRole('menuitem', { name: 'Set reminder for message' }))
    const dialog = await screen.findByRole('dialog', { name: 'Set reminder' })
    await user.click(within(dialog).getByRole('button', { name: 'Set reminder' }))

    await waitFor(() => expect(harness.service.ScheduleReminder).toHaveBeenCalledWith(room.id, '$latest', '', expect.any(Number)))
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

    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('menu', { name: 'Message actions' })).toBeNull())
  })
})
