package services

import (
	"testing"
	"time"
)

func TestKeyedMutex_ReleasesEntriesAfterSequentialDifferentKeys(t *testing.T) {
	var locks keyedMutex

	for i := 1; i <= 32; i++ {
		unlock := locks.Lock(int64(i))
		if got := locks.len(); got != 1 {
			t.Fatalf("ожидали одну активную блокировку для ключа %d, получили %d", i, got)
		}

		unlock()

		if got := locks.len(); got != 0 {
			t.Fatalf("ожидали очистку карты после ключа %d, получили %d элементов", i, got)
		}
	}
}

func TestKeyedMutex_KeepsEntryWhileWaiterExists(t *testing.T) {
	var locks keyedMutex

	unlockFirst := locks.Lock(42)
	started := make(chan struct{})
	acquiredSecond := make(chan struct{})
	releaseSecond := make(chan struct{})
	done := make(chan struct{})

	go func() {
		close(started)
		unlockSecond := locks.Lock(42)
		close(acquiredSecond)
		<-releaseSecond
		unlockSecond()
		close(done)
	}()

	<-started
	waitForLockCondition(t, func() bool {
		locks.mu.Lock()
		defer locks.mu.Unlock()

		entry := locks.locks[42]
		return entry != nil && entry.refs == 2
	}, "ожидали, что счётчик ссылок для ключа 42 станет равен 2")

	unlockFirst()

	waitForLockCondition(t, func() bool {
		select {
		case <-acquiredSecond:
			return true
		default:
			return false
		}
	}, "ожидали, что вторая горутина получит блокировку после освобождения первой")

	if got := locks.len(); got != 1 {
		t.Fatalf("ожидали, что запись о ключе сохранится, пока вторая горутина держит блокировку, получили %d", got)
	}

	close(releaseSecond)

	waitForLockCondition(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "ожидали завершения второй горутины")

	if got := locks.len(); got != 0 {
		t.Fatalf("ожидали очистку карты после завершения всех ожиданий, получили %d элементов", got)
	}
}

func TestBitrixIncomingService_LockDealDoesNotAccumulateKeys(t *testing.T) {
	svc := &bitrixIncomingService{}

	for i := 1; i <= 32; i++ {
		unlock := svc.lockDeal(int64(i))
		if got := svc.dealLocks.len(); got != 1 {
			t.Fatalf("ожидали одну активную блокировку сделки %d, получили %d", i, got)
		}

		unlock()

		if got := svc.dealLocks.len(); got != 0 {
			t.Fatalf("ожидали пустую карту блокировок после сделки %d, получили %d элементов", i, got)
		}
	}
}

func TestPyrusIncomingService_LockTaskDoesNotAccumulateKeys(t *testing.T) {
	svc := &pyrusIncomingService{}

	for i := 1; i <= 32; i++ {
		unlock := svc.lockTask(int64(i))
		if got := svc.taskLocks.len(); got != 1 {
			t.Fatalf("ожидали одну активную блокировку задачи %d, получили %d", i, got)
		}

		unlock()

		if got := svc.taskLocks.len(); got != 0 {
			t.Fatalf("ожидали пустую карту блокировок после задачи %d, получили %d элементов", i, got)
		}
	}
}

func waitForLockCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal(message)
}
