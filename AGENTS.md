# Autonomous Sub-Agent Orchestration Manifest (`AGENTS.md`)

## Architectural Invariants
1. **Personal WhatsApp Account Safety**:
   - Never auto-reply to un-triggered 1-on-1 DMs.
   - A message triggers Nova if and only if:
     a) `trigger.ContainsTrigger(text)` is true (or group `@mention` of Nova), OR
     b) `isReply` is true AND the quoted message ID `repliedMsgID` was sent by Nova (`isNovaSent(repliedMsgID)` or `chatLogger.IsNovaMessage(chatType, chatID, repliedMsgID)`), OR
     c) `attentionGate.HasActiveLease(chatID, senderID, isFromMe)` is true (where a 90s lease is granted to `(chatID, senderID)` only after Nova replies to a message from `senderID` that was triggered by (a) or (b)).
   - Never emit `MarkRead` or `SendChatPresence` on messages that do not trigger Nova.
2. **Zero Message Dropping on Bursts**:
   - Rapid multi-bubble messages from a triggered/leased user are coalesced via `internal/pacing/coalescer.go` rather than dropped by a hard 3s reject.
3. **Deterministic Quality Gate**:
   - Every sub-agent workstream must pass `go build ./...`, `go vet ./...`, and `go test -race ./...` with exit code `0`.

## Workstream Ownership Matrix

### 1. `domain-core-worker`
- **Owned Files**:
  - `internal/pacing/diversity.go` & `internal/pacing/diversity_test.go`
  - `internal/pacing/coalescer.go` & `internal/pacing/coalescer_test.go`
  - `internal/pacing/pacer.go` & `internal/pacing/pacer_test.go`
  - `internal/trigger/attention.go` & `internal/trigger/attention_test.go`
  - `internal/trigger/matcher.go` & `internal/trigger/matcher_test.go`
  - `internal/emotion/engine.go` & `internal/emotion/emotion_test.go`
- **Responsibilities**:
  - Build `AttentionGate` (90s per-`(chatID, senderID)` lease, strict personal-account DM protection, Nova message-ID reply verification).
  - Build `TurnCoalescer` (presence-aware burst coalescing) and `HumanPacer` (read delay, typing duration calculation, bubble splitting).
  - Build `StyleDiversityFilter` (n-gram vocative & emoji cluster repetition detector over recent assistant turns).
  - Upgrade `emotion.Engine` with Arabic negation awareness (`مش`, `ما`, `مبقتش`, `بطلت`, `غير`) and continuous PAD coordinates.

### 2. `persistence-infra-worker`
- **Owned Files**:
  - `internal/config/config.go`
  - `internal/storage/chat_logger.go` & `internal/storage/memory_store.go` & `internal/storage/storage_test.go`
  - `internal/trigger/context_builder.go`
  - `internal/ai/payload.go`, `internal/ai/parser.go`, `internal/ai/parser_test.go`, `internal/ai/client.go`
  - `config/system_prompt.md`, `config/system_prompt_female.md`
- **Responsibilities**:
  - Add `ChatLogger.IsNovaMessage(chatType, chatID, messageID string) bool` with an in-memory LRU/map cache backed by `.jsonl` scan so quote-replies to Nova's messages are always recognized even across restarts without matching the human owner's personal messages.
  - Filter `[Reaction: ...]` lines from LLM `RecentContext` in `BuildContext`, bound default `CONTEXT_HISTORY_LIMIT` to `25`, sanitize `"null"` memory notes in `MemoryStore` and `ParseResponse`.
  - Extend `ResponsePayload` (`Bubbles []string`, `Action string`, `ReactionEmoji *string`) and `RequestPayload` (`StyleGuidance *string`, `AttentionLeaseActive bool`).
  - Update `internal/ai/client.go`: remove `"تم البحث\n\n"`, add `frequency_penalty: 0.45` & `presence_penalty: 0.35`, inject `TimeSinceLastMessage`, `StyleGuidance`, and lease-aware instructions, and update `config/system_prompt*.md` with multi-bubble (`||`) and anti-repetition directives.

### 3. `api-transport-worker`
- **Owned Files**:
  - `internal/whatsapp/client.go`, `internal/whatsapp/media.go`, `internal/whatsapp/handler.go`
  - `cmd/novabot/main.go`
- **Responsibilities**:
  - Add `MarkRead`, `SendChatPresence`, `SendReaction`, and `SendText` (unquoted message) to `whatsapp.Client`.
  - Handle `*events.ChatPresence` in `EventHandler.HandleEvent` to notify `TurnCoalescer` when a leased/triggered user is actively typing.
  - Integrate `AttentionGate`, `TurnCoalescer`, `HumanPacer`, `StyleDiversityFilter`, `TriTierEngine` (with live `OpenRouterEmbedder` & `OpenRouterEpisodeSummarizer`), and `dashboardServer` metrics/logging into `handler.go` and `cmd/novabot/main.go`.

### 4. `qa-verification-worker`
- **Owned Files**:
  - Integration & replay tests verifying all personal-account safety invariants, burst coalescing, anti-repetition, and full test suite execution.
