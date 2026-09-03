package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"revguard/backend/internal/domain"
	"revguard/backend/internal/repository"
)

// AnalysisOutcome describes what AnalyzeCase did.
type AnalysisOutcome struct {
	Case      *domain.RecoveryCase
	Diagnosis *domain.RecoveryDiagnosis
	// Analyzed is true only if this call transitioned the case to
	// ANALYZED. False means either the case was not in ANALYZING (no-op)
	// or a diagnosis was persisted but a concurrent call already
	// performed the transition first.
	Analyzed bool
}

// AnalysisOrchestrator resumes a RecoveryCase from ANALYZING: it builds
// the recovery context, calls the AI service for diagnosis, validates and
// persists the result, and transitions the case to ANALYZED. It never
// continues further — no POLICY_CHECK, no execution — those are later
// milestones.
//
// The AI call happens OUTSIDE any database transaction: an external HTTP
// call has no business holding Postgres connections/locks for its
// duration. Persistence (diagnosis + transition + audit) happens in a
// single short transaction afterward.
type AnalysisOrchestrator struct {
	pool           *pgxpool.Pool
	contextBuilder *RecoveryContextBuilder
	aiClient       AIClient
	logger         *slog.Logger
}

func NewAnalysisOrchestrator(pool *pgxpool.Pool, contextBuilder *RecoveryContextBuilder, aiClient AIClient, logger *slog.Logger) *AnalysisOrchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &AnalysisOrchestrator{pool: pool, contextBuilder: contextBuilder, aiClient: aiClient, logger: logger}
}

// AnalyzeCase resumes analysis for recoveryCaseID. It is safe to call more
// than once for the same case:
//
//   - If the case is not currently ANALYZING (already ANALYZED, or never
//     reached ANALYZING), this is a no-op: it returns the case unchanged
//     with Analyzed=false, no error, no duplicate diagnosis, no duplicate
//     transition. This is the idempotency guard for repeated analysis.
//   - If the case IS ANALYZING and the AI call fails, the case is left in
//     ANALYZING — never partially transitioned — and an error is
//     returned. An analysis failure is never treated as a payment
//     failure, a recovery failure, or a successful recovery.
//   - If the case IS ANALYZING and the AI call succeeds, a new
//     RecoveryDiagnosis row is persisted, the case moves to ANALYZED, and
//     an audit record is written, all in one transaction. Repeated
//     successful analysis (once a future milestone reopens a case to
//     ANALYZING) produces a new, separate RecoveryDiagnosis row rather
//     than overwriting the previous one — it never executes anything or
//     creates a duplicate financial effect, because this milestone
//     doesn't execute anything at all.
func (o *AnalysisOrchestrator) AnalyzeCase(ctx context.Context, recoveryCaseID uuid.UUID, triggeringEventType string) (*AnalysisOutcome, error) {
	caseRepo := repository.NewPostgresRecoveryCaseRepository(o.pool)

	recoveryCase, err := caseRepo.GetByID(ctx, recoveryCaseID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrRecoveryCaseNotFound, recoveryCaseID)
		}
		return nil, fmt.Errorf("service: load recovery case: %w", err)
	}

	if recoveryCase.Status != domain.RecoveryCaseStatusAnalyzing {
		o.logger.Info("analyze case skipped: case is not in ANALYZING",
			"recovery_case_id", recoveryCaseID, "current_state", string(recoveryCase.Status))
		return &AnalysisOutcome{Case: recoveryCase}, nil
	}

	request, err := o.contextBuilder.Build(ctx, recoveryCase, triggeringEventType)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	o.logger.Info("AI diagnosis request started", "recovery_case_id", recoveryCaseID)
	recommendation, err := o.aiClient.Diagnose(ctx, request)
	latency := time.Since(started)
	if err != nil {
		// AI failure is an analysis failure. The case stays in
		// ANALYZING — never advanced, never mistaken for a payment or
		// recovery outcome.
		o.logger.Warn("AI diagnosis request failed",
			"recovery_case_id", recoveryCaseID, "latency_ms", latency.Milliseconds(), "error", err)
		return nil, err
	}
	o.logger.Info("AI diagnosis request succeeded",
		"recovery_case_id", recoveryCaseID, "provider", recommendation.Provider,
		"model", recommendation.Model, "prompt_version", recommendation.PromptVersion,
		"latency_ms", latency.Milliseconds(), "action", recommendation.Recommendation.Action,
		"confidence", recommendation.Recommendation.Confidence)

	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful Commit

	now := time.Now().UTC()
	diagnosis := &domain.RecoveryDiagnosis{
		ID:                   uuid.New(),
		RecoveryCaseID:       recoveryCaseID,
		FailureCategory:      domain.FailureCategory(recommendation.Diagnosis.FailureCategory),
		DiagnosisReason:      recommendation.Diagnosis.Reason,
		CustomerContext:      recommendation.Diagnosis.CustomerContext,
		RecommendedStrategy:  recommendation.Diagnosis.RecommendedStrategy,
		RecommendedAction:    domain.RecommendedAction(recommendation.Recommendation.Action),
		RecommendationReason: recommendation.Recommendation.Reason,
		Confidence:           recommendation.Recommendation.Confidence,
		RiskFlags:            recommendation.RiskFlags,
		Explanation:          recommendation.Explanation,
		Provider:             recommendation.Provider,
		Model:                recommendation.Model,
		PromptVersion:        recommendation.PromptVersion,
		GeneratedAt:          recommendation.GeneratedAt,
		CreatedAt:            now,
	}

	diagnosisRepo := repository.NewPostgresRecoveryDiagnosisRepository(tx)
	if err := diagnosisRepo.Create(ctx, diagnosis); err != nil {
		return nil, fmt.Errorf("service: persist recovery diagnosis: %w", err)
	}

	txCaseRepo := repository.NewPostgresRecoveryCaseRepository(tx)
	from, to := domain.RecoveryCaseStatusAnalyzing, domain.RecoveryCaseStatusAnalyzed
	if err := ValidateTransition(from, to); err != nil {
		// The transition table is static and this edge is declared
		// there; reaching this would be a programming error, not a
		// runtime condition.
		return nil, fmt.Errorf("service: unreachable transition validation failure: %w", err)
	}
	if err := txCaseRepo.UpdateStatus(ctx, recoveryCaseID, from, to, now); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// The case moved out of ANALYZING between our read and this
			// update (e.g. a concurrent AnalyzeCase call already won).
			// The diagnosis we just built is still a valid, persisted
			// historical record — commit it, but don't force a
			// transition the state machine would reject.
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return nil, fmt.Errorf("service: commit: %w", commitErr)
			}
			o.logger.Info("recovery diagnosis persisted but case already left ANALYZING",
				"recovery_case_id", recoveryCaseID)
			return &AnalysisOutcome{Case: recoveryCase, Diagnosis: diagnosis}, nil
		}
		return nil, fmt.Errorf("service: transition recovery case: %w", err)
	}

	auditRepo := repository.NewPostgresAuditEventRepository(tx)
	if err := auditRepo.Create(ctx, &domain.AuditEvent{
		ID:             uuid.New(),
		RecoveryCaseID: recoveryCaseID,
		EventType:      "recovery_case.transitioned",
		ActorType:      domain.AuditActorTypeAI,
		Metadata: auditJSON(map[string]any{
			"from":                  string(from),
			"to":                    string(to),
			"reason":                "AI diagnosis completed",
			"recovery_diagnosis_id": diagnosis.ID,
			"recommended_action":    string(diagnosis.RecommendedAction),
			"confidence":            diagnosis.Confidence,
			"provider":              diagnosis.Provider,
			"model":                 diagnosis.Model,
		}),
		CreatedAt: now,
	}); err != nil {
		return nil, fmt.Errorf("service: audit transition: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit: %w", err)
	}

	recoveryCase.Status = to
	recoveryCase.UpdatedAt = now

	o.logger.Info("recovery case analyzed",
		"recovery_case_id", recoveryCaseID, "from_state", string(from), "to_state", string(to))

	return &AnalysisOutcome{Case: recoveryCase, Diagnosis: diagnosis, Analyzed: true}, nil
}
