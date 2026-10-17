package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScheduledJobStorePersistsAndRecoversInterruptedSend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "scheduled-jobs.json")
	store, err := newScheduledJobStore(path)
	if err != nil {
		t.Fatalf("create scheduled job store: %v", err)
	}

	dueAt := time.Now().Add(time.Hour).UnixMilli()
	job, err := store.scheduleMessage(primaryAccountID, "!room:example.org", "send later", dueAt)
	if err != nil {
		t.Fatalf("schedule message: %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat scheduled jobs file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("scheduled jobs permissions = %04o, want 0600", fileInfo.Mode().Perm())
	}

	claimed, err := store.claimDue(scheduledMessageKind, dueAt)
	if err != nil || len(claimed) != 1 || claimed[0].Status != scheduledSending {
		t.Fatalf("claim due message = %+v, %v", claimed, err)
	}

	reopened, err := newScheduledJobStore(path)
	if err != nil {
		t.Fatalf("reopen scheduled job store: %v", err)
	}
	jobs := reopened.list()
	if len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].Text != "send later" || jobs[0].Status != scheduledFailed {
		t.Fatalf("recovered scheduled jobs = %+v", jobs)
	}
	if !strings.Contains(jobs[0].Error, "Check the room") {
		t.Fatalf("recovered error = %q, want duplicate-send warning", jobs[0].Error)
	}
}

func TestScheduledJobStoreReschedulesCancelsAndFiresReminders(t *testing.T) {
	store, err := newScheduledJobStore(filepath.Join(t.TempDir(), "scheduled-jobs.json"))
	if err != nil {
		t.Fatalf("create scheduled job store: %v", err)
	}

	message, err := store.scheduleMessage(primaryAccountID, "!room:example.org", "change this", time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	newDueAt := time.Now().Add(2 * time.Hour).UnixMilli()
	if err = store.update(message.ID, newDueAt); err != nil {
		t.Fatalf("reschedule message: %v", err)
	}
	if err = store.cancel(message.ID); err != nil {
		t.Fatalf("cancel message: %v", err)
	}
	if jobs := store.list(); len(jobs) != 0 {
		t.Fatalf("cancelled message remained in pending list: %+v", jobs)
	}

	reminder, err := store.scheduleReminder(primaryAccountID, "!room:example.org", "$event", "Follow up", time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.claimDue(scheduledReminderKind, reminder.DueAt)
	if err != nil || len(claimed) != 1 || claimed[0].Status != scheduledFired {
		t.Fatalf("claim reminder = %+v, %v", claimed, err)
	}
	if err = store.dismiss(reminder.ID); err != nil {
		t.Fatalf("dismiss reminder: %v", err)
	}
	if jobs := store.list(); len(jobs) != 0 {
		t.Fatalf("dismissed reminder remained in active list: %+v", jobs)
	}
}

func TestScheduledJobStoreKeepsLateSendVisibleUntilDismissed(t *testing.T) {
	store, err := newScheduledJobStore(filepath.Join(t.TempDir(), "scheduled-jobs.json"))
	if err != nil {
		t.Fatalf("create scheduled job store: %v", err)
	}
	dueAt := time.Now().Add(time.Hour).UnixMilli()
	job, err := store.scheduleMessage(primaryAccountID, "!room:example.org", "send later", dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.claimDue(scheduledMessageKind, dueAt); err != nil {
		t.Fatal(err)
	}
	if err = store.finishMessage(job.ID, nil, dueAt+11_000); err != nil {
		t.Fatalf("finish delayed message: %v", err)
	}
	jobs := store.list()
	if len(jobs) != 1 || jobs[0].Status != scheduledSentLate {
		t.Fatalf("late send list = %+v", jobs)
	}
	if err = store.dismiss(job.ID); err != nil {
		t.Fatalf("dismiss delayed send: %v", err)
	}
	if jobs = store.list(); len(jobs) != 0 {
		t.Fatalf("dismissed late send remained in list: %+v", jobs)
	}
}

func TestScheduledReplyPersistsTargetAndDispatchesAsReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-jobs.json")
	store, err := newScheduledJobStore(path)
	if err != nil {
		t.Fatalf("create scheduled job store: %v", err)
	}
	job, err := store.scheduleReply(primaryAccountID, "!room:example.org", "follow up", "$quoted-event", time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatalf("schedule reply: %v", err)
	}
	reopened, err := newScheduledJobStore(path)
	if err != nil {
		t.Fatalf("reopen scheduled job store: %v", err)
	}
	jobs := reopened.list()
	if len(jobs) != 1 || jobs[0].ReplyToEventID != "$quoted-event" {
		t.Fatalf("persisted reply target = %+v", jobs)
	}

	sender := &scheduledSenderProbe{}
	if err = dispatchScheduledMessage(context.Background(), sender, jobs[0]); err != nil {
		t.Fatalf("dispatch scheduled reply: %v", err)
	}
	if sender.replyCalls != 1 || sender.messageCalls != 0 || sender.replyTo != "$quoted-event" {
		t.Fatalf("reply dispatch = %+v", sender)
	}
	if err = dispatchScheduledMessage(context.Background(), sender, ScheduledJob{RoomID: job.RoomID, Text: "legacy send"}); err != nil {
		t.Fatalf("dispatch legacy scheduled message: %v", err)
	}
	if sender.messageCalls != 1 || sender.replyCalls != 1 {
		t.Fatalf("legacy dispatch = %+v", sender)
	}
}

func TestScheduledJobsAreScopedToTheirAccount(t *testing.T) {
	store, err := newScheduledJobStore(filepath.Join(t.TempDir(), "scheduled-jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	dueAt := time.Now().Add(time.Hour).UnixMilli()
	primaryJob, err := store.scheduleMessage(primaryAccountID, "!room:example.org", "primary send", dueAt)
	if err != nil {
		t.Fatal(err)
	}
	secondaryJob, err := store.scheduleMessage("account-2", "!room:example.org", "secondary send", dueAt)
	if err != nil {
		t.Fatal(err)
	}

	primaryJobs, err := store.claimDueForAccount(scheduledMessageKind, dueAt, primaryAccountID)
	if err != nil || len(primaryJobs) != 1 || primaryJobs[0].ID != primaryJob.ID {
		t.Fatalf("primary account claimed jobs = %+v, %v", primaryJobs, err)
	}
	secondaryJobs, err := store.claimDueForAccount(scheduledMessageKind, dueAt, "account-2")
	if err != nil || len(secondaryJobs) != 1 || secondaryJobs[0].ID != secondaryJob.ID {
		t.Fatalf("secondary account claimed jobs = %+v, %v", secondaryJobs, err)
	}
	if jobs := store.listForAccount(primaryAccountID); len(jobs) != 1 || jobs[0].ID != primaryJob.ID {
		t.Fatalf("primary account job list = %+v", jobs)
	}
}

func TestScheduledMessagesDispatchThroughOwningAccount(t *testing.T) {
	store, err := newScheduledJobStore(filepath.Join(t.TempDir(), "scheduled-jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	dueAt := time.Now().Add(time.Hour).UnixMilli()
	if _, err = store.scheduleMessage(primaryAccountID, "!room:example.org", "primary send", dueAt); err != nil {
		t.Fatal(err)
	}
	if _, err = store.scheduleMessage("account-2", "!room:example.org", "secondary send", dueAt); err != nil {
		t.Fatal(err)
	}
	primarySender := &scheduledSenderProbe{}
	secondarySender := &scheduledSenderProbe{}

	if err = dispatchDueScheduledMessagesForAccount(context.Background(), store, primaryAccountID, primarySender, dueAt); err != nil {
		t.Fatal(err)
	}
	if primarySender.messageCalls != 1 || secondarySender.messageCalls != 0 {
		t.Fatalf("primary dispatch counts = primary:%d secondary:%d", primarySender.messageCalls, secondarySender.messageCalls)
	}
	if err = dispatchDueScheduledMessagesForAccount(context.Background(), store, "account-2", secondarySender, dueAt); err != nil {
		t.Fatal(err)
	}
	if primarySender.messageCalls != 1 || secondarySender.messageCalls != 1 {
		t.Fatalf("account dispatch counts = primary:%d secondary:%d", primarySender.messageCalls, secondarySender.messageCalls)
	}
}

func TestScheduledJobWithoutAccountIDBelongsToPrimaryAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduled-jobs.json")
	legacy := `[{"id":"legacy-job","kind":"message","roomId":"!room:example.org","text":"legacy send","dueAt":4102444800000,"createdAt":1,"status":"pending","error":""}]`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := newScheduledJobStore(path)
	if err != nil {
		t.Fatal(err)
	}
	jobs := store.listForAccount(primaryAccountID)
	if len(jobs) != 1 || jobs[0].AccountID != primaryAccountID {
		t.Fatalf("migrated legacy jobs = %+v", jobs)
	}
}

type scheduledSenderProbe struct {
	messageCalls int
	replyCalls   int
	replyTo      string
}

func (s *scheduledSenderProbe) SendMessage(context.Context, string, string) error {
	s.messageCalls++
	return nil
}

func (s *scheduledSenderProbe) SendReply(_ context.Context, _, _, replyTo string) error {
	s.replyCalls++
	s.replyTo = replyTo
	return nil
}
