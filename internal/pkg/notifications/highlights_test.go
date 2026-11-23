package notifications

import (
	"MySportWeb/internal/pkg/types"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHighlightsMessage(t *testing.T) {
	previous := func(v float64) *float64 { return &v }
	id := uuid.New()
	highlights := []types.Highlight{
		{Kind: types.HighlightGoal, Name: "weekly", Rank: 1, Current: false},
		{Kind: types.HighlightEffort, EffortKind: types.EffortDistance, Target: 5000, Value: 1400, Previous: previous(1450), Rank: 1, Current: true},
		{Kind: types.HighlightEffort, EffortKind: types.EffortDistance, Target: 21097.5, Value: 5400, Previous: previous(5500), Rank: 1, Current: true},
		{Kind: types.HighlightEffort, EffortKind: types.EffortPower, Target: 1200, Value: 260, Previous: previous(250), Rank: 1, Current: true},
		{Kind: types.HighlightSegment, Name: "the hill", Value: 290, Previous: previous(300), Rank: 1, Current: true},
		// beaten since, or second: not notified
		{Kind: types.HighlightEffort, EffortKind: types.EffortDistance, Target: 10000, Value: 3000, Rank: 1, Current: false},
		{Kind: types.HighlightEffort, EffortKind: types.EffortDistance, Target: 1000, Value: 250, Rank: 2},
	}
	message, ok := highlightsMessage(id, "Morning run", highlights)
	if !ok {
		t.Fatal("no message")
	}
	want := "5 km in 23:20 (−0:50); half marathon in 1:30:00 (−1:40); 20 min at 260 W (+10 W), and 1 more."
	if message.Title != "New bests: Morning run" || message.Body != want || !strings.HasSuffix(message.URL, id.String()) {
		t.Errorf("message %+v", message)
	}
	if _, ok := highlightsMessage(id, "x", highlights[5:]); ok {
		t.Error("a message without a best still standing")
	}
	single, _ := highlightsMessage(id, "Climb", highlights[4:5])
	if single.Title != "New best: Climb" || single.Body != "the hill in 4:50 (−0:10)." {
		t.Errorf("single %+v", single)
	}
	for kind, targets := range map[string]map[float64]string{
		types.EffortDistance: {10000: "10 km", 1609.344: "1 mile", 100000: "100 km", 42195: "marathon"},
		types.EffortPower:    {5: "5 s", 60: "1 min", 3600: "1 h"},
	} {
		for target, name := range targets {
			if got := effortTarget(kind, target); got != name {
				t.Errorf("%s %v: %q, want %q", kind, target, got, name)
			}
		}
	}
}
