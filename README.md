# Enkit (engineering toolkit)

## Importing into a downstream Bazel workspace

`enkit` is now consumed as a normal Bzlmod dependency. There is no local
registry to configure and no `go_repositories` macro to call from a downstream
repo.

The recommended pattern is:

```starlark
module(name = "myproj")

bazel_dep(name = "enkit", version = "0.0.0")
git_override(
    module_name = "enkit",
    remote = "https://github.com/ccontavalli/enkit.git",
    commit = "<pin a commit or tag>",
)
```

For local development against a checkout, replace the `git_override(...)` with
`local_path_override(...)`.

## Adding a Go module dependency

Use the Bazel-managed Go tool, not raw `go get` and not `@gosdk//...` labels:

```bash
bazel run @rules_go//go -- <go command>
```

When adding or updating a Go module dependency in this repo:

1. Update `go.mod`.

   Common commands:

   Add or pin a dependency:

   ```bash
   bazel run @rules_go//go -- get example.com/module@version
   ```

   Update one dependency to its latest version:

   ```bash
   bazel run @rules_go//go -- get example.com/module@latest
   ```

   Inspect available updates:

   ```bash
   bazel run @rules_go//go -- list -m -u all
   ```

   Update dependencies used by packages in this module:

   ```bash
   bazel run @rules_go//go -- get -u ./...
   ```

1. Sync Bazel metadata:

   ```bash
   bazel run //tools/go:update_go_deps
   ```

   This runs:

   - `bazel run @rules_go//go -- mod tidy -v`
   - `bazel run //:gazelle`
   - `bazel mod tidy`

1. Review the resulting changes to `go.mod`, `go.sum`, `MODULE.bazel.lock`,
   and any BUILD files updated by Gazelle.

If you want a safer bulk refresh, prefer:

```bash
bazel run @rules_go//go -- get -u=patch ./...
```

`-u=patch` means “upgrade only within the current minor line”. For example,
`v1.2.3` may move to `v1.2.9`, but not to `v1.3.0`. That usually causes less
churn and is the better default when you want to update broadly without taking
on unnecessary minor-version changes.

Use `bazel run //:gazelle` by itself only when you need to regenerate BUILD
files after source layout or import changes. Do not use `gazelle update-repos`;
this repo no longer uses that workflow.

## Testing

### Setting up for tests

#### Install non-bazel managed dependencies

1. `google-cloud-sdk`

   - Install here https://cloud.google.com/sdk/docs/install

     PLEASE NOTE: do not install using snap/brew/apt-get etc., as emulators do
     not work.

   - Run the following command to get access to the emulators:

     ```
     gcloud components install beta
     ```

   - Add the gcloud binary to the local binaries directory with the following
     symlink:

     ```
     ln -s $(which gcloud) /usr/local/bin
     ```

1. Get a service account from \<x, Y, Z person>

   - Put it in `//astore/testdata/credentials.json`

### Examples of Running Tests

- Running a specific go test target

  ```
  bazel test //astore:go_default_test
  ```

- Running specific test of a test file

  ```
  bazel test //astore:go_default_test --test_filter=^TestServer$
  ```

- Running Everything

  ```
  bazel test //...
  ```

- Remove all emulator spawned processes

  Sometimes emulator processes can be left behind after a test run. These can
  be cleaned up with:

  ```
  ps aux | grep gcloud/emulators/datastore | awk '{print $2}' | xargs kill
  ```

### Adding Tests

1. Create the test in `* \_test.go`

1. Run Gazelle:

   ```
   bazel run //:gazelle
   ```

1. If your test needs server dependencies, such as astore or minio, add the
   attribute `local = True` to the test rule.
