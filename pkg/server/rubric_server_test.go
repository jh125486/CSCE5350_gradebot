package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jh125486/CSCE5350_gradebot/pkg/contextlog"
	"github.com/jh125486/CSCE5350_gradebot/pkg/server"
	"github.com/jh125486/CSCE5350_gradebot/pkg/storage"
)

// Test constants for rubric server testing
const (
	geoLocalIP         = "Local/Unknown"
	geoUnknownLocation = "Unknown"
	testGeoIP          = "1.1.1.1"
)

// TestNewRubricServer tests the NewRubricServer constructor
func TestNewRubricServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		storage storage.Storage
		wantNil bool
	}{
		{
			name:    "WithValidStorage",
			storage: newMockStorage(),
			wantNil: false,
		},
		{
			name:    "WithNilStorage",
			storage: nil,
			wantNil: false, // Constructor doesn't panic on nil
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rs := server.NewRubricServer(tt.storage)
			if tt.wantNil {
				assert.Nil(t, rs)
			} else {
				assert.NotNil(t, rs)
			}
		})
	}
}

// mockRoundTripper for geo location testing
type mockGeoRoundTripper struct {
	respFunc func(*http.Request) (*http.Response, error)
}

func (m *mockGeoRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.respFunc(req)
}

// TestGeoLocationClientDo tests the GeoLocationClient.Do method
func TestGeoLocationClientDo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		ip               string
		mockResponse     interface{}
		mockStatusCode   int
		mockError        error
		expectedLocation string
	}{
		{
			name: "SuccessfulGeoLookup",
			ip:   "8.8.8.8",
			mockResponse: map[string]string{
				"city":         "Mountain View",
				"region":       "California",
				"country_name": "United States",
			},
			mockStatusCode:   http.StatusOK,
			expectedLocation: "Mountain View, California, United States",
		},
		{
			name:             "LocalhostIP",
			ip:               "127.0.0.1",
			mockStatusCode:   http.StatusOK,
			expectedLocation: "Local/Unknown", // Should skip lookup
		},
		{
			name:             "EmptyIP",
			ip:               "",
			mockStatusCode:   http.StatusOK,
			expectedLocation: "Local/Unknown", // Should skip lookup
		},
		{
			name:             "IPv6Loopback",
			ip:               "::1",
			mockStatusCode:   http.StatusOK,
			expectedLocation: "Local/Unknown", // Should skip lookup
		},
		{
			name:             "NetworkError",
			ip:               "1.1.1.1",
			mockError:        errors.New("connection refused"),
			expectedLocation: "Unknown",
		},
		{
			name:             "HTTP400Status",
			ip:               "1.1.1.1",
			mockStatusCode:   http.StatusBadRequest,
			expectedLocation: "Unknown",
		},
		{
			name: "PartialGeoData",
			ip:   "1.1.1.1",
			mockResponse: map[string]string{
				"city":         "Sydney",
				"region":       "",
				"country_name": "Australia",
			},
			mockStatusCode:   http.StatusOK,
			expectedLocation: "Sydney, Australia",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &http.Client{
				Transport: &mockGeoRoundTripper{
					respFunc: func(req *http.Request) (*http.Response, error) {
						if tt.mockError != nil {
							return nil, tt.mockError
						}

						body := ""
						if tt.mockResponse != nil {
							data, _ := json.Marshal(tt.mockResponse)
							body = string(data)
						}

						return &http.Response{
							StatusCode: tt.mockStatusCode,
							Body:       io.NopCloser(strings.NewReader(body)),
						}, nil
					},
				},
			}

			geoClient := &server.GeoLocationClient{Client: client}
			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			result := geoClient.Do(ctx, tt.ip)

			assert.Equal(t, tt.expectedLocation, result)
		})
	}
}
