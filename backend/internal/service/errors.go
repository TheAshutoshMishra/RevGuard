// Package service contains the application/orchestration layer: event
// validation, idempotent ingestion, and the RecoveryCase state machine.
// It coordinates repositories but holds no persistence details itself.
package service

import "errors"

var (
	// ErrInvalidEvent means an EventInput failed validation. Wrapped with
	// fmt.Errorf("%w: <detail>", ErrInvalidEvent) at the point of failure.
	ErrInvalidEvent = errors.New("service: invalid event")

	// ErrUnsupportedAggregate means the event's aggregate_type cannot be
	// resolved to a domain entity that recovery cases can attach to.
	ErrUnsupportedAggregate = errors.New("service: aggregate type is not supported for recovery case correlation")

	// ErrAggregateNotFound means the event's aggregate_id does not
	// resolve to an existing record.
	ErrAggregateNotFound = errors.New("service: referenced aggregate does not exist")

	// ErrMerchantMismatch means the resolved aggregate belongs to a
	// different merchant than the one claimed by the event.
	ErrMerchantMismatch = errors.New("service: aggregate does not belong to the specified merchant")

	// ErrInvalidTransition means a requested RecoveryCase status change
	// is not permitted by the state machine.
	ErrInvalidTransition = errors.New("service: invalid recovery case state transition")

	// ErrRecoveryCaseNotFound means a requested RecoveryCase does not exist.
	ErrRecoveryCaseNotFound = errors.New("service: recovery case not found")

	// ErrDiagnosisFailed means the AI service could not be reached, timed
	// out, or returned a non-2xx response. This is an analysis failure,
	// never a payment or recovery failure — see AnalyzeCase.
	ErrDiagnosisFailed = errors.New("service: AI diagnosis request failed")

	// ErrDiagnosisInvalidResponse means the AI service responded but its
	// response failed Go's own validation (malformed JSON, out-of-range
	// confidence, unknown action/category, missing required field).
	ErrDiagnosisInvalidResponse = errors.New("service: AI diagnosis response is invalid")

	// ErrRecoveryDiagnosisNotFound means a requested RecoveryDiagnosis
	// does not exist.
	ErrRecoveryDiagnosisNotFound = errors.New("service: recovery diagnosis not found")

	// ErrDiagnosisCaseMismatch means a RecoveryDiagnosis was found but
	// belongs to a different RecoveryCase than the one requested — the
	// caller passed inconsistent IDs.
	ErrDiagnosisCaseMismatch = errors.New("service: recovery diagnosis does not belong to the specified recovery case")

	// ErrRecoveryEconomicEvaluationNotFound means a requested
	// RecoveryEconomicEvaluation does not exist.
	ErrRecoveryEconomicEvaluationNotFound = errors.New("service: recovery economic evaluation not found")

	// ErrEconomicEvaluationCaseMismatch means a RecoveryEconomicEvaluation
	// was found but belongs to a different RecoveryCase than the one
	// requested.
	ErrEconomicEvaluationCaseMismatch = errors.New("service: recovery economic evaluation does not belong to the specified recovery case")

	// ErrEconomicEvaluationDiagnosisMismatch means a
	// RecoveryEconomicEvaluation was found but was computed for a
	// different RecoveryDiagnosis than the one requested.
	ErrEconomicEvaluationDiagnosisMismatch = errors.New("service: recovery economic evaluation does not belong to the specified recovery diagnosis")

	// ErrRecoveryCaseNotAnalyzed means a policy evaluation was requested
	// for a RecoveryCase that is not currently ANALYZED — either it
	// hasn't reached that state yet, or it has already moved past it
	// (e.g. already policy-evaluated with different inputs). Policy
	// evaluation only ever starts fresh from ANALYZED.
	ErrRecoveryCaseNotAnalyzed = errors.New("service: recovery case is not in ANALYZED status")

	// ErrPolicyDecisionNotFound means a requested PolicyDecision does not
	// exist.
	ErrPolicyDecisionNotFound = errors.New("service: policy decision not found")

	// ErrPolicyDecisionCaseMismatch means a PolicyDecision was found but
	// belongs to a different RecoveryCase than the one requested.
	ErrPolicyDecisionCaseMismatch = errors.New("service: policy decision does not belong to the specified recovery case")

	// ErrPolicyDecisionNotAllow means execution was requested for a
	// PolicyDecision whose Outcome is not ALLOW. Only ALLOW may execute —
	// see docs/architecture/execution-engine.md.
	ErrPolicyDecisionNotAllow = errors.New("service: policy decision is not ALLOW")

	// ErrMissingAuthorizedAction means an ALLOW PolicyDecision has no
	// AuthorizedAction set. This should be structurally impossible given
	// PolicyEngine's invariants (Milestone 5), but ExecutionEngine checks
	// it explicitly rather than trusting that invariant blindly.
	ErrMissingAuthorizedAction = errors.New("service: policy decision has no authorized action")

	// ErrActionNotExecutable means the AuthorizedAction is a recognized
	// domain.RecommendedAction but has no real execution path implemented
	// yet (Milestone 6 implements only retry_payment). The policy
	// decision is still respected — nothing is executed, and this is
	// reported as a clear, typed error rather than a fabricated result.
	ErrActionNotExecutable = errors.New("service: authorized action has no execution implementation")

	// ErrRecoveryCaseNotAllow means execution was requested for a
	// RecoveryCase that is not currently ALLOW.
	ErrRecoveryCaseNotAllow = errors.New("service: recovery case is not in ALLOW status")

	// ErrInvalidWebhookSignature means a webhook request's signature was
	// missing, malformed, or did not match the expected HMAC — including
	// when no webhook secret is configured at all (fail-closed, never
	// fail-open). No webhook payload is ever trusted or persisted when
	// this error occurs.
	ErrInvalidWebhookSignature = errors.New("service: invalid webhook signature")

	// ErrMalformedWebhookPayload means a signature-verified webhook body
	// could not be parsed into a normalized provider event.
	ErrMalformedWebhookPayload = errors.New("service: malformed webhook payload")

	// ErrRecoveryCaseNotVerifying means reconciliation was requested for
	// a RecoveryCase that is not currently VERIFYING — either it hasn't
	// reached that state yet, or financial truth has already been
	// established (SUCCESS/FAILED), or it was never executed at all.
	ErrRecoveryCaseNotVerifying = errors.New("service: recovery case is not in VERIFYING status")

	// ErrNoRecoveryActionForCase means reconciliation was requested for a
	// case that has no RecoveryAction to reconcile against.
	ErrNoRecoveryActionForCase = errors.New("service: recovery case has no recovery action to reconcile")

	// ErrNoProviderReferenceToReconcile means the RecoveryAction being
	// reconciled has no ProviderReference — there is nothing to look up
	// on the provider side (e.g. an execution that timed out before ever
	// receiving a reference from the provider).
	ErrNoProviderReferenceToReconcile = errors.New("service: recovery action has no provider reference to reconcile")

	// ErrReconciliationReferenceNotFound means the provider reports it
	// has no record of the given reference at all. Distinct from a
	// transport/timeout error, but still not treated as ambiguous — it
	// simply means reconciliation could not resolve UNKNOWN this time and
	// the case remains unresolved.
	ErrReconciliationReferenceNotFound = errors.New("service: provider reports no record of this reference")

	// ErrUnknownReconciliationProvider means a PaymentReconciler was asked
	// to reconcile a reference for a provider it does not implement.
	ErrUnknownReconciliationProvider = errors.New("service: unknown reconciliation provider")
)
