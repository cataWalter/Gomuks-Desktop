package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	scheduledMessageKind  = "message"
	scheduledReminderKind = "reminder"

	scheduledPending   = "pending"
	scheduledSending   = "sending"
	scheduledSent      = "sent"
	scheduledSentLate  = "sent-late"
	scheduledFailed    = "failed"
	scheduledFired     = "fired"
	scheduledCancelled = "cancelled"
	scheduledDismissed = "dismissed"
)

type ScheduledJob struct {
	ID             string `json:"id"`
	AccountID      string `json:"accountId"`
	Kind           string `json:"kind"`
	RoomID         string `json:"roomId"`
	Text           string `json:"text"`
	EventID        string `json:"eventId"`
	ReplyToEventID string `json:"replyToEventId,omitempty"`
	DueAt          int64  `json:"dueAt"`
	CreatedAt      int64  `json:"createdAt"`
	Status         string `json:"status"`
	Error          string `json:"error"`
}

type scheduledJobStore struct {
	mu   sync.Mutex
	path string
	jobs []ScheduledJob
}

func newScheduledJobStore(path string) (*scheduledJobStore, error) {
	if path == "" {
		return nil, errors.New("scheduled jobs path is empty")
	}
	store := &scheduledJobStore{path: path, jobs: []ScheduledJob{}}
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read scheduled jobs: %w", err)
	}
	if err = json.Unmarshal(contents, &store.jobs); err != nil {
		return nil, fmt.Errorf("decode scheduled jobs: %w", err)
	}
	if store.jobs == nil {
		store.jobs = []ScheduledJob{}
	}
	if err = os.Chmod(path, 0o600); err != nil && runtime.GOOS != "windows" {
		return nil, fmt.Errorf("secure scheduled jobs file: %w", err)
	}
	for index := range store.jobs {
		if store.jobs[index].AccountID == "" {
			store.jobs[index].AccountID = primaryAccountID
		}
		if store.jobs[index].Status == scheduledSending {
			store.jobs[index].Status = scheduledFailed
			store.jobs[index].Error = "The app closed while sending. Check the room before retrying."
		}
	}
	if err = store.saveLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *scheduledJobStore) scheduleMessage(accountID, roomID, text string, dueAt int64) (ScheduledJob, error) {
	return s.create(scheduledMessageKind, accountID, roomID, strings.TrimSpace(text), "", "", dueAt)
}

func (s *scheduledJobStore) scheduleReminder(accountID, roomID, eventID, text string, dueAt int64) (ScheduledJob, error) {
	return s.create(scheduledReminderKind, accountID, roomID, strings.TrimSpace(text), eventID, "", dueAt)
}

func (s *scheduledJobStore) scheduleReply(accountID, roomID, text, replyToEventID string, dueAt int64) (ScheduledJob, error) {
	replyToEventID = strings.TrimSpace(replyToEventID)
	if replyToEventID == "" {
		return ScheduledJob{}, errors.New("scheduled reply requires a target event ID")
	}
	return s.create(scheduledMessageKind, accountID, roomID, strings.TrimSpace(text), "", replyToEventID, dueAt)
}

func (s *scheduledJobStore) create(kind, accountID, roomID, text, eventID, replyToEventID string, dueAt int64) (ScheduledJob, error) {
	if kind != scheduledMessageKind && kind != scheduledReminderKind {
		return ScheduledJob{}, errors.New("unsupported scheduled job kind")
	}
	if strings.TrimSpace(accountID) == "" {
		return ScheduledJob{}, errors.New("scheduled job requires an account ID")
	}
	if strings.TrimSpace(roomID) == "" {
		return ScheduledJob{}, errors.New("scheduled job requires a room ID")
	}
	if kind == scheduledMessageKind && text == "" {
		return ScheduledJob{}, errors.New("scheduled message cannot be empty")
	}
	if dueAt <= time.Now().UnixMilli() {
		return ScheduledJob{}, errors.New("scheduled time must be in the future")
	}
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return ScheduledJob{}, fmt.Errorf("generate scheduled job ID: %w", err)
	}
	job := ScheduledJob{
		ID:             hex.EncodeToString(randomID[:]),
		AccountID:      accountID,
		Kind:           kind,
		RoomID:         roomID,
		Text:           text,
		EventID:        eventID,
		ReplyToEventID: replyToEventID,
		DueAt:          dueAt,
		CreatedAt:      time.Now().UnixMilli(),
		Status:         scheduledPending,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, job)
	if err := s.saveLocked(); err != nil {
		s.jobs = s.jobs[:len(s.jobs)-1]
		return ScheduledJob{}, err
	}
	return job, nil
}

func (s *scheduledJobStore) list() []ScheduledJob {
	return s.listForAccount("")
}

func (s *scheduledJobStore) listForAccount(accountID string) []ScheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]ScheduledJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if accountID != "" && job.AccountID != accountID {
			continue
		}
		if job.Status == scheduledSent || job.Status == scheduledCancelled || job.Status == scheduledDismissed {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.SliceStable(jobs, func(left, right int) bool {
		return jobs[left].DueAt < jobs[right].DueAt
	})
	return jobs
}

func (s *scheduledJobStore) claimDue(kind string, now int64) ([]ScheduledJob, error) {
	return s.claimDueForAccount(kind, now, "")
}

func (s *scheduledJobStore) claimDueForAccount(kind string, now int64, accountID string) ([]ScheduledJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := append([]ScheduledJob(nil), s.jobs...)
	claimed := make([]ScheduledJob, 0)
	for index := range s.jobs {
		job := &s.jobs[index]
		if job.Kind != kind || job.Status != scheduledPending || job.DueAt > now || accountID != "" && job.AccountID != accountID {
			continue
		}
		if kind == scheduledMessageKind {
			job.Status = scheduledSending
		} else {
			job.Status = scheduledFired
		}
		claimed = append(claimed, *job)
	}
	if len(claimed) == 0 {
		return nil, nil
	}
	if err := s.saveLocked(); err != nil {
		s.jobs = previous
		return nil, err
	}
	return claimed, nil
}

func (s *scheduledJobStore) finishMessage(id string, sendErr error, startedAt int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.jobs {
		job := &s.jobs[index]
		if job.ID != id || job.Status != scheduledSending {
			continue
		}
		previous := *job
		if sendErr != nil {
			job.Status = scheduledFailed
			job.Error = sendErr.Error()
		} else {
			job.Status = scheduledSent
			if startedAt > job.DueAt+10_000 {
				job.Status = scheduledSentLate
			}
			job.Error = ""
		}
		if err := s.saveLocked(); err != nil {
			*job = previous
			return err
		}
		return nil
	}
	return errors.New("scheduled message is no longer being sent")
}

func (s *scheduledJobStore) update(id string, dueAt int64) error {
	if dueAt <= time.Now().UnixMilli() {
		return errors.New("scheduled time must be in the future")
	}
	return s.updateJob(id, func(job *ScheduledJob) error {
		if job.Status != scheduledPending && job.Status != scheduledFailed {
			return errors.New("only pending or failed jobs can be rescheduled")
		}
		job.DueAt = dueAt
		job.Status = scheduledPending
		job.Error = ""
		return nil
	})
}

func (s *scheduledJobStore) cancel(id string) error {
	return s.updateJob(id, func(job *ScheduledJob) error {
		if job.Status != scheduledPending && job.Status != scheduledFailed {
			return errors.New("only pending or failed jobs can be cancelled")
		}
		job.Status = scheduledCancelled
		job.Error = ""
		return nil
	})
}

func (s *scheduledJobStore) retryMessage(id string, now int64) error {
	return s.updateJob(id, func(job *ScheduledJob) error {
		if job.Kind != scheduledMessageKind || job.Status != scheduledFailed {
			return errors.New("only failed scheduled messages can be retried")
		}
		job.DueAt = now
		job.Status = scheduledPending
		job.Error = ""
		return nil
	})
}

func (s *scheduledJobStore) dismiss(id string) error {
	return s.updateJob(id, func(job *ScheduledJob) error {
		if job.Status == scheduledFired && job.Kind == scheduledReminderKind {
			job.Status = scheduledDismissed
			return nil
		}
		if job.Status == scheduledSentLate && job.Kind == scheduledMessageKind {
			job.Status = scheduledDismissed
			return nil
		}
		return errors.New("only fired reminders or delayed sends can be dismissed")
	})
}

func (s *scheduledJobStore) updateJob(id string, update func(*ScheduledJob) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.jobs {
		job := &s.jobs[index]
		if job.ID != id {
			continue
		}
		previous := *job
		if err := update(job); err != nil {
			return err
		}
		if err := s.saveLocked(); err != nil {
			*job = previous
			return err
		}
		return nil
	}
	return errors.New("scheduled job not found")
}

func (s *scheduledJobStore) saveLocked() error {
	contents, err := json.Marshal(s.jobs)
	if err != nil {
		return fmt.Errorf("encode scheduled jobs: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create scheduled jobs directory: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err = os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure scheduled jobs directory: %w", err)
		}
	}
	temporary, err := os.CreateTemp(directory, ".gomuks-scheduled-*")
	if err != nil {
		return fmt.Errorf("create temporary scheduled jobs file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		var written int
		written, err = temporary.Write(append(contents, '\n'))
		if err == nil && written != len(contents)+1 {
			err = errors.New("short write")
		}
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write scheduled jobs: %w", err)
	}
	if err = os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace scheduled jobs file: %w", err)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open scheduled jobs directory: %w", err)
	}
	if err = directoryFile.Sync(); err != nil {
		_ = directoryFile.Close()
		return fmt.Errorf("sync scheduled jobs directory: %w", err)
	}
	if err = directoryFile.Close(); err != nil {
		return fmt.Errorf("close scheduled jobs directory: %w", err)
	}
	return nil
}
