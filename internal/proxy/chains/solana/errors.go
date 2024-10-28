package solana

import (
	"errors"
	"fmt"
)

var (
	ErrMethodNotAvailable = errors.New("method not available")
	ErrEmptyResponseBody  = errors.New("empty response body")
	ErrEmptyResponseField = errors.New("empty response field")
	ErrEmptyRequestArr    = errors.New("empty requests arr")
)

type AnalyzeError struct {
	err     error
	payload map[string]struct{} // uniq map
}

func NewAnalyzeError(err error, elem string) *AnalyzeError {
	return &AnalyzeError{
		err:     err,
		payload: map[string]struct{}{elem: {}},
	}
}
func (e *AnalyzeError) AddToPayload(elem string) {
	e.payload[elem] = struct{}{}
}
func (e *AnalyzeError) Error() string {
	return fmt.Sprintf("AnalyzeError: %v", e.payload)
}
