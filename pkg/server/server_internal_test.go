package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

// Test constants for reducing duplication.
const (
	testToken                    = "s3cr3t"
	testTokenBearer              = "Bearer " + testToken
	testTokenWrong               = "Bearer wrong"
	testIP1                      = "127.0.0.1:12345"
	testIP2                      = "192.168.1.100"
	testIP3                      = "192.168.1.101"
	testIP4                      = "192.168.1.102"
	testIP5                      = "10.0.0.1"
	testIP6                      = "192.168.1.103"
	testIP7                      = "192.168.1.104"
	testIP8                      = "203.0.113.4:8080"
	testIP9                      = "203.0.113.4"
	testGeoIP                    = "1.2.3.4"
	testSubmissionID1            = "test-123"
	testSubmissionID2            = "test-456"
	testSubmissionID3            = "test-789"
	testItem1                    = "Item 1"
	testItem2                    = "Item 2"
	testRubricNote1              = "Good"
	testRubricNote2              = "Excellent"
	testRubricNote3              = "Very Good"
	testRubricNote4              = "No points"
	testRubricNote5              = "Good work"
	testLocation1                = "New York, NY, United States"
	testLocation2                = "Los Angeles, CA, United States"
	testUploadPath               = "/rubric.RubricService/UploadRubricResult"
	testSubmissionsPath          = "/submissions"
	testFilename                 = "f.py"
	testFileContent              = "print(1)"
	testAuthHeaderMissing        = "missing authorization header"
	testAuthHeaderMalformed      = "invalid authorization header format"
	testAuthHeaderInvalid        = "invalid token"
	testAuthOK                   = "ok"
	testNoFilesProvided          = "no files provided"
	testFailedToReviewCode       = "failed to review code"
	testQualityScore             = 77
	testFeedback                 = "good"
	testDoctype                  = "<!DOCTYPE html>"
	testResponseShouldContain    = "Response should contain: %s"
	testHTMXResponseShouldCtn    = "HTMX response should contain: %s"
	testHTMXResponseShouldNotCtn = "HTMX response should NOT contain: %s"
	testErrorResponseShouldCtn   = "Error response should contain: %s"
	testRespShouldNotContain     = "Response should NOT contain: %s"
	testHTMXContentType          = "text/html"
	testHTMXAttrGet              = "hx-get"
	testHTMXAttrTarget           = "hx-target"
	testSubmission0              = "submission-0"
	testSubmission17             = "submission-17"
	testSubmission18             = "submission-18"
	testSubmission4              = "submission-4"
	testSubmission5              = "submission-5"
	testSubmission9              = "submission-9"
	testTotalSubmissions         = "Total Submissions"
	testHighestScore             = "Highest Score"
	testInvalidSubmissionID      = "Invalid submission ID"
	testSubmissionNotFound       = "Submission not found"
	testInternalServerError      = "Internal server error"
	testStorageConnFailed        = "storage connection failed"
	testInvalidTimestamp         = "invalid-timestamp"
	testPageNavigation           = "<nav aria-label=\"Page navigation\""
	testPaginationList           = "<ul class=\"pagination"
	testPaginationUpdateURL      = "hx-push-url=\"true\""
)

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
		return fmt.Errorf("invalid result")
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
		return nil, fmt.Errorf("result not found")
	}
	return result, nil
}

func (m *mockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create sorted list of all submissions
	keys := make([]string, 0, len(m.results))
	for k := range m.results {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	totalCount = len(keys)

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
		startIdx = max(totalCount-params.PageSize, 0)
		endIdx = totalCount
	}
	if endIdx > totalCount {
		endIdx = totalCount
	}

	// Get the requested page
	results = make(map[string]*pb.Result)
	for i := startIdx; i < endIdx; i++ {
		key := keys[i]
		results[key] = m.results[key]
	}

	return results, totalCount, nil
}

func (m *mockStorage) Close() error {
	return nil
}

// mockConnectRequest wraps a connect request with mock peer info for testing
type mockConnectRequest struct {
	*connect.Request[pb.UploadRubricResultRequest]
	peerAddr string
}

func (m *mockConnectRequest) Peer() connect.Peer {
	return connect.Peer{Addr: m.peerAddr}
}

func (m *mockConnectRequest) Header() http.Header {
	return m.Request.Header()
}

// getClientIPFromMock is a test version of getClientIP that works with our mock
func getClientIPFromMock(ctx context.Context, req *mockConnectRequest) string {
	return extractClientIP(ctx, req)
}

func TestGetClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		headers  map[string]string
		peerAddr string
		expected string
	}{
		{
			name:     "RealIPFromContext",
			headers:  map[string]string{},
			peerAddr: testIP1,
			expected: testIP2,
		},
		{
			name:     "XForwardedFor",
			headers:  map[string]string{"X-Forwarded-For": testIP3},
			peerAddr: testIP1,
			expected: testIP3,
		},
		{
			name:     "XForwardedForMultiple",
			headers:  map[string]string{"X-Forwarded-For": testIP4 + ", " + testIP5},
			peerAddr: testIP1,
			expected: testIP4,
		},
		{
			name:     "XRealIP",
			headers:  map[string]string{"X-Real-IP": testIP6},
			peerAddr: testIP1,
			expected: testIP6,
		},
		{
			name:     "CFConnectingIP",
			headers:  map[string]string{"CF-Connecting-IP": testIP7},
			peerAddr: testIP1,
			expected: testIP7,
		},
		{
			name:     "PeerAddrFallback",
			headers:  map[string]string{},
			peerAddr: testIP8,
			expected: testIP9,
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
			t.Parallel()
			// Create base context - use t.Context() or context.Background() for test setup
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			if tt.name == "RealIPFromContext" {
				ctx = context.WithValue(ctx, realIPKey, "192.168.1.100")
			}

			// Create a mock request
			req := connect.NewRequest(&pb.UploadRubricResultRequest{
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

func TestRealIPMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		requestIP      string
		expectedRealIP string
	}{
		{
			name:           "ValidRemoteAddr",
			requestIP:      testIP2 + ":8080",
			expectedRealIP: testIP2,
		},
		{
			name:           "Localhost",
			requestIP:      "127.0.0.1:8080",
			expectedRealIP: "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a test handler that checks the context
			handler := realIPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				realIP := r.Context().Value(realIPKey)
				assert.Equal(t, tt.expectedRealIP, realIP)
				w.WriteHeader(http.StatusOK)
			}))
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = tt.requestIP
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
		})
	}
}

func TestRequiresAuth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "UploadRubricResultRequiresAuth",
			path:     testUploadPath,
			expected: true,
		},
		{
			name:     "OtherPathsDontRequireAuth",
			path:     testSubmissionsPath,
			expected: false,
		},
		{
			name:     "RootPath",
			path:     "/",
			expected: false,
		},
		{
			name:     "SubmissionsDetail",
			path:     testSubmissionsPath + "/123",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.path, http.NoBody)
			result := requiresAuth(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServeSubmissionsPage(t *testing.T) {
	t.Parallel()
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
			expectedContent:    []string{testTotalSubmissions, testHighestScore},
		},
		{
			name: "SingleSubmission",
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
						{Name: testItem2, Points: 5.0, Awarded: 4.0, Note: testRubricNote2},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testSubmissionID1, "80.0%", testIP2, testLocation1},
		},
		{
			name: "MultipleSubmissions",
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
				testSubmissionID2: {
					SubmissionId: testSubmissionID2,
					Timestamp:    time.Now().Add(-time.Hour).Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 20.0, Awarded: 15.0, Note: testRubricNote3},
					},
					IpAddress:   testIP5,
					GeoLocation: testLocation2,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testHighestScore, testSubmissionID1, testSubmissionID2},
		},
		{
			name: "ZeroTotalPoints",
			setupResults: map[string]*pb.Result{
				testSubmissionID3: {
					SubmissionId: testSubmissionID3,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 0.0, Awarded: 0.0, Note: testRubricNote4},
					},
					IpAddress:   "127.0.0.1",
					GeoLocation: localUnknown,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testHighestScore},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

			// Create a test server with mock storage
			mockStore := newMockStorage()
			for _, result := range tt.setupResults {
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/submissions", http.NoBody)
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

func TestServeSubmissionsPageHTMX(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		totalSubmissions   int
		page               int
		pageSize           int
		expectedStatusCode int
		expectedContent    []string
		notExpectedContent []string
	}{
		{
			name:               "HTMXFirstPage",
			totalSubmissions:   25,
			page:               1,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission0, testSubmission17, testHTMXAttrGet, testHTMXAttrTarget},
			notExpectedContent: []string{testDoctype, "<head>", "<body>", testSubmission18, testSubmission5},
		},
		{
			name:               "HTMXSecondPage",
			totalSubmissions:   25,
			page:               2,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission18, testSubmission4, testHTMXAttrGet},
			notExpectedContent: []string{testDoctype, testSubmission0, testSubmission17, testSubmission5},
		},
		{
			name:               "HTMXLastPage",
			totalSubmissions:   25,
			page:               3,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission5, testSubmission9},
			notExpectedContent: []string{testDoctype, testSubmission0, testSubmission4},
		},
		{
			name:               "HTMXEmptyResults",
			totalSubmissions:   0,
			page:               1,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			notExpectedContent: []string{testDoctype, "<head>", "<body>"},
		},
		{
			name:               "HTMXSingleResult",
			totalSubmissions:   1,
			page:               1,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission0},
			notExpectedContent: []string{testDoctype},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
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
					GeoLocation: "Test City",
				}
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request with HX-Request header
			url := fmt.Sprintf("/submissions?page=%d&pageSize=%d", tt.page, tt.pageSize)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			req.Header.Set("HX-Request", "true")
			rr := httptest.NewRecorder()

			// Call the handler
			serveSubmissionsPage(rr, req, server)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Check response body contains expected content
			body := rr.Body.String()
			for _, expected := range tt.expectedContent {
				assert.Contains(t, body, expected, "HTMX response should contain: %s", expected)
			}

			// Check response body does NOT contain full page HTML
			for _, notExpected := range tt.notExpectedContent {
				assert.NotContains(t, body, notExpected, "HTMX response should NOT contain: %s", notExpected)
			}
		})
	}
}

func TestServeSubmissionsPageErrorCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		storageError       error
		expectedStatusCode int
		expectedContent    string
	}{
		{
			name:               "StorageListError",
			storageError:       errors.New(testStorageConnFailed),
			expectedStatusCode: http.StatusInternalServerError,
			expectedContent:    testInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			// Create a custom mock storage that returns an error for ListResultsPaginated
			mockStore := &errorMockStorage{
				listResultsPagErr: tt.storageError,
			}
			server := newMockRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/submissions", http.NoBody)
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
	listResultsPagErr error
	loadResultErr     error
}

func (m *errorMockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
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
	t.Parallel()
	// Test case where timestamp parsing fails
	mockStore := newMockStorage()
	result := &pb.Result{
		SubmissionId: "test-invalid-timestamp",
		Timestamp:    testInvalidTimestamp, // This will fail to parse
		Rubric: []*pb.RubricItem{
			{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
		},
		IpAddress:   testIP2,
		GeoLocation: "Test Location",
	}
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	err := mockStore.SaveResult(ctx, result)
	assert.NoError(t, err)

	server := newMockRubricServer(mockStore)

	// Create a test request
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/submissions", http.NoBody)
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
	t.Parallel()
	tests := []struct {
		name               string
		urlPath            string
		setupResults       map[string]*pb.Result
		expectedStatusCode int
		expectedContent    []string
	}{
		{
			name:               "InvalidSubmissionID_Empty",
			urlPath:            testSubmissionsPath + "/",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusBadRequest,
			expectedContent:    []string{testInvalidSubmissionID},
		},
		{
			name:               "InvalidSubmissionID_Root",
			urlPath:            testSubmissionsPath,
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusBadRequest,
			expectedContent:    []string{testInvalidSubmissionID},
		},
		{
			name:               "SubmissionNotFound",
			urlPath:            testSubmissionsPath + "/nonexistent",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{testSubmissionNotFound},
		},
		{
			name:    "ValidSubmission",
			urlPath: testSubmissionsPath + "/" + testSubmissionID1,
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote5},
						{Name: testItem2, Points: 5.0, Awarded: 4.0, Note: testRubricNote2},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmissionID1, "80.0%", "15.0", "12.0", testItem1, testItem2, testIP2, testLocation1},
		},
		{
			name:    "ZeroTotalPoints",
			urlPath: testSubmissionsPath + "/" + testSubmissionID2,
			setupResults: map[string]*pb.Result{
				testSubmissionID2: {
					SubmissionId: testSubmissionID2,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 0.0, Awarded: 0.0, Note: testRubricNote4},
					},
					IpAddress:   "127.0.0.1",
					GeoLocation: localUnknown,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmissionID2, "0.0%", "0.0", "0.0", testItem1, testRubricNote4},
		},
		{
			name:    "EmptyRubric",
			urlPath: testSubmissionsPath + "/" + testSubmissionID3,
			setupResults: map[string]*pb.Result{
				testSubmissionID3: {
					SubmissionId: testSubmissionID3,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{},
					IpAddress:    testIP5,
					GeoLocation:  "Test Location",
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmissionID3, "0.0%", "0.0", "0.0", testIP5, "Test Location"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			// Create a test server with mock storage
			mockStore := newMockStorage()
			for _, result := range tt.setupResults {
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.urlPath, http.NoBody)
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
	t.Parallel()
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
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

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
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create request for specific page and page size
			url := fmt.Sprintf("/submissions?page=%d&pageSize=%d", tt.requestPage, tt.pageSize)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
