# Flux / GitOps deployment

This app is built for a GitOps flow: **Conventional Commits → release-please →
semver release → CI builds the image → Flux rolls it out**, with secrets sealed
via SOPS and quote/skit content served from ConfigMaps for live `!reload`.

## How it fits together

1. **Releases.** Every commit here uses Conventional Commits + DCO sign-off.
   `release-please` (`.github/workflows/release.yml`) keeps a release PR open;
   merging it tags a semver release.
2. **Image.** On release, CI builds and pushes
   `ghcr.io/mrcupp/annoybots:<version>` (+ `latest`).
3. **Rollout.** Flux's `ImagePolicy` (semver range, `image-automation.yaml`)
   picks up the new version and `ImageUpdateAutomation` commits the new tag into
   the overlay's `images:` marker, which the `Kustomization` then applies.

## What to put where

These manifests are examples for your **cluster-management** repo (the one Flux
already reconciles). You likely already have a `GitRepository` and possibly an
`ImageUpdateAutomation`; reuse them and adjust names.

- `kustomizations.yaml` — one Flux `Kustomization` per bot + Redis. They set
  `targetNamespace: annoybots`, `decryption.provider: sops`, and point `path:` at
  this repo's overlays. (Flux's kustomize-controller builds with the load
  restrictor disabled, so the overlays' `../../../../configs` / `data` references
  resolve.)
- `image-automation.yaml` — `ImageRepository` + `ImagePolicy` (+ an example
  `ImageUpdateAutomation`).

## Secrets (SOPS)

Secrets live as `deploy/k8s/overlays/<bot>/secret.sops.yaml`, committed
**encrypted**. Set up once:

```sh
# Generate a cluster age key (private key goes into flux-system as `sops-age`):
age-keygen -o age.agekey
kubectl -n flux-system create secret generic sops-age --from-file=age.agekey

# Put the PUBLIC key in .sops.yaml (replace the REPLACEME recipient), then:
sops --encrypt --in-place deploy/k8s/overlays/arywen/secret.sops.yaml
sops --encrypt --in-place deploy/k8s/overlays/kurkutu/secret.sops.yaml
git commit -s -am "chore: encrypt bot secrets"
```

Flux decrypts them at apply time using the `sops-age` Secret.

## Content updates without a rollout

Quote packs (`data/quotes/*.txt`) and skits (`data/skits.yaml`) are mounted from
fixed-name ConfigMaps. Edit them in Git → Flux updates the ConfigMap → the
kubelet syncs the files into the running pods → DM a bot `!reload` and the new
content is live. No image rebuild, no restart. (Changing `configs/<bot>.yaml`
— networks/personality — still rolls the pod, which is what you want.)
