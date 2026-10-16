# k8s-fm

Manage Spotify playlists as Kubernetes resources. List track IDs in a
`Playlist` and the operator creates the playlist on Spotify if it doesn't
exist, then keeps its name, description, visibility and tracks (in order)
matching the spec.

```yaml
apiVersion: operators.gophers.jubril.xyz/v1beta1
kind: Playlist
metadata:
  name: road-trip
spec:
  name: Road Trip            # optional, defaults to metadata.name
  description: Managed by k8s-fm
  public: false
  tracks:                    # Spotify track IDs, in playlist order
  - 4uLU6hMCjMI75M1A2tKUQC
  - 7GhIk7Il098yCjg4BQjzvb
```

```sh
$ kubectl get playlists
NAME        PLAYLIST ID              TRACKS   READY   AGE
road-trip   3cEYpjA9oz9GiPac4AsH4n   2        True    1m
```

A track ID is the last part of a track link: `https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC`.

### How syncing works

- **Create:** the first reconcile creates the playlist for the authorized user
  and stores its ID in `status.playlistID`. If that playlist later disappears
  from Spotify, it is created again.
- **Update:** changes to `spec` are applied straight away. Every
  `--resync-period` (default 5m) the playlist is also re-checked, so tracks
  added, removed or reordered in the Spotify app are put back to match the spec.
- **Delete:** deleting the `Playlist` leaves the playlist on Spotify.

Failures show up on the `Ready` condition (`kubectl describe playlist <name>`).

## Spotify credentials

Creating playlists needs a token for your Spotify account, not just app
credentials, so you authorize once and give the operator a refresh token.

1. Create an app at https://developer.spotify.com/dashboard and add
   `http://127.0.0.1:8888/callback` as a Redirect URI.
2. Get a refresh token:

   ```sh
   SPOTIFY_ID=<client id> SPOTIFY_SECRET=<client secret> node hack/get-refresh-token.mjs
   ```

   Open the printed URL, approve, and copy the refresh token from the terminal.
3. For `make run`, put all three values in `.env` (see `.env.example`) and
   export them. For an in-cluster deploy, create the Secret the manager reads:

   ```sh
   kubectl create namespace k8s-fm-system
   kubectl -n k8s-fm-system create secret generic spotify-credentials \
     --from-literal=SPOTIFY_ID=<client id> \
     --from-literal=SPOTIFY_SECRET=<client secret> \
     --from-literal=SPOTIFY_REFRESH_TOKEN=<refresh token>
   ```

## Deploy with Helm

Create the namespace and the Secret holding your Spotify credentials (see
[Spotify credentials](#spotify-credentials) for how to get them):

```sh
kubectl create namespace k8s-fm
kubectl -n k8s-fm create secret generic spotify-credentials \
  --from-literal=SPOTIFY_ID=<client id> \
  --from-literal=SPOTIFY_SECRET=<client secret> \
  --from-literal=SPOTIFY_REFRESH_TOKEN=<refresh token>
```

Install the chart:

```sh
helm install k8s-fm charts/k8s-fm -n k8s-fm \
  --set spotify.existingSecret=spotify-credentials
kubectl -n k8s-fm rollout status deploy/k8s-fm
```

Other chart values: `resyncPeriod` (default `5m`), `image.tag`, and
`spotify.clientId` / `spotify.clientSecret` / `spotify.refreshToken` if you
want the chart to create the Secret instead.

Create a playlist:

```sh
kubectl apply -f - <<'EOF'
apiVersion: operators.gophers.jubril.xyz/v1beta1
kind: Playlist
metadata:
  name: friday-night
spec:
  name: Friday Night
  description: Deployed with k8s-fm
  tracks:
    - 0VjIjW4GlUZAMYd2vXMi3b   # Blinding Lights - The Weeknd
    - 3n3Ppam7vgaVa1iaRUc9Lp   # Mr. Brightside - The Killers
    - 2374M0fQpWi3dLnB54qaLX   # Africa - TOTO
EOF
kubectl wait playlist/friday-night --for=condition=Ready --timeout=60s
kubectl get playlist friday-night -o jsonpath='{.status.url}{"\n"}'
```

### Building the image

The Dockerfile is a multi-stage build that cross-compiles on the build machine,
so a multi-arch image needs no emulation:

```sh
docker buildx create --name k8s-fm --driver docker-container --use
docker buildx build --platform linux/amd64,linux/arm64 \
  -t ghcr.io/s1ntaxe770r/k8s-fm:0.1.0 --push .
```

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running locally

```sh
make install                      # install the CRD
set -a; source .env; set +a       # load Spotify credentials
make run
kubectl apply -f config/samples/
```

### Running on the cluster
1. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/k8s-fm:tag
```

2. Create the `spotify-credentials` Secret (see above), then deploy the controller with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/k8s-fm:tag
```

3. Create a playlist:

```sh
kubectl apply -f config/samples/
```

### Tests

```sh
go test ./internal/controller/ -run TestReconcile   # reconciler against a fake Spotify API
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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

