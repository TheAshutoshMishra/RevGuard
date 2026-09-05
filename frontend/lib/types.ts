// Mirrors backend/internal/http/recovery_cases.go's recoveryCaseSummary
// exactly — every field here is either present on the wire or undefined,
// never a placeholder.
export type RecoveryCaseSummary = {
  id: string;
  merchant_id: string;
  customer_id: string;
  payment_id: string;
  status: string;
  revenue_at_risk_minor_units: number;
  currency: string;
  created_at: string;
  updated_at: string;
  failure_category?: string;
  recommended_action?: string;
  confidence?: number;
  expected_incremental_value_minor_units?: number;
  action_cost_minor_units?: number;
  risk_cost_minor_units?: number;
  policy_decision?: string;
  authorized_action?: string;
  execution_status?: string;
  outcome_status?: string;
  recovered_amount_minor_units?: number;
};

export type RecoveryCaseListResponse = {
  cases: RecoveryCaseSummary[];
  total: number;
  limit: number;
  offset: number;
};

export type RecoveryDiagnosis = {
  id: string;
  failure_category: string;
  diagnosis_reason: string;
  recommended_action: string;
  recommendation_reason: string;
  confidence: number;
  risk_flags: string[];
  explanation: string;
  provider: string;
  model: string;
  prompt_version: string;
  generated_at: string;
};

export type EconomicEvaluation = {
  id: string;
  recovery_case_id: string;
  recovery_diagnosis_id: string;
  recommended_action: string;
  currency: string;
  revenue_at_risk_minor_units: number;
  recovery_probability_bps: number;
  expected_gross_recovery_minor_units: number;
  action_cost_minor_units: number;
  risk_cost_minor_units: number;
  expected_incremental_value_minor_units: number;
  estimator_name: string;
  estimator_version: string;
  economic_model_version: string;
  created_at: string;
};

export type PolicyDecisionDetail = {
  id: string;
  recovery_case_id: string;
  recovery_diagnosis_id: string;
  recovery_economic_evaluation_id: string;
  decision: string;
  authorized_action?: string;
  policy_version: string;
  reason_codes: string[];
  explanation: string;
  evaluated_at: string;
  created_at: string;
};

export type RecoveryOutcomeDetail = {
  status: string;
  recovered_amount_minor_units: number;
  currency: string;
  external_reference?: string;
  provider: string;
  source: string;
  observed_at: string;
};

export type RecoveryActionDetail = {
  id: string;
  action_type: string;
  status: string;
  attempt_number: number;
  provider: string;
  provider_reference?: string;
  error_code?: string;
  requested_at: string;
  executed_at?: string;
  outcome?: RecoveryOutcomeDetail;
};

export type AuditEvent = {
  id: string;
  event_type: string;
  actor_type: string;
  actor_id?: string;
  metadata?: unknown;
  created_at: string;
};

export type RecoveryCaseDetail = {
  case: RecoveryCaseSummary;
  diagnoses: RecoveryDiagnosis[];
  economic_evaluation?: EconomicEvaluation;
  policy_decision?: PolicyDecisionDetail;
  actions: RecoveryActionDetail[];
  audit_trail: AuditEvent[];
};

export type PolicyProfile = {
  key: string;
  version: string;
  minimum_confidence: number;
  max_auto_amount_minor_units: number;
  minimum_expected_incremental_value_minor_units: number;
  max_payment_attempts: number;
  max_prior_recovery_actions: number;
  auto_allowed_actions: string[];
};

export type PoliciesResponse = {
  profiles: PolicyProfile[];
  currency: string;
  note: string;
};

// Mirrors backend/internal/service/evaluation_engine.go's JSON output
// (service.EvaluationResult) exactly.
export type DatasetSummary = {
  seed: number;
  opportunities: number;
  type: string;
  revenue_at_risk_minor_units: number;
  potentially_recoverable_revenue_minor_units: number;
  currency: string;
};

export type StrategyMetrics = {
  name: string;
  revenue_recovered_minor_units: number;
  recovery_rate: number;
  recovery_cost_minor_units: number;
  risk_cost_minor_units: number;
  expected_recovery_value_minor_units: number;
  net_incremental_value_minor_units: number;
  actions_taken: number;
  actions_blocked: number;
  human_escalations: number;
  unsupported_actions: number;
  ambiguous_outcomes: number;
  unnecessary_actions: number;
  average_attempts: number;
  currency: string;
};

export type EvaluationComparison = {
  profile_name: string;
  baseline_name: string;
  incremental_recovered_revenue_minor_units: number;
  incremental_net_value_minor_units: number;
  action_reduction_percent: number;
  incremental_recovery_rate: number;
};

export type EvaluationResult = {
  dataset: DatasetSummary;
  strategies: Record<string, StrategyMetrics>;
  comparisons: Record<string, EvaluationComparison>;
  disclaimer: string;
};

export type ComponentHealth = {
  name: string;
  status: string;
  detail?: string;
  checked_at: string;
};

export type SystemHealthResponse = {
  components: ComponentHealth[];
  checked_at: string;
};
