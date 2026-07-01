// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package url_generator

import "fmt"

// GenerateEndpointURL generates a video stream endpoint URL
// Pattern: https://{line_display_name}.{domain}/{stream_path}.flv
func GenerateEndpointURL(lineDisplayName, domain, streamPath string) string {
	return fmt.Sprintf("https://%s.%s/%s.flv", lineDisplayName, domain, streamPath)
}

