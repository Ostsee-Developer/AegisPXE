# GitLab migration and CI operation

AegisPXE is moving its canonical development and release workflow to the
self-managed GitLab instance at:

- Project: `https://git.night-hunter.net/ryder/AegisPXE`
- Releases: `https://git.night-hunter.net/ryder/AegisPXE/-/releases`

The migration is intentionally performed as a GitHub project import rather than
as a repository-only `git push --mirror`. The GitLab GitHub importer can retain
project metadata such as issues, pull requests/merge requests, comments,
releases, labels, milestones, wiki content, Git LFS objects and protected branch
rules in addition to Git branches and tags.

## Cutover sequence

1. Freeze destructive repository administration changes on GitHub for the
   duration of the import. Normal source commits may continue until the final
   cutover point is selected.
2. In GitLab, start **New project/repository -> Import project -> GitHub**.
3. Import `Ostsee-Developer/AegisPXE` into the `ryder` namespace as `AegisPXE`.
4. When offered, include Markdown attachments so release notes, issue text and
   review discussions do not retain GitHub-only attachment URLs.
5. Verify that `main`, all active development branches, tags, releases, issues
   and imported merge requests are present before declaring GitLab canonical.
6. Register the two AegisPXE runner classes described below.
7. Run the `.gitlab-ci.yml` pipeline manually once before enabling merge gates.
8. Protect `main` and release tags according to the project security policy.
9. Require the validation, test, security and package gates before merging.
10. Disable GitHub Actions only after the equivalent GitLab pipeline has passed.
11. Update public documentation and clone/release links to GitLab after the
    destination has been verified.
12. Optional: configure GitLab as a push mirror back to GitHub if a public
    read-only GitHub mirror is desired.

Do not delete the GitHub project immediately after cutover. Keep it available
until imported discussions, release assets and external links have been
verified.

## Runner layout

### `aegis-ci`

Purpose:

- project constitution checks
- `gofmt`
- normal Go tests
- race detector
- `go vet`
- `govulncheck`
- non-privileged build checks
- publishing release metadata and package-registry uploads

Recommended execution model:

- Docker executor
- unprivileged
- no Docker socket
- no host filesystem mounts
- no production credentials
- outbound access only as required for Go modules and the GitLab registry/API

Jobs select this runner through the `aegis-ci` tag.

### `aegis-release`

Purpose:

- Debian package construction
- package metadata verification
- package installation smoke test
- systemd/TFTP integration smoke test
- cross-architecture release package construction

This runner executes code that intentionally installs the freshly built
AegisPXE package and manipulates its local TFTP test state. It MUST therefore be
an isolated, dedicated runner and MUST NOT share a host with production
AegisPXE, GitLab, databases or unrelated workloads.

Recommended execution model:

- dedicated disposable VM where practical
- Debian-based host matching supported packaging assumptions
- Shell executor is acceptable only on that isolated host
- passwordless `sudo` is limited to the dedicated CI machine
- runner is protected for release use

Jobs select this runner through the `aegis-release` tag. The GitLab pipeline
also serializes package jobs through `resource_group: aegispxe-package-runner`
to avoid two package-install smoke tests mutating the same runner concurrently.

## Pipeline stages

The root `.gitlab-ci.yml` defines:

```text
validate
  constitution
  format_build

test
  unit_tests
  race_detector

security
  vet
  vulnerability_scan

package
  package_smoke
  release_build

release
  publish_packages
  publish_release
```

Normal package-smoke artifacts expire after one day. Release packages are
uploaded to the GitLab Generic Package Registry first and then linked from the
GitLab Release, so release downloads do not depend on temporary job-artifact
retention.

## Release contract

Release pipelines are tag-driven. A release tag must exactly match the project
`VERSION` value with a leading `v`.

Example:

```text
VERSION: 0.2.0-dev.1
Tag:     v0.2.0-dev.1
```

The release gate builds both supported Debian architectures:

```text
aegispxe_<version>_amd64.deb
aegispxe_<version>_arm64.deb
SHA256SUMS
```

These files are uploaded under the Generic Package Registry namespace
`aegispxe/<tag>/` and exposed as release assets.

## Security rules

- Secrets are never committed to `.gitlab-ci.yml`.
- Prefer built-in `$CI_JOB_TOKEN` access for same-project package publication.
- Any future external credentials belong in masked and protected GitLab CI/CD
  variables.
- The `aegis-release` runner must be protected before it receives release
  signing material or other privileged deployment credentials.
- Do not run arbitrary fork pipelines on the privileged package runner.
- Do not expose the Docker socket to the general CI runner.
- Preserve the AegisPXE rule that security-sensitive mutations remain
  auditable and fail closed.

## Final verification

GitLab becomes the source of truth only after all of the following are true:

```text
[ ] main imported
[ ] active branches imported
[ ] tags imported
[ ] release history imported
[ ] issues imported
[ ] pull requests represented as merge requests
[ ] comments/reviews present
[ ] protected branch rules reviewed
[ ] aegis-ci registered and green
[ ] aegis-release registered and green
[ ] package smoke green
[ ] release package publication tested
[ ] GitLab release links resolve
[ ] clone/release documentation points to git.night-hunter.net
```
