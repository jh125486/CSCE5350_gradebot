package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bufbuild/connect-go"

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
	geoLocation := s.geoClient.Do(clientIP)

	// Create a copy of the result with IP and geo data
	resultWithIP := &proto.Result{
		SubmissionId: result.SubmissionId,
		Timestamp:    result.Timestamp,
		Rubric:       result.Rubric,
		IpAddress:    clientIP,
		GeoLocation:  geoLocation,
	}

	// Save to persistent storage
	err := s.storage.SaveResult(ctx, result.SubmissionId, resultWithIP)
	if err != nil {
		slog.Error("Failed to save result to storage", "error", err, "submission_id", result.SubmissionId)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save result: %w", err))
	}

	slog.Info("Stored rubric result",
		"submission_id", result.SubmissionId, "items", len(result.Rubric),
		"ip", clientIP, "location", geoLocation)

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
func (c *GeoLocationClient) Do(ip string) string {
	if skipGeoLookup(ip) {
		return localUnknown
	}

	req, err := newGeoRequest(ip)
	if err != nil {
		slog.Warn("Failed to create geo location request", "ip", ip, "error", err)
		return unknownLocation
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		slog.Warn("Failed to fetch geo location", "ip", ip, "error", err)
		return unknownLocation
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("Geo location API returned non-200 status", "ip", ip, "status", resp.StatusCode)
		return unknownLocation
	}

	geo, err := decodeGeoLocation(resp.Body)
	if err != nil {
		slog.Warn("Failed to parse geo location response", "ip", ip, "error", err)
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
