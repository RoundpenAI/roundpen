package photos_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/photos"
)

func TestMatchAnniversaryName(t *testing.T) {
	y, ok := photos.MatchAnniversaryName("IMG_20191105_1200.jpg", 11, 5, 2026)
	if !ok || y != 7 {
		t.Fatalf("got years=%d ok=%v", y, ok)
	}
	if _, ok := photos.MatchAnniversaryName("random.jpg", 11, 5, 2026); ok {
		t.Fatal("expected miss")
	}
}

func TestScanOnThisDayFilename(t *testing.T) {
	dir := t.TempDir()
	name := "vacation-2019-11-05.jpg"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	hits, err := photos.ScanOnThisDay(context.Background(), photos.ScanOptions{
		Roots: []string{dir}, Now: now, MaxHits: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].YearsAgo != 7 || hits[0].Match != "filename" {
		t.Fatalf("hits=%+v", hits)
	}
}

func TestTakenDateFromName(t *testing.T) {
	tm, ok := photos.TakenDateFromName("IMG_20200315_001.jpg")
	if !ok || tm.Format("2006-01-02") != "2020-03-15" {
		t.Fatalf("got %v ok=%v", tm, ok)
	}
}
