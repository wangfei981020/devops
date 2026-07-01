// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "time"

// VideoStreamEndpoint represents a video stream endpoint entity
type VideoStreamEndpoint struct {
	ID           int64        `json:"id" db:"id"`
	ProviderID   int64        `json:"provider_id" db:"provider_id"`
	Provider     *CDNProvider `json:"provider,omitempty"`
	LineID       int64        `json:"line_id" db:"line_id"`
	Line         *CDNLine     `json:"line,omitempty"`
	DomainID     int64        `json:"domain_id" db:"domain_id"`
	Domain       *Domain      `json:"domain,omitempty"`
	StreamID     int64        `json:"stream_id" db:"stream_id"`
	Stream       *Stream      `json:"stream,omitempty"`
	StreamPathID int64        `json:"stream_path_id" db:"stream_path_id"`
	StreamPath   *StreamPath  `json:"stream_path,omitempty"`
	FullURL      string       `json:"full_url" db:"full_url"`
	Status       int          `json:"status" db:"status"`
	Resolution   string       `json:"resolution" db:"resolution"`
	CreatedAt    time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at" db:"updated_at"`
}

// CreateVideoStreamEndpointRequest represents the request payload for creating a video stream endpoint
type CreateVideoStreamEndpointRequest struct {
	ProviderID   int64 `json:"provider_id" binding:"required"`
	LineID       int64 `json:"line_id" binding:"required"`
	DomainID     int64 `json:"domain_id" binding:"required"`
	StreamID     int64 `json:"stream_id" binding:"required"`
	StreamPathID int64 `json:"stream_path_id" binding:"required"`
	Status       int   `json:"status"`
}

// UpdateVideoStreamEndpointRequest represents the request payload for updating a video stream endpoint
type UpdateVideoStreamEndpointRequest struct {
	ProviderID   int64 `json:"provider_id" binding:"required"`
	LineID       int64 `json:"line_id" binding:"required"`
	DomainID     int64 `json:"domain_id" binding:"required"`
	StreamID     int64 `json:"stream_id" binding:"required"`
	StreamPathID int64 `json:"stream_path_id" binding:"required"`
	Status       int   `json:"status"`
}

// BatchGenerateRequest represents the request payload for batch generation
type BatchGenerateRequest struct {
	LineIDs       []int64 `json:"line_ids"`
	DomainIDs     []int64 `json:"domain_ids"`
	StreamIDs     []int64 `json:"stream_ids"`
	StreamPathIDs []int64 `json:"stream_path_ids"`
}
