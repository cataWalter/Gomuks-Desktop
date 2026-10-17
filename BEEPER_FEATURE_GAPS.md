# Beeper Feature Gaps for Pinger

This checklist compares Pinger with the Beeper settings and feature controls visible in the supplied screenshots. It is not an inventory of every Beeper feature. `Missing` means Pinger has no equivalent; `Partial` means a related behavior exists but lacks the shown control or workflow.

Pinger is a Matrix client, so Beeper-specific provider integrations and subscription benefits are product-scope decisions rather than automatic requirements for Matrix compatibility.

## Already Covered in Pinger

- A single Matrix account can log in, verify the device, log out, and manage other Matrix devices.
- Room search, message search, quick switching, DM/group filters, unread/needs-reply/favorite filters, pinning, and muting are available.
- Needs-reply triage, mark-answered, and undoing the last triage action are available.
- Replies, edits, redactions, reactions, file attachments, scheduled messages/replies, and reminders are available.
- Appearance includes system/light/dark/OLED themes, Pinger themes, outgoing message color, font size, avatars, compact message layout, 12/24-hour time, and relative timestamps.
- Notifications include system permission, enable/disable, focus filtering, sound/volume, privacy levels, and room muting. macOS alerts use Notification Center when launched from the app bundle.
- Devices can be listed and revoked; room keys can be exported/imported; database and media-cache usage can be inspected, with media-cache clearing and database optimization.
- A keyboard shortcut list is available. The mark-answered key is configurable and Ctrl+Enter sending can be enabled.

## Missing or Partial

### Accounts and Integrations

- [ ] **Multiple-account manager**: add, switch, and manage several accounts from one account list. Pinger currently runs one Matrix account per client.
- [ ] **Native service integrations** shown in Beeper: connect and manage Instagram, Telegram, and WhatsApp accounts alongside Beeper. Matrix bridges are not the same as first-party account connectors.
- [ ] **Filter by account** and account-switching shortcuts; these depend on multi-account support.
- [ ] **Provider-specific spaces** for WhatsApp Communities and Telegram Topics.
- [ ] **Integration management section** with connected-service status and per-service account settings.

### General and Inbox Workflow

- [ ] **General settings section** with controls comparable to Beeper's general preferences.
- [ ] **App language selector and localized UI**; Pinger's interface is currently English-only.
- [ ] **Incognito mode** that keeps a chat unread when opened and suppresses read receipts, plus an incognito indicator.
- [ ] **Typing privacy control** to choose whether recipients see that the user is typing. Pinger currently sends typing state while composing, with no preference to disable it.
- [ ] **Archive workflow**: archive/restore rooms, archive all read rooms, mark unread rooms read when archiving, choose the next room after archive, and archive rooms older than a selected age.
- [ ] **Archive-related reminder behavior**, including an option to archive after setting a reminder.
- [ ] **After-read navigation preferences**, such as selecting the next chat after marking a chat read or unread.
- [ ] **Default action for Remind Later** and **default action for Send Later**. Pinger supports scheduling but has no configurable default policy.
- [ ] **Editable time suggestions** for reminders and scheduled sends. Pinger has quick 5-minute/1-hour start actions and a date-time input, but no user-managed suggestion list.
- [ ] **Undo-send window** with a configurable delay and a way to cancel a message from a pending-send state. Pinger's Ctrl+Z undo is for triage, not sent messages.
- [ ] **Copy-message timestamp preference**.
- [ ] **Download preferences** for choosing a download folder and always asking where to save.
- [ ] **Touch ID requirement on launch** and other local app-lock controls.

### Notifications

- [ ] **Separate notification categories** for direct messages, group messages, message requests, reactions, and typing events. Pinger currently alerts for eligible Matrix direct/highlight messages and scheduled reminders.
- [ ] **Per-category sound selection** and separate sound behavior for message, group, and request notifications. Pinger has one sound toggle and volume control.
- [ ] **Batch/delay notifications** for successive messages and a repeat reminder for notifications that remain unread.
- [ ] **Notification-specific OTP handling**, such as always notifying immediately when other messages are delayed and automatically copying OTP/2FA codes.
- [ ] **Sound-effects policy** separate from notification sound.
- [x] **Notify while the app is focused** is partially covered: turning off Pinger's "Only notify when the window is unfocused" allows focused-window alerts, but there is no positively worded setting or category-specific focus rule.

### Appearance and Desktop Behavior

- [ ] **Theme accent-color palette** independent of the outgoing-message color. Pinger has selectable themes and an outgoing bubble color, but no Beeper-style theme-color swatches.
- [ ] **Chat wallpaper selection**.
- [ ] **App icon picker** with alternate icon designs. Pinger currently has one fixed app icon.
- [ ] **Sidebar/chat-list controls** for pinned-chat style, transparency, showing unread counts, showing message previews, compact chat-list layout, and network icons/coloring. Pinger has basic sidebar visibility, unread indicators, previews, and compact message layout, but not this set of independent controls.
- [ ] **Conversation display controls** for showing/hiding timestamps, hiding deleted messages, message animations, balanced message text, external embeds, and membership events.
- [ ] **Quick-reaction preferences** to enable/disable quick reactions, choose frequent or custom emoji, select a default quick-reaction emoji, and control when reactions appear. Pinger has a fixed small reaction picker and reaction chips.
- [ ] **Show/hide menu-bar icon**, pointer-cursor preference, and configurable Dock badge count.
- [ ] **App zoom slider and zoom shortcuts**; Pinger currently offers discrete message font sizes instead.
- [ ] **General launch/close controls** for keeping the app minimized on launch and quitting when the main window closes. Pinger has backend support for launch-at-login and minimize-to-tray preferences, but no matching visible General settings page or full set of controls.

### Messaging and Composer

- [ ] **Rich composer formatting toolbar** and a setting to open the emoji picker on hover.
- [ ] **Emoji autocomplete** and a setting to disable automatic emoticon-to-emoji replacement.
- [ ] **GIF and sticker browsers/pickers**, plus an option to send a selected GIF immediately. Raw file attachments do not provide these discovery workflows.
- [ ] **Voice-note recording controls**, microphone selection, and automatic playback of the next voice note.
- [ ] **Spellcheck preference**.
- [ ] **Outgoing link-preview preference** to disable previews.
- [ ] **Configurable primary and secondary composer actions**.
- [ ] **Move focus from the composer to the message list with Up-arrow** as a configurable behavior.
- [ ] **Double-click actions** for chats and messages, including choosing Reply for a double-clicked message.

### Keyboard Shortcuts

- [ ] **Full shortcut editor** to record, change, enable, or disable shortcuts. Pinger currently exposes a shortcut reference and only a small number of configurable shortcut preferences.
- [ ] **Global shortcuts** to focus Pinger or focus Pinger and its command bar while another app is active.
- [ ] **Command bar**, filter cycling, filter-by-account, and a configurable shortcut for each inbox filter (All, DMs, Drafts, Groups, Unanswered, and Unread).
- [ ] **Beeper chat actions/shortcuts** for archive-all-read, toggle archive/mute/pin, cycle chats, chat-history navigation, open user profile, Remind Later, scroll to bottom, select next chat/unread chat, and send-and-archive.
- [ ] **Beeper message actions/shortcuts** for open-first-link, download attachments, quote-or-edit the selected message, show replies, record audio, send GIF/sticker, and Talk to Type. Pinger supports some related actions, such as reply, edit, reactions, and attachments, but not this complete shortcut mapping/editor.
- [ ] **App-level shortcut set** for minimize-to-menu-bar, reload, report a problem, reset zoom, switch spaces/accounts, toggle incognito/sidebar, and the other Beeper-specific actions shown.
- [ ] **Alternative keybinding presets**, such as Beeper's option to switch to another app's bindings.

### AI, Subscription, Storage, Help, and About

- [ ] **AI features**: mention an AI assistant in chat, AI answers, translation, and voice transcription. Pinger has no AI settings or AI service.
- [ ] **Beeper Plus subscription/entitlement system**, account limits, and Plus-only feature gating. This is Beeper-specific and only relevant if Pinger intends to adopt a paid tier.
- [ ] **Per-chat storage breakdown** and cleanup by age, media type, or chat. Pinger reports total database/cache usage and can clear the media cache, but does not attribute storage to individual rooms.
- [ ] **Dedicated Help center/support section**. Pinger has an in-app shortcut reference and a repository issue link, but no comparable help center.
- [ ] **About/update experience** with installed version, update status, changelog, privacy/legal links, and feedback widget. Pinger's About page currently shows app/engine/license details and repository/report-issue links.