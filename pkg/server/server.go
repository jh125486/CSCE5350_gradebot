package server

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bufbuild/connect-go"
	"github.com/tomasen/realip"

	"github.com/jh125486/CSCE5350_gradebot/pkg/openai"
	pb "github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

const (
	submissionsTmplFile    = "submissions.go.tmpl"
	submissionTmplFile     = "submission.go.tmpl"
	unknownIP              = "unknown"
	unknownLocation        = "Unknown"
	localUnknown           = "Local/Unknown"
	contentTypeHeader      = "Content-Type"
	templateExecErrMsg     = "template execution error"
	htmlContentType        = "text/html"
	xForwardedForHeader    = "X-Forwarded-For"
	xRealIPHeader          = "X-Real-IP"
	cfConnectingIPHeader   = "CF-Connecting-IP"
)

var (
	//go:embed templates
	templatesFS embed.FS
)

// IPExtractable interface for types that can provide IP extraction methods
type IPExtractable interface {
	Header() http.Header
	Peer() connect.Peer
}

// extractClientIP extracts client IP from any object that implements IPExtractable
func extractClientIP(ctx context.Context, req IPExtractable) string {
	// Method 1: Try to get from context (set by realIP middleware)
	if realIP, ok := ctx.Value(realIPKey).(string); ok && realIP != "" && realIP != unknownIP {
		return realIP
	}

	// Method 2: Try to extract from HTTP headers
	if ip := extractFromXForwardedFor(req); ip != unknownIP {
		return ip
	}

	if ip := extractFromXRealIP(req); ip != unknownIP {
		return ip
	}

	if ip := extractFromCFConnectingIP(req); ip != unknownIP {
		return ip
	}

	// Method 3: Try peer info as fallback
	return extractFromPeer(req)
}

// extractFromXForwardedFor extracts IP from X-Forwarded-For header
func extractFromXForwardedFor(req IPExtractable) string {
	headers := req.Header()
	if xff := headers.Get(xForwardedForHeader); xff != "" {
		if ips := strings.Split(xff, ","); len(ips) > 0 {
			if ip := strings.TrimSpace(ips[0]); ip != "" && ip != unknownIP {
				return ip
			}
		}
	}
	return unknownIP
}

// extractFromXRealIP extracts IP from X-Real-IP header
func extractFromXRealIP(req IPExtractable) string {
	headers := req.Header()
	if xri := headers.Get(xRealIPHeader); xri != "" && xri != unknownIP {
		return xri
	}
	return unknownIP
}

// extractFromCFConnectingIP extracts IP from CF-Connecting-IP header
func extractFromCFConnectingIP(req IPExtractable) string {
	headers := req.Header()
	if cfip := headers.Get(cfConnectingIPHeader); cfip != "" && cfip != unknownIP {
		return cfip
	}
	return unknownIP
}

// extractFromPeer extracts IP from peer address
func extractFromPeer(req IPExtractable) string {
	peer := req.Peer()
	if peer.Addr != "" {
		if ip, _, err := net.SplitHostPort(peer.Addr); err == nil {
			return ip
		}
		return peer.Addr
	}
	return unknownIP
}

// TemplateManager manages HTML templates
type TemplateManager struct {
	submissionsTmpl *template.Template
	submissionTmpl  *template.Template
}

// NewTemplateManager creates a new template manager with loaded templates
func NewTemplateManager() *TemplateManager {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}

	return &TemplateManager{
		submissionsTmpl: template.Must(template.New(submissionsTmplFile).Funcs(funcMap).ParseFS(templatesFS, filepath.Join("templates", submissionsTmplFile))),
		submissionTmpl:  template.Must(template.New(submissionTmplFile).Funcs(funcMap).ParseFS(templatesFS, filepath.Join("templates", submissionTmplFile))),
	}
}

type SubmissionData struct {
	SubmissionID  string
	Timestamp     time.Time
	TotalPoints   float64
	AwardedPoints float64
	Score         float64
	IPAddress     string
	GeoLocation   string
}

type RubricItemData struct {
	Name    string
	Points  float64
	Awarded float64
	Note    string
}

type SubmissionsPageData struct {
	TotalSubmissions int
	HighScore        float64
	Submissions      []SubmissionData
	CurrentPage      int
	TotalPages       int
	PageSize         int
	HasPrevPage      bool
	HasNextPage      bool
}

// GradingServer represents a grading server
type (
	Config struct {
		ID           string
		Port         string
		OpenAIClient openai.Reviewer
		Storage      storage.Storage
	}
	GradingServer struct {
		ID string
	}
)

type QualityServer struct {
	protoconnect.UnimplementedQualityServiceHandler
	OpenAIClient openai.Reviewer
}

// Start handles the server mode logic
func Start(ctx context.Context, cfg Config) error {
	log.Printf("Server will start on port %s", cfg.Port)
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", ":"+cfg.Port)
	if err != nil {
		return err
	}

	// Build Connect handlers for both services
	qualityPath, qualityHandler := protoconnect.NewQualityServiceHandler(&QualityServer{
		OpenAIClient: cfg.OpenAIClient,
	})

	rubricServer := NewRubricServer(cfg.Storage)
	rubricPath, rubricHandler := protoconnect.NewRubricServiceHandler(rubricServer)

	// Create a multiplexer to handle both services
	mux := http.NewServeMux()

	// Wrap handlers with realip middleware to extract real client IPs
	mux.Handle(qualityPath, realIPMiddleware(AuthMiddleware(cfg.ID)(qualityHandler)))
	mux.Handle(rubricPath, realIPMiddleware(AuthMiddleware(cfg.ID)(rubricHandler)))

	// Serve embedded HTML page (no auth required)
	mux.Handle("/submissions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveSubmissionsPage(w, r, rubricServer)
	}))

	// Serve individual submission details (no auth required)
	mux.Handle("/submissions/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveSubmissionDetailPage(w, r, rubricServer)
	}))

	// Health check endpoint for Koyeb and monitoring
	mux.Handle("/health", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
	}))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
	}
	slog.Info("Connect HTTP server listening", "port", cfg.Port)

	// Run Serve in goroutine so we can respond to ctx cancellation and
	// attempt graceful shutdown.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(lis)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		// attempt graceful shutdown with timeout
		// Use a fresh context for shutdown since the original is already cancelled
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return ctx.Err()
	}
}

// parseSubmissionsFromResults converts storage results to submission data
func parseSubmissionsFromResults(results map[string]*pb.Result) ([]SubmissionData, float64) {
	submissions := make([]SubmissionData, 0, len(results))
	var highScore float64

	for _, result := range results {
		totalPoints := 0.0
		awardedPoints := 0.0

		for _, item := range result.Rubric {
			totalPoints += item.Points
			awardedPoints += item.Awarded
		}

		score := 0.0
		if totalPoints > 0 {
			score = (awardedPoints / totalPoints) * 100
		}

		timestamp, err := time.Parse(time.RFC3339, result.Timestamp)
		if err != nil {
			slog.Error("Failed to parse timestamp", "error", err, "timestamp", result.Timestamp)
			timestamp = time.Now()
		}

		submissions = append(submissions, SubmissionData{
			SubmissionID:  result.SubmissionId,
			Timestamp:     timestamp,
			TotalPoints:   totalPoints,
			AwardedPoints: awardedPoints,
			Score:         score,
			IPAddress:     result.IpAddress,
			GeoLocation:   result.GeoLocation,
		})
		if score > highScore {
			highScore = score
		}
	}

	sort.Slice(submissions, func(i, j int) bool {
		return submissions[i].SubmissionID < submissions[j].SubmissionID
	})

	return submissions, highScore
}

// getPaginationParams extracts and validates pagination parameters from the request
func getPaginationParams(r *http.Request) (int, int) {
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")

	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	pageSize := 20 // Default page size
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	return page, pageSize
}

// executeTableContent renders just the table content for HTMX partial updates
func executeTableContent(w http.ResponseWriter, data *SubmissionsPageData) error {
	// Write table wrapper with Bootstrap classes
	fmt.Fprint(w, "<div class=\"table-responsive\">\n<table id=\"submissions-table\" class=\"table table-hover\">\n<thead>\n<tr>\n")
	fmt.Fprint(w, "<th>Submission ID</th>\n<th>Timestamp</th>\n<th>Total Points</th>\n<th>Awarded Points</th>\n<th>Score</th>\n<th>IP Address</th>\n<th>Location</th>\n")
	fmt.Fprint(w, "</tr>\n</thead>\n<tbody>\n")

	// Write table rows
	for _, sub := range data.Submissions {
		scoreClass := "score-low"
		if sub.Score >= 80.0 {
			scoreClass = "score-high"
		} else if sub.Score >= 60.0 {
			scoreClass = "score-medium"
		}

		fmt.Fprintf(w, "<tr>\n")
		fmt.Fprintf(w, "<td><a href=\"/submissions/%s\" class=\"submission-link\"><code class=\"submission-id\">%s</code> 🔗</a></td>\n", sub.SubmissionID, sub.SubmissionID)
		fmt.Fprintf(w, "<td><small>%s</small></td>\n", sub.Timestamp.Format("2006-01-02 15:04"))
		fmt.Fprintf(w, "<td>%.1f</td>\n", sub.TotalPoints)
		fmt.Fprintf(w, "<td>%.1f</td>\n", sub.AwardedPoints)
		fmt.Fprintf(w, "<td><span class=\"score-badge %s\">%.1f%%</span></td>\n", scoreClass, sub.Score)
		fmt.Fprintf(w, "<td><code class=\"submission-id\">%s</code></td>\n", sub.IPAddress)
		fmt.Fprintf(w, "<td><small>%s</small></td>\n", sub.GeoLocation)
		fmt.Fprintf(w, "</tr>\n")
	}

	fmt.Fprint(w, "</tbody>\n</table>\n</div>\n")

	// Write pagination controls
	if data.TotalPages > 1 {
		fmt.Fprint(w, "<nav aria-label=\"Page navigation\" class=\"mt-4\">\n<ul class=\"pagination justify-content-center\">\n")

		// Previous buttons
		if data.HasPrevPage {
			fmt.Fprintf(w, "<li class=\"page-item\"><a class=\"page-link\" hx-get=\"?page=1&pageSize=%d\" hx-target=\"#page-container\" hx-swap=\"innerHTML\" hx-push-url=\"true\" href=\"#\">« First</a></li>\n", data.PageSize)
			fmt.Fprintf(w, "<li class=\"page-item\"><a class=\"page-link\" hx-get=\"?page=%d&pageSize=%d\" hx-target=\"#page-container\" hx-swap=\"innerHTML\" hx-push-url=\"true\" href=\"#\">‹ Previous</a></li>\n", data.CurrentPage-1, data.PageSize)
		} else {
			fmt.Fprint(w, "<li class=\"page-item disabled\"><span class=\"page-link\">« First</span></li>\n")
			fmt.Fprint(w, "<li class=\"page-item disabled\"><span class=\"page-link\">‹ Previous</span></li>\n")
		}

		// Current page info
		fmt.Fprintf(w, "<li class=\"page-item active\"><span class=\"page-link\">%d / %d</span></li>\n", data.CurrentPage, data.TotalPages)

		// Next buttons
		if data.HasNextPage {
			fmt.Fprintf(w, "<li class=\"page-item\"><a class=\"page-link\" hx-get=\"?page=%d&pageSize=%d\" hx-target=\"#page-container\" hx-swap=\"innerHTML\" hx-push-url=\"true\" href=\"#\">Next ›</a></li>\n", data.CurrentPage+1, data.PageSize)
			fmt.Fprintf(w, "<li class=\"page-item\"><a class=\"page-link\" hx-get=\"?page=%d&pageSize=%d\" hx-target=\"#page-container\" hx-swap=\"innerHTML\" hx-push-url=\"true\" href=\"#\">Last »</a></li>\n", data.TotalPages, data.PageSize)
		} else {
			fmt.Fprint(w, "<li class=\"page-item disabled\"><span class=\"page-link\">Next ›</span></li>\n")
			fmt.Fprint(w, "<li class=\"page-item disabled\"><span class=\"page-link\">Last »</span></li>\n")
		}

		fmt.Fprintf(w, "</ul>\n</nav>\n<div class=\"text-center text-muted small\"><p>Page %d of %d • %d total submissions</p></div>\n", data.CurrentPage, data.TotalPages, data.TotalSubmissions)
	}

	return nil
}

// serveSubmissionsPage serves the HTML page for viewing submissions
func serveSubmissionsPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer) {
	ctx := r.Context()

	// Get pagination parameters
	page, pageSize := getPaginationParams(r)

	w.Header().Set(contentTypeHeader, htmlContentType)

	// Fetch paginated results from storage
	results, totalCount, err := rubricServer.storage.ListResultsPaginated(ctx, storage.ListResultsParams{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		slog.Error("Failed to list paginated results from storage", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Parse submissions and calculate scores
	submissions, highScore := parseSubmissionsFromResults(results)

	// Calculate pagination with actual total count from storage
	totalPages := (totalCount + pageSize - 1) / pageSize
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}

	data := SubmissionsPageData{
		TotalSubmissions: totalCount,
		HighScore:        highScore,
		Submissions:      submissions,
		CurrentPage:      page,
		TotalPages:       totalPages,
		PageSize:         pageSize,
		HasPrevPage:      page > 1,
		HasNextPage:      page < totalPages,
	}

	slog.Info("Serving submissions page",
		"total_submissions", totalCount,
		"page", page,
		"total_pages", totalPages,
		"page_submissions", len(submissions))

	// Check if this is an HTMX request (partial update)
	if r.Header.Get("HX-Request") == "true" {
		// Return only table content for HTMX
		w.Header().Set(contentTypeHeader, htmlContentType)
		if err := executeTableContent(w, &data); err != nil {
			slog.Error("Failed to execute table content", "error", err)
			http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
			return
		}
		return
	}

	// Execute full template for initial page load
	if err := rubricServer.templates.submissionsTmpl.Execute(w, data); err != nil {
		slog.Error(templateExecErrMsg, "error", err)
		http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
		return
	}
}

// serveSubmissionDetailPage serves the HTML page for a specific submission's details
func serveSubmissionDetailPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer) {
	// Set content type
	w.Header().Set(contentTypeHeader, htmlContentType)

	// Extract submission ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/submissions/")
	if path == "" || path == r.URL.Path {
		http.Error(w, "Invalid submission ID", http.StatusBadRequest)
		return
	}

	submissionID := path

	ctx := r.Context()
	result, err := rubricServer.storage.LoadResult(ctx, submissionID)
	if err != nil {
		slog.Error("Failed to load result from storage", "error", err, "submission_id", submissionID)
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}

	// Calculate totals
	totalPoints := 0.0
	awardedPoints := 0.0
	for _, item := range result.Rubric {
		totalPoints += item.Points
		awardedPoints += item.Awarded
	}

	score := 0.0
	if totalPoints > 0 {
		score = (awardedPoints / totalPoints) * 100
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, result.Timestamp)
	if err != nil {
		slog.Error("Failed to parse timestamp", "error", err, "timestamp", result.Timestamp)
		timestamp = time.Now()
	}

	// Convert rubric items to template data
	rubricItems := make([]RubricItemData, len(result.Rubric))
	for i, item := range result.Rubric {
		rubricItems[i] = RubricItemData{
			Name:    item.Name,
			Points:  item.Points,
			Awarded: item.Awarded,
			Note:    item.Note,
		}
	}

	data := struct {
		SubmissionID  string
		Timestamp     time.Time
		TotalPoints   float64
		AwardedPoints float64
		Score         float64
		IPAddress     string
		GeoLocation   string
		Rubric        []RubricItemData
	}{
		SubmissionID:  result.SubmissionId,
		Timestamp:     timestamp,
		TotalPoints:   totalPoints,
		AwardedPoints: awardedPoints,
		Score:         score,
		IPAddress:     result.IpAddress,
		GeoLocation:   result.GeoLocation,
		Rubric:        rubricItems,
	}

	if err := rubricServer.templates.submissionTmpl.Execute(w, data); err != nil {
		slog.Error(templateExecErrMsg, "error", err)
		http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
		return
	}
}

// AuthRubricHandler wraps the rubric handler with selective authentication
func AuthRubricHandler(handler http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a method that requires authentication
		if requiresAuth(r) {
			// Apply authentication
			t := time.Now()
			defer func() {
				duration := time.Since(t)
				slog.Info("Request completed", slog.Any("IP", r.RemoteAddr), slog.Any("duration", duration))
			}()
			authHeader := r.Header.Get("authorization")
			if authHeader == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}
			const bearerPrefix = "Bearer "
			if len(authHeader) < len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
				http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
				return
			}
			bearer := authHeader[len(bearerPrefix):]
			if bearer != token {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
		}
		// No auth required, or auth passed - proceed
		handler.ServeHTTP(w, r)
	})
} // AuthMiddleware returns an http.Handler middleware that enforces authorization
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := time.Now()
			defer func() {
				duration := time.Since(t)
				slog.Info("Request completed", slog.Any("IP", r.RemoteAddr), slog.Any("duration", duration))
			}()
			authHeader := r.Header.Get("authorization")
			if authHeader == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}
			const bearerPrefix = "Bearer "
			if len(authHeader) < len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
				http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
				return
			}
			bearer := authHeader[len(bearerPrefix):]
			if bearer != token {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Context key for real IP
type contextKey string

const realIPKey contextKey = "real-ip"

// realIPMiddleware extracts the real client IP and stores it in request context
func realIPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract real IP using tomasen/realip
		realIP := realip.RealIP(r)

		// Store the real IP in the request context
		ctx := context.WithValue(r.Context(), realIPKey, realIP)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// requiresAuth determines if the current request requires authentication
func requiresAuth(r *http.Request) bool {
	// Only UploadRubricResult requires authentication
	return r.URL.Path == "/rubric.RubricService/UploadRubricResult"
}

func (s *QualityServer) EvaluateCodeQuality(
	ctx context.Context,
	req *connect.Request[pb.EvaluateCodeQualityRequest],
) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	// Basic validation: ensure files are present
	if len(req.Msg.Files) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no files provided"))
	}

	review, err := s.OpenAIClient.ReviewCode(ctx, req.Msg.Instructions, req.Msg.Files)
	if err != nil {
		slog.Error("failed to review code", "error", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to review code"))
	}

	return connect.NewResponse(&pb.EvaluateCodeQualityResponse{
		QualityScore: review.QualityScore,
		Feedback:     review.Feedback,
	}), nil
}
