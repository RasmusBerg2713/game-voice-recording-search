package voicepipeline

import "testing"

func TestDecideQueue(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		wantQueue  string
		wantReason string
	}{
		{name: "ordinary squad call", transcript: "Rotate left after the next round", wantQueue: "live_events", wantReason: "routine_comms"},
		{name: "direct threat", transcript: "I will kill you after this match", wantQueue: "moderation_review", wantReason: "safety_term"},
		{name: "personal data", transcript: "Post their home address in chat", wantQueue: "moderation_review", wantReason: "safety_term"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideQueue(tt.transcript)
			if got.Queue != tt.wantQueue || got.Reason != tt.wantReason {
				t.Fatalf("DecideQueue(%q) = %#v, want queue=%q reason=%q", tt.transcript, got, tt.wantQueue, tt.wantReason)
			}
		})
	}
}
