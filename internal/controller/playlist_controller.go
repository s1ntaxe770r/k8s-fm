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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorsv1beta1 "github.com/s1ntaxe770r/kates-playback/api/v1beta1"
	"github.com/zmb3/spotify"
)

// PlaylistReconciler reconciles a Playlist object
type PlaylistReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	sptfy  *spotify.Client
}

//+kubebuilder:rbac:groups=operators.gophers.jubril.xyz,resources=playlists,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operators.gophers.jubril.xyz,resources=playlists/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operators.gophers.jubril.xyz,resources=playlists/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Playlist object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *PlaylistReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	playlist := operatorsv1beta1.Playlist{}

	// Get Playlist
	if err := r.Get(ctx, req.NamespacedName, &playlist); err != nil {

		//Playlist notfound
		if err != nil && errors.IsNotFound(err) {
			log.Info("**** not found creating...")

			_, err := r.playlistFromCrd(playlist)
			// update playlist status
			playlist.Status.Message = "Created"
			playlist.Status.Reason = "Succesfully created playlist"
			r.Status().Update(ctx, &playlist)

			if err != nil {
				// update status on creation failure
				playlist.Status.Message = "Failed"
				playlist.Status.Reason = err.Error()
				updateErr := r.Status().Update(ctx, &playlist)
				if updateErr != nil {

					// requeue and try again
					log.Info("Error when updating status. Let's try again")
					return ctrl.Result{RequeueAfter: time.Second * 3}, updateErr
				}

				return ctrl.Result{}, err
			}
			r.Create(ctx, &playlist)
		}

		// Check for object deletetion
		if !playlist.ObjectMeta.DeletionTimestamp.IsZero() {

		}

	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlaylistReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv1beta1.Playlist{}).
		Complete(r)
}

// playlistFromCrd creates a playlist from the Playlist crd
func (r *PlaylistReconciler) playlistFromCrd(crd operatorsv1beta1.Playlist) (spotify.FullPlaylist, error) {
	log := log.FromContext(context.Background())
	user, err := r.sptfy.CurrentUser()
	if err != nil {
		log.Error(err, "unable to retrieve user info")
		return spotify.FullPlaylist{}, err
	}
	playlist, err := r.sptfy.CreatePlaylistForUser(user.ID, crd.ObjectMeta.Name, crd.Spec.Description, true)
	if err != nil {
		log.Error(err, "unable to create playlist", playlist.Name)
		return spotify.FullPlaylist{}, err

	}
	for _, track := range crd.Spec.Tracks {
		_, err = r.sptfy.AddTracksToPlaylist(playlist.ID, track)
		if err != nil {
			log.Error(err, "unable to add track to playlist", track)
			return spotify.FullPlaylist{}, err
		}
	}

	return *playlist, nil
}

func (r PlaylistReconciler) DeletePlaylist(crd operatorsv1beta1.Playlist) {
	log := log.FromContext(context.Background())

	user, err := r.sptfy.CurrentUser()
	if err != nil {
		log.Error(err, "unable to retrieve user info")
	}

	playlist := crd.ObjectMeta.Name

}
