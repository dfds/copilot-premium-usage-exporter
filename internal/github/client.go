package github

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const apiBase = "https://api.github.com"
const apiVersion = "2022-11-28"

type Client struct {
	httpClient *http.Client
	token      string
}

func NewClient(token string) *Client {
	return &Client{
		httpClient: &http.Client{},
		token:      token,
	}
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
}

func (c *Client) get(url string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	return json.NewDecoder(resp.Body).Decode(out)
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
