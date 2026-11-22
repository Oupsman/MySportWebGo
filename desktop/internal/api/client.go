// Package api uploads the activities to MySportWeb with a device token
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"MySportWebSync/internal/health"
)

// TokenPrefix starts the device tokens of MySportWeb
const TokenPrefix = "msw_"

// ErrAuth is a token refused by the server: missing, expired, revoked, or lacking activities:create.
// Nothing can be uploaded until the settings change.
var ErrAuth = errors.New("the server refused the device token")

// BusyError is the server analysing too many activities at once (503): the same file can be sent again
// after RetryAfter
type BusyError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *BusyError) Error() string {
	return "the server is busy: " + e.Message
}

// defaultRetryAfter is the wait of a busy server that doesn't say how long
const defaultRetryAfter = 30 * time.Second

type Status int

const (
	Uploaded  Status = iota
	Duplicate        // the server already has the activity
	Rejected         // the server refused the file, sending it again changes nothing
)

type UploadResult struct {
	Status     Status
	ActivityID string
	Message    string
}

type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

func New(serverURL, token string) (*Client, error) {
	serverURL = strings.TrimSpace(serverURL)
	base, err := url.Parse(strings.TrimRight(serverURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("the server URL must look like https://mysportweb.example.com")
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) {
		return nil, fmt.Errorf("the token must be a device token, starting with %s", TokenPrefix)
	}
	// the server analyses each activity and draws its thumbnail: an upload takes seconds
	return &Client{base: base, token: token, http: &http.Client{Timeout: 3 * time.Minute}}, nil
}

func (c *Client) endpoint(p string) string {
	return c.base.JoinPath(p).String()
}

// ErrNoTokenRoute is a server older than /api/v1/token: the token is only checked by the uploads
var ErrNoTokenRoute = errors.New("the server can't check the token, it is checked at the first upload")

// TokenInfo describes a device token, as the server knows it
type TokenInfo struct {
	Name        string    `json:"name"`
	Hint        string    `json:"hint"`
	Permissions []string  `json:"permissions"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// CanUpload tells whether the token holds the permission of the uploads
func (t TokenInfo) CanUpload() bool {
	return slices.Contains(t.Permissions, "activities:create")
}

// CheckToken returns the token as the server knows it, ErrAuth when it refuses it
func (c *Client) CheckToken(ctx context.Context) (TokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/v1/token"), nil)
	if err != nil {
		return TokenInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return TokenInfo{}, err
	}
	defer resp.Body.Close()
	var answer struct {
		DeviceToken TokenInfo `json:"device_token"`
		Message     string    `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&answer)
	switch resp.StatusCode {
	case http.StatusOK:
		return answer.DeviceToken, nil
	case http.StatusNotFound:
		return TokenInfo{}, ErrNoTokenRoute
	case http.StatusUnauthorized, http.StatusForbidden:
		return TokenInfo{}, fmt.Errorf("%w: %s", ErrAuth, answer.Message)
	default:
		return TokenInfo{}, fmt.Errorf("the server answered %s", resp.Status)
	}
}

// Ping checks the server answers, without the token
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/v1/healthcheck"), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the server answered %s, is it the URL of MySportWeb?", resp.Status)
	}
	return nil
}

// Upload sends a FIT file. item and count number the files of a synchronization: the server resets
// the "last import" mark of the activities at the first one.
func (c *Client) Upload(ctx context.Context, name string, content []byte, item, count int) (UploadResult, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		return UploadResult{}, err
	}
	part.Write(content)
	form.WriteField("item", strconv.Itoa(item))
	form.WriteField("count", strconv.Itoa(count))
	if err := form.Close(); err != nil {
		return UploadResult{}, err
	}

	// /activity is the route of the API clients, the frontend uses /activities/upload
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/v1/activity"), &body)
	if err != nil {
		return UploadResult{}, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return UploadResult{}, err
	}
	defer resp.Body.Close()

	var answer struct {
		ID      string `json:"id"`
		Error   string `json:"error"`
		Message string `json:"message"` // the errors of the authentication
	}
	json.NewDecoder(resp.Body).Decode(&answer)
	message := answer.Error
	if message == "" {
		message = answer.Message
	}
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		return UploadResult{Status: Uploaded, ActivityID: answer.ID}, nil
	case resp.StatusCode == http.StatusConflict:
		return UploadResult{Status: Duplicate, ActivityID: answer.ID, Message: message}, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return UploadResult{}, fmt.Errorf("%w: %s", ErrAuth, message)
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusRequestEntityTooLarge:
		return UploadResult{Status: Rejected, Message: message}, nil
	case resp.StatusCode == http.StatusServiceUnavailable:
		wait := defaultRetryAfter
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
			wait = time.Duration(seconds) * time.Second
		}
		return UploadResult{}, &BusyError{RetryAfter: wait, Message: message}
	default:
		// the server or a proxy fails: the file is tried again at the next synchronization
		return UploadResult{}, fmt.Errorf("the server answered %s %s", resp.Status, message)
	}
}

// MaxTitleLength is the longest title the server takes, in characters
const MaxTitleLength = 200

// SetTitle renames an activity, and changes nothing else. ErrAuth when the token lacks activities:write.
func (c *Client) SetTitle(ctx context.Context, activityID, title string) error {
	if runes := []rune(strings.TrimSpace(title)); len(runes) > MaxTitleLength {
		title = string(runes[:MaxTitleLength])
	}
	body, err := json.Marshal(map[string]string{"title": title})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint("/api/v1/activity/"+url.PathEscape(activityID)+"/title"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var answer struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&answer)
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrAuth, answer.Message)
	case http.StatusNotFound:
		// a server older than the route, or the activity deleted since
		return fmt.Errorf("the server can't rename the activity (%s), is it up to date?", answer.Error)
	default:
		return fmt.Errorf("the server answered %s %s", resp.Status, answer.Error)
	}
}

// CanRename tells whether the token holds the permission of the titles imported with the activities
func (t TokenInfo) CanRename() bool {
	return slices.Contains(t.Permissions, "activities:write")
}

// healthBatch is the number of measures of an import request, the server takes up to 5000
const healthBatch = 1000

// HealthRejection is a measure the server refused, Index in the measures given to ImportHealth
type HealthRejection struct {
	Index int    `json:"index"`
	Error string `json:"error"`
}

// HealthImport is what the server did with the measures
type HealthImport struct {
	Created   int               `json:"created"`
	Merged    int               `json:"merged"`    // values added to a measure of the same date
	Unchanged int               `json:"unchanged"` // already imported
	Rejected  []HealthRejection `json:"rejected"`
}

// ImportHealth sends measures, by batches. The server completes the measures of the same date instead
// of duplicating them: an import can be sent again. progress receives the measures sent.
func (c *Client) ImportHealth(ctx context.Context, measures []health.Measure, progress func(done, total int)) (HealthImport, error) {
	var total HealthImport
	for start := 0; start < len(measures); start += healthBatch {
		batch := measures[start:min(start+healthBatch, len(measures))]
		result, err := c.importHealthBatch(ctx, batch)
		if err != nil {
			return total, err
		}
		total.Created += result.Created
		total.Merged += result.Merged
		total.Unchanged += result.Unchanged
		for _, r := range result.Rejected {
			total.Rejected = append(total.Rejected, HealthRejection{Index: start + r.Index, Error: r.Error})
		}
		if progress != nil {
			progress(start+len(batch), len(measures))
		}
	}
	return total, nil
}

func (c *Client) importHealthBatch(ctx context.Context, measures []health.Measure) (HealthImport, error) {
	body, err := json.Marshal(map[string][]health.Measure{"measures": measures})
	if err != nil {
		return HealthImport{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/api/v1/healthdatas/import"), bytes.NewReader(body))
	if err != nil {
		return HealthImport{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return HealthImport{}, err
	}
	defer resp.Body.Close()
	var answer struct {
		HealthImport
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&answer)
	switch {
	case resp.StatusCode == http.StatusOK:
		return answer.HealthImport, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return HealthImport{}, fmt.Errorf("%w: %s", ErrAuth, answer.Message)
	case resp.StatusCode == http.StatusNotImplemented || resp.StatusCode == http.StatusUnsupportedMediaType:
		return HealthImport{}, errors.New("the server is too old to import health measures, update MySportWeb")
	default:
		message := answer.Error
		if message == "" {
			message = answer.Message
		}
		return HealthImport{}, fmt.Errorf("the server answered %s %s", resp.Status, message)
	}
}

// CanImportHealth tells whether the token holds the permission of the health imports
func (t TokenInfo) CanImportHealth() bool {
	return slices.Contains(t.Permissions, "healthdatas:create")
}
