package loki

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"opsplatform-alert-backend/models"
)

// Client wraps Loki HTTP API
type Client struct {
	baseURL    string
	httpClient *http.Client
	username   string
	password   string
	orgID      string
}

// NewClient creates a Loki client
func NewClient(conn models.LokiConnection) *Client {
	transport := &http.Transport{}
	if conn.SkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		baseURL:    conn.URL,
		httpClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
		username:   conn.Username,
		password:   conn.Password,
		orgID:      conn.OrgID,
	}
}

// QueryResult represents parsed Loki query result
type QueryResult struct {
	Streams []Stream `json:"streams"`
	Total   int      `json:"total"`
}

// Stream represents a Loki log stream
type Stream struct {
	Labels map[string]string        `json:"labels"`
	Entries []map[string]interface{} `json:"entries"`
}

// Ping tests Loki connection by calling /loki/api/v1/labels
func (c *Client) Ping(ctx context.Context) error {
	// Try /loki/api/v1/labels as it works across all Loki versions
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/loki/api/v1/labels", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Loki error: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

// QueryRange executes a LogQL query_range
func (c *Client) QueryRange(ctx context.Context, logql string, start, end time.Time, limit int) (*QueryResult, error) {
	if limit <= 0 {
		limit = 100
	}

	params := url.Values{}
	params.Set("query", logql)
	params.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("direction", "backward")

	reqURL := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())
	log.Printf("[Loki] Query: %s", reqURL)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Loki query error: %d %s", resp.StatusCode, string(body))
	}

	return parseLokiResponse(body)
}

// Labels returns available label names
func (c *Client) Labels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/loki/api/v1/labels", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Data []string `json:"data"`
	}
	json.Unmarshal(body, &result)
	return result.Data, nil
}

// LabelValues returns values for a label
func (c *Client) LabelValues(ctx context.Context, label string) ([]string, error) {
	return c.LabelValuesWithQuery(ctx, label, "")
}

// LabelValuesWithQuery returns label values filtered by a LogQL selector
func (c *Client) LabelValuesWithQuery(ctx context.Context, label, query string) ([]string, error) {
	reqURL := fmt.Sprintf("%s/loki/api/v1/label/%s/values", c.baseURL, url.PathEscape(label))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	if query != "" {
		q := req.URL.Query()
		q.Set("query", query)
		req.URL.RawQuery = q.Encode()
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Data []string `json:"data"`
	}
	json.Unmarshal(body, &result)
	return result.Data, nil
}

func (c *Client) setHeaders(req *http.Request) {
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	if c.orgID != "" {
		req.Header.Set("X-Scope-OrgID", c.orgID)
	}
}

func parseLokiResponse(body []byte) (*QueryResult, error) {
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := &QueryResult{}

	if raw.Data.ResultType == "streams" {
		var streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"` // [timestamp_ns, line]
		}
		if err := json.Unmarshal(raw.Data.Result, &streams); err != nil {
			return nil, err
		}

		for _, s := range streams {
			stream := Stream{Labels: s.Stream}
			for _, v := range s.Values {
				if len(v) >= 2 {
					entry := map[string]interface{}{
						"timestamp": v[0],
						"line":      v[1],
					}
					// Add labels as fields
					for k, val := range s.Stream {
						entry[k] = val
					}
					stream.Entries = append(stream.Entries, entry)
					result.Total++
				}
			}
			result.Streams = append(result.Streams, stream)
		}
	}

	return result, nil
}

// ToHits converts Loki result to flat hit list (compatible with ES hit format)
func (r *QueryResult) ToHits() []map[string]interface{} {
	var hits []map[string]interface{}
	for _, s := range r.Streams {
		for _, entry := range s.Entries {
			hit := make(map[string]interface{})
			// Copy labels
			for k, v := range s.Labels {
				hit[k] = v
			}
			// Copy entry fields
			for k, v := range entry {
				hit[k] = v
			}
			// Map "line" to "message" for template compatibility
			if line, ok := entry["line"]; ok {
				hit["message"] = line
			}
			// Parse timestamp: convert nanosecond string to readable time
			if ts, ok := entry["timestamp"].(string); ok {
				if nsec, err := strconv.ParseInt(ts, 10, 64); err == nil {
					formatted := time.Unix(0, nsec).Format("2006/01/02 15:04:05")
					hit["@timestamp"] = formatted
					hit["timestamp"] = formatted
				} else {
					hit["@timestamp"] = ts
				}
			}
			hits = append(hits, hit)
		}
	}
	return hits
}
