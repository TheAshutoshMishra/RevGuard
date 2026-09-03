package domain

import (
	"time"

	"github.com/google/uuid"
)

// AuditActorType identifies what kind of actor performed the audited
// decision/action. The policy engine referenced here is not implemented in
// this milestone.
type AuditActorType string

const (
	AuditActorTypeSystem       AuditActorType = "SYSTEM"
	AuditActorTypeAI           AuditActorType = "AI"
	AuditActorTypePolicyEngine AuditActorType = "POLICY_ENGINE"
	AuditActorTypeHuman        AuditActorType = "HUMAN"
	AuditActorTypeWebhook      AuditActorType = "WEBHOOK"
)

// ValidAuditActorTypes lists every actor type an AuditEvent may have.
var ValidAuditActorTypes = []AuditActorType{
	AuditActorTypeSystem,
	AuditActorTypeAI,
	AuditActorTypePolicyEngine,
	AuditActorTypeHuman,
	AuditActorTypeWebhook,
}

func (t AuditActorType) Valid() bool {
	for _, v := range ValidAuditActorTypes {
		if t == v {
			return true
		}
	}
	return false
}

// AuditEvent represents an auditable record of an important system
// decision or action taken in the course of handling a RecoveryCase.
type AuditEvent struct {
	ID             uuid.UUID
	RecoveryCaseID uuid.UUID
	EventType      string
	ActorType      AuditActorType
	ActorID        string // empty when not applicable
	Metadata       []byte // raw JSON
	CreatedAt      time.Time
}
