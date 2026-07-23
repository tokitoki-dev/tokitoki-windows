// Package apikey verifies an API key directly with the Tokitoki server.
//
// The key exists only in the request header and is never placed in a URL,
// error message, or stored field. The client carries no cookie jar and no
// cache, so a credential check cannot leave anything behind.
package apikey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const verifyTimeout = 15 * time.Second

// ErrUnavailable marks a check the server could not answer — network trouble,
// a 5xx, or a malformed response. The key may still be valid; try again.
var ErrUnavailable = errors.New("verification is temporarily unavailable")

// Verifier checks keys against one server.
type Verifier struct {
	baseURL string
	client  *http.Client
}

// NewVerifier creates a Verifier for the server at baseURL.
func NewVerifier(baseURL string) *Verifier {
	return &Verifier{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: verifyTimeout},
	}
}

// Verify reports whether the server accepts apiKey. False means the server
// rejected the key as invalid or revoked; every other failure is
// ErrUnavailable, because it says nothing about the key itself.
func (v *Verifier) Verify(ctx context.Context, apiKey string) (bool, error) {
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, v.baseURL+"/api/auth/api-key/verify", nil,
	)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := v.client.Do(request)
	if err != nil {
		return false, ErrUnavailable
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		var payload struct {
			Valid bool `json:"valid"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || !payload.Valid {
			return false, ErrUnavailable
		}
		return true, nil
	case http.StatusUnauthorized:
		return false, nil
	default:
		return false, ErrUnavailable
	}
}
