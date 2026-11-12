// Package noise ranks Prometheus alerts by how noisy they are: how often they
// fire, how long they take to clear, how often they clear within minutes on
// their own, and whether they flap inside their own "for" window. The firing
// history comes from the ALERTS series, either from a saved query_range
// response or fetched live from the Prometheus HTTP API.
package noise

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultQuery selects the firing state of every alert.
const DefaultQuery = `ALERTS{alertstate="firing"}`

// maxPoints is how many samples per series one query_range call may return.
// Prometheus rejects queries above 11,000 points per series; staying below it
// leaves room for an inclusive end.
const maxPoints = 10000

// Series is one ALERTS time series: its label set and the timestamps at which
// it had a sample.
type Series struct {
	Labels  map[string]string
	Samples []time.Time
}

// Rule is the part of an alerting rule definition the report needs.
type Rule struct {
	Name   string
	For    time.Duration
	Labels map[string]string
}

type apiResponse struct {
	Status    string          `json:"status"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
}

type matrixData struct {
	ResultType string `json:"resultType"`
	Result     []struct {
		Metric map[string]string `json:"metric"`
		Values [][2]any          `json:"values"`
	} `json:"result"`
}

type rulesData struct {
	Groups []struct {
		Rules []struct {
			Type     string            `json:"type"`
			Name     string            `json:"name"`
			Duration float64           `json:"duration"` // seconds
			Labels   map[string]string `json:"labels"`
		} `json:"rules"`
	} `json:"groups"`
}

func unwrap(data []byte, what string) (json.RawMessage, error) {
	var r apiResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", what, err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("%s: prometheus returned %s: %s %s", what, r.Status, r.ErrorType, r.Error)
	}
	return r.Data, nil
}

// ParseRange decodes a Prometheus /api/v1/query_range response (resultType
// "matrix"), as saved with curl, into series.
func ParseRange(data []byte) ([]Series, error) {
	raw, err := unwrap(data, "query_range")
	if err != nil {
		return nil, err
	}
	var m matrixData
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("query_range: parse data: %w", err)
	}
	if m.ResultType != "matrix" {
		return nil, fmt.Errorf("query_range: result type %q, want matrix (use /api/v1/query_range)", m.ResultType)
	}
	out := make([]Series, 0, len(m.Result))
	for i, r := range m.Result {
		s := Series{Labels: r.Metric, Samples: make([]time.Time, 0, len(r.Values))}
		for _, v := range r.Values {
			ts, ok := v[0].(float64)
			if !ok {
				return nil, fmt.Errorf("query_range: series %d: bad sample timestamp %v", i, v[0])
			}
			s.Samples = append(s.Samples, time.UnixMilli(int64(ts*1000)).UTC())
		}
		out = append(out, s)
	}
	return out, nil
}

// ParseRules decodes a Prometheus /api/v1/rules response and returns the
// alerting rules grouped by alert name.
func ParseRules(data []byte) (map[string][]Rule, error) {
	raw, err := unwrap(data, "rules")
	if err != nil {
		return nil, err
	}
	var d rulesData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("rules: parse data: %w", err)
	}
	rules := map[string][]Rule{}
	for _, g := range d.Groups {
		for _, r := range g.Rules {
			if r.Type != "alerting" {
				continue
			}
			rules[r.Name] = append(rules[r.Name], Rule{
				Name:   r.Name,
				For:    time.Duration(r.Duration * float64(time.Second)),
				Labels: r.Labels,
			})
		}
	}
	return rules, nil
}

// Client reads from the Prometheus HTTP API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := strings.TrimSuffix(c.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Prometheus returns a JSON error body on 4xx/5xx; prefer its message.
	if resp.StatusCode != http.StatusOK {
		if _, err := unwrap(body, path); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return body, nil
}

// QueryRange runs a range query over [start, end], split into chunks that
// stay under the per-series point limit, and merges the chunks per series.
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error) {
	if step <= 0 {
		return nil, fmt.Errorf("step must be positive")
	}
	span := step * (maxPoints - 1)
	merged := map[string]*Series{}
	var order []string
	for from := start; !from.After(end); from = from.Add(span + step) {
		to := from.Add(span)
		if to.After(end) {
			to = end
		}
		q := url.Values{
			"query": {query},
			"start": {formatTS(from)},
			"end":   {formatTS(to)},
			"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
		}
		body, err := c.get(ctx, "/api/v1/query_range", q)
		if err != nil {
			return nil, err
		}
		chunk, err := ParseRange(body)
		if err != nil {
			return nil, err
		}
		for _, s := range chunk {
			key := labelKey(s.Labels)
			m, ok := merged[key]
			if !ok {
				m = &Series{Labels: s.Labels}
				merged[key] = m
				order = append(order, key)
			}
			m.Samples = append(m.Samples, s.Samples...)
		}
	}
	out := make([]Series, 0, len(order))
	for _, k := range order {
		out = append(out, *merged[k])
	}
	return out, nil
}

// Rules fetches the alerting rules from /api/v1/rules.
func (c *Client) Rules(ctx context.Context) (map[string][]Rule, error) {
	body, err := c.get(ctx, "/api/v1/rules", url.Values{"type": {"alert"}})
	if err != nil {
		return nil, err
	}
	return ParseRules(body)
}

func formatTS(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', -1, 64)
}

// labelKey renders a label set in a stable order, e.g. `{a="1", b="2"}`.
func labelKey(l map[string]string) string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(l[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
