package client

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
)

func TestPromptForSubmission(t *testing.T) {
	t.Parallel()

	type args struct {
		ctx context.Context
		w   io.Writer
		r   io.Reader
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "user_answers_yes",
			args: args{
				r: strings.NewReader("y\n"),
			},
			want: true,
		},
		{
			name: "user_answers_no",
			args: args{
				r: strings.NewReader("n\n"),
			},
			want: false,
		},
		{
			name: "user_answers_yes_uppercase",
			args: args{
				r: strings.NewReader("Y\n"),
			},
			want: true,
		},
		{
			name: "user_answers_invalid",
			args: args{
				r: strings.NewReader("maybe\n"),
			},
			want: false,
		},
		{
			name: "read_error_eof",
			args: args{
				r: strings.NewReader(""),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.args.ctx == nil {
				tt.args.ctx = t.Context()
			}
			tt.args.ctx = contextlog.With(tt.args.ctx, contextlog.DiscardLogger())
			// Use a bytes.Buffer to capture output
			output := new(bytes.Buffer)
			if tt.args.w == nil {
				tt.args.w = output
			}
			got := promptForSubmission(tt.args.ctx, tt.args.w, tt.args.r)

			if got != tt.want {
				t.Errorf("promptForSubmission() = %v, want %v", got, tt.want)
			}

			// Verify the prompt was written to the output
			if output.Len() == 0 {
				t.Errorf("promptForSubmission() did not write to output")
			}
		})
	}
}
