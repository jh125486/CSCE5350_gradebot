package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	connectgo "github.com/bufbuild/connect-go"
	"github.com/jh125486/CSCE5350_gradebot/pkg/openai"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/rubrics"
	"github.com/stretchr/testify/assert"
)

// mockReviewer implements the openai.Reviewer interface for tests.
type mockReviewer struct {
	review *openai.AIReview
	err    error
}

func (m *mockReviewer) ReviewCode(ctx context.Context, instructions string, files []*pb.File) (*openai.AIReview, error) {
	// tests pass a single file in these cases
	return m.review, m.err
}

// mockConnectRequest wraps a connect request with mock peer info for testing
type mockConnectRequest struct {
	*connectgo.Request[pb.UploadRubricResultRequest]
	peerAddr string
}

func (m *mockConnectRequest) Peer() connectgo.Peer {
	return connectgo.Peer{Addr: m.peerAddr}
}

func (m *mockConnectRequest) Header() http.Header {
	return m.Request.Header()
}

// getClientIPFromMock is a test version of getClientIP that works with our mock
func getClientIPFromMock(ctx context.Context, req *mockConnectRequest) string {
	// Method 1: Try to get from context (set by realIP middleware)
	if realIP, ok := ctx.Value(realIPKey).(string); ok && realIP != "" && realIP != "unknown" {
		return realIP
	}

	// Method 2: Try to extract from HTTP headers in the Connect request
	headers := req.Header()
	if xff := headers.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP from comma-separated list
		if ips := strings.Split(xff, ","); len(ips) > 0 {
			if ip := strings.TrimSpace(ips[0]); ip != "" && ip != "unknown" {
				return ip
			}
		}
	}
	if xri := headers.Get("X-Real-IP"); xri != "" && xri != "unknown" {
		return xri
	}
	if cfip := headers.Get("CF-Connecting-IP"); cfip != "" && cfip != "unknown" {
		return cfip
	}

	// Method 3: Try peer info as fallback
	peer := req.Peer()
	if peer.Addr != "" {
		if ip, _, err := net.SplitHostPort(peer.Addr); err == nil {
			return ip
		}
		return peer.Addr
	}

	return "unknown"
}

func TestStart_InvalidPort(t *testing.T) {
	cfg := Config{ID: "id", Port: "notaport", OpenAIClient: &mockReviewer{}}
	err := Start(t.Context(), cfg)
	if err == nil {
		t.Fatalf("expected error from Start with invalid port, got nil")
	}
}

func TestAuthMiddleware(t *testing.T) {
	token := "s3cr3t"
	handler := AuthMiddleware(token)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantSubstr string
	}{
		{name: "MissingHeader", header: "", wantStatus: http.StatusUnauthorized, wantSubstr: "missing authorization header"},
		{name: "Malformed", header: "BadToken", wantStatus: http.StatusUnauthorized, wantSubstr: "invalid authorization header format"},
		{name: "WrongToken", header: "Bearer wrong", wantStatus: http.StatusUnauthorized, wantSubstr: "invalid token"},
		{name: "Correct", header: "Bearer " + token, wantStatus: http.StatusOK, wantSubstr: "ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
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

func TestEvaluateCodeQuality_Behaviors(t *testing.T) {
	// Empty file list -> invalid argument error
	qs := &QualityServer{OpenAIClient: &mockReviewer{}}
	req := connectgo.NewRequest(&pb.EvaluateCodeQualityRequest{Files: []*pb.File{}})
	resp, err := qs.EvaluateCodeQuality(t.Context(), req)
	assert.Nil(t, resp)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no files provided")
	}

	// Reviewer returns error -> internal error
	qs = &QualityServer{OpenAIClient: &mockReviewer{err: errors.New("boom")}}
	req = connectgo.NewRequest(&pb.EvaluateCodeQualityRequest{Files: []*pb.File{{Name: "f.py", Content: "print(1)"}}})
	resp, err = qs.EvaluateCodeQuality(t.Context(), req)
	assert.Nil(t, resp)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "failed to review code")
	}

	// Reviewer returns a valid review -> success
	mr := &mockReviewer{review: &openai.AIReview{QualityScore: 77, Feedback: "good"}}
	qs = &QualityServer{OpenAIClient: mr}
	req = connectgo.NewRequest(&pb.EvaluateCodeQualityRequest{Files: []*pb.File{{Name: "f.py", Content: "print(1)"}}})
	resp, err = qs.EvaluateCodeQuality(t.Context(), req)
	assert.NoError(t, err)
	if assert.NotNil(t, resp) {
		assert.EqualValues(t, int32(77), resp.Msg.QualityScore)
		assert.Equal(t, "good", resp.Msg.Feedback)
	}
}

func TestStart_CancelReturnsContextErr(t *testing.T) {
	// start with a short timeout context so Start will shut down cleanly
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	cfg := Config{ID: "id", Port: "0", OpenAIClient: &mockReviewer{}}
	err := Start(ctx, cfg)
	if err == nil {
		t.Fatalf("expected Start to return an error due to cancelled context, got nil")
	}
	// ensure it's the context error
	if err != ctx.Err() {
		t.Fatalf("expected ctx.Err() (%v), got %v", ctx.Err(), err)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		headers  map[string]string
		peerAddr string
		expected string
	}{
		{
			name:     "RealIPFromContext",
			ctx:      context.WithValue(context.Background(), realIPKey, "192.168.1.100"),
			headers:  map[string]string{},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.100",
		},
		{
			name:     "XForwardedFor",
			ctx:      context.Background(),
			headers:  map[string]string{"X-Forwarded-For": "192.168.1.101"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.101",
		},
		{
			name:     "XForwardedForMultiple",
			ctx:      context.Background(),
			headers:  map[string]string{"X-Forwarded-For": "192.168.1.102, 10.0.0.1"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.102",
		},
		{
			name:     "XRealIP",
			ctx:      context.Background(),
			headers:  map[string]string{"X-Real-IP": "192.168.1.103"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.103",
		},
		{
			name:     "CFConnectingIP",
			ctx:      context.Background(),
			headers:  map[string]string{"CF-Connecting-IP": "192.168.1.104"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.104",
		},
		{
			name:     "PeerAddrFallback",
			ctx:      context.Background(),
			headers:  map[string]string{},
			peerAddr: "203.0.113.4:8080",
			expected: "203.0.113.4",
		},
		{
			name:     "UnknownFallback",
			ctx:      context.Background(),
			headers:  map[string]string{},
			peerAddr: "",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock request
			req := connectgo.NewRequest(&pb.UploadRubricResultRequest{
				Result: &pb.Result{SubmissionId: "test"},
			})

			// Set headers
			for k, v := range tt.headers {
				req.Header().Set(k, v)
			}

			// For peer testing, we need to create a request with peer info
			if tt.name == "PeerAddrFallback" {
				// Create a custom request structure to simulate peer info
				mockReq := &mockConnectRequest{
					Request:  req,
					peerAddr: tt.peerAddr,
				}
				result := getClientIPFromMock(tt.ctx, mockReq)
				assert.Equal(t, tt.expected, result)
				return
			}

			if tt.name == "UnknownFallback" {
				result := getClientIP(tt.ctx, req)
				assert.Equal(t, tt.expected, result)
				return
			}

			result := getClientIP(tt.ctx, req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetGeoLocation(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected string
	}{
		{
			name:     "EmptyIP",
			ip:       "",
			expected: "Local/Unknown",
		},
		{
			name:     "UnknownIP",
			ip:       "unknown",
			expected: "Local/Unknown",
		},
		{
			name:     "LocalhostIPv4",
			ip:       "127.0.0.1",
			expected: "Local/Unknown",
		},
		{
			name:     "LocalhostIPv6",
			ip:       "::1",
			expected: "Local/Unknown",
		},
		{
			name:     "ValidIP",
			ip:       "8.8.8.8",
			expected: "Mountain View, California, United States", // Real geo data for 8.8.8.8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getGeoLocation(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertRubricsResultToProto(t *testing.T) {
	rubricResult := &rubrics.Result{
		SubmissionID: "test-submission",
		Timestamp:    time.Now(),
		Rubric: []rubrics.RubricItem{
			{Name: "Test Item 1", Points: 10.0, Awarded: 8.0, Note: "Good work"},
			{Name: "Test Item 2", Points: 5.0, Awarded: 3.0, Note: "Needs improvement"},
		},
	}

	result := ConvertRubricsResultToProto(rubricResult)

	assert.Equal(t, "test-submission", result.SubmissionId)
	assert.Equal(t, "unknown", result.IpAddress)
	assert.Equal(t, "Unknown", result.GeoLocation)
	assert.Len(t, result.Rubric, 2)

	assert.Equal(t, "Test Item 1", result.Rubric[0].Name)
	assert.Equal(t, float64(10.0), result.Rubric[0].Points)
	assert.Equal(t, float64(8.0), result.Rubric[0].Awarded)
	assert.Equal(t, "Good work", result.Rubric[0].Note)

	assert.Equal(t, "Test Item 2", result.Rubric[1].Name)
	assert.Equal(t, float64(5.0), result.Rubric[1].Points)
	assert.Equal(t, float64(3.0), result.Rubric[1].Awarded)
	assert.Equal(t, "Needs improvement", result.Rubric[1].Note)
}

func TestUploadRubricResult(t *testing.T) {
	server := NewRubricServer()

	tests := []struct {
		name         string
		submissionID string
		rubric       []*pb.RubricItem
		expectError  bool
		errorCode    connectgo.Code
	}{
		{
			name:         "ValidSubmission",
			submissionID: "test-123",
			rubric: []*pb.RubricItem{
				{Name: "Item 1", Points: 10.0, Awarded: 8.0, Note: "Good"},
			},
			expectError: false,
		},
		{
			name:         "EmptySubmissionID",
			submissionID: "",
			rubric: []*pb.RubricItem{
				{Name: "Item 1", Points: 10.0, Awarded: 8.0, Note: "Good"},
			},
			expectError: true,
			errorCode:   connectgo.CodeInvalidArgument,
		},
		{
			name:         "EmptyRubric",
			submissionID: "test-456",
			rubric:       []*pb.RubricItem{},
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := connectgo.NewRequest(&pb.UploadRubricResultRequest{
				Result: &pb.Result{
					SubmissionId: tt.submissionID,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       tt.rubric,
				},
			})

			resp, err := server.UploadRubricResult(context.Background(), req)

			if tt.expectError {
				assert.Error(t, err)
				if err != nil {
					connectErr := err.(*connectgo.Error)
					assert.Equal(t, tt.errorCode, connectErr.Code())
				}
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, tt.submissionID, resp.Msg.SubmissionId)
				assert.Contains(t, resp.Msg.Message, "uploaded successfully")

				// Verify the result was stored
				server.mu.RLock()
				stored, exists := server.results[tt.submissionID]
				server.mu.RUnlock()
				assert.True(t, exists)
				assert.Equal(t, tt.submissionID, stored.SubmissionId)
			}
		})
	}
}

func TestRealIPMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		requestIP      string
		expectedRealIP string
	}{
		{
			name:           "ValidRemoteAddr",
			requestIP:      "192.168.1.100:8080",
			expectedRealIP: "192.168.1.100",
		},
		{
			name:           "Localhost",
			requestIP:      "127.0.0.1:8080",
			expectedRealIP: "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler that checks the context
			handler := realIPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				realIP := r.Context().Value(realIPKey)
				assert.Equal(t, tt.expectedRealIP, realIP)
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.requestIP
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
		})
	}
}

func TestRequiresAuth(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "UploadRubricResultRequiresAuth",
			path:     "/rubric.RubricService/UploadRubricResult",
			expected: true,
		},
		{
			name:     "OtherPathsDontRequireAuth",
			path:     "/submissions",
			expected: false,
		},
		{
			name:     "RootPath",
			path:     "/",
			expected: false,
		},
		{
			name:     "SubmissionsDetail",
			path:     "/submissions/123",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			result := requiresAuth(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthRubricHandler(t *testing.T) {
	token := "test-token"
	handler := AuthRubricHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}), token)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "UploadPathRequiresAuth",
			path:       "/rubric.RubricService/UploadRubricResult",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "UploadPathWithValidAuth",
			path:       "/rubric.RubricService/UploadRubricResult",
			authHeader: "Bearer " + token,
			wantStatus: http.StatusOK,
		},
		{
			name:       "OtherPathsDontRequireAuth",
			path:       "/submissions",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

func TestServeSubmissionsPage(t *testing.T) {
	tests := []struct {
		name               string
		setupResults       map[string]*pb.Result
		expectedStatusCode int
		expectedContent    []string
	}{
		{
			name:               "EmptyResults",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"Total Submissions", "Average Score", "Highest Score"},
		},
		{
			name: "SingleSubmission",
			setupResults: map[string]*pb.Result{
				"test-123": {
					SubmissionId: "test-123",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 10.0, Awarded: 8.0, Note: "Good"},
						{Name: "Item 2", Points: 5.0, Awarded: 4.0, Note: "Excellent"},
					},
					IpAddress:   "192.168.1.100",
					GeoLocation: "New York, NY, United States",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"Total Submissions", "test-123", "80.0%", "192.168.1.100", "New York, NY, United States"},
		},
		{
			name: "MultipleSubmissions",
			setupResults: map[string]*pb.Result{
				"test-123": {
					SubmissionId: "test-123",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 10.0, Awarded: 8.0, Note: "Good"},
					},
					IpAddress:   "192.168.1.100",
					GeoLocation: "New York, NY, United States",
				},
				"test-456": {
					SubmissionId: "test-456",
					Timestamp:    time.Now().Add(-time.Hour).Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 20.0, Awarded: 15.0, Note: "Very Good"},
					},
					IpAddress:   "10.0.0.1",
					GeoLocation: "Los Angeles, CA, United States",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"Total Submissions", "Average Score", "Highest Score", "test-123", "test-456"},
		},
		{
			name: "ZeroTotalPoints",
			setupResults: map[string]*pb.Result{
				"test-789": {
					SubmissionId: "test-789",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 0.0, Awarded: 0.0, Note: "No points"},
					},
					IpAddress:   "127.0.0.1",
					GeoLocation: "Local/Unknown",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"Total Submissions", "Average Score", "Highest Score"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server with the setup results
			server := NewRubricServer()
			server.results = tt.setupResults

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, "/submissions", nil)
			rr := httptest.NewRecorder()

			// Call the handler
			serveSubmissionsPage(rr, req, server)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Check content type
			assert.Equal(t, "text/html", rr.Header().Get("Content-Type"))

			// Check response body contains expected content
			body := rr.Body.String()
			for _, expected := range tt.expectedContent {
				assert.Contains(t, body, expected, "Response should contain: %s", expected)
			}

			// Verify it's valid HTML (contains basic HTML structure)
			assert.Contains(t, body, "<!DOCTYPE html>")
		})
	}
}

func TestServeSubmissionDetailPage(t *testing.T) {
	tests := []struct {
		name               string
		urlPath            string
		setupResults       map[string]*pb.Result
		expectedStatusCode int
		expectedContent    []string
	}{
		{
			name:               "InvalidSubmissionID_Empty",
			urlPath:            "/submissions/",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusBadRequest,
			expectedContent:    []string{"Invalid submission ID"},
		},
		{
			name:               "InvalidSubmissionID_Root",
			urlPath:            "/submissions",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusBadRequest,
			expectedContent:    []string{"Invalid submission ID"},
		},
		{
			name:               "SubmissionNotFound",
			urlPath:            "/submissions/nonexistent",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{"Submission not found"},
		},
		{
			name:    "ValidSubmission",
			urlPath: "/submissions/test-123",
			setupResults: map[string]*pb.Result{
				"test-123": {
					SubmissionId: "test-123",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 10.0, Awarded: 8.0, Note: "Good work"},
						{Name: "Item 2", Points: 5.0, Awarded: 4.0, Note: "Excellent"},
					},
					IpAddress:   "192.168.1.100",
					GeoLocation: "New York, NY, United States",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"test-123", "80.0%", "15.0", "12.0", "Item 1", "Item 2", "192.168.1.100", "New York, NY, United States"},
		},
		{
			name:    "ZeroTotalPoints",
			urlPath: "/submissions/test-456",
			setupResults: map[string]*pb.Result{
				"test-456": {
					SubmissionId: "test-456",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 0.0, Awarded: 0.0, Note: "No points"},
					},
					IpAddress:   "127.0.0.1",
					GeoLocation: "Local/Unknown",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"test-456", "0.0%", "0.0", "0.0", "Item 1", "No points"},
		},
		{
			name:    "EmptyRubric",
			urlPath: "/submissions/test-789",
			setupResults: map[string]*pb.Result{
				"test-789": {
					SubmissionId: "test-789",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{},
					IpAddress:    "10.0.0.1",
					GeoLocation:  "Test Location",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"test-789", "0.0%", "0.0", "0.0", "10.0.0.1", "Test Location"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server with the setup results
			server := NewRubricServer()
			server.results = tt.setupResults

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, tt.urlPath, nil)
			rr := httptest.NewRecorder()

			// Call the handler
			serveSubmissionDetailPage(rr, req, server)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			if tt.expectedStatusCode == http.StatusOK {
				// Check content type
				assert.Equal(t, "text/html", rr.Header().Get("Content-Type"))

				// Check response body contains expected content
				body := rr.Body.String()
				for _, expected := range tt.expectedContent {
					assert.Contains(t, body, expected, "Response should contain: %s", expected)
				}

				// Verify it's valid HTML
				assert.Contains(t, body, "<!DOCTYPE html>")
			} else {
				// For error cases, check the error message
				body := rr.Body.String()
				for _, expected := range tt.expectedContent {
					assert.Contains(t, body, expected, "Error response should contain: %s", expected)
				}
			}
		})
	}
}
