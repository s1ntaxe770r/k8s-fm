// Package spotify is a small client for the Spotify Web API calls k8s-fm needs.
//
// It talks to the current playlist endpoints (POST /me/playlists and
// /playlists/{id}/items). The older /users/{id}/playlists and
// /playlists/{id}/tracks endpoints, which existing Go clients still use, return
// 403 for newer apps.
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

const (
	DefaultBaseURL = "https://api.spotify.com/v1"
	tokenURL       = "https://accounts.spotify.com/api/token"

	// MaxTracksPerRequest is the most tracks Spotify accepts in one add or
	// replace call.
	MaxTracksPerRequest = 100
)

// Client calls the Spotify Web API as a single user.
type Client struct {
	http    *http.Client
	baseURL string
}

// New returns a client that sends requests to baseURL using httpClient, which
// must add the user's access token.
func New(httpClient *http.Client, baseURL string) *Client {
	return &Client{http: httpClient, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// NewFromRefreshToken returns a client for the user who issued refreshToken.
// Access tokens are fetched and renewed automatically.
func NewFromRefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) *Client {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInHeader},
	}
	return New(config.Client(ctx, &oauth2.Token{RefreshToken: refreshToken}), DefaultBaseURL)
}

// Playlist is the subset of a Spotify playlist k8s-fm reads.
type Playlist struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Public       bool              `json:"public"`
	ExternalURLs map[string]string `json:"external_urls"`
}

// Error is an error response from the Spotify API.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("spotify: %d %s", e.Status, e.Message)
}

// IsNotFound reports whether err is a 404 from Spotify.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// GetPlaylist returns the playlist with the given ID.
func (c *Client) GetPlaylist(ctx context.Context, id string) (*Playlist, error) {
	var p Playlist
	if err := c.do(ctx, http.MethodGet, "/playlists/"+id, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePlaylist creates a playlist owned by the current user.
func (c *Client) CreatePlaylist(ctx context.Context, name, description string, public bool) (*Playlist, error) {
	body := map[string]any{"name": name, "description": description, "public": public}
	var p Playlist
	if err := c.do(ctx, http.MethodPost, "/me/playlists", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateDetails sets a playlist's name, description and visibility.
func (c *Client) UpdateDetails(ctx context.Context, id, name, description string, public bool) error {
	body := map[string]any{"name": name, "description": description, "public": public}
	return c.do(ctx, http.MethodPut, "/playlists/"+id, body, nil)
}

// TrackIDs returns the IDs of the items on a playlist, in order. Items that
// aren't tracks (episodes, local files, unavailable tracks) are returned as
// an empty string so they still count towards the playlist's contents.
func (c *Client) TrackIDs(ctx context.Context, id string) ([]string, error) {
	type page struct {
		Items []struct {
			Item *struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
		} `json:"items"`
		Next string `json:"next"`
	}

	ids := []string{}
	next := c.baseURL + "/playlists/" + id + "/items?limit=50&fields=items(item(id,type)),next"
	for next != "" {
		var p page
		if err := c.doURL(ctx, http.MethodGet, next, nil, &p); err != nil {
			return nil, err
		}
		for _, entry := range p.Items {
			if entry.Item == nil || entry.Item.Type != "track" {
				ids = append(ids, "")
				continue
			}
			ids = append(ids, entry.Item.ID)
		}
		next = p.Next
	}
	return ids, nil
}

// ReplaceTracks replaces every item on a playlist with the given tracks. At
// most MaxTracksPerRequest tracks can be sent; add the rest with AddTracks.
func (c *Client) ReplaceTracks(ctx context.Context, id string, trackIDs []string) error {
	return c.do(ctx, http.MethodPut, "/playlists/"+id+"/items", map[string]any{"uris": trackURIs(trackIDs)}, nil)
}

// AddTracks appends tracks to a playlist. At most MaxTracksPerRequest tracks
// can be sent per call.
func (c *Client) AddTracks(ctx context.Context, id string, trackIDs []string) error {
	return c.do(ctx, http.MethodPost, "/playlists/"+id+"/items", map[string]any{"uris": trackURIs(trackIDs)}, nil)
}

func trackURIs(ids []string) []string {
	uris := make([]string, len(ids))
	for i, id := range ids {
		uris[i] = "spotify:track:" + id
	}
	return uris
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doURL(ctx, method, c.baseURL+path, body, out)
}

func (c *Client) doURL(ctx context.Context, method, url string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &Error{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
		var parsed struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&parsed) == nil && parsed.Error.Message != "" {
			apiErr.Message = parsed.Error.Message
		}
		return apiErr
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
