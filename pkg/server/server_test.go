package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	connectgo "github.com/bufbuild/connect-go"
	"github.com/stretchr/testify/assert"

	"github.com/jh125486/CSCE5350_gradebot/pkg/openai"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
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

func (m *mockStorage) SaveResult(ctx context.Context, submissionID string, result *pb.Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[submissionID] = result
	return nil
}

func (m *mockStorage) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, exists := m.results[submissionID]
	if !exists {
		return nil, fmt.Errorf("result not found")
	}
	return result, nil
}

func (m *mockStorage) ListResults(ctx context.Context) (map[string]*pb.Result, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to avoid concurrent map writes
	results := make(map[string]*pb.Result)
	maps.Copy(results, m.results)
	return results, nil
}

func (m *mockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (map[string]*pb.Result, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create sorted list of all submissions
	var keys []string
	for k := range m.results {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	totalCount := len(keys)

	// Calculate pagination
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 20
	}

	startIdx := (params.Page - 1) * params.PageSize
	endIdx := startIdx + params.PageSize
	if startIdx >= totalCount && totalCount > 0 {
		startIdx = totalCount - params.PageSize
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx = totalCount
	}
	if endIdx > totalCount {
		endIdx = totalCount
	}

	// Get the requested page
	results := make(map[string]*pb.Result)
	for i := startIdx; i < endIdx; i++ {
		key := keys[i]
		results[key] = m.results[key]
	}

	return results, totalCount, nil
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
	return extractClientIP(ctx, req)
}

func TestStartInvalidPort(t *testing.T) {
	cfg := Config{
		ID:           "id",
		Port:         "notaport",
		OpenAIClient: &mockReviewer{},
		Storage:      newMockStorage(),
	}
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
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
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

func TestEvaluateCodeQualityBehaviors(t *testing.T) {
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

func TestStartCancelReturnsContextErr(t *testing.T) {
	// start with a short timeout context so Start will shut down cleanly
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	cfg := Config{ID: "id", Port: "0", OpenAIClient: &mockReviewer{}}
	err := Start(ctx, cfg)
	if err == nil {
		t.Fatalf("expected Start to return an error due to cancelled context, got nil")
	}
	// ensure it's the context error
	if !errors.Is(err, ctx.Err()) {
		t.Fatalf("expected ctx.Err() (%v), got %v", ctx.Err(), err)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		peerAddr string
		expected string
	}{
		{
			name:     "RealIPFromContext",
			headers:  map[string]string{},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.100",
		},
		{
			name:     "XForwardedFor",
			headers:  map[string]string{"X-Forwarded-For": "192.168.1.101"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.101",
		},
		{
			name:     "XForwardedForMultiple",
			headers:  map[string]string{"X-Forwarded-For": "192.168.1.102, 10.0.0.1"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.102",
		},
		{
			name:     "XRealIP",
			headers:  map[string]string{"X-Real-IP": "192.168.1.103"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.103",
		},
		{
			name:     "CFConnectingIP",
			headers:  map[string]string{"CF-Connecting-IP": "192.168.1.104"},
			peerAddr: "127.0.0.1:12345",
			expected: "192.168.1.104",
		},
		{
			name:     "PeerAddrFallback",
			headers:  map[string]string{},
			peerAddr: "203.0.113.4:8080",
			expected: "203.0.113.4",
		},
		{
			name:     "UnknownFallback",
			headers:  map[string]string{},
			peerAddr: "",
			expected: unknownIP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create base context - use t.Context() or context.Background() for test setup
			var ctx context.Context
			if tt.name == "RealIPFromContext" {
				ctx = context.WithValue(t.Context(), realIPKey, "192.168.1.100")
			} else {
				ctx = t.Context()
			}

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
				result := getClientIPFromMock(ctx, mockReq)
				assert.Equal(t, tt.expected, result)
				return
			}

			if tt.name == "UnknownFallback" {
				result := getClientIP(ctx, req)
				assert.Equal(t, tt.expected, result)
				return
			}

			result := getClientIP(ctx, req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetGeoLocationWithMockClient(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected string
	}{
		{
			name:     "EmptyIP",
			ip:       "",
			expected: localUnknown,
		},
		{
			name:     "UnknownIP",
			ip:       unknownIP,
			expected: localUnknown,
		},
		{
			name:     "LocalhostIPv4",
			ip:       "127.0.0.1",
			expected: localUnknown,
		},
		{
			name:     "LocalhostIPv6",
			ip:       "::1",
			expected: localUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use mock client that never makes real HTTP calls
			client := &GeoLocationClient{
				Client: &http.Client{
					Transport: &mockRoundTripper{},
				},
			}
			result := client.Do(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGeoLocationClient(t *testing.T) {
	// Test basic functionality with mock HTTP responses - all in memory
	tests := []struct {
		name       string
		statusCode int
		body       string
		expected   string
	}{
		{
			name:       "APIError404",
			statusCode: http.StatusNotFound,
			body:       "",
			expected:   unknownLocation,
		},
		{
			name:       "APIError500",
			statusCode: http.StatusInternalServerError,
			body:       "",
			expected:   unknownLocation,
		},
		{
			name:       "InvalidJSON",
			statusCode: http.StatusOK,
			body:       "invalid json",
			expected:   unknownLocation,
		},
		{
			name:       "ValidResponse",
			statusCode: http.StatusOK,
			body:       `{"city": "Test City", "region": "Test Region", "country_name": "Test Country"}`,
			expected:   "Test City, Test Region, Test Country",
		},
		{
			name:       "PartialResponse",
			statusCode: http.StatusOK,
			body:       `{"city": "Test City", "country_name": "Test Country"}`,
			expected:   "Test City, Test Country",
		},
		{
			name:       "OnlyCountry",
			statusCode: http.StatusOK,
			body:       `{"country_name": "Test Country"}`,
			expected:   "Test Country",
		},
		{
			name:       "OnlyCity",
			statusCode: http.StatusOK,
			body:       `{"city": "Test City"}`,
			expected:   "Test City",
		},
		{
			name:       "OnlyRegion",
			statusCode: http.StatusOK,
			body:       `{"region": "Test Region"}`,
			expected:   "Test Region",
		},
		{
			name:       "EmptyResponse",
			statusCode: http.StatusOK,
			body:       `{}`,
			expected:   unknownLocation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a GeoLocationClient with mock transport - all in memory
			client := &GeoLocationClient{
				Client: &http.Client{
					Transport: &mockRoundTripper{
						response: &http.Response{
							StatusCode: tt.statusCode,
							Status:     http.StatusText(tt.statusCode),
							Header:     make(http.Header),
							Body:       io.NopCloser(strings.NewReader(tt.body)),
						},
					},
				},
			}

			// Test with a fake IP - completely mocked, no real HTTP calls
			result := client.Do("1.2.3.4")
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGeoLocationClientLocalIPs(t *testing.T) {
	client := &GeoLocationClient{
		Client: &http.Client{},
	}

	tests := []struct {
		name     string
		ip       string
		expected string
	}{
		{
			name:     "EmptyIP",
			ip:       "",
			expected: localUnknown,
		},
		{
			name:     "UnknownIP",
			ip:       unknownIP,
			expected: localUnknown,
		},
		{
			name:     "LocalhostIPv4",
			ip:       "127.0.0.1",
			expected: localUnknown,
		},
		{
			name:     "LocalhostIPv6",
			ip:       "::1",
			expected: localUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.Do(tt.ip)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockRoundTripper returns a pre-configured HTTP response without any real HTTP calls
type mockRoundTripper struct {
	response *http.Response
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// If no response is configured, return a default successful geo location response
	if m.response == nil {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"city": "Test City", "region": "Test Region", "country_name": "Test Country"}`)),
			Request:    req,
		}, nil
	}

	// Set the request on the response and return it
	m.response.Request = req
	return m.response, nil
}

func TestGeoLocationClientRequestError(t *testing.T) {
	// Test when the HTTP request itself fails
	client := &GeoLocationClient{
		Client: &http.Client{
			Transport: &errorRoundTripper{},
		},
	}

	result := client.Do("1.2.3.4")
	assert.Equal(t, unknownLocation, result)
}

// errorRoundTripper always returns an error
type errorRoundTripper struct{}

func (e *errorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network error")
}

func TestUploadRubricResult(t *testing.T) {
	mockStore := newMockStorage()
	server := newMockRubricServer(mockStore)

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

			resp, err := server.UploadRubricResult(t.Context(), req)

			if tt.expectError {
				assert.Error(t, err)
				if err != nil {
					connectErr := func() *connectgo.Error {
						target := &connectgo.Error{}
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

				// Verify the result was stored
				stored, err := mockStore.LoadResult(t.Context(), tt.submissionID)
				assert.NoError(t, err)
				assert.NotNil(t, stored)
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

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
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
			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
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
			req := httptest.NewRequest(http.MethodPost, tt.path, http.NoBody)
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
			expectedContent:    []string{"Total Submissions", "Highest Score"},
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
			expectedContent:    []string{"Total Submissions", "Highest Score", "test-123", "test-456"},
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
					GeoLocation: localUnknown,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"Total Submissions", "Highest Score"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server with mock storage
			mockStore := newMockStorage()
			for id, result := range tt.setupResults {
				err := mockStore.SaveResult(t.Context(), id, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, "/submissions", http.NoBody)
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

func TestServeSubmissionsPageErrorCases(t *testing.T) {
	tests := []struct {
		name               string
		storageError       error
		expectedStatusCode int
		expectedContent    string
	}{
		{
			name:               "StorageListError",
			storageError:       fmt.Errorf("storage connection failed"),
			expectedStatusCode: http.StatusInternalServerError,
			expectedContent:    "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a custom mock storage that returns an error for ListResultsPaginated
			mockStore := &errorMockStorage{
				listResultsPagErr: tt.storageError,
			}
			server := newMockRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, "/submissions", http.NoBody)
			rr := httptest.NewRecorder()

			// Call the handler
			serveSubmissionsPage(rr, req, server)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Check response body contains error message
			body := rr.Body.String()
			assert.Contains(t, body, tt.expectedContent)
		})
	}
}

// errorMockStorage is a mock storage that can be configured to return errors
type errorMockStorage struct {
	mockStorage
	listResultsErr    error
	listResultsPagErr error
	loadResultErr     error
}

func (m *errorMockStorage) ListResults(ctx context.Context) (map[string]*pb.Result, error) {
	if m.listResultsErr != nil {
		return nil, m.listResultsErr
	}
	return m.mockStorage.ListResults(ctx)
}

func (m *errorMockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (map[string]*pb.Result, int, error) {
	if m.listResultsPagErr != nil {
		return nil, 0, m.listResultsPagErr
	}
	return m.mockStorage.ListResultsPaginated(ctx, params)
}

func (m *errorMockStorage) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
	if m.loadResultErr != nil {
		return nil, m.loadResultErr
	}
	return m.mockStorage.LoadResult(ctx, submissionID)
}

// newMockRubricServer creates a RubricServer with mock storage and geo client for testing
func newMockRubricServer(stor storage.Storage) *RubricServer {
	server := NewRubricServer(stor)
	// Replace with mock geo client that never makes real HTTP calls
	server.geoClient = &GeoLocationClient{
		Client: &http.Client{
			Transport: &mockRoundTripper{},
		},
	}
	return server
}

func TestServeSubmissionsPageTimestampParseError(t *testing.T) {
	// Test case where timestamp parsing fails
	mockStore := newMockStorage()
	result := &pb.Result{
		SubmissionId: "test-invalid-timestamp",
		Timestamp:    "invalid-timestamp", // This will fail to parse
		Rubric: []*pb.RubricItem{
			{Name: "Item 1", Points: 10.0, Awarded: 8.0, Note: "Good"},
		},
		IpAddress:   "192.168.1.100",
		GeoLocation: "Test Location",
	}
	err := mockStore.SaveResult(context.Background(), "test-invalid-timestamp", result)
	assert.NoError(t, err)

	server := newMockRubricServer(mockStore)

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/submissions", http.NoBody)
	rr := httptest.NewRecorder()

	// Call the handler
	serveSubmissionsPage(rr, req, server)

	// Should still return OK status (handler uses fallback timestamp)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Should contain the submission data
	body := rr.Body.String()
	assert.Contains(t, body, "test-invalid-timestamp")
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
					GeoLocation: localUnknown,
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
			// Create a test server with mock storage
			mockStore := newMockStorage()
			for id, result := range tt.setupResults {
				err := mockStore.SaveResult(t.Context(), id, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, tt.urlPath, http.NoBody)
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

// TestServeSubmissionsPagePagination verifies that pagination correctly shows all submissions across pages
func TestServeSubmissionsPagePagination(t *testing.T) {
	tests := []struct {
		name               string
		totalSubmissions   int
		pageSize           int
		requestPage        int
		expectedPageCount  int
		expectedItemCount  int
		expectedHasPrev    bool
		expectedHasNext    bool
		expectedContent    []string
		notExpectedContent []string
	}{
		{
			name:               "Page1Of3_15ItemsPerPage",
			totalSubmissions:   35,
			pageSize:           15,
			requestPage:        1,
			expectedPageCount:  3,
			expectedItemCount:  15,
			expectedHasPrev:    false,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 1 of 3", "1 / 3", "Next", "Last", "page-item disabled", "« First"},
			notExpectedContent: []string{},
		},
		{
			name:               "Page2Of3_15ItemsPerPage",
			totalSubmissions:   35,
			pageSize:           15,
			requestPage:        2,
			expectedPageCount:  3,
			expectedItemCount:  15,
			expectedHasPrev:    true,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 2 of 3", "2 / 3", "First", "Previous", "Next", "Last", "hx-get", "hx-target"},
			notExpectedContent: []string{},
		},
		{
			name:               "Page3Of3_15ItemsPerPage",
			totalSubmissions:   35,
			pageSize:           15,
			requestPage:        3,
			expectedPageCount:  3,
			expectedItemCount:  5,
			expectedHasPrev:    true,
			expectedHasNext:    false,
			expectedContent:    []string{"Page 3 of 3", "3 / 3", "First", "Previous", "page-item disabled", "Next ›", "Last »"},
			notExpectedContent: []string{},
		},
		{
			name:               "525SubmissionsWithPageSize15",
			totalSubmissions:   525,
			pageSize:           15,
			requestPage:        1,
			expectedPageCount:  35,
			expectedItemCount:  15,
			expectedHasPrev:    false,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 1 of 35", "1 / 35", "525 total", "Next", "Last", "« First"},
			notExpectedContent: []string{},
		},
		{
			name:               "525SubmissionsMiddlePage",
			totalSubmissions:   525,
			pageSize:           15,
			requestPage:        18,
			expectedPageCount:  35,
			expectedItemCount:  15,
			expectedHasPrev:    true,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 18 of 35", "18 / 35", "525 total", "First", "Previous", "Next", "Last"},
			notExpectedContent: []string{},
		},
		{
			name:               "525SubmissionsLastPage",
			totalSubmissions:   525,
			pageSize:           15,
			requestPage:        35,
			expectedPageCount:  35,
			expectedItemCount:  15,
			expectedHasPrev:    true,
			expectedHasNext:    false,
			expectedContent:    []string{"Page 35 of 35", "35 / 35", "525 total", "First", "Previous", "page-item disabled", "Next ›", "Last »"},
			notExpectedContent: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test results
			mockStore := newMockStorage()
			for i := 0; i < tt.totalSubmissions; i++ {
				submissionID := fmt.Sprintf("submission-%d", i)
				result := &pb.Result{
					SubmissionId: submissionID,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 100.0, Awarded: float64(50 + i%50), Note: "Test"},
					},
					IpAddress:   fmt.Sprintf("192.168.1.%d", i%256),
					GeoLocation: "Test Location",
				}
				err := mockStore.SaveResult(t.Context(), submissionID, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create request for specific page and page size
			url := fmt.Sprintf("/submissions?page=%d&pageSize=%d", tt.requestPage, tt.pageSize)
			req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
			rr := httptest.NewRecorder()

			// Call handler
			serveSubmissionsPage(rr, req, server)

			// Verify status
			assert.Equal(t, http.StatusOK, rr.Code)

			body := rr.Body.String()

			// Verify page information is in response
			for _, expected := range tt.expectedContent {
				assert.Contains(t, body, expected, "Response should contain: %s", expected)
			}

			// Verify unwanted content is NOT in response
			for _, notExpected := range tt.notExpectedContent {
				assert.NotContains(t, body, notExpected, "Response should NOT contain: %s", notExpected)
			}

			// Count table rows: Extract tbody and count <tr> tags
			// Find tbody section
			tbodyStart := strings.Index(body, "<tbody>")
			tbodyEnd := strings.Index(body, "</tbody>")
			if tbodyStart > 0 && tbodyEnd > tbodyStart {
				tbody := body[tbodyStart:tbodyEnd]
				// Count <tr> tags in tbody (each submission row has one)
				bodyRows := strings.Count(tbody, "<tr>")
				assert.Equal(t, tt.expectedItemCount, bodyRows, "Page should have %d submissions", tt.expectedItemCount)
			} else {
				t.Fatal("Could not find tbody in response")
			}

			// Verify pagination controls exist
			assert.Contains(t, body, "<nav aria-label=\"Page navigation\"", "Should have pagination nav")
			assert.Contains(t, body, "<ul class=\"pagination", "Should have pagination list")

			// Verify HTMX attributes on pagination links
			assert.Contains(t, body, "hx-get=", "Pagination should use HTMX")
			assert.Contains(t, body, "hx-target=\"#page-container\"", "Should target page-container")
			assert.Contains(t, body, "hx-push-url=\"true\"", "Should update URL")

			// Verify total count
			assert.Contains(t, body, fmt.Sprintf("%d total", tt.totalSubmissions), "Should show total count")
		})
	}
}
