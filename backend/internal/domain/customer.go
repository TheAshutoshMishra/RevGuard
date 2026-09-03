package domain

import (
	"time"

	"github.com/google/uuid"
)

// Customer represents the customer whose payment/revenue event is being
// recovered. A customer always belongs to exactly one merchant.
type Customer struct {
	ID                 uuid.UUID
	MerchantID         uuid.UUID
	ExternalCustomerID string
	Email              string
	Name               string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
