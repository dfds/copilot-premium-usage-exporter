package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

const apiBase = "https://api.github.com"
const apiVersion = "2022-11-28"
const maxRetries = 3
const defaultFallbackSleep = 60 * time.Second
const rateLimitResetBuffer = 5 * time.Second

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

func (c *Client) get(url string, out any) error {
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
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}

		switch resp.StatusCode {
		case http.StatusOK:
			c.updateRateLimit(resp)
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(out)

		case http.StatusTooManyRequests: // 429 secondary rate limit
			retriesRemaining := maxRetries - attempt - 1
			waited := sleepSecondaryRateLimit(resp)
			c.logger.Warn("github secondary rate limit hit",
				zap.String("url", url),
				zap.Duration("waited", waited),
				zap.Int("retriesRemaining", retriesRemaining),
			)
			if retriesRemaining == 0 {
				return fmt.Errorf("secondary rate limited on %s after %d retries", url, maxRetries)
			}

		case http.StatusForbidden:
			if resp.Header.Get("X-RateLimit-Remaining") != "0" {
				// Not a rate limit (auth error, permissions, etc.) — fail immediately.
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				return fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
			}
			// Primary rate limit exhausted.
			retriesRemaining := maxRetries - attempt - 1
			waited := sleepPrimaryRateLimit(resp)
			c.logger.Warn("github primary rate limit hit",
				zap.String("url", url),
				zap.Duration("waited", waited),
				zap.Int("retriesRemaining", retriesRemaining),
			)
			if retriesRemaining == 0 {
				return fmt.Errorf("primary rate limited on %s after %d retries", url, maxRetries)
			}

		default:
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
		}
	}
	return fmt.Errorf("get %s: exceeded max retries", url)
}

func (c *Client) ListCopilotSeats(enterprise string) ([]string, error) {
	var logins []string
	page := 1
	perPage := 100

	for {
		url := fmt.Sprintf("%s/enterprises/%s/copilot/billing/seats?per_page=%d&page=%d",
			apiBase, enterprise, perPage, page)

		var resp SeatsResponse
		if err := c.get(url, &resp); err != nil {
			return nil, fmt.Errorf("listing copilot seats page %d: %w", page, err)
		}

		for _, seat := range resp.Seats {
			logins = append(logins, seat.Assignee.Login)
		}

		if len(resp.Seats) < perPage {
			break
		}
		page++
	}

	return logins, nil
}

func (c *Client) GetUserPremiumUsage(enterprise, user string) (*UsageResponse, error) {
	url := fmt.Sprintf("%s/enterprises/%s/settings/billing/premium_request/usage?user=%s",
		apiBase, enterprise, user)

	var resp UsageResponse
	if err := c.get(url, &resp); err != nil {
		return nil, fmt.Errorf("getting premium usage for user %q: %w", user, err)
	}

	return &resp, nil
}
