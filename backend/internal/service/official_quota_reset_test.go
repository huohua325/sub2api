package service

import (
	"testing"
	"time"
)

func TestSharedOfficialResetRequiresAgreement(t *testing.T) {
	day := time.Date(2026, 11, 1, 8, 23, 0, 0, time.UTC)
	shared, ok := SharedOfficialReset(PurposeGrok, []time.Time{day, day.Add(2 * time.Minute)})
	if !ok || !shared.Equal(day) {
		t.Fatalf("shared = %s ok=%v", shared, ok)
	}
	if _, ok := SharedOfficialReset(PurposeGrok, []time.Time{day, day.Add(3 * 24 * time.Hour)}); ok {
		t.Fatal("differing account clocks were treated as one reset")
	}
	if _, ok := SharedOfficialReset(PurposeGrok, nil); ok {
		t.Fatal("missing resets were shared")
	}
	if _, ok := SharedOfficialReset(PurposeGrok, []time.Time{day}); ok {
		t.Fatal("one Grok account was treated as an overall reset")
	}
	codex, ok := SharedOfficialReset(PurposeOpenAI, []time.Time{day})
	if !ok || !codex.Equal(day) {
		t.Fatalf("codex overall reset = %s ok=%v", codex, ok)
	}
}

func TestStudentCodingResetKeepsPersonalWindow(t *testing.T) {
	personal := time.Date(2026, 11, 4, 7, 0, 0, 0, time.UTC)
	sharedDay := time.Date(2026, 11, 1, 8, 23, 0, 0, time.UTC)
	if got := StudentCodingReset(PurposeGrok, personal, []time.Time{sharedDay, sharedDay.Add(time.Minute)}); !got.Equal(sharedDay) {
		t.Fatalf("collective reset = %s", got)
	}
	early := sharedDay.Add(-3 * 24 * time.Hour)
	if got := StudentCodingReset(PurposeGrok, personal, []time.Time{early, sharedDay}); !got.Equal(personal) {
		t.Fatalf("split clocks moved the personal window to %s", got)
	}
	if got := StudentCodingReset(PurposeGrok, personal, nil); !got.Equal(personal) {
		t.Fatalf("missing official reset = %s", got)
	}
	if got := StudentCodingReset(PurposeGrok, personal, []time.Time{sharedDay}); !got.Equal(personal) {
		t.Fatalf("one Grok account replaced the personal window with %s", got)
	}
	if got := StudentCodingReset(PurposeOpenAI, personal, []time.Time{sharedDay}); !got.Equal(sharedDay) {
		t.Fatalf("codex overall reset fell back to %s", got)
	}
}
