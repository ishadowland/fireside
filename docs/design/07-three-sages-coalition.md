# Three-Sages Coalition Agent (EVA MAGI)

> **Status**: 📝 drafted (2026-07-26) — references D26 / D27 / D28 / D29
> **Intent**: MVP composite-agent design for the "三贤人" feature
> **Codebase**: not yet implemented

## 1. Overview

A Three-Sages ("三贤人") coalition is a composite agent that binds 3 existing single agents together and renders as **one** room participant. When @-mentioned, the three nodes execute a hybrid 3-round protocol (debate → converge → vote → synthesize) and emit a single conclusion message that contains the full discussion record.

The design's philosophical foundation is the **MAGI** supercomputer from *Neon Genesis Evangelion* — Dr. Akagi Naoko's personality-transplant OS, which split her psyche into three facets (Scientist, Mother, Woman) so that no single logic could dominate.

## 2. Reference

- `D26` — Three-Sages (EVA MAGI)
- `D27` — Voting threshold
- `D28` — Conflict as system foundation
- `D29` — Room global prompt (announcement)

## 3. The MAGI Metaphor

### 3.1 Three facets, one person

MAGI is the central nervous system of NERV, modeled as a tri-partite mind:

| Node | Persona | Superego/Ego/Id | Drive |
|------|---------|-----------------|-------|
| **MELCHIOR-1** | Scientist | Superego | Rationality, evidence, optimization |
| **BALTHASAR-2** | Mother | Ego | Protection, continuity, harm-reduction |
| **CASPER-3** | Woman | Id | Desire, attachment, emotional truth |

The three are not "team members" — they are **conflict-as-foundation**. Single-criterion optimization causes blind spots; mutually-incompatible viewpoints provide structural redundancy against unknown risks.

### 3.2 Biblical & psychoanalytic mirrors

- **Magi = the three wise men** of the Nativity story; here they venerate not God but humanity's attempt to create a new god (the Complement Plan).
- **Id / Ego / Superego** — MAGI's three nodes literally instantiate Freud's structural model of the psyche.

### 3.3 The failure mode (EVA *Air*)

When Rei invokes self-destruct, MELCHIOR and BALTHASAR vote Yes, CASPER votes No. The self-destruct fails. Naoko's "woman" attachment to Gendo overrode her "scientist" and "mother" logics.

**Software interpretation**: Human contradiction is uncomputable. MAGI's failure is not a bug — it is the boundary where the system makes its own impossibility visible. We do not fix this; we **document the limit**.

## 4. Roles (default prompts)

All three role prompts are user-editable per coalition. Defaults ship with the codebase and reflect the archetype described above. See `docs/requirements/02-conversation-summary.md` (Phase 7) for the full text.

| Role | Default name | Drive | Output style |
|------|--------------|-------|--------------|
| 1 | MELCHIOR (科学家) | Rationality, evidence | Asks for proof, refuses vague claims |
| 2 | BALTHASAR (母亲) | Protection, continuity | Weighs human cost, frames long view |
| 3 | CASPER (女人) | Desire, attachment | Names the unspoken, refuses to be assimilated |

User overrides are stored per-coalition, not globally. The MAGI default is the *suggestion*, not the *law*.

## 5. Protocol

```
[Room announcement] (D29)
       ↓
┌────────────────────────────────┐
│ R1 — Debate (parallel, 3 calls) │
│   Each node: own role prompt +  │
│   original @message.            │
│   Output: position + reasoning  │
└────────────────────────────────┘
       ↓
┌────────────────────────────────┐
│ R2 — Converge (parallel, 3 calls)│
│   Each node: R1 outputs + own   │
│   role prompt.                  │
│   Output: response, concession  │
│   or strengthening of stance.   │
└────────────────────────────────┘
       ↓
┌────────────────────────────────┐
│ R3 — Vote (parallel, 3 calls)   │
│   Each node: R1+R2 + own prompt.│
│   Output: explicit vote + final │
│   position. Rolls into D27.     │
└────────────────────────────────┘
       ↓
┌────────────────────────────────┐
│ Synthesize (1 call, by host    │
│   agent or designated node)     │
│   Output: final conclusion with │
│   full transcript + tally.      │
└────────────────────────────────┘
```

**Cost**: 10 LLM calls per @-mention (3+3+3+1). Latency ≈ 3× single-agent (parallel batches), not 10×.

## 6. Voting (D27)

Two thresholds:

| Mode | Required outcome | Default? |
|------|------------------|----------|
| **Normal** | 2:1 majority | ✅ default |
| **Extreme** | 3:0 unanimous | triggered |

### 6.1 Extreme escalation triggers

A round enters "extreme" mode if **any** of the following holds:

1. Any node explicitly tags its vote with `extreme: true`.
2. The @-message or any R1/R2 output contains a configurable keyword regex (e.g. `delete`, `ban`, `self-destruct`, `revoke`, `force-quit`).
3. The host/admin pre-marks the message as `critical` (e.g. via `/critical` slash-command).
4. The room is configured to **always** require unanimity (config field `rooms.require_unanimous BOOLEAN`).

### 6.2 Failed extreme round

If extreme mode is active and 3:0 is not reached, the system **does not** fall back to majority. Correct behavior:

- Suspend the conclusion.
- Emit a structured "blocked" message identifying the dissenting node(s) and their stated reasons.
- Push a Feishu/Telegram notification to the host asking for human intervention.
- Wait for the host to either: reframe the question, override with a recorded `host_override`, or cancel.

This is the EVA *Air* outcome in software: the system refuses to launder a partial consensus into a "unanimous" decision.

### 6.3 Normal majority with dissent

A 2:1 majority is **reported with the dissent**, not flattened. The synthesized conclusion includes:

- The winning majority's position.
- The dissenting node's position, attributed.
- A "dissent score" in the metadata (1 = non-trivial disagreement; 0 = consensus).

This honors D28: disagreement is information, not noise to be smoothed.

## 7. Conflict as System Foundation (D28)

Implementation rules:

- **No tie-breaking by escalation.** A 1:1:1 deadlock (one possible if we ever allow abstention) is not "broken" by a host agent or a tenth vote. It is reported as-is.
- **No synthesis smoothing.** The synthesizer (Round 4) is forbidden from "softening" a 2:1 majority. Dissent is preserved verbatim.
- **Re-rolling is not error correction.** If a coalition produces a conclusion the user dislikes, the response is *re-roll the whole 3 rounds*, not patch the synthesis.
- **Coalition stability over coalition success.** A coalition that produces frequent 2:1 splits is *more* useful than one that always 3:0s, because the former is doing the work the system is designed to do (D28).

## 8. Room Announcement (D29)

Every agent prompt is constructed as:

```
[
  room.announcement,
  coalition_member.role_prompt,
  agent.persona_prompt,
  context_window.history,
  user_message
]
```

The announcement is **always first** (highest priority, because LLMs give more weight to early system-prompt content). It is appended to MAGI role prompts untouched — the role identity is preserved.

### 8.1 Live updates

If the host edits the announcement mid-debate, ongoing rounds complete with the old announcement. The next round (whichever of R1/R2/R3 next fires) uses the new value. This is intentional: re-rolling mid-debate would erase the partial product and is more disruptive than helpful.

### 8.2 Length

≤500 characters per room. Long enough for a topic sentence + a few constraints; short enough to keep the announcement focused.

## 9. Data Model

```sql
-- agents table (extended)
ALTER TABLE agents ADD COLUMN kind TEXT NOT NULL DEFAULT 'single';
-- 'single' | 'coalition'

ALTER TABLE agents ADD COLUMN consensus_threshold TEXT NOT NULL DEFAULT 'majority';
-- 'majority' | 'unanimous' | 'majority_with_extreme_flag'

-- coalition_members table (new)
CREATE TABLE coalition_members (
  coalition_id   INT  NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  agent_id       INT  NOT NULL REFERENCES agents(id),
  role_name      TEXT NOT NULL,                    -- "MELCHIOR" / "BALTHASAR" / "CASPER" / custom
  role_prompt    TEXT NOT NULL,                    -- user-editable
  weight         REAL NOT NULL DEFAULT 1.0,        -- vote weight (Phase 2)
  position       INT  NOT NULL,                    -- 1 = first speaker, 2, 3
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (coalition_id, agent_id)
);

-- rooms table (extended)
ALTER TABLE rooms ADD COLUMN announcement TEXT NOT NULL DEFAULT '';
ALTER TABLE rooms ADD COLUMN require_unanimous BOOLEAN NOT NULL DEFAULT FALSE;

-- coalition_runs (new) — for transcript persistence
CREATE TABLE coalition_runs (
  id              BIGSERIAL PRIMARY KEY,
  coalition_id    INT NOT NULL REFERENCES agents(id),
  room_id         INT NOT NULL REFERENCES rooms(id),
  trigger_msg_id  TEXT NOT NULL,                    -- the @-message
  started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at     TIMESTAMPTZ,
  outcome         TEXT,                            -- 'concluded' | 'blocked_extreme' | 'aborted'
  transcript_json JSONB NOT NULL                   -- R1/R2/R3 + synth + votes
);

-- extreme_triggers (new) — audit log
CREATE TABLE extreme_decisions (
  run_id        BIGINT NOT NULL REFERENCES coalition_runs(id),
  triggered_by  TEXT NOT NULL,                     -- 'node_flag' | 'keyword' | 'host_critical' | 'room_config'
  triggered_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  detail        JSONB
);
```

## 10. UI Flow

### 10.1 Adding a Three-Sages agent

```
[ Add AI Agent ]
  │
  ├─ Name:        [________]
  ├─ Avatar:      [____]
  ├─ Kind:        ( ) Single agent  (•) 三贤人
  │
  └─ (collapsed when Single; expanded when 三贤人)
      │
      ├─ Node 1
      │   ├─ Agent:   [ Decision-maker ▾ ]   (existing agent from pool)
      │   ├─ Role:    [ MELCHIOR (科学家) ]
      │   └─ Prompt:  [default, editable in <textarea>]
      │
      ├─ Node 2
      │   ├─ Agent:   [ Critic ▾ ]
      │   ├─ Role:    [ BALTHASAR (母亲) ]
      │   └─ Prompt:  [default, editable]
      │
      ├─ Node 3
      │   ├─ Agent:   [ Innovator ▾ ]
      │   ├─ Role:    [ CASPER (女人) ]
      │   └─ Prompt:  [default, editable]
      │
      └─ Voting:    [ Hybrid (majority + extreme) ▾ ]
```

Default prompts are auto-populated when the user selects a role from the dropdown. The user can edit before saving.

### 10.2 Room view

```
┌─────────────────────────────────────────────────────────────┐
│  📌 [Today we're scoping Q3 EU expansion. Stay focused.]   │ ← sticky bar (D29)
└─────────────────────────────────────────────────────────────┘

[Human-host]: @三贤人  should we ship Q3 EU beta?

🔔 三贤人 正在辩论...   (3 rounds + synth)

  R1
  🗣️ MELCHIOR: Q3 is too aggressive for our infra capacity…
  🗣️ BALTHASAR: Agreed on delay, but the team is fragile…
  🗣️ CASPER:   Are we delaying because we're scared, or rational?

  R2
  🗣️ MELCHIOR: Conceding on tone, holding on timeline…
  🗣️ BALTHASAR: Same.
  🗣️ CASPER:   I hold.

  R3
  🗣️ MELCHIOR → vote: Q4 start   (extreme: false)
  🗣️ BALTHASAR → vote: Q4 start  (extreme: false)
  🗣️ CASPER → vote: Q3 start     (extreme: true   ← escalation)

  ⚠️ Extreme-mode triggered (1 node):  3:0 not reached.
  Conclusion blocked. Awaiting host decision.

  [See transcript] [Override → Q4] [Override → Q3] [Abort]
```

### 10.3 Room announcement edit

```
📌 [Today we're scoping Q3 EU expansion. Stay focused.]   ✏️

(single click on ✏️ opens inline editor, ≤500 chars, host/admin only)
```

## 11. Performance & Cost

| Metric | Estimate |
|--------|----------|
| LLM calls per @-mention | 10 (3 R1 + 3 R2 + 3 R3 + 1 Synth) |
| Wall-clock latency | ≈ 3× single-agent (parallel batches) |
| Token cost | 10× * (role_prompt + history) + 10× * full_message |
| Storage per run | ~5–50 KB transcript JSON |

### 11.1 Mitigations

- **Single-coalition-per-room gate**: a room can have at most one active three-sages run at a time. New @-mentions queue behind the running one.
- **Caller-supplied LLM**: the *outer* agent (the human's chosen model) provides compute. The Three-Sages does NOT pull from a separate model pool.
- **Transcript pruning**: pre-R1 context is summarized (existing memory-core summarizer) before being sent to R2/R3.

## 12. Failure Modes

| Failure | Cause | Response |
|---------|-------|----------|
| Node LLM timeout | Upstream provider down | Retry once with exponential backoff; on 2nd failure, abort the round and notify host |
| 3:0 extreme deadlock | D27 escalation | Block conclusion, emit structured message, notify host (D28) |
| 1:1:1 (abstention mode) | Phase 2 — possible if abstention allowed | Report as deadlock; do NOT synthesize |
| Coalition member deleted | User deletes an agent used in a coalition | Block new runs; allow existing runs to complete; mark coalition as `incomplete` |
| Announcement edit during R1 | D29 race | R1 completes with old announcement; R2+ uses new |

## 13. Phase 2 Considerations

- **Custom weights** (`coalition_members.weight`): two MACHIAVELLI agents outweigh one CASPER in close votes.
- **Per-round temperature**: BALTHASAR at 0.2 (calm), CASPER at 0.9 (erratic). Default all 0.7.
- **Abstention**: a node can vote `abstain`; counted as null. 1:1:1 with abstention = 1:1 deadlock.
- **Coalition inspector**: room-side view showing each coalition's MAGI history, dissent frequency, host override frequency.
- **Cross-coalition debates**: one 三贤人 debates another. Order-of-speaking deterministic by coalition_id.

## 14. Open Questions

1. **Should the user's @-message be passed verbatim to all 3 nodes, or with a MAGI-aware rephrase?** (Verbatim is simpler; rephrased is more on-character.)
2. **Should R1/R2/R3 outputs be visible to the room in real-time, or only after the synthesis?** (Real-time is more dramatic but reveals intermediate incoherence.)
3. **When a coalition is `kind='coalition'`, can its members be re-used in another coalition?** (Probably yes, but cap at N coalitions per agent to avoid hub-and-spoke.)
4. **Does the "Scientist" archetype dominate by default?** (Concern: if all 3 LLMs are GPT-4, MELCHIOR's "rational" outputs become CASPER's "irrational" ones — i.e. the persona is the model, not the prompt.)
5. **Should `extreme` escalation be visible to the room?** (Yes — it's information. But the dissenting node's *vote* is part of the transcript; the *escalation flag* could be styling.)

## 15. References

- D26 / D27 / D28 / D29 in [`../requirements/00-overview.md`](../requirements/00-overview.md)
- Phase 7 in [`../requirements/02-conversation-summary.md`](../requirements/02-conversation-summary.md)
- EVA reference: *Neon Genesis Evangelion* (1995–1996), MAGI episodes (ep. 17, 18, 22, 24, 25, End of Evangelion / *Air*)
- Freud structural model: *Das Ich und das Es* (1923)
- *Spiegel* interview on Evangelion, Gainax (1996), for character motivation sources
