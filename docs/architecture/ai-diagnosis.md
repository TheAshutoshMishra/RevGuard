# AI Diagnosis & Recommendation — Milestone 3

This document describes how a `RecoveryCase` moves from `ANALYZING` to
`ANALYZED` via the Python AI service. It covers only what Milestone 3
implements: diagnosis and recommendation. Policy decisions, economic
calculations, and payment execution are later milestones and are not
described here beyond their integration points.

**The central rule: AI recommendation ≠ authorization.** Nothing the AI
service returns causes a payment action, a policy decision, or any
durable-state mutation beyond Go persisting the recommendation itself and
performing the `ANALYZING -> ANALYZED` transition. The AI service has no
database credentials, no infrastructure credentials, and no code path that
calls out to anything beyond returning a JSON response to Go.

## Pipeline

```
RecoveryCase (ANALYZING, created by Milestone 2)
    |
    v
RecoveryContextBuilder (Go, backend/internal/service/recovery_context_builder.go)
    | reads Payment, PaymentAttempts, prior RecoveryActions
    v
AIClient.Diagnose (Go, backend/internal/service/ai_client.go)
    | HTTP POST {AI_SERVICE_URL}/v1/diagnose  -- OUTSIDE any DB transaction
    v
POST /v1/diagnose (Python, ai-service/app/main.py)
    v
DiagnosisService (Python, ai-service/app/services/diagnosis_service.py)
    v
LLMProvider.generate_diagnosis (mock or anthropic)
    v
Pydantic validation (Python) -> JSON response
    v
Go validation (validateRecommendation in ai_client.go) -- independent of Python's
    v
AnalysisOrchestrator.AnalyzeCase (Go, backend/internal/service/analysis_orchestrator.go)
    | ONE transaction: persist RecoveryDiagnosis, ANALYZING -> ANALYZED, audit
    v
RecoveryCase (ANALYZED) -- STOP. Milestone 4 resumes from here.
```

`EventProcessor.Process` (Milestone 2) calls `AnalyzeCase` automatically,
once, immediately after it creates a fresh `RecoveryCase` — but only after
that case-creation transaction has already committed. See "Transaction
boundaries" below for why the AI call happens outside any transaction, and
`docs/architecture/event-flow.md` for the Milestone 2 half of this
pipeline (event ingestion, idempotency, case creation).

## Request contract

`POST {AI_SERVICE_URL}/v1/diagnose`:

```json
{
  "case_id": "<uuid>",
  "context": {
    "recovery_case_id": "<uuid>",
    "merchant_id": "<uuid>",
    "customer_id": "<uuid>",
    "payment_id": "<uuid>",
    "amount_minor_units": 49950,
    "currency": "INR",
    "payment_status": "FAILED",
    "triggering_event_type": "payment.failed",
    "payment_attempts": [
      {"attempt_number": 1, "status": "FAILED", "failure_code": "insufficient_funds", "failure_reason": "..."}
    ],
    "previous_recovery_actions": [
      {"action_type": "RETRY_PAYMENT", "status": "FAILED", "attempt_number": 1}
    ]
  }
}
```

Built by `RecoveryContextBuilder.Build` from `domain.RecoveryCase`,
`domain.Payment` (via `PaymentRepository`), `domain.PaymentAttempt` (via
the new `PaymentAttemptRepository.ListByPaymentID`), and
`domain.RecoveryAction` (via the new
`RecoveryActionRepository.ListByRecoveryCaseID`). **Never** includes card
numbers, CVV, authentication credentials, API keys, or any other raw
payment secret — `domain.Payment` doesn't model any of those fields in the
first place, so there is nothing to leak; the context is limited to
identifiers, amounts, statuses, and attempt/action history.
`TestRecoveryContextBuilder_ExcludesSensitiveInformation` is a static
regression test that marshals the context and asserts it never contains
forbidden substrings (`card_number`, `cvv`, `api_key`, `secret`, etc.).

## Response contract

```json
{
  "case_id": "<uuid>",
  "diagnosis": {
    "reason": "...",
    "failure_category": "insufficient_funds",
    "customer_context": "...",
    "recommended_strategy": "..."
  },
  "recommendation": {
    "action": "send_payment_link",
    "reason": "...",
    "confidence": 0.75
  },
  "risk_flags": ["high_value_payment"],
  "explanation": "...",
  "provider": "mock",
  "model": "mock-rule-based-v1",
  "prompt_version": "v1",
  "generated_at": "2026-01-01T00:00:00Z"
}
```

### Controlled vocabularies

`failure_category` (7 values, mirrored exactly in
`ai-service/app/models/diagnosis.py:FailureCategory` and
`backend/internal/domain/recovery_diagnosis.go:FailureCategory`):

`transient_failure`, `insufficient_funds`, `payment_method_issue`,
`authentication_issue`, `mandate_issue`, `customer_abandonment`, `unknown`.

`recommendation.action` (6 values, mirrored in `RecommendedAction` on both
sides — deliberately a distinct type from `domain.RecoveryActionType` on
the Go side; see that file for why):

`retry_payment`, `send_payment_link`, `request_payment_method_change`,
`send_reminder`, `escalate_to_human`, `stop_recovery`.

Both are small and closed by design. Adding a value means updating both
enums, the system prompt, and the SQL `CHECK` constraint in migration
`000012` — do not add a category/action to only one side.

### Confidence semantics

`confidence` is a float in `[0.0, 1.0]`. It expresses **how confident the
AI is in its own recommendation** — nothing else. It is not, and must
never be treated as, an authorization signal:

> `confidence = 0.82` means "the AI is reasonably confident in this
> recommendation." It does **not** mean "Go is authorized to execute this
> action."

Confidence is validated twice, independently:

1. **Python**: `pydantic.Field(ge=0.0, le=1.0)` on `Recommendation.confidence`
   rejects the value before it ever leaves the AI service.
2. **Go**: `validateRecommendation` in `ai_client.go` checks the range
   again on the way in. The AI service is a trusted internal component,
   but this is still a durable-state boundary — Go never assumes a remote
   service's output is safe to persist without checking it itself.
   `validateRecommendation` also checks `action` and `failure_category`
   against the Go enums, and that required text fields
   (`reason`/`explanation`) and versioning metadata
   (`provider`/`model`/`prompt_version`/`generated_at`) are present.

## Persistence: `domain.RecoveryDiagnosis`

A new table, `recovery_diagnoses` (migration `000012`), separate from
`recovery_actions`/`recovery_outcomes`:

- `recovery_actions` represents an action RevGuard **decided to attempt**
  (a future milestone's policy engine will create these).
- `recovery_diagnoses` represents what the AI **recommended** — a
  suggestion, never a decision. Keeping these in separate tables with
  separate Go types (`domain.RecommendedAction` vs.
  `domain.RecoveryActionType`) makes it structurally impossible to
  accidentally treat a recommendation as if it had already been
  authorized.

A `RecoveryCase` can accumulate more than one `RecoveryDiagnosis` row over
time (e.g. a failed AI call followed by a successful retry — see
"Idempotency" below); rows are immutable and never updated in place.
Every row records `provider`, `model`, `prompt_version`, and
`generated_at` so a past recommendation stays fully reproducible/
attributable, without storing the raw provider response.

## Transaction boundaries

`AnalysisOrchestrator.AnalyzeCase` is deliberately two-phase:

1. **Outside any transaction**: load the case (read-only), build the
   context (read-only), call the AI service (network I/O — potentially
   seconds of latency). An HTTP call to another service has no business
   holding a PostgreSQL transaction (and its connection, and any row
   locks) open for its duration.
2. **Inside one short transaction**: persist the `RecoveryDiagnosis`,
   validate and perform the `ANALYZING -> ANALYZED` transition
   (`service.ValidateTransition`, then the same guarded
   `RecoveryCaseRepository.UpdateStatus` pattern Milestone 2 established),
   and write an `AuditEvent` (`ActorType: AI`, distinguishing this
   AI-driven transition from Milestone 2's `SYSTEM`-driven
   `DETECTED -> ANALYZING`). All three commit or roll back together.

If the AI call itself fails (phase 1), there is nothing to roll back —
no transaction was ever open for it, and the case is simply left in
`ANALYZING`.

## Failure handling

An AI failure — timeout, connection refused, HTTP 5xx, malformed JSON, or
a response that fails Go's validation — is **an analysis failure**. It is
never mistaken for:

- a payment failure,
- a recovery failure, or
- a successful recovery.

`AnalyzeCase` returns the error; the case is left in `ANALYZING`, exactly
where it started — never partially transitioned, never given a fabricated
recommendation. `EventProcessor.Process` (which calls `AnalyzeCase`
immediately after a fresh case is created) does **not** fail the whole
`POST /events` request when this happens: the event was durably ingested
and the case was durably created regardless of whether analysis succeeds,
so those two facts are reported as successful (HTTP 201) while
`AnalysisError` in the response body carries the analysis failure
separately. `case_status` in the response will read `"ANALYZING"` rather
than `"ANALYZED"` in that case — callers can tell the difference without
parsing error text.

## Retry behavior

`HTTPAIClient` retries **at most once**, and only for transport-level
failures that occur before any HTTP response is received (connection
refused, DNS failure) — never for a non-2xx response, a timeout that has
already exhausted the request's context budget, or a response that fails
to parse/validate. This is deliberately not a queue or a sophisticated
retry framework; it exists only to absorb a single flaky connection
attempt. See `HTTPAIClient.Diagnose`/`doRequest` in `ai_client.go`.

## Idempotency

Repeated analysis never creates a duplicate financial effect, because
Milestone 3 doesn't create any financial effect at all — it only produces
and persists a recommendation.

- **A case not currently `ANALYZING`** (already `ANALYZED`, or never
  reached `ANALYZING`): `AnalyzeCase` is a no-op. No AI call is made, no
  diagnosis is persisted, no transition occurs. This is the primary
  idempotency guard — see `TestAnalyzeCase_NoopWhenNotAnalyzing`.
- **A case that is `ANALYZING` and gets analyzed again** (e.g. manual
  re-analysis after a prior AI failure, once some future milestone
  reopens a case to `ANALYZING`): this produces a **new**, separate
  `RecoveryDiagnosis` row rather than overwriting the previous one. Each
  row is an immutable historical record.
- **Concurrent `AnalyzeCase` calls for the same case**: the guarded
  `UpdateStatus(ctx, id, from=ANALYZING, to=ANALYZED, ...)` (same pattern
  Milestone 2 uses for `DETECTED -> ANALYZING`) only succeeds for the
  first caller; a second caller's `RecoveryDiagnosis` is still persisted
  as a legitimate historical record, but its transition attempt is
  recognized as a no-op (case already left `ANALYZING`) rather than an
  error.

## Provider abstraction

```python
class LLMProvider(ABC):
    @property
    def name(self) -> str: ...
    @property
    def model(self) -> str: ...
    async def generate_diagnosis(self, context: RecoveryContext) -> ProviderOutput: ...
```

- **`MockProvider`** (`app/providers/mock_provider.py`) — deterministic,
  rule-based, no credentials, no network I/O. `name` is always `"mock"`
  and `model` is always `"mock-rule-based-v1"`; that combination is the
  explicit, permanent signal (recorded on every stored recommendation)
  that a diagnosis did not come from a real model. This is the default
  (`AI_PROVIDER=mock`) and what every automated test in this repository
  uses.
- **`AnthropicProvider`** (`app/providers/anthropic_provider.py`) — real
  model calls via the Anthropic Messages API over `httpx` (no vendor SDK
  dependency). Requires `ANTHROPIC_API_KEY`; `AI_PROVIDER=anthropic`
  without it fails fast at service startup rather than silently falling
  back to the mock.

Swapping providers, or adding a third, means writing one new class
satisfying `LLMProvider` and adding one branch to `build_provider()` in
`main.py` — no changes to `DiagnosisService`, the HTTP route, or anything
on the Go side.

## Model/prompt versioning

`ai-service/app/prompts/diagnosis_v1.py` holds `SYSTEM_PROMPT` and
`PROMPT_VERSION = "v1"`. Every response carries `provider`, `model`, and
`prompt_version`; Go persists all three on every `RecoveryDiagnosis` row.
Bump `PROMPT_VERSION` (and consider a new `diagnosis_v2.py`) whenever the
prompt changes meaningfully, so historical recommendations stay
attributable to the exact prompt that produced them.

## Security boundaries

- The AI service has no Postgres/Redis/Redpanda credentials and no code
  path to any infrastructure — it is a stateless HTTP service that reads
  a request and returns a response.
- `RecoveryContextBuilder` never includes payment secrets in what it sends
  (see "Request contract" above).
- API keys (`ANTHROPIC_API_KEY`) are read from environment variables only,
  never hardcoded, never logged, and never included in error messages
  (`AnthropicProvider` explicitly avoids echoing raw response bodies from
  the vendor API into its error type).
- `.env.example` contains only placeholder configuration.
- Structured logging (`main.py`'s `/v1/diagnose` handler,
  `AnalysisOrchestrator` on the Go side) records `recovery_case_id`,
  provider, model, prompt_version, latency, and success/failure — never
  secrets, never card data, and never the full prompt/context body.

## What is explicitly out of scope (by design)

- Policy decisions (confidence thresholds, retry limits, amount limits,
  human-approval thresholds) — `POLICY_CHECK` remains a state name only.
- Economic calculations (expected value, expected recovery amount).
- Payment execution — `RecommendedAction` values are never invoked against
  any payment gateway.
- Webhook/reconciliation.
- Continuing the state machine past `ANALYZED` (no `POLICY_CHECK`,
  `ALLOW`/`BLOCK`/`ESCALATE`, `EXECUTING`, `VERIFYING`).
