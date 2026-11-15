package domain

import (
	"testing"
	"time"
)

func TestFireSettlesAOneShotReminder(t *testing.T) {
	rem, err := New("u1", "check the fix", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	rem.Fire()

	if rem.Status != StatusFired {
		t.Fatalf("status = %v, want fired", rem.Status)
	}
	if rem.FiredAt == nil {
		t.Fatal("fired_at was not set")
	}
}

func TestFireReschedulesARecurringReminder(t *testing.T) {
	rem, err := New("u1", "50 prospects", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	every := 24 * time.Hour
	if err := rem.SetRunEvery(&every); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC()
	rem.Fire()

	if rem.Status != StatusPending {
		t.Fatalf("status = %v, want pending: a recurring reminder must not settle", rem.Status)
	}
	if rem.FiredAt == nil {
		t.Fatal("fired_at was not set")
	}
	// The next due date is computed from now, not from the elapsed due_at, so
	// a poller that was down for a while resumes the cadence instead of
	// replaying every occurrence it missed.
	if rem.DueAt.Before(before.Add(every)) || rem.DueAt.After(before.Add(every+time.Minute)) {
		t.Fatalf("due_at = %v, want about %v from now", rem.DueAt, every)
	}
}

func TestSetRunEveryRejectsAnIntervalOutOfRange(t *testing.T) {
	rem, err := New("u1", "check the fix", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	tooShort := 30 * time.Second
	if err := rem.SetRunEvery(&tooShort); err == nil {
		t.Fatal("an interval shorter than MinRunEvery was accepted")
	}

	tooLong := 400 * 24 * time.Hour
	if err := rem.SetRunEvery(&tooLong); err == nil {
		t.Fatal("an interval longer than MaxRunEvery was accepted")
	}
}

func TestSetRunEveryOnlyAppliesToAPendingReminder(t *testing.T) {
	rem, err := New("u1", "check the fix", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rem.Fire()

	every := time.Hour
	if err := rem.SetRunEvery(&every); err == nil {
		t.Fatal("recurrence was set on a reminder that already fired")
	}
}

func TestSetRunEveryNilClearsRecurrence(t *testing.T) {
	rem, err := New("u1", "50 prospects", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	every := 24 * time.Hour
	if err := rem.SetRunEvery(&every); err != nil {
		t.Fatal(err)
	}
	if !rem.IsRecurring() {
		t.Fatal("reminder should be recurring")
	}

	if err := rem.SetRunEvery(nil); err != nil {
		t.Fatal(err)
	}
	if rem.IsRecurring() {
		t.Fatal("reminder should no longer be recurring")
	}
}

func TestNewKeepsAPastDueTimeAsAnOverdueTask(t *testing.T) {
	rem, err := New("u1", "call the bank", time.Now().UTC().Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("a past due time must be accepted: %v", err)
	}
	if rem.Status != StatusFired {
		t.Fatalf("status = %v, want fired so the poller does not notify it", rem.Status)
	}
	if !rem.IsOverdue(time.Now().UTC()) {
		t.Fatal("a task past its due time must be overdue")
	}
}

func TestRescheduleToTheFutureArmsTheReminderAgain(t *testing.T) {
	rem, _ := New("u1", "call the bank", time.Now().UTC().Add(-time.Hour))

	if err := rem.Reschedule(time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if rem.Status != StatusPending || rem.FiredAt != nil {
		t.Fatalf("status = %v fired_at = %v, want pending and unfired", rem.Status, rem.FiredAt)
	}
	if rem.IsOverdue(time.Now().UTC()) {
		t.Fatal("a task moved to the future is no longer overdue")
	}
}

func TestRescheduleRefusesASettledReminder(t *testing.T) {
	rem, _ := New("u1", "call the bank", time.Now().UTC().Add(time.Hour))
	rem.Done()

	if err := rem.Reschedule(time.Now().UTC().Add(2 * time.Hour)); err != ErrSettled {
		t.Fatalf("err = %v, want ErrSettled", err)
	}
	if rem.IsOverdue(time.Now().UTC().Add(3 * time.Hour)) {
		t.Fatal("a done task is never overdue")
	}
}
