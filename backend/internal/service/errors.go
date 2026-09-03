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
)
