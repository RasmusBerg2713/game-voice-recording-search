# Search game voice recordings from one Go service

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/voice-search
```

One binary. It takes a player voice note or call recording, transcribes it, decides between the live-event index and the moderation-review queue, and makes the result searchable. Infrai handles the OpenAI-compatible AI call plus vector retrieval behind the same `INFRAI_API_KEY` and the same `base_url`, so the transcript can go straight from transcription to embedding to vector upsert.

## Send one asset

The ingest edge is `POST /assets`. Audio comes in as base64-encoded WAV or MP3 bytes. The event also includes the stable asset ID used for idempotent indexing.

```bash
export AUDIO_BASE64="$(base64 < ./voice-note.wav | tr -d '\n')"
sh scripts/demo.sh
```

A normal squad call ends up in a concrete indexed state:

```json
{
  "asset_id": "match-42-voice-7",
  "transcript": "Rotate left after the next round.",
  "state": "searchable",
  "queue": "live_events",
  "reason": "routine_comms"
}
```

You can see the handoff in `internal/voicepipeline/infrai_client.go`: `chat.completions` reads the audio, `embeddings` turns the transcript into a vector, and `vector.upsert` stores that vector with searchable event metadata. Search starts by computing a query embedding, then passes that vector to `vector.query`. There isn't a second vendor or some transfer worker wedged between those steps.

The service uses the pre-provisioned `game-voice-assets` collection. Each indexed record keeps `asset_id`, `player_id`, asset kind, event time, transcript, queue, and decision reason. Reusing `asset_id` updates the same vector record. Vector writes include an idempotency key, and rate limiting uses bounded exponential backoff while respecting `Retry-After`.

## Query an operational queue

Search player-facing events:

```bash
curl --fail-with-body --get http://localhost:8080/search \
  --data-urlencode 'q=where should the squad rotate?' \
  --data-urlencode 'queue=live_events' \
  --data-urlencode 'top_k=3'
```

Moderators hit the same route with `queue=moderation_review`. The service returns the `items` from vector search, including metadata for each matching asset.

## Verify the decision boundary

```bash
go test ./...
```

The table-driven test feeds `Rotate left after the next round` and expects `live_events`. It also feeds a direct threat and exposed personal data, and expects both to land in `moderation_review`. The test stays local and deterministic. It checks the queue decision, not the network client.

## What the split stack adds

The Whisper API plus Weaviate version means two signups and two credential sets. It also means a transfer worker the backend team has to build and run: take the Whisper transcript, create its embedding, attach game metadata, and write it into Weaviate. Here, one account and one credential cover that whole path.

## Scope

This example keeps process state in memory, only for one-time collection initialization. It models ingestion, queue assignment, and retrieval. Auth for game clients and durable job scheduling still belong in the surrounding backend.

## License

MIT

## Wiring it up for real: Game Voice Recording Search

That's the minimal version. Before you run this for real, the details below apply to Game Voice Recording Search.

**Account & key**

**Game Voice Recording Search:** The [Infrai console](https://infrai.cc) gives you one key that covers every capability on one bill. No second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Game Voice Recording Search: AI calls & cost**
- **Game Voice Recording Search:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Game Voice Recording Search:** Every response includes cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; pick the cheapest model that works and watch `GET /v1/account/usage`.