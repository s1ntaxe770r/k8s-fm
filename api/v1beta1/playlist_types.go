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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlaylistSpec defines the desired state of a Spotify playlist
type PlaylistSpec struct {
	// Name of the playlist on Spotify. Defaults to metadata.name.
	// +optional
	Name string `json:"name,omitempty"`

	// +optional
	Description string `json:"description,omitempty"`

	// Public controls whether the playlist is visible on the owner's profile.
	// +optional
	Public bool `json:"public,omitempty"`

	// Tracks is the ordered list of Spotify track IDs (e.g. 4uLU6hMCjMI75M1A2tKUQC).
	// The playlist is kept in sync with this list.
	// +optional
	Tracks []string `json:"tracks,omitempty"`
}

// PlaylistStatus defines the observed state of Playlist
type PlaylistStatus struct {
	// PlaylistID is the Spotify ID of the playlist managed by this resource.
	// +optional
	PlaylistID string `json:"playlistID,omitempty"`

	// URL is the open.spotify.com link to the playlist.
	// +optional
	URL string `json:"url,omitempty"`

	// TrackCount is the number of tracks on the playlist after the last sync.
	// +optional
	TrackCount int `json:"trackCount,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Playlist ID",type=string,JSONPath=`.status.playlistID`
// +kubebuilder:printcolumn:name="Tracks",type=integer,JSONPath=`.status.trackCount`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Playlist is a Spotify playlist managed from Kubernetes
type Playlist struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlaylistSpec   `json:"spec,omitempty"`
	Status PlaylistStatus `json:"status,omitempty"`
}

// PlaylistList contains a list of Playlist
// +kubebuilder:object:root=true
type PlaylistList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Playlist `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Playlist{}, &PlaylistList{})
}
