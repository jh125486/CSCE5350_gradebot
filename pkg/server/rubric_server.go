package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

// RubricServer implements the RubricService with persistent storage
type RubricServer struct {
	protoconnect.UnimplementedRubricServiceHandler
	storage   storage.Storage
	templates *TemplateManager
	geoClient *GeoLocationClient
}

// NewRubricServer creates a new RubricServer with persistent storage
func NewRubricServer(stor storage.Storage) *RubricServer {
	return &RubricServer{
		storage:   stor,
		templates: NewTemplateManager(),
		geoClient: &GeoLocationClient{
			Client: &http.Client{},
		},
	}
}

// UploadRubricResult stores a rubric result using persistent storage
func (s *RubricServer) UploadRubricResult(
	ctx context.Context,
	req *connect.Request[proto.UploadRubricResultRequest],
) (*connect.Response[proto.UploadRubricResultResponse], error) {
	result := req.Msg.Result
	if result.SubmissionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("submission_id is required"))
	}

	// Capture client IP and geo location
	clientIP := getClientIP(ctx, req)
	geoLocation := s.geoClient.Do(ctx, clientIP)

	// Create a copy of the result with IP and geo data
	resultWithIP := &proto.Result{
		SubmissionId: result.SubmissionId,
		Timestamp:    result.Timestamp,
		Rubric:       result.Rubric,
		IpAddress:    clientIP,
		GeoLocation:  geoLocation,
	}

	// Save to persistent storage
	err := s.storage.SaveResult(ctx, resultWithIP)
	if err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "Failed to save result to storage",
			slog.Any("error", err),
			slog.String("submission_id", result.SubmissionId),
		)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save result: %w", err))
	}

	contextlog.From(ctx).InfoContext(ctx, "Stored rubric result",
		slog.String("submission_id", result.SubmissionId),
		slog.Int("items", len(result.Rubric)),
		slog.String("ip", clientIP),
		slog.String("location", geoLocation))

	return connect.NewResponse(&proto.UploadRubricResultResponse{
		SubmissionId: result.SubmissionId,
		Message:      "Rubric result uploaded successfully",
	}), nil
}

// getClientIP extracts the real client IP address from the request
// Tries multiple methods to get the real IP
func getClientIP(ctx context.Context, req *connect.Request[proto.UploadRubricResultRequest]) string {
	return extractClientIP(ctx, req)
}

// GeoLocation represents the geo location data from the IP lookup
type GeoLocation struct {
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country_name"`
}

// GeoLocationClient handles geo location lookups
type GeoLocationClient struct {
	*http.Client
}

// Do fetches geo location data for an IP address
func (c *GeoLocationClient) Do(ctx context.Context, ip string) string {
	if skipGeoLookup(ip) {
		return localUnknown
	}

	req, err := newGeoRequest(ip)
	if err != nil {
		contextlog.From(ctx).WarnContext(ctx, "Failed to create geo location request",
			slog.String("ip", ip),
			slog.Any("error", err))
		return unknownLocation
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		contextlog.From(ctx).WarnContext(ctx, "Failed to fetch geo location",
			slog.String("ip", ip),
			slog.Any("error", err),
		)
		return unknownLocation
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		contextlog.From(ctx).WarnContext(ctx, "Geo location API returned non-200 status",
			slog.String("ip", ip),
			slog.Int("status", resp.StatusCode),
		)
		return unknownLocation
	}

	geo, err := decodeGeoLocation(resp.Body)
	if err != nil {
		contextlog.From(ctx).WarnContext(ctx, "Failed to parse geo location response",
			slog.String("ip", ip),
			slog.Any("error", err),
		)
		return unknownLocation
	}

	return formatLocation(geo)
}

func skipGeoLookup(ip string) bool {
	return ip == "" || ip == unknownIP || ip == "127.0.0.1" || ip == "::1"
}

func newGeoRequest(ip string) (*http.Request, error) {
	url := fmt.Sprintf("http://ipapi.co/%s/json/", ip)
	return http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
}

func decodeGeoLocation(body io.Reader) (GeoLocation, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return GeoLocation{}, err
	}

	var geo GeoLocation
	if err := json.Unmarshal(data, &geo); err != nil {
		return GeoLocation{}, err
	}

	return geo, nil
}

func formatLocation(geo GeoLocation) string {
	parts := appendNonEmpty(nil, geo.City, geo.Region, geo.Country)
	if len(parts) == 0 {
		return unknownLocation
	}
	return strings.Join(parts, ", ")
}

func appendNonEmpty(parts []string, values ...string) []string {
	for _, value := range values {
		if value != "" {
			parts = append(parts, value)
		}
	}
	return parts
}
