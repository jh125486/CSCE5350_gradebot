package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

func TestListResultsParams_Validate(t *testing.T) {
	tests := []struct {
		name     string
		input    storage.ListResultsParams
		expected storage.ListResultsParams
	}{
		{
			name:     "valid params unchanged",
			input:    storage.ListResultsParams{Page: 5, PageSize: 50},
			expected: storage.ListResultsParams{Page: 5, PageSize: 50},
		},
		{
			name:     "negative page defaults to 1",
			input:    storage.ListResultsParams{Page: -1, PageSize: 20},
			expected: storage.ListResultsParams{Page: 1, PageSize: 20},
		},
		{
			name:     "zero page defaults to 1",
			input:    storage.ListResultsParams{Page: 0, PageSize: 20},
			expected: storage.ListResultsParams{Page: 1, PageSize: 20},
		},
		{
			name:     "negative page_size defaults to DefaultPageSize",
			input:    storage.ListResultsParams{Page: 1, PageSize: -1},
			expected: storage.ListResultsParams{Page: 1, PageSize: storage.DefaultPageSize},
		},
		{
			name:     "zero page_size defaults to DefaultPageSize",
			input:    storage.ListResultsParams{Page: 1, PageSize: 0},
			expected: storage.ListResultsParams{Page: 1, PageSize: storage.DefaultPageSize},
		},
		{
			name:     "page_size exceeds max clamped to MaxPageSize",
			input:    storage.ListResultsParams{Page: 1, PageSize: 200},
			expected: storage.ListResultsParams{Page: 1, PageSize: storage.MaxPageSize},
		},
		{
			name:     "both invalid uses defaults",
			input:    storage.ListResultsParams{Page: 0, PageSize: 0},
			expected: storage.ListResultsParams{Page: 1, PageSize: storage.DefaultPageSize},
		},
		{
			name:     "page_size exactly at max unchanged",
			input:    storage.ListResultsParams{Page: 1, PageSize: storage.MaxPageSize},
			expected: storage.ListResultsParams{Page: 1, PageSize: storage.MaxPageSize},
		},
		{
			name:     "page_size one over max clamped",
			input:    storage.ListResultsParams{Page: 1, PageSize: storage.MaxPageSize + 1},
			expected: storage.ListResultsParams{Page: 1, PageSize: storage.MaxPageSize},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.Validate()
			assert.Equal(t, tt.expected, result)
		})
	}
}
