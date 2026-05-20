package osrsge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *wikiClient) getJSON(ctx context.Context, path string, dst any) error {
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: HTTP %d: %s", u, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (c *wikiClient) getJSONRaw(ctx context.Context, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: HTTP %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (c *wikiClient) mapping(ctx context.Context) ([]mappingItem, error) {
	var items []mappingItem
	err := c.getJSON(ctx, "mapping", &items)
	return items, err
}

func (c *wikiClient) latest(ctx context.Context) (latestResponse, error) {
	var resp latestResponse
	err := c.getJSON(ctx, "latest", &resp)
	return resp, err
}

func (c *wikiClient) interval(ctx context.Context, interval string) (intervalResponse, error) {
	var resp intervalResponse
	err := c.getJSON(ctx, interval, &resp)
	return resp, err
}

func (c *wikiClient) intervalAt(ctx context.Context, interval string, timestamp int64) (intervalResponse, error) {
	var resp intervalResponse
	raw := fmt.Sprintf("%s/%s?timestamp=%d", c.baseURL, url.PathEscape(interval), timestamp)
	err := c.getJSONRaw(ctx, raw, &resp)
	if resp.Timestamp == 0 {
		resp.Timestamp = timestamp
	}
	return resp, err
}

func (c *wikiClient) timeseries(ctx context.Context, id int64, step string) (timeseriesResponse, error) {
	var resp timeseriesResponse
	raw := fmt.Sprintf("%s/timeseries?id=%d&timestep=%s", c.baseURL, id, url.QueryEscape(step))
	err := c.getJSONRaw(ctx, raw, &resp)
	return resp, err
}
