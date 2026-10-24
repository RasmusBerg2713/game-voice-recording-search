package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/infrai-examples/game-voice-search/internal/voicepipeline"
)

const collection = "game-voice-assets"

type server struct {
	client *voicepipeline.Client
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	s := &server{client: voicepipeline.NewClient(key)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /assets", s.ingestAsset)
	mux.HandleFunc("GET /search", s.search)

	addr := ":8080"
	log.Printf("game voice search listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *server) ingestAsset(w http.ResponseWriter, r *http.Request) {
	var asset voicepipeline.Asset
	if err := json.NewDecoder(r.Body).Decode(&asset); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if asset.ID == "" || asset.PlayerID == "" || asset.AudioBase64 == "" || asset.AudioFormat == "" ||
		(asset.Kind != voicepipeline.VoiceNote && asset.Kind != voicepipeline.CallRecording) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "asset_id, player_id, supported kind, audio_base64, and audio_format are required"})
		return
	}

	transcript, err := s.client.Transcribe(r.Context(), asset)
	if err != nil {
		s.writeUpstreamError(w, err)
		return
	}
	decision := voicepipeline.DecideQueue(transcript)
	embedding, err := s.client.Embed(r.Context(), transcript)
	if err != nil {
		s.writeUpstreamError(w, err)
		return
	}
	if err := s.client.Upsert(r.Context(), collection, asset, transcript, decision, embedding); err != nil {
		s.writeUpstreamError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"asset_id": asset.ID, "transcript": transcript, "state": "searchable",
		"queue": decision.Queue, "reason": decision.Reason,
	})
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	queue := r.URL.Query().Get("queue")
	if queue == "" {
		queue = "live_events"
	}
	topK := 5
	if raw := r.URL.Query().Get("top_k"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 20 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "top_k must be between 1 and 20"})
			return
		}
		topK = parsed
	}
	if query == "" || (queue != "live_events" && queue != "moderation_review") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q and a supported queue are required"})
		return
	}

	hits, err := s.client.Query(r.Context(), collection, query, queue, topK)
	if err != nil {
		s.writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

func (s *server) writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *voicepipeline.InfraiError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	log.Printf("pipeline request failed: %v", err)
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "pipeline request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
