// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/pkg/database"
	"github.com/video-manager/backend/pkg/response"
)

type StatsHandler struct{}

func NewStatsHandler() *StatsHandler {
	return &StatsHandler{}
}

// GetStats handles GET /api/stats
// @Summary Get system statistics
// @Description Retrieve comprehensive system statistics including counts of providers, lines, domains, streams, stream paths, endpoints, and various aggregations
// @Tags stats
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=object} "Statistics object containing counts and aggregations"
// @Failure 500 {object} response.Response
// @Router /api/stats [get]
func (h *StatsHandler) GetStats(c *gin.Context) {
	ctx := c.Request.Context()

	stats := make(map[string]interface{})

	// Count providers
	var providerCount int
	err := database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM cdn_providers").Scan(&providerCount)
	if err != nil {
		response.InternalServerError(c, "Failed to get provider count")
		return
	}
	stats["providers"] = providerCount

	// Count lines
	var lineCount int
	err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM cdn_lines").Scan(&lineCount)
	if err != nil {
		response.InternalServerError(c, "Failed to get line count")
		return
	}
	stats["lines"] = lineCount

	// Count domains
	var domainCount int
	err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM domains").Scan(&domainCount)
	if err != nil {
		response.InternalServerError(c, "Failed to get domain count")
		return
	}
	stats["domains"] = domainCount

	// Count streams
	var streamCount int
	err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM streams").Scan(&streamCount)
	if err != nil {
		response.InternalServerError(c, "Failed to get stream count")
		return
	}
	stats["streams"] = streamCount

	// Count stream paths
	var streamPathCount int
	err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM stream_paths").Scan(&streamPathCount)
	if err != nil {
		response.InternalServerError(c, "Failed to get stream path count")
		return
	}
	stats["stream_paths"] = streamPathCount

	// Count endpoints
	var endpointCount int
	err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM video_stream_endpoints").Scan(&endpointCount)
	if err != nil {
		response.InternalServerError(c, "Failed to get endpoint count")
		return
	}
	stats["endpoints"] = endpointCount

	// Count enabled/disabled endpoints
	var enabledCount, disabledCount int
	err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM video_stream_endpoints WHERE status = 1").Scan(&enabledCount)
	if err == nil {
		err = database.DB.QueryRow(ctx, "SELECT COUNT(*) FROM video_stream_endpoints WHERE status = 0").Scan(&disabledCount)
	}
	if err != nil {
		response.InternalServerError(c, "Failed to get endpoint status counts")
		return
	}
	stats["endpoints_enabled"] = enabledCount
	stats["endpoints_disabled"] = disabledCount

	// Count lines per provider
	rows, err := database.DB.Query(ctx, `
		SELECT p.id, p.name, COUNT(l.id) as line_count
		FROM cdn_providers p
		LEFT JOIN cdn_lines l ON p.id = l.provider_id
		GROUP BY p.id, p.name
		ORDER BY line_count DESC
	`)
	if err != nil {
		response.InternalServerError(c, "Failed to get provider line counts")
		return
	}
	defer rows.Close()

	type ProviderLineCount struct {
		ProviderID   int64  `json:"provider_id"`
		ProviderName string `json:"provider_name"`
		LineCount    int    `json:"line_count"`
	}

	var providerLineCounts []ProviderLineCount
	for rows.Next() {
		var plc ProviderLineCount
		if err := rows.Scan(&plc.ProviderID, &plc.ProviderName, &plc.LineCount); err != nil {
			response.InternalServerError(c, "Failed to scan provider line counts")
			return
		}
		providerLineCounts = append(providerLineCounts, plc)
	}

	stats["lines_by_provider"] = providerLineCounts

	// Count endpoints by stream
	rows, err = database.DB.Query(ctx, `
		SELECT s.id, s.name, s.code, COUNT(vse.id) as endpoint_count
		FROM streams s
		LEFT JOIN video_stream_endpoints vse ON s.id = vse.stream_id
		GROUP BY s.id, s.name, s.code
		ORDER BY endpoint_count DESC
	`)
	if err == nil {
		defer rows.Close()
		type StreamEndpointCount struct {
			StreamID      int64  `json:"stream_id"`
			StreamName    string `json:"stream_name"`
			StreamCode    string `json:"stream_code"`
			EndpointCount int    `json:"endpoint_count"`
		}

		var streamEndpointCounts []StreamEndpointCount
		for rows.Next() {
			var sec StreamEndpointCount
			if err := rows.Scan(&sec.StreamID, &sec.StreamName, &sec.StreamCode, &sec.EndpointCount); err == nil {
				streamEndpointCounts = append(streamEndpointCounts, sec)
			}
		}
		stats["endpoints_by_stream"] = streamEndpointCounts
	}

	// Count endpoints by domain
	rows, err = database.DB.Query(ctx, `
		SELECT d.id, d.name, COUNT(vse.id) as endpoint_count
		FROM domains d
		LEFT JOIN video_stream_endpoints vse ON d.id = vse.domain_id
		GROUP BY d.id, d.name
		ORDER BY endpoint_count DESC
	`)
	if err == nil {
		defer rows.Close()
		type DomainEndpointCount struct {
			DomainID      int64  `json:"domain_id"`
			DomainName    string `json:"domain_name"`
			EndpointCount int    `json:"endpoint_count"`
		}

		var domainEndpointCounts []DomainEndpointCount
		for rows.Next() {
			var dec DomainEndpointCount
			if err := rows.Scan(&dec.DomainID, &dec.DomainName, &dec.EndpointCount); err == nil {
				domainEndpointCounts = append(domainEndpointCounts, dec)
			}
		}
		stats["endpoints_by_domain"] = domainEndpointCounts
	}

	response.Success(c, stats)
}

