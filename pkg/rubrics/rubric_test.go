package rubrics_test

import (
	"bytes"
	"strings"
	"testing"

	r "github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
)

func TestResult_Awarded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		items     []r.RubricItem
		wantTotal float64
	}{
		{name: "empty", items: nil, wantTotal: 0},
		{name: "single", items: []r.RubricItem{{Name: "A", Note: "ok", Awarded: 5}}, wantTotal: 5},
		{name: "multiple", items: []r.RubricItem{{Name: "A", Note: "ok", Awarded: 3}, {Name: "B", Note: "also", Awarded: 7}}, wantTotal: 10},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := &r.Result{Rubric: tc.items}
			if got := res.Awarded(); got != tc.wantTotal {
				t.Fatalf("Awarded() = %f, want %f", got, tc.wantTotal)
			}
		})
	}
}

func TestResult_ToTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		submissionID string
		items        []r.RubricItem
	}{
		{name: "empty", submissionID: "s1", items: nil},
		{name: "single", submissionID: "s2", items: []r.RubricItem{{Name: "A", Note: "ok", Points: 5}}},
		{name: "multiple", submissionID: "s3", items: []r.RubricItem{{Name: "A", Note: "ok", Points: 3}, {Name: "B", Note: "also", Points: 7}}},
	}

	for i := range tests {
		tc := tests[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := &r.Result{SubmissionID: tc.submissionID, Rubric: tc.items}

			var buf bytes.Buffer
			res.Render(&buf)

			out := buf.String()

			for _, it := range tc.items {
				if !strings.Contains(out, it.Name) {
					t.Fatalf("output missing item name %q: %s", it.Name, out)
				}
			}
			if !strings.Contains(out, tc.submissionID) {
				t.Fatalf("output missing SubmissionID %q: %s", tc.submissionID, out)
			}
		})
	}
}

func TestResult_ToTable_NilWriter(t *testing.T) {
	t.Parallel()

	res := &r.Result{SubmissionID: "test", Rubric: []r.RubricItem{{Name: "A", Note: "ok", Points: 5, Awarded: 5}}}
	// Should not panic or error
	res.Render(nil)
}

func TestNewResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{
			name: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := r.NewResult()
			if res == nil {
				t.Fatalf("empty Result")
			}
		})
	}
}
