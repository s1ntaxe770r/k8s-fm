/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	operatorsv1beta1 "github.com/s1ntaxe770r/k8s-fm/api/v1beta1"
	"github.com/s1ntaxe770r/k8s-fm/internal/spotify"
)

const conditionReady = "Ready"

// PlaylistReconciler reconciles a Playlist object
type PlaylistReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Spotify *spotify.Client

	// ResyncPeriod is how often a playlist is re-checked against Spotify, so
	// edits made in the Spotify app are reverted to match the spec.
	ResyncPeriod time.Duration
}

//+kubebuilder:rbac:groups=operators.gophers.jubril.xyz,resources=playlists,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operators.gophers.jubril.xyz,resources=playlists/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operators.gophers.jubril.xyz,resources=playlists/finalizers,verbs=update

// Reconcile makes the Spotify playlist match the Playlist spec: it creates the
// playlist if it doesn't exist yet, then syncs its details and tracks.
func (r *PlaylistReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var playlist operatorsv1beta1.Playlist
	if err := r.Get(ctx, req.NamespacedName, &playlist); err != nil {
		// Deleted CRs are left alone; the playlist stays on Spotify.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !playlist.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	remote, err := r.ensurePlaylist(ctx, &playlist)
	if err != nil {
		return r.fail(ctx, &playlist, "CreateFailed", err)
	}

	if err := r.syncDetails(ctx, &playlist, remote); err != nil {
		return r.fail(ctx, &playlist, "UpdateFailed", err)
	}

	count, err := r.syncTracks(ctx, &playlist)
	if err != nil {
		return r.fail(ctx, &playlist, "TrackSyncFailed", err)
	}

	playlist.Status.TrackCount = count
	playlist.Status.ObservedGeneration = playlist.Generation
	meta.SetStatusCondition(&playlist.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            "Playlist matches spec",
		ObservedGeneration: playlist.Generation,
	})
	if err := r.Status().Update(ctx, &playlist); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("playlist synced", "playlistID", playlist.Status.PlaylistID, "tracks", count)
	return ctrl.Result{RequeueAfter: r.ResyncPeriod}, nil
}

// ensurePlaylist returns the Spotify playlist for this CR, creating it when the
// CR has no playlist yet or the recorded one no longer exists.
func (r *PlaylistReconciler) ensurePlaylist(ctx context.Context, playlist *operatorsv1beta1.Playlist) (*spotify.Playlist, error) {
	if id := playlist.Status.PlaylistID; id != "" {
		remote, err := r.Spotify.GetPlaylist(ctx, id)
		if err == nil {
			return remote, nil
		}
		if !spotify.IsNotFound(err) {
			return nil, err
		}
		log.FromContext(ctx).Info("playlist no longer exists on Spotify, recreating", "playlistID", id)
	}

	remote, err := r.Spotify.CreatePlaylist(ctx, playlistName(playlist), playlist.Spec.Description, playlist.Spec.Public)
	if err != nil {
		return nil, err
	}

	// Record the ID straight away so a later failure doesn't create a duplicate.
	playlist.Status.PlaylistID = remote.ID
	playlist.Status.URL = remote.ExternalURLs["spotify"]
	if err := r.Status().Update(ctx, playlist); err != nil {
		return nil, err
	}

	log.FromContext(ctx).Info("created playlist", "playlistID", remote.ID)
	return remote, nil
}

// syncDetails updates the playlist's name, description and visibility if they
// differ from the spec.
func (r *PlaylistReconciler) syncDetails(ctx context.Context, playlist *operatorsv1beta1.Playlist, remote *spotify.Playlist) error {
	name := playlistName(playlist)
	if remote.Name == name && remote.Description == playlist.Spec.Description && remote.Public == playlist.Spec.Public {
		return nil
	}
	return r.Spotify.UpdateDetails(ctx, remote.ID, name, playlist.Spec.Description, playlist.Spec.Public)
}

// syncTracks replaces the playlist's tracks when they differ from the spec,
// keeping the spec's order. It returns the resulting track count.
func (r *PlaylistReconciler) syncTracks(ctx context.Context, playlist *operatorsv1beta1.Playlist) (int, error) {
	id := playlist.Status.PlaylistID

	current, err := r.Spotify.TrackIDs(ctx, id)
	if err != nil {
		return 0, err
	}

	desired := playlist.Spec.Tracks
	if desired == nil {
		desired = []string{}
	}
	if slices.Equal(current, desired) {
		return len(current), nil
	}

	first := desired[:min(len(desired), spotify.MaxTracksPerRequest)]
	if err := r.Spotify.ReplaceTracks(ctx, id, first); err != nil {
		return 0, err
	}
	for rest := desired[len(first):]; len(rest) > 0; {
		batch := rest[:min(len(rest), spotify.MaxTracksPerRequest)]
		if err := r.Spotify.AddTracks(ctx, id, batch); err != nil {
			return 0, err
		}
		rest = rest[len(batch):]
	}

	log.FromContext(ctx).Info("updated tracks", "playlistID", id, "before", len(current), "after", len(desired))
	return len(desired), nil
}

// fail records a failed sync on the Ready condition and returns the error so
// the request is retried with backoff.
func (r *PlaylistReconciler) fail(ctx context.Context, playlist *operatorsv1beta1.Playlist, reason string, err error) (ctrl.Result, error) {
	meta.SetStatusCondition(&playlist.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            err.Error(),
		ObservedGeneration: playlist.Generation,
	})
	if updateErr := r.Status().Update(ctx, playlist); updateErr != nil {
		log.FromContext(ctx).Error(updateErr, "unable to update status")
	}
	return ctrl.Result{}, err
}

func playlistName(playlist *operatorsv1beta1.Playlist) string {
	if playlist.Spec.Name != "" {
		return playlist.Spec.Name
	}
	return playlist.Name
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlaylistReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Status updates bump resourceVersion but not generation; skip them so
		// writing status doesn't trigger another round of Spotify calls.
		For(&operatorsv1beta1.Playlist{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
