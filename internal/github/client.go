package github

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

const apiBase = "https://api.github.com"
const apiVersion = "2026-03-10"
const maxRetries = 3
const defaultFallbackSleep = 60 * time.Second
const rateLimitResetBuffer = 5 * time.Second

// Async report polling: GitHub's docs suggest 30s; 15s gives faster turnaround
// when the report completes mid-interval at the cost of a few extra polls.
const reportPollInterval = 15 * time.Second
const reportPollMaxAttempts = 160 // ~40 minutes ceiling — month-to-date reports take longer to render than a single day

type Client struct {
	httpClient         *http.Client
	token              string
	logger             *zap.Logger
	rateLimitRemaining int
	rateLimitReset     time.Time
}

func NewClient(token string, logger *zap.Logger) *Client {
	return &Client{
		httpClient:         &http.Client{},
		token:              token,
		logger:             logger,
		rateLimitRemaining: -1,
	}
}

func (c *Client) updateRateLimit(resp *http.Response) {
	if s := resp.Header.Get("X-RateLimit-Remaining"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			c.rateLimitRemaining = n
		}
	}
	if s := resp.Header.Get("X-RateLimit-Reset"); s != "" {
		if unix, err := strconv.ParseInt(s, 10, 64); err == nil {
			c.rateLimitReset = time.Unix(unix, 0)
		}
	}
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
}

// sleepSecondaryRateLimit handles a 429 response by sleeping for the duration
// specified in the Retry-After header. Falls back to defaultFallbackSleep if
// the header is absent or unparseable. Always drains and closes the body.
func sleepSecondaryRateLimit(resp *http.Response) time.Duration {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.ParseInt(s, 10, 64); err == nil && secs > 0 {
			d := time.Duration(secs) * time.Second
			time.Sleep(d)
			return d
		}
	}
	time.Sleep(defaultFallbackSleep)
	return defaultFallbackSleep
}

// sleepPrimaryRateLimit handles a 403+X-RateLimit-Remaining=0 response by
// sleeping until the reset time from X-RateLimit-Reset (plus a small buffer).
// Falls back to defaultFallbackSleep if the header is absent or unparseable.
// Always drains and closes the body.
func sleepPrimaryRateLimit(resp *http.Response) time.Duration {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if s := resp.Header.Get("X-RateLimit-Reset"); s != "" {
		if unix, err := strconv.ParseInt(s, 10, 64); err == nil {
			if d := time.Until(time.Unix(unix, 0)) + rateLimitResetBuffer; d > 0 {
				time.Sleep(d)
				return d
			}
		}
	}
	time.Sleep(defaultFallbackSleep)
	return defaultFallbackSleep
}

// do executes a single HTTP call against the GitHub API with rate-limit-aware
// retry. acceptStatuses lists status codes that count as success (the response
// is returned for the caller to consume). buildReq must produce a fresh request
// each call because the body is consumed on retry.
func (c *Client) do(buildReq func() (*http.Request, error), acceptStatuses ...int) (*http.Response, error) {
	if c.rateLimitRemaining == 0 {
		if d := time.Until(c.rateLimitReset) + rateLimitResetBuffer; d > 0 {
			c.logger.Info("preemptively waiting for github rate limit reset",
				zap.Duration("wait", d),
				zap.Time("resetAt", c.rateLimitReset),
			)
			time.Sleep(d)
		}
	}

	for attempt := range maxRetries {
		req, err := buildReq()
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		for _, ok := range acceptStatuses {
			if resp.StatusCode == ok {
				c.updateRateLimit(resp)
				return resp, nil
			}
		}

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			retriesRemaining := maxRetries - attempt - 1
			waited := sleepSecondaryRateLimit(resp)
			c.logger.Warn("github secondary rate limit hit",
				zap.String("url", req.URL.String()),
				zap.Duration("waited", waited),
				zap.Int("retriesRemaining", retriesRemaining),
			)
			if retriesRemaining == 0 {
				return nil, fmt.Errorf("secondary rate limited on %s after %d retries", req.URL.String(), maxRetries)
			}

		case http.StatusForbidden:
			if resp.Header.Get("X-RateLimit-Remaining") != "0" {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, req.URL.String())
			}
			retriesRemaining := maxRetries - attempt - 1
			waited := sleepPrimaryRateLimit(resp)
			c.logger.Warn("github primary rate limit hit",
				zap.String("url", req.URL.String()),
				zap.Duration("waited", waited),
				zap.Int("retriesRemaining", retriesRemaining),
			)
			if retriesRemaining == 0 {
				return nil, fmt.Errorf("primary rate limited on %s after %d retries", req.URL.String(), maxRetries)
			}

		default:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status %d for %s: %s", resp.StatusCode, req.URL.String(), string(body))
		}
	}
	return nil, fmt.Errorf("exceeded max retries")
}

func (c *Client) getJSON(url string, out any) error {
	resp, err := c.do(func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, url, nil)
	}, http.StatusOK)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetEnterpriseBillingUsage fetches the aggregate billing usage report for the
// enterprise. The endpoint returns one item per (date, product, sku, org, repo)
// bucket year-to-date by default; no per-user breakdown is available.
func (c *Client) GetEnterpriseBillingUsage(enterprise string) (*BillingUsageResponse, error) {
	url := fmt.Sprintf("%s/enterprises/%s/settings/billing/usage", apiBase, enterprise)

	var resp BillingUsageResponse
	if err := c.getJSON(url, &resp); err != nil {
		return nil, fmt.Errorf("getting enterprise billing usage: %w", err)
	}

	return &resp, nil
}

// CreateAndAwaitBillingReport posts a billing report creation request, polls
// until it completes (or fails), and returns the signed download URLs. The
// flow is async because GitHub renders CSV reports off-line; expect tens of
// seconds to several minutes wall-clock per call.
func (c *Client) CreateAndAwaitBillingReport(enterprise, reportType, startDate, endDate string) ([]string, error) {
	id, err := c.createBillingReport(enterprise, reportType, startDate, endDate)
	if err != nil {
		return nil, err
	}
	c.logger.Info("billing report queued",
		zap.String("id", id),
		zap.String("type", reportType),
		zap.String("start", startDate),
		zap.String("end", endDate),
	)

	for attempt := range reportPollMaxAttempts {
		st, notFound, err := c.getBillingReport(enterprise, id)
		if err != nil {
			return nil, err
		}
		if notFound {
			// GitHub's POST-create and GET-read paths are eventually consistent;
			// the freshly-issued ID can 404 for the first few polls.
			time.Sleep(reportPollInterval)
			continue
		}
		switch st.Status {
		case "completed":
			c.logger.Info("billing report ready",
				zap.String("id", id),
				zap.Int("polls", attempt+1),
				zap.Int("downloadURLs", len(st.DownloadURLs)),
			)
			return st.DownloadURLs, nil
		case "failed":
			return nil, fmt.Errorf("billing report %s failed", id)
		case "processing":
			time.Sleep(reportPollInterval)
		default:
			return nil, fmt.Errorf("billing report %s in unexpected status %q", id, st.Status)
		}
	}
	return nil, fmt.Errorf("billing report %s did not complete within %s",
		id, time.Duration(reportPollMaxAttempts)*reportPollInterval)
}

func (c *Client) createBillingReport(enterprise, reportType, startDate, endDate string) (string, error) {
	body, err := json.Marshal(BillingReportCreateRequest{
		ReportType: reportType,
		StartDate:  startDate,
		EndDate:    endDate,
		SendEmail:  false,
	})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/enterprises/%s/settings/billing/reports", apiBase, enterprise)

	resp, err := c.do(func() (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, http.StatusAccepted, http.StatusOK, http.StatusCreated)
	if err != nil {
		return "", fmt.Errorf("creating billing report: %w", err)
	}
	defer resp.Body.Close()

	var out BillingReportStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding billing report create response: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("billing report create returned empty id")
	}
	return out.ID, nil
}

func (c *Client) getBillingReport(enterprise, id string) (*BillingReportStatus, bool, error) {
	url := fmt.Sprintf("%s/enterprises/%s/settings/billing/reports/%s", apiBase, enterprise, id)
	resp, err := c.do(func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, url, nil)
	}, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return nil, false, fmt.Errorf("polling billing report %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body)
		return nil, true, nil
	}
	var out BillingReportStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, false, fmt.Errorf("decoding billing report %s status: %w", id, err)
	}
	return &out, false, nil
}

// FetchAICreditRows downloads each signed URL produced by an ai_credit billing
// report and decodes the CSV body into typed rows. The download URLs are
// pre-signed Azure SAS tokens — no auth headers are sent to them.
func (c *Client) FetchAICreditRows(urls []string) ([]AICreditRow, error) {
	var all []AICreditRow
	for _, u := range urls {
		rows, err := c.fetchAICreditCSV(u)
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}
	return all, nil
}

func (c *Client) fetchAICreditCSV(url string) ([]AICreditRow, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading ai_credit csv: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("ai_credit csv download returned %d: %s", resp.StatusCode, string(body))
	}

	reader := csv.NewReader(resp.Body)
	reader.FieldsPerRecord = -1 // tolerate trailing-column drift
	reader.LazyQuotes = true    // GitHub's CSV embeds bare quotes inside model names
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading ai_credit csv header: %w", err)
	}
	idx := indexHeader(header)

	var rows []AICreditRow
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading ai_credit csv row: %w", err)
		}
		rows = append(rows, AICreditRow{
			Date:           field(rec, idx, "date"),
			Username:       field(rec, idx, "username"),
			Product:        field(rec, idx, "product"),
			SKU:            field(rec, idx, "sku"),
			Model:          field(rec, idx, "model"),
			Quantity:       parseFloat(field(rec, idx, "quantity")),
			UnitType:       field(rec, idx, "unit_type"),
			GrossAmount:    parseFloat(field(rec, idx, "gross_amount")),
			DiscountAmount: parseFloat(field(rec, idx, "discount_amount")),
			NetAmount:      parseFloat(field(rec, idx, "net_amount")),
			Organization:   field(rec, idx, "organization"),
			Repository:     field(rec, idx, "repository"),
			CostCenterName: field(rec, idx, "cost_center_name"),
		})
	}
	return rows, nil
}

func indexHeader(h []string) map[string]int {
	m := make(map[string]int, len(h))
	for i, name := range h {
		m[name] = i
	}
	return m
}

func field(rec []string, idx map[string]int, name string) string {
	if i, ok := idx[name]; ok && i < len(rec) {
		return rec[i]
	}
	return ""
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
