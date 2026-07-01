// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/q191201771/lal/pkg/avc"
	"github.com/q191201771/lal/pkg/httpflv"
)

// DetectResolutionFromPath detects resolution from stream path
// Returns: "普清", "高清", or "超清"
func DetectResolutionFromPath(fullPath string) string {
	path := strings.ToUpper(fullPath)
	
	// Check for UHD/4K first (highest priority)
	if strings.Contains(path, "UHD") || strings.Contains(path, "4K") {
		return "超清"
	}
	
	// Check for HD
	if strings.Contains(path, "HD") {
		return "高清"
	}
	
	// Check for SD
	if strings.Contains(path, "SD") {
		return "普清"
	}
	
	// Default to 普清
	return "普清"
}

// DetectResolutionFromStream detects resolution from actual video stream using lal library
// Returns: "普清", "高清", or "超清", and error
// Currently supports HTTP-FLV protocol. For other protocols, falls back to path-based detection.
func DetectResolutionFromStream(url string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Check if URL is HTTP-FLV (ends with .flv or contains http:///https://)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("unsupported protocol, only HTTP-FLV is supported currently")
	}

	// Create a channel to receive the resolution result
	type result struct {
		resolution string
		err        error
	}
	resultChan := make(chan result, 1)
	var once sync.Once

	// Create pull session
	session := httpflv.NewPullSession()
	defer session.Dispose()

	// Start pulling stream in a goroutine
	go func() {
		// Use recover to catch any panic from Pull
		defer func() {
			if r := recover(); r != nil {
				once.Do(func() {
					resultChan <- result{"", fmt.Errorf("panic during stream pull: %v", r)}
				})
			}
		}()
		
		err := session.Pull(url, func(tag httpflv.Tag) {
			// Check if this is an AVC sequence header (contains SPS)
			if tag.IsAvcKeySeqHeader() {
				once.Do(func() {
					payload := tag.Payload()
					if len(payload) < 2 {
						resultChan <- result{"", fmt.Errorf("invalid sequence header: payload too short")}
						return
					}

					// Parse SPS from sequence header
					// The payload format is: [frame_type(4bit) codec_id(4bit)] [packet_type(1byte)] [sequence_header_data...]
					// Skip the first 2 bytes (frame_type+codec_id and packet_type)
					seqHeaderData := payload[2:]

					// Extract SPS from sequence header
					sps, _, err := avc.ParseSpsPpsFromSeqHeader(seqHeaderData)
					if err != nil {
						resultChan <- result{"", fmt.Errorf("failed to parse SPS from sequence header: %w", err)}
						return
					}

					if len(sps) == 0 {
						resultChan <- result{"", fmt.Errorf("no SPS found in sequence header")}
						return
					}

					// Parse SPS to get resolution
					var ctx avc.Context
					if err := avc.ParseSps(sps, &ctx); err != nil {
						resultChan <- result{"", fmt.Errorf("failed to parse SPS: %w", err)}
						return
					}

					// Classify resolution based on width
					resolution := ClassifyResolutionByWidth(int(ctx.Width))
					resultChan <- result{resolution, nil}
				})
			}
		})

		// If pull failed and we haven't sent a result yet, send error
		if err != nil {
			once.Do(func() {
				resultChan <- result{"", fmt.Errorf("failed to pull stream: %w", err)}
			})
		}
	}()

	// Wait for result or timeout
	// We don't use WaitChan() as it can cause nil pointer dereference
	// Instead, we rely on timeout and the result channel
	select {
	case <-ctx.Done():
		session.Dispose()
		return "", fmt.Errorf("resolution detection timeout after %v", timeout)
	case res := <-resultChan:
		session.Dispose()
		return res.resolution, res.err
	}
}

// ClassifyResolutionByWidth classifies resolution based on video width
func ClassifyResolutionByWidth(width int) string {
	if width <= 720 {
		return "普清"
	} else if width <= 1080 {
		return "高清"
	}
	return "超清"
}

