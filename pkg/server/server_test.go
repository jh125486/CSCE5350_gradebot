package server_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/jh125486/CSCE5350_gradebot/pkg/openai"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/server"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
) // Test constants for reducing duplication.
const (
	testToken               = "s3cr3t"
	testTokenBearer         = "Bearer " + testToken
	testTokenWrong          = "Bearer wrong"
	testSubmissionID1       = "test-123"
	testSubmissionID2       = "test-456"
	testSubmissionID3       = "test-789"
	testItem1               = "Item 1"
	testItem2               = "Item 2"
	testRubricNote1         = "Good"
	testRubricNote2         = "Excellent"
	testFilename            = "f.py"
	testFileContent         = "print(1)"
	testAuthHeaderMissing   = "missing authorization header"
	testAuthHeaderMalformed = "invalid authorization header format"
	testAuthHeaderInvalid   = "invalid token"
	testAuthOK              = "ok"
	testNoFilesProvided     = "no files provided"
	testFailedToReviewCode  = "failed to review code"
	testQualityScore        = 77
	testFeedback            = "good"
	testUploadPath          = "/rubric.RubricService/UploadRubricResult"
	testSubmissionsPath     = "/submissions"
)

// mockReviewer implements the openai.Reviewer interface for tests.
type mockReviewer struct {
	review *openai.AIReview
	err    error
}

func (m *mockReviewer) ReviewCode(ctx context.Context, instructions string, files []*pb.File) (*openai.AIReview, error) {
	return m.review, m.err
}

// mockStorage implements the storage.Storage interface for tests.
type mockStorage struct {
	results map[string]*pb.Result
	mu      sync.RWMutex
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		results: make(map[string]*pb.Result),
	}
}

func (m *mockStorage) SaveResult(ctx context.Context, result *pb.Result) error {
	if result == nil || result.SubmissionId == "" {
		return errors.New("invalid result")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.SubmissionId] = result
	return nil
}

func (m *mockStorage) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, exists := m.results[submissionID]
	if !exists {
		return nil, errors.New("result not found")
	}
	return result, nil
}

func (m *mockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	results = make(map[string]*pb.Result)
	totalCount = len(m.results)
	maps.Copy(results, m.results)
	return results, totalCount, nil
}

func (m *mockStorage) Close() error {
	return nil
} // TestStart tests the Start function with various configurations and contexts.
func TestStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		port     string
		setupCtx func() context.Context
		wantErr  bool
		checkErr func(error) bool
	}{
		{
			name: "InvalidPort",
			port: "notaport",
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
			wantErr: true,
			checkErr: func(err error) bool {
				// Any error is acceptable for invalid port
				return true
			},
		},
		{
			name: "CancelledContext",
			port: "0",
			setupCtx: func() context.Context {
				ctx, cancel := context.WithTimeout(contextlog.With(context.Background(), contextlog.DiscardLogger()), 50*time.Millisecond)
				defer cancel()
				return ctx
			},
			wantErr: true,
			checkErr: func(err error) bool {
				// Should fail due to context cancellation
				return err != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := server.Config{
				ID:           "id",
				Port:         tt.port,
				OpenAIClient: &mockReviewer{},
			}
			ctx := tt.setupCtx()
			err := server.Start(ctx, cfg)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.checkErr != nil {
					assert.True(t, tt.checkErr(err), "error check failed for error: %v", err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestAuthMiddleware tests the AuthMiddleware function with various authorization headers.
func TestAuthMiddleware(t *testing.T) {
	t.Parallel()
	handler := server.AuthMiddleware(testToken)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testAuthOK))
	}))

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantSubstr string
	}{
		{name: "MissingHeader", header: "", wantStatus: http.StatusUnauthorized, wantSubstr: testAuthHeaderMissing},
		{name: "Malformed", header: "BadToken", wantStatus: http.StatusUnauthorized, wantSubstr: testAuthHeaderMalformed},
		{name: "WrongToken", header: testTokenWrong, wantStatus: http.StatusUnauthorized, wantSubstr: testAuthHeaderInvalid},
		{name: "Correct", header: testTokenBearer, wantStatus: http.StatusOK, wantSubstr: testAuthOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
			if tt.header != "" {
				req.Header.Set("authorization", tt.header)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.wantSubstr)
		})
	}
}

// TestEvaluateCodeQualityBehaviors tests QualityServer.EvaluateCodeQuality with various scenarios.
func TestEvaluateCodeQualityBehaviors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		reviewer         *mockReviewer
		files            []*pb.File
		wantErr          bool
		wantErrSubstring string
		wantScore        int32
		wantFeedback     string
		setupCtx         func() context.Context
	}{
		{
			name:             "EmptyFiles",
			reviewer:         &mockReviewer{},
			files:            []*pb.File{},
			wantErr:          true,
			wantErrSubstring: testNoFilesProvided,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:             "ReviewerError",
			reviewer:         &mockReviewer{err: errors.New("boom")},
			files:            []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:          true,
			wantErrSubstring: testFailedToReviewCode,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "SuccessfulReview",
			reviewer:     &mockReviewer{review: &openai.AIReview{QualityScore: int32(testQualityScore), Feedback: testFeedback}},
			files:        []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:      false,
			wantScore:    int32(testQualityScore),
			wantFeedback: testFeedback,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "CancelledContext",
			reviewer:     &mockReviewer{review: &openai.AIReview{QualityScore: int32(testQualityScore), Feedback: testFeedback}},
			files:        []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:      false,
			wantScore:    int32(testQualityScore),
			wantFeedback: testFeedback,
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return contextlog.With(ctx, contextlog.DiscardLogger())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tt.setupCtx()
			qs := &server.QualityServer{OpenAIClient: tt.reviewer}
			req := connect.NewRequest(&pb.EvaluateCodeQualityRequest{Files: tt.files})
			resp, err := qs.EvaluateCodeQuality(ctx, req)

			if tt.wantErr {
				assert.Nil(t, resp)
				if assert.Error(t, err) {
					assert.Contains(t, err.Error(), tt.wantErrSubstring)
				}
				return
			}

			assert.NoError(t, err)
			if assert.NotNil(t, resp) {
				assert.EqualValues(t, tt.wantScore, resp.Msg.QualityScore)
				assert.Equal(t, tt.wantFeedback, resp.Msg.Feedback)
			}
		})
	}
}

// TestUploadRubricResult tests RubricServer.UploadRubricResult with various scenarios.
func TestUploadRubricResult(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()
	qs := server.NewRubricServer(mockStore)

	tests := []struct {
		name         string
		submissionID string
		rubric       []*pb.RubricItem
		expectError  bool
		errorCode    connect.Code
		setupCtx     func() context.Context
	}{
		{
			name:         "ValidSubmission",
			submissionID: testSubmissionID1,
			rubric: []*pb.RubricItem{
				{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
			},
			expectError: false,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "EmptySubmissionID",
			submissionID: "",
			rubric: []*pb.RubricItem{
				{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
			},
			expectError: true,
			errorCode:   connect.CodeInvalidArgument,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "EmptyRubric",
			submissionID: testSubmissionID2,
			rubric:       []*pb.RubricItem{},
			expectError:  false,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "CancelledContext",
			submissionID: testSubmissionID1,
			rubric: []*pb.RubricItem{
				{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
			},
			expectError: false,
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return contextlog.With(ctx, contextlog.DiscardLogger())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := connect.NewRequest(&pb.UploadRubricResultRequest{
				Result: &pb.Result{
					SubmissionId: tt.submissionID,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       tt.rubric,
				},
			})

			ctx := tt.setupCtx()
			resp, err := qs.UploadRubricResult(ctx, req)

			if tt.expectError {
				assert.Error(t, err)
				if err != nil {
					connectErr := func() *connect.Error {
						target := &connect.Error{}
						_ = errors.As(err, &target)
						return target
					}()
					assert.Equal(t, tt.errorCode, connectErr.Code())
				}
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, tt.submissionID, resp.Msg.SubmissionId)
				assert.Contains(t, resp.Msg.Message, "uploaded successfully")

				stored, err := mockStore.LoadResult(t.Context(), tt.submissionID)
				assert.NoError(t, err)
				assert.NotNil(t, stored)
				assert.Equal(t, tt.submissionID, stored.SubmissionId)
			}
		})
	}
}

// TestAuthRubricHandler tests AuthRubricHandler with selective authentication.
func TestAuthRubricHandler(t *testing.T) {
	t.Parallel()
	handler := server.AuthRubricHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}), testToken)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "UploadPathRequiresAuth",
			path:       testUploadPath,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "UploadPathWithValidAuth",
			path:       testUploadPath,
			authHeader: testTokenBearer,
			wantStatus: http.StatusOK,
		},
		{
			name:       "OtherPathsDontRequireAuth",
			path:       testSubmissionsPath,
			authHeader: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, tt.path, http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}
