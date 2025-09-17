package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bufbuild/connect-go"

	"github.com/jh125486/CSCE5350_gradebot/pkg/proto"
	"github.com/jh125486/CSCE5350_gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

// RubricServer implements the RubricService with persistent storage
type RubricServer struct {
	protoconnect.UnimplementedRubricServiceHandler
	storage storage.Storage
}

// NewRubricServer creates a new RubricServer with persistent storage
func NewRubricServer(storage storage.Storage) *RubricServer {
	return &RubricServer{
		storage: storage,
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
	geoLocation := getGeoLocation(clientIP)

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

	slog.Info("Stored rubric result", "submission_id", result.SubmissionId, "items", len(result.Rubric), "ip", clientIP, "location", geoLocation)

	return connect.NewResponse(&proto.UploadRubricResultResponse{
		SubmissionId: result.SubmissionId,
		Message:      "Rubric result uploaded successfully",
	}), nil
}

// getClientIP extracts the real client IP address from the request
// Tries multiple methods to get the real IP
func getClientIP(ctx context.Context, req *connect.Request[proto.UploadRubricResultRequest]) string {
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

// GeoLocation represents the geo location data from the IP lookup
type GeoLocation struct {
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country_name"`
}

// getGeoLocation fetches geo location data for an IP address
func getGeoLocation(ip string) string {
	if ip == "" || ip == "unknown" || ip == "127.0.0.1" || ip == "::1" {
		return "Local/Unknown"
	}

	// Use ipapi.co for free geo location lookup
	url := fmt.Sprintf("http://ipapi.co/%s/json/", ip)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		slog.Warn("Failed to fetch geo location", "ip", ip, "error", err)
		return "Unknown"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("Geo location API returned non-200 status", "ip", ip, "status", resp.StatusCode)
		return "Unknown"
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("Failed to read geo location response", "ip", ip, "error", err)
		return "Unknown"
	}

	var geo GeoLocation
	if err := json.Unmarshal(body, &geo); err != nil {
		slog.Warn("Failed to parse geo location response", "ip", ip, "error", err)
		return "Unknown"
	}

	// Format the location
	location := ""
	if geo.City != "" {
		location = geo.City
	}
	if geo.Region != "" {
		if location != "" {
			location += ", " + geo.Region
		} else {
			location = geo.Region
		}
	}
	if geo.Country != "" {
		if location != "" {
			location += ", " + geo.Country
		} else {
			location = geo.Country
		}
	}

	if location == "" {
		return "Unknown"
	}

	return location
}
