# Product Requirements Document (PRD): NovaBot v2.0 — Human-Like Cognitive WhatsApp Agent

## 1. Core Mandate & Personal Account Safety Invariant
NovaBot runs directly on the owner's **personal WhatsApp account** (`go.mau.fi/whatsmeow`), sharing the same JID/LID as the human account owner.
- **CRITICAL PERSONAL ACCOUNT INVARIANT**:
  1. **NO Auto-Reply in 1-on-1 DMs**: Nova must NEVER automatically intercept or reply to regular contacts in private 1-on-1 chats (`chatType == "private"`) unless explicitly triggered.
  2. **Initiation Triggers (Private DMs & Groups)**:
     - Explicit wake-word/name call in the message text (`"يا نوفا"`, `"يانوفا"`, `"@nova"`, `"@نوفا"`, etc. via `trigger.ContainsTrigger`).
     - Direct WhatsApp quote-reply to a message **specifically sent by Nova** (verified via `isNovaSent(repliedMsgID)` or `chatLogger.IsNovaMessage(chatType, chatID, repliedMsgID)` — NEVER merely matching the owner's JID/LID on non-Nova messages, so replies to the human owner's personal messages are never hijacked).
     - In group chats only: `@mention` of the bot JID/number or quote-reply where the quoted text contained a Nova trigger.
  3. **Dynamic Attention Lease (Follow-up Window)**:
     - Once a specific user `(chatID, senderID)` initiates a conversation with Nova via an Initiation Trigger and Nova replies, an active **Attention Lease** (default TTL: `90 seconds`, refreshed on each Nova reply to that sender) is granted to `(chatID, senderID)`.
     - While the lease is active for `(chatID, senderID)`, follow-up messages from that same `senderID` in `chatID` are routed to Nova without requiring `"يا نوفا"` on every line.
     - If `evt.Info.IsFromMe` is true (the human owner typing from their phone) and the message does NOT contain an explicit `"يا نوفا"` trigger or quote a Nova message, Nova must NOT trigger on it.
     - During an active lease turn (where `"يا نوفا"` was not explicitly used), the LLM can return `should_reply: false` or `action: "ignore"` / `action: "react_only"` if the user has clearly finished talking to Nova or shifted to addressing someone else.

## 2. Human Interaction & Pacing Requirements (`internal/pacing/`)
1. **Presence-Aware Turn Coalescing (`TurnCoalescer`)**:
   - Replace the 3-second message-dropping rate limiter (`limiter.Allow`) with an asynchronous per-`(chatID, senderID)` burst coalescer.
   - When a triggered/leased message arrives, open a `1800ms` debounce window. If another message arrives from the same sender in the same chat within the window, append it (joined by `\n`) and reset the window (up to a maximum total wait of `4500ms`).
   - If a WhatsApp `*events.ChatPresence` (`ChatPresenceComposing`) event arrives from that sender, extend the active debounce window by `1500ms` (within the `4500ms` cap) so Nova waits for the user to finish typing their second bubble.
2. **Human Kinematics & Presence (`HumanPacer`)**:
   - **Strictly Post-Trigger**: Never send read receipts or typing indicators for un-triggered personal messages.
   - **Simulated Read Receipt**: Once a turn is accepted for Nova processing, wait a realistic reading delay `clamp(350ms + 15ms * runeCount, 400ms, 1800ms)` and call `waClient.MarkRead`.
   - **Typing / Audio Presence**: During LLM generation and before each outgoing bubble, emit `SendChatPresence(ChatPresenceComposing, ChatPresenceMediaText)` (or `ChatPresenceMediaAudio` when synthesizing a voice note).
   - **Multi-Bubble Splitting**: Support splitting a single AI response into 1–3 natural sequential WhatsApp bubbles (via `Bubbles []string` in JSON or `||` separator in text), with proportional inter-bubble typing delays (`clamp(45ms * runeCount, 800ms, 4500ms)`).
   - **Smart Quote Suppression**: In private DMs or immediate consecutive turns where no message intervened, allow sending unquoted messages (`SendText`) for natural flow, while quoting (`SendReply`) when replying in busy groups or to older messages.
   - **Native WhatsApp Emoji Reactions**: Support `waClient.SendReaction(ctx, chatJID, senderJID, targetMsgID, emoji)` when `ReactionEmoji` is returned by the AI (either alongside a reply or as `action: "react_only"`).

## 3. Anti-Repetition & Persona Realism (`internal/pacing/diversity.go` & `internal/emotion/`)
1. **Anti-Repetition Style Diversity Filter (`StyleDiversityFilter`)**:
   - Inspect Nova's last 10 responses in the current chat.
   - Detect repeated opening vocatives/phrases (2–4 word prefixes appearing `>= 2` times, e.g., `"يا تينا يا قمر"`) and repeated emoji clusters (appearing `>= 2` times, e.g., `"🦋🌸😂"`).
   - Inject a dynamic `[STYLE DIVERSITY & ANTI-REPETITION DIRECTIVE]` into the system prompt banning those exact overused phrases/emojis for the current turn.
   - Apply `frequency_penalty: 0.45` and `presence_penalty: 0.35` on OpenRouter chat completion requests.
   - Remove the robotic `"تم البحث\n\n"` prefix from `internal/ai/client.go`.
2. **Negation-Aware PAD Emotion & Per-User Affinity (`internal/emotion/`)**:
   - Fix negation blindness in `emotion/engine.go` (e.g., `"مش زعلان"`, `"مش متضايق"` must NOT trigger sadness/annoyance).
   - Track continuous Pleasure-Arousal-Dominance (PAD) state alongside per-user relational affinity and inside-joke cooldowns.

## 4. Memory, Context Bounding & Subsystem Wiring (`internal/memory/`, `internal/storage/`)
1. **Bounded Clean Context Window**:
   - Change default `CONTEXT_HISTORY_LIMIT` from `0` (`all`) to `25` messages in `internal/config/config.go`.
   - Filter out `[Reaction: ...]` placeholder noise when building LLM `RecentContext` in `internal/trigger/context_builder.go`.
   - Inject `payload.TimeSinceLastMessage` into `buildSystemPrompt` in `internal/ai/client.go`.
   - Block literal `"null"` or `"none"` strings from being written into `data/users/*.md` or `TriTierEngine`.
2. **Full Tri-Tier Memory & Dashboard Wiring**:
   - Initialize `TriTierEngine` in `cmd/novabot/main.go` with live `NewOpenRouterEmbedder` and `NewOpenRouterEpisodeSummarizer`.
   - Call `memoryEngine.RecordMessage` in `internal/whatsapp/handler.go` for meaningful messages.
   - Wire `dashboardServer.Metrics().RecordEvent` and `dashboardServer.Logs().AddLog` into `handler.go` for live observability.
