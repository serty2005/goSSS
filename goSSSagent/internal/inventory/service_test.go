package inventory

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type stubCollector struct {
	collect func(context.Context) (Snapshot, error)
}

func (c stubCollector) Collect(ctx context.Context) (Snapshot, error) {
	return c.collect(ctx)
}

func TestServiceSnapshotReportsPresence(t *testing.T) {
	t.Parallel()

	service := NewService(time.Minute)

	if snapshot, ok := service.Snapshot(); ok {
		t.Fatalf("до первого сбора не ожидался snapshot, получено %+v", snapshot)
	}

	expected := Snapshot{
		CollectedAt: time.Unix(100, 0).UTC(),
		Hostname:    "test-host",
		OS:          "windows",
		Arch:        "amd64",
	}
	service.collector = stubCollector{
		collect: func(context.Context) (Snapshot, error) {
			return expected, nil
		},
	}

	got, err := service.CollectNow(t.Context())
	if err != nil {
		t.Fatalf("CollectNow завершился ошибкой: %v", err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("CollectNow вернул неожиданный snapshot: %+v", got)
	}

	snapshot, ok := service.Snapshot()
	if !ok {
		t.Fatal("после успешного сбора ожидался сохраненный snapshot")
	}
	if !reflect.DeepEqual(snapshot, expected) {
		t.Fatalf("Snapshot вернул неожиданный снимок: %+v", snapshot)
	}
}

func TestServiceCollectorErrorKeepsPreviousSnapshot(t *testing.T) {
	t.Parallel()

	service := NewService(time.Minute)
	first := Snapshot{
		CollectedAt: time.Unix(200, 0).UTC(),
		Hostname:    "stable-host",
		OS:          "windows",
		Arch:        "amd64",
	}
	call := 0
	service.collector = stubCollector{
		collect: func(context.Context) (Snapshot, error) {
			call++
			if call == 1 {
				return first, nil
			}
			return Snapshot{}, errors.New("ошибка коллектора")
		},
	}

	if _, err := service.CollectNow(t.Context()); err != nil {
		t.Fatalf("первый CollectNow завершился ошибкой: %v", err)
	}
	if _, err := service.CollectNow(t.Context()); err == nil {
		t.Fatal("ожидалась ошибка от второго CollectNow")
	}

	snapshot, ok := service.Snapshot()
	if !ok {
		t.Fatal("после ошибки должен оставаться предыдущий валидный snapshot")
	}
	if !reflect.DeepEqual(snapshot, first) {
		t.Fatalf("ошибка коллектора не должна затирать предыдущий snapshot: %+v", snapshot)
	}
}
