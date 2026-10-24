package voicepipeline

import (
	"strings"
	"time"
)

type AssetKind string

const (
	VoiceNote     AssetKind = "voice_note"
	CallRecording AssetKind = "call_recording"
)

type Asset struct {
	ID          string    `json:"asset_id"`
	PlayerID    string    `json:"player_id"`
	Kind        AssetKind `json:"kind"`
	AudioBase64 string    `json:"audio_base64"`
	AudioFormat string    `json:"audio_format"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type QueueDecision struct {
	Queue  string `json:"queue"`
	Reason string `json:"reason"`
}

var reviewTerms = []string{"threaten", "kill you", "credit card", "home address"}

// DecideQueue is the deterministic business boundary between live events and moderation work.
func DecideQueue(transcript string) QueueDecision {
	normalized := strings.ToLower(transcript)
	for _, term := range reviewTerms {
		if strings.Contains(normalized, term) {
			return QueueDecision{Queue: "moderation_review", Reason: "safety_term"}
		}
	}
	return QueueDecision{Queue: "live_events", Reason: "routine_comms"}
}
