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
	submissionsTmplFile = "submissions.go.tmpl"
	submissionTmplFile  = "submission.go.tmpl"
	unknownIP           = "unknown"
	unknownLocation     = "Unknown"
	localUnknown        = "Local/Unknown"
	contentTypeHeader   = "Content-Type"
	templateExecErrMsg  = "Failed to execute template"
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
	if ip := realIPFromContext(ctx); ip != "" {
		return ip
	}

	if ip := headerIP(req.Header()); ip != "" {
		return ip
	}

	if ip := peerIP(req.Peer()); ip != "" {
		return ip
	}

	return unknownIP
}

func realIPFromContext(ctx context.Context) string {
	realIP, ok := ctx.Value(realIPKey).(string)
	if !ok {
		return ""
	}
	if isUnknownIP(realIP) {
		return ""
	}
	return realIP
}

func headerIP(headers http.Header) string {
	for _, key := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"} {
		value := headers.Get(key)
		if value == "" {
			continue
		}

		ip := value
		if key == "X-Forwarded-For" {
			ip = firstForwardedIP(value)
		}

		if !isUnknownIP(ip) {
			return ip
		}
	}
	return ""
}

func firstForwardedIP(value string) string {
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func peerIP(peer connect.Peer) string {
	if peer.Addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(peer.Addr); err == nil {
		if !isUnknownIP(host) {
			return host
		}
		return ""
	}
	if isUnknownIP(peer.Addr) {
		return ""
	}
	return peer.Addr
}

func isUnknownIP(ip string) bool {
	return ip == "" || ip == unknownIP
}

// TemplateManager manages HTML templates
type TemplateManager struct {
	submissionsTmpl *template.Template
	submissionTmpl  *template.Template
}

// NewTemplateManager creates a new template manager with loaded templates
func NewTemplateManager() *TemplateManager {
	return &TemplateManager{
		submissionsTmpl: template.Must(template.New(submissionsTmplFile).ParseFS(templatesFS, filepath.Join("templates", submissionsTmplFile))),
		submissionTmpl:  template.Must(template.New(submissionTmplFile).ParseFS(templatesFS, filepath.Join("templates", submissionTmplFile))),
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

// serveSubmissionsPage serves the HTML page for viewing submissions
func serveSubmissionsPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer) {
	ctx := r.Context()
	results, err := rubricServer.storage.ListResults(ctx)
	if err != nil {
		slog.Error("Failed to list results from storage", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set content type after we know we have valid data
	w.Header().Set(contentTypeHeader, "text/html")

	// Prepare template data by accessing the storage
	submissions := make([]SubmissionData, 0, len(results))
	var totalScore float64
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
			timestamp = time.Now() // fallback
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
		totalScore += score
		if score > highScore {
			highScore = score
		}
	}

	avgScore := 0.0
	if len(submissions) > 0 {
		avgScore = totalScore / float64(len(submissions))
		sort.Slice(submissions, func(i, j int) bool {
			return submissions[i].SubmissionID < submissions[j].SubmissionID
		})
	}

	data := struct {
		TotalSubmissions int
		AvgScore         float64
		HighScore        float64
		Submissions      []SubmissionData
	}{
		TotalSubmissions: len(submissions),
		AvgScore:         avgScore,
		HighScore:        highScore,
		Submissions:      submissions,
	}

	slog.Info("Template data", "total_submissions", len(submissions), "submissions_count", len(data.Submissions))

	// Execute template (already parsed at package initialization)
	if err := rubricServer.templates.submissionsTmpl.Execute(w, data); err != nil {
		slog.Error(templateExecErrMsg, "error", err)
		http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
		return
	}
}

// serveSubmissionDetailPage serves the HTML page for a specific submission's details
func serveSubmissionDetailPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer) {
	// Set content type
	w.Header().Set(contentTypeHeader, "text/html")

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
