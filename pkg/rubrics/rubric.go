package rubrics

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

type (
	// Result represents the grading result structure
	Result struct {
		SubmissionID string       `json:"submissionID"`
		Timestamp    time.Time    `json:"timestamp"`
		Rubric       []RubricItem `json:"rubric"`
	}
	// RunBag is a map for passing data between evaluation steps
	RunBag map[string]any

	// RubricItem represents a single item in the grading rubric
	RubricItem struct {
		Name    string  `json:"name"`
		Note    string  `json:"note"`
		Points  float64 `json:"points"`
		Awarded float64 `json:"awarded"`
	}

	// Evaluator defines a function that evaluates a specific rubric item
	Evaluator func(context.Context, ProgramRunner, RunBag) RubricItem
)

func (r *Result) Render(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	// Configure per-column alignment: Name (left), Points (right), Awarded (right), Notes (left)
	table := tablewriter.NewTable(w, tablewriter.WithConfig(tablewriter.Config{
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{
				PerColumn: []tw.Align{tw.AlignLeft, tw.AlignRight, tw.AlignRight, tw.AlignLeft},
			},
		},
	}))
	table.Header("Name", "Points", "Awarded", "Notes")
	for _, item := range r.Rubric {
		points := fmt.Sprintf("%.2f", item.Points)
		awarded := fmt.Sprintf("%.2f", item.Awarded)
		_ = table.Append(item.Name, points, awarded, item.Note)
	}
	grade := fmt.Sprintf("%.2f%%", r.Awarded()/r.Points()*100)
	table.Footer(r.SubmissionID, "Grade:", grade)
	_ = table.Render()
}

func (r *Result) Points() float64 {
	sum := 0.0
	for _, item := range r.Rubric {
		sum += item.Points
	}
	return sum
}

func (r *Result) Awarded() float64 {
	sum := 0.0
	for _, item := range r.Rubric {
		sum += item.Awarded
	}
	return sum
}

// NewResult returns a Result prepared to collect rubric items.
func NewResult() *Result {
	return &Result{
		SubmissionID: namesgenerator.GetRandomName(0),
		Timestamp:    time.Now(),
		Rubric:       make([]RubricItem, 0),
	}
}
