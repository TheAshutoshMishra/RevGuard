package domain

import (
	"time"

	"github.com/google/uuid"
)

// Merchant represents the business using RevGuard.
type Merchant struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
