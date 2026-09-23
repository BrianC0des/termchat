package ghauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultClientID is GitHub CLI's public OAuth App client ID. It is safe
// to embed in source: device flow client IDs are not secrets (there is
// no client_secret involved), and using gh's well-known ID lets TermChat
// piggyback on scopes/UX GitHub users already recognize. Callers may
// override it via Client.ClientID for a TermChat-specific OAuth App.
const DefaultClientID = "178c6fc778ccc68e1d6a"

const (
	deviceCodeURL  = "https://github.com/login/device/code"
	accessTokenURL = "https://github.com/login/oauth/access_token"
	userAPIURL     = "https://api.github.com/user"

	defaultPollInterval = 5 * time.Second
)

// DefaultScopes is what TermChat requests for its Git & GitHub Collab Hub
// features: read access to the user's profile and full repo access.
var DefaultScopes = []string{"read:user", "repo"}

// Client drives the RFC 8628 Device Authorization Grant against GitHub.
// The zero value is usable; it defaults to DefaultClientID, DefaultScopes,
// and http.DefaultClient.
type Client struct {
	ClientID   string
	Scopes     []string
	HTTPClient *http.Client
}

func (c *Client) clientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return DefaultClientID
}

func (c *Client) scopes() string {
	if len(c.Scopes) > 0 {
		return strings.Join(c.Scopes, " ")
	}
	return strings.Join(DefaultScopes, " ")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// DeviceCodeResponse is GitHub's response from POST /login/device/code,
// per RFC 8628 section 3.2.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// RequestDeviceCode starts the device flow: it asks GitHub for a
// device_code / user_code pair. The caller should show VerificationURI
// and UserCode to the user, then call PollToken with the returned
// DeviceCode and Interval.
func (c *Client) RequestDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	form := url.Values{
		"client_id": {c.clientID()},
		"scope":     {c.scopes()},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("ghauth: build device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ghauth: request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ghauth: device code request failed: HTTP %d", resp.StatusCode)
	}

	var out DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ghauth: decode device code response: %w", err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return nil, errors.New("ghauth: malformed device code response")
	}
	if out.Interval <= 0 {
		out.Interval = int(defaultPollInterval.Seconds())
	}
	return &out, nil
}

// tokenResponse is GitHub's response shape from the access_token
// endpoint, covering both the success case and the RFC 8628 error cases.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Interval         int    `json:"interval"` // present on slow_down responses
}

// PollError signals a fatal (non-retryable) device flow failure, as
// opposed to the expected transient "authorization_pending" state.
type PollError struct {
	Code    string
	Message string
}

func (e *PollError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("ghauth: device flow failed (%s): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("ghauth: device flow failed (%s)", e.Code)
}

// PollToken polls GitHub's OAuth access_token endpoint until the user
// authorizes the device (or a fatal error/timeout occurs), implementing
// the RFC 8628 section 3.5 polling behavior:
//   - "authorization_pending": keep polling at the current interval.
//   - "slow_down": increase the interval (GitHub returns a new interval,
//     or per spec we add 5s) and keep polling.
//   - "expired_token" / "access_denied" / any other error: return a
//     *PollError immediately.
//   - success: return the access token.
//
// interval is the starting poll interval in seconds, as returned by
// RequestDeviceCode. PollToken respects ctx cancellation/deadline as the
// overall timeout (e.g. derived from expires_in).
func (c *Client) PollToken(ctx context.Context, deviceCode string, interval int) (string, error) {
	if deviceCode == "" {
		return "", errors.New("ghauth: empty device code")
	}
	if interval <= 0 {
		interval = int(defaultPollInterval.Seconds())
	}
	wait := time.Duration(interval) * time.Second

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}

		tok, slowDown, newInterval, err := c.pollOnce(ctx, deviceCode)
		if err != nil {
			return "", err
		}
		if tok != "" {
			return tok, nil
		}
		if slowDown {
			if newInterval > 0 {
				wait = time.Duration(newInterval) * time.Second
			} else {
				wait += 5 * time.Second
			}
		}
		// else: authorization_pending, keep polling at current interval.
	}
}

// pollOnce performs a single access_token poll. Return values:
//   - (token, false, 0, nil): success
//   - ("", true, newInterval, nil): slow_down (newInterval may be 0)
//   - ("", false, 0, nil): authorization_pending, keep going
//   - ("", false, 0, err): fatal error (*PollError or transport error)
func (c *Client) pollOnce(ctx context.Context, deviceCode string) (token string, slowDown bool, newInterval int, err error) {
	form := url.Values{
		"client_id":   {c.clientID()},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", false, 0, fmt.Errorf("ghauth: build token poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", false, 0, fmt.Errorf("ghauth: poll token: %w", err)
	}
	defer resp.Body.Close()

	var out tokenResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&out); decErr != nil {
		return "", false, 0, fmt.Errorf("ghauth: decode token poll response: %w", decErr)
	}

	switch out.Error {
	case "":
		if out.AccessToken == "" {
			return "", false, 0, &PollError{Code: "empty_token", Message: "server returned no access_token and no error"}
		}
		return out.AccessToken, false, 0, nil
	case "authorization_pending":
		return "", false, 0, nil
	case "slow_down":
		return "", true, out.Interval, nil
	default:
		// expired_token, access_denied, unsupported_grant_type,
		// incorrect_client_credentials, incorrect_device_code, etc.
		return "", false, 0, &PollError{Code: out.Error, Message: out.ErrorDescription}
	}
}

// User is the subset of GitHub's /user API response ghauth cares about.
type User struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// FetchUser fetches the authenticated user's profile from the GitHub API
// using the given token.
func (c *Client) FetchUser(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, errors.New("ghauth: empty token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userAPIURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ghauth: build user request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ghauth: fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ghauth: fetch user failed: HTTP %d", resp.StatusCode)
	}

	var u User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("ghauth: decode user response: %w", err)
	}
	return &u, nil
}

// Login runs the full device flow end-to-end: request a device code,
// invoke onPrompt with the verification URL and user code (so the caller
// can display it / open a browser), poll until authorized, fetch the
// user's profile, and persist the result via SaveToken. It returns the
// authenticated User on success.
//
// onPrompt may be nil, in which case the caller is expected to have
// already surfaced dc.VerificationURI / dc.UserCode from a prior
// RequestDeviceCode call, or simply doesn't want a prompt callback.
func (c *Client) Login(ctx context.Context, onPrompt func(dc *DeviceCodeResponse)) (*User, error) {
	dc, err := c.RequestDeviceCode(ctx)
	if err != nil {
		return nil, err
	}
	if onPrompt != nil {
		onPrompt(dc)
	}

	pollCtx := ctx
	if dc.ExpiresIn > 0 {
		var cancel context.CancelFunc
		pollCtx, cancel = context.WithTimeout(ctx, time.Duration(dc.ExpiresIn)*time.Second)
		defer cancel()
	}

	token, err := c.PollToken(pollCtx, dc.DeviceCode, dc.Interval)
	if err != nil {
		return nil, err
	}

	user, err := c.FetchUser(ctx, token)
	if err != nil {
		return nil, err
	}

	if err := SaveToken(user.Login, token, c.scopesSlice()); err != nil {
		return nil, fmt.Errorf("ghauth: save token after login: %w", err)
	}

	return user, nil
}

func (c *Client) scopesSlice() []string {
	if len(c.Scopes) > 0 {
		return c.Scopes
	}
	return DefaultScopes
}

// parseIntervalHeader is a small helper reserved for future use if
// GitHub ever communicates interval/backoff via headers instead of the
// JSON body; kept here (rather than inlined) so pollOnce stays focused
// on the RFC 8628 state machine. Currently unused in the JSON-only flow
// but retained for symmetry with SaveToken's defensive style.
func parseIntervalHeader(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}
