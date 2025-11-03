package storage

import (
	"context"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
)

const (
	// DefaultPageSize is the default number of results per page when not specified
	DefaultPageSize = 20
	// MaxPageSize is the maximum allowed results per page
	MaxPageSize = 100
)

// ListResultsParams holds pagination parameters for ListResults
type ListResultsParams struct {
	Page     int // 1-indexed page number
	PageSize int // Number of results per page
}

// Validate normalizes and validates pagination parameters.
// Returns a normalized copy with defaults applied and bounds enforced.
func (p ListResultsParams) Validate() ListResultsParams {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = DefaultPageSize
	} else if p.PageSize > MaxPageSize {
		p.PageSize = MaxPageSize
	}
	return p
}

// CalculatePaginationBounds computes start and end indices for pagination
func (p ListResultsParams) CalculatePaginationBounds(totalCount int) (startIdx, endIdx int) {
	startIdx = (p.Page - 1) * p.PageSize
	endIdx = startIdx + p.PageSize

	if startIdx >= totalCount && totalCount > 0 {
		startIdx = max(totalCount-p.PageSize, 0)
		endIdx = totalCount
	}
	if endIdx > totalCount {
		endIdx = totalCount
	}

	return startIdx, endIdx
}

// Storage defines the interface for persistent storage of rubric results
type Storage interface {
	SaveResult(ctx context.Context, result *proto.Result) error
	LoadResult(ctx context.Context, submissionID string) (*proto.Result, error)
	ListResultsPaginated(ctx context.Context, params ListResultsParams) (map[string]*proto.Result, int, error)
}
