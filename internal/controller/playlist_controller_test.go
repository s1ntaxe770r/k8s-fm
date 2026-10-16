package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorsv1beta1 "github.com/s1ntaxe770r/k8s-fm/api/v1beta1"
	"github.com/s1ntaxe770r/k8s-fm/internal/spotify"
)

// fakeSpotify is an in-memory stand-in for the parts of the Spotify Web API
// the reconciler uses. Like the real API, the old /users/{id}/playlists and
// /playlists/{id}/tracks endpoints return 403.
type fakeSpotify struct {
	mu        sync.Mutex
	server    *httptest.Server
	playlists map[string]*fakePlaylist
	created   int
}

type fakePlaylist struct {
	Name        string
	Description string
	Public      bool
	Tracks      []string
}

func newFakeSpotify(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{playlists: map[string]*fakePlaylist{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeSpotify) client() *spotify.Client {
	return spotify.New(f.server.Client(), f.server.URL)
}

func (f *fakeSpotify) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case parts[0] == "users" || (len(parts) == 3 && parts[2] == "tracks"):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"status": 403, "message": "Forbidden"}})

	case r.Method == http.MethodPost && r.URL.Path == "/me/playlists":
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Public      bool   `json:"public"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.created++
		id := fmt.Sprintf("pl%d", f.created)
		f.playlists[id] = &fakePlaylist{Name: body.Name, Description: body.Description, Public: body.Public}
		writeJSON(w, http.StatusCreated, f.playlistJSON(id))

	case len(parts) >= 2 && parts[0] == "playlists" && (len(parts) == 2 || parts[2] == "items"):
		p, ok := f.playlists[parts[1]]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"status": 404, "message": "Not found."}})
			return
		}
		switch {
		case len(parts) == 2 && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, f.playlistJSON(parts[1]))
		case len(parts) == 2 && r.Method == http.MethodPut:
			var body struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Public      bool   `json:"public"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			p.Name, p.Description, p.Public = body.Name, body.Description, body.Public
			w.WriteHeader(http.StatusOK)
		case len(parts) == 3 && r.Method == http.MethodGet:
			f.writeItems(w, r, parts[1], p)
		case len(parts) == 3 && (r.Method == http.MethodPut || r.Method == http.MethodPost):
			var body struct {
				URIs []string `json:"uris"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.URIs) > spotify.MaxTracksPerRequest {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"status": 400, "message": "too many"}})
				return
			}
			if r.Method == http.MethodPut {
				p.Tracks = nil
			}
			for _, u := range body.URIs {
				p.Tracks = append(p.Tracks, strings.TrimPrefix(u, "spotify:track:"))
			}
			writeJSON(w, http.StatusCreated, map[string]string{"snapshot_id": "s"})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeSpotify) playlistJSON(id string) map[string]any {
	p := f.playlists[id]
	return map[string]any{
		"id":            id,
		"name":          p.Name,
		"description":   p.Description,
		"public":        p.Public,
		"external_urls": map[string]string{"spotify": "https://open.spotify.com/playlist/" + id},
	}
}

func (f *fakeSpotify) writeItems(w http.ResponseWriter, r *http.Request, id string, p *fakePlaylist) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 100
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	end := min(offset+limit, len(p.Tracks))

	items := []map[string]any{}
	for _, track := range p.Tracks[min(offset, end):end] {
		items = append(items, map[string]any{"item": map[string]any{"type": "track", "id": track}})
	}
	var next any
	if end < len(p.Tracks) {
		next = fmt.Sprintf("%s/playlists/%s/items?offset=%d&limit=%d", f.server.URL, id, end, limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(p.Tracks), "limit": limit, "offset": offset, "next": next})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type reconcileEnv struct {
	spotify    *fakeSpotify
	client     client.Client
	reconciler *PlaylistReconciler
	key        types.NamespacedName
}

func newTestEnv(t *testing.T, spec operatorsv1beta1.PlaylistSpec) *reconcileEnv {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := operatorsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	playlist := &operatorsv1beta1.Playlist{
		ObjectMeta: metav1.ObjectMeta{Name: "road-trip", Namespace: "default"},
		Spec:       spec,
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(playlist).WithStatusSubresource(playlist).Build()
	fs := newFakeSpotify(t)
	return &reconcileEnv{
		spotify:    fs,
		client:     c,
		reconciler: &PlaylistReconciler{Client: c, Scheme: scheme, Spotify: fs.client()},
		key:        client.ObjectKeyFromObject(playlist),
	}
}

func (e *reconcileEnv) reconcile(t *testing.T) operatorsv1beta1.Playlist {
	t.Helper()
	if _, err := e.reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: e.key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return e.get(t)
}

func (e *reconcileEnv) get(t *testing.T) operatorsv1beta1.Playlist {
	t.Helper()
	var p operatorsv1beta1.Playlist
	if err := e.client.Get(context.Background(), e.key, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *reconcileEnv) updateSpec(t *testing.T, mutate func(*operatorsv1beta1.PlaylistSpec)) {
	t.Helper()
	p := e.get(t)
	mutate(&p.Spec)
	if err := e.client.Update(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileCreatesPlaylistWithTracks(t *testing.T) {
	env := newTestEnv(t, operatorsv1beta1.PlaylistSpec{Description: "songs", Tracks: []string{"a", "b", "c"}})

	got := env.reconcile(t)

	if got.Status.PlaylistID != "pl1" {
		t.Fatalf("playlistID = %q, want pl1", got.Status.PlaylistID)
	}
	if got.Status.URL != "https://open.spotify.com/playlist/pl1" {
		t.Errorf("url = %q", got.Status.URL)
	}
	if got.Status.TrackCount != 3 {
		t.Errorf("trackCount = %d, want 3", got.Status.TrackCount)
	}
	if !meta.IsStatusConditionTrue(got.Status.Conditions, conditionReady) {
		t.Errorf("Ready condition not true: %+v", got.Status.Conditions)
	}
	remote := env.spotify.playlists["pl1"]
	if remote.Name != "road-trip" || remote.Description != "songs" {
		t.Errorf("remote playlist = %+v, want name from metadata.name", remote)
	}
	if !slices.Equal(remote.Tracks, []string{"a", "b", "c"}) {
		t.Errorf("remote tracks = %v", remote.Tracks)
	}
}

func TestReconcileSyncsSpecChangesWithoutDuplicating(t *testing.T) {
	env := newTestEnv(t, operatorsv1beta1.PlaylistSpec{Tracks: []string{"a", "b"}})
	env.reconcile(t)

	env.updateSpec(t, func(s *operatorsv1beta1.PlaylistSpec) {
		s.Name = "Road Trip"
		s.Public = true
		s.Tracks = []string{"c", "a"}
	})
	got := env.reconcile(t)

	if env.spotify.created != 1 {
		t.Fatalf("created %d playlists, want 1", env.spotify.created)
	}
	remote := env.spotify.playlists[got.Status.PlaylistID]
	if remote.Name != "Road Trip" || !remote.Public {
		t.Errorf("details not synced: %+v", remote)
	}
	if !slices.Equal(remote.Tracks, []string{"c", "a"}) {
		t.Errorf("remote tracks = %v, want [c a]", remote.Tracks)
	}
}

func TestReconcileRevertsChangesMadeOnSpotify(t *testing.T) {
	env := newTestEnv(t, operatorsv1beta1.PlaylistSpec{Tracks: []string{"a", "b"}})
	env.reconcile(t)

	env.spotify.playlists["pl1"].Tracks = []string{"b", "x"}
	env.reconcile(t)

	if got := env.spotify.playlists["pl1"].Tracks; !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("remote tracks = %v, want [a b]", got)
	}
}

func TestReconcileRecreatesPlaylistDeletedOnSpotify(t *testing.T) {
	env := newTestEnv(t, operatorsv1beta1.PlaylistSpec{Tracks: []string{"a"}})
	env.reconcile(t)

	delete(env.spotify.playlists, "pl1")
	got := env.reconcile(t)

	if got.Status.PlaylistID != "pl2" {
		t.Fatalf("playlistID = %q, want recreated pl2", got.Status.PlaylistID)
	}
	if tracks := env.spotify.playlists["pl2"].Tracks; !slices.Equal(tracks, []string{"a"}) {
		t.Errorf("remote tracks = %v", tracks)
	}
}

func TestReconcileHandlesMoreThan100Tracks(t *testing.T) {
	tracks := make([]string, 250)
	for i := range tracks {
		tracks[i] = fmt.Sprintf("t%d", i)
	}
	env := newTestEnv(t, operatorsv1beta1.PlaylistSpec{Tracks: tracks})

	got := env.reconcile(t)

	if !slices.Equal(env.spotify.playlists["pl1"].Tracks, tracks) {
		t.Fatalf("remote has %d tracks, want all 250 in order", len(env.spotify.playlists["pl1"].Tracks))
	}
	if got.Status.TrackCount != 250 {
		t.Errorf("trackCount = %d", got.Status.TrackCount)
	}

	// A second pass reads all pages, sees no difference and changes nothing.
	env.spotify.playlists["pl1"].Tracks = append([]string(nil), tracks...)
	env.reconcile(t)
	if env.spotify.created != 1 || len(env.spotify.playlists["pl1"].Tracks) != 250 {
		t.Errorf("second reconcile changed the playlist")
	}
}

func TestReconcileReportsSpotifyErrors(t *testing.T) {
	env := newTestEnv(t, operatorsv1beta1.PlaylistSpec{Tracks: []string{"a"}})
	env.spotify.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"status": 401, "message": "The access token expired"}})
	})

	_, err := env.reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: env.key})
	if err == nil {
		t.Fatal("expected an error")
	}
	cond := meta.FindStatusCondition(env.get(t).Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "CreateFailed" {
		t.Errorf("Ready condition = %+v, want False/CreateFailed", cond)
	}
}
