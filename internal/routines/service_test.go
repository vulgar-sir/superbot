package routines

import (
	"log/slog"
	"testing"
	"time"
)

func TestNextRunEvery(t *testing.T) {
	service := NewService(nil, nil, slog.Default())
	from := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	next, err := service.nextRun("@every 1m", from)
	if err != nil {
		t.Fatalf("nextRun: %v", err)
	}
	if next == nil || !next.After(from) {
		t.Fatalf("next run = %v, want after %v", next, from)
	}
	if next.Sub(from) != time.Minute {
		t.Fatalf("next run delta = %v, want 1m", next.Sub(from))
	}
}

func TestNextRunStandardCron(t *testing.T) {
	service := NewService(nil, nil, slog.Default())
	from := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)
	next, err := service.nextRun("0 10 * * *", from)
	if err != nil {
		t.Fatalf("nextRun: %v", err)
	}
	if next == nil || next.Hour() != 10 || next.Minute() != 0 {
		t.Fatalf("next run = %v, want 10:00", next)
	}
}

func TestNextRunInvalid(t *testing.T) {
	service := NewService(nil, nil, slog.Default())
	if _, err := service.nextRun("not a cron", time.Now()); err == nil {
		t.Fatal("expected invalid cron to fail")
	}
}

func TestNextRunFollowsTheGivenTimeZone(t *testing.T) {
	service := NewService(nil, nil, slog.Default())
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("time zone database not available")
	}
	// 00:30 UTC is 08:30 in Shanghai: "9:00 every day" is half an hour away
	// there, not eight and a half hours.
	from := time.Date(2026, 11, 16, 0, 30, 0, 0, time.UTC)
	next, err := service.nextRun("0 9 * * *", from.In(shanghai))
	if err != nil {
		t.Fatalf("nextRun: %v", err)
	}
	if got := next.Sub(from); got != 30*time.Minute {
		t.Fatalf("next run in %v, want 30m", got)
	}
	if service.accountLocation(nil, [16]byte{}).String() != "Asia/Shanghai" {
		t.Fatal("without a setting the platform default time zone applies")
	}
}
