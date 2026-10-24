#!/bin/sh
set -eu

: "${AUDIO_BASE64:?set AUDIO_BASE64 to a base64-encoded WAV sample}"

curl --fail-with-body --request POST http://localhost:8080/assets \
  --header 'Content-Type: application/json' \
  --data "{\"asset_id\":\"match-42-voice-7\",\"player_id\":\"player-17\",\"kind\":\"voice_note\",\"audio_base64\":\"$AUDIO_BASE64\",\"audio_format\":\"wav\",\"occurred_at\":\"2026-09-18T10:30:00Z\"}"

curl --fail-with-body --get http://localhost:8080/search \
  --data-urlencode 'q=where should the squad rotate?' \
  --data-urlencode 'queue=live_events' \
  --data-urlencode 'top_k=3'
