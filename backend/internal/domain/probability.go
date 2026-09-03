package domain

import (
	"errors"
	"fmt"
)

// ProbabilityBasisPoints represents a probability as an integer number of
// basis points: 0 = 0%, 10000 = 100% (e.g. 5000 = 50%, 8000 = 80%).
//
// Probabilities are never represented as float/double in this codebase,
// for the same reason Money never is: integer arithmetic is exact and
// deterministic, so two evaluations of the same inputs always produce
// the same result, and rounding behavior is explicit rather than an
// artifact of floating-point representation.
type ProbabilityBasisPoints int32

// MaxProbabilityBasisPoints is 100%.
const MaxProbabilityBasisPoints ProbabilityBasisPoints = 10000

// ErrInvalidProbability is returned when a basis-point value falls
// outside [0, 10000].
var ErrInvalidProbability = errors.New("domain: probability basis points must be between 0 and 10000")

// NewProbabilityBasisPoints validates and returns a ProbabilityBasisPoints.
func NewProbabilityBasisPoints(bps int) (ProbabilityBasisPoints, error) {
	if bps < 0 || bps > int(MaxProbabilityBasisPoints) {
		return 0, fmt.Errorf("%w: got %d", ErrInvalidProbability, bps)
	}
	return ProbabilityBasisPoints(bps), nil
}

func (p ProbabilityBasisPoints) String() string {
	return fmt.Sprintf("%d bps", int(p))
}
