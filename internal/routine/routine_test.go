package routine

import (
	"strings"
	"testing"
	"time"
)

func TestSchedule_WeeklyShanghai(t *testing.T) {
	s, err := ParseSchedule("0 8 * * 1", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	// Wednesday 2026-09-30 12:00 CST → next Monday 2026-10-05 08:00 CST = 00:00 UTC.
	from := time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)
	next := s.NextAfter(from)
	want := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
	if s.Interval(from) != 7*24*time.Hour {
		t.Fatalf("interval = %s", s.Interval(from))
	}
}

func TestSchedule_EveryMinuteAllowed(t *testing.T) {
	if _, err := ParseSchedule("* * * * *", "UTC"); err != nil {
		t.Fatal(err)
	}
}

func TestSchedule_RejectsNonFiveField(t *testing.T) {
	if _, err := ParseSchedule("* * * * * *", "UTC"); err == nil {
		t.Fatal("six fields must be rejected")
	}
	if _, err := ParseSchedule("@daily", "UTC"); err == nil {
		t.Fatal("descriptor must be rejected")
	}
}

func TestCheckMinGap_RejectsSubMinute(t *testing.T) {
	every := 30 * time.Second
	err := checkMinGap(func(t time.Time) time.Time { return t.Add(every) }, time.Now())
	if err == nil || !strings.Contains(err.Error(), "1 minute") {
		t.Fatalf("got %v", err)
	}
}

func TestClassifyDue(t *testing.T) {
	now := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	weekly, err := ParseSchedule("0 8 * * 1", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	grace := GraceFor(weekly.Interval(now))
	if grace != 24*time.Hour {
		t.Fatalf("weekly grace = %s", grace)
	}
	if got := ClassifyDue(now.Add(-2*time.Hour), now, grace); got != DueFire {
		t.Fatalf("2h late weekly: %s", got)
	}
	if got := ClassifyDue(now.Add(-48*time.Hour), now, grace); got != DueStale {
		t.Fatalf("48h late weekly: %s", got)
	}

	hourly, err := ParseSchedule("0 * * * *", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	hgrace := GraceFor(hourly.Interval(now))
	if hgrace != time.Hour {
		t.Fatalf("hourly grace = %s", hgrace)
	}
	if got := ClassifyDue(now.Add(-30*time.Minute), now, hgrace); got != DueFire {
		t.Fatalf("30m late hourly: %s", got)
	}
	if got := ClassifyDue(now.Add(-3*time.Hour), now, hgrace); got != DueStale {
		t.Fatalf("3h late hourly: %s", got)
	}
}

func TestValidateCreate(t *testing.T) {
	ok := CreateInput{
		Title: "论文周报", Brief: "汇总", Autonomy: AutonomyRead,
		Cron: "0 8 * * 1", Timezone: "Asia/Shanghai",
	}
	if _, err := ValidateCreate(&ok); err != nil {
		t.Fatal(err)
	}
	if ok.MaxDurationSec != DefaultMaxDuration {
		t.Fatalf("duration default = %d", ok.MaxDurationSec)
	}

	cases := []CreateInput{
		{Title: "x", Autonomy: AutonomyBrowse, Cron: "0 8 * * 1", Timezone: "UTC"},
		{Title: "x", Autonomy: AutonomyRead, Hosts: []string{"arxiv.org"}, Cron: "0 8 * * 1", Timezone: "UTC"},
		{Title: "x", Autonomy: AutonomyRead, Cron: "0 8 * * 1", Timezone: "Not/AZone"},
		{Autonomy: AutonomyRead, Cron: "0 8 * * 1", Timezone: "UTC"},
		{Title: "x", Autonomy: AutonomyBrowse, Hosts: []string{"https://arxiv.org/abs"}, Cron: "0 8 * * 1", Timezone: "UTC"},
	}
	for _, in := range cases {
		if _, err := ValidateCreate(&in); err == nil {
			t.Fatalf("expected error for %+v", in)
		}
	}
}
