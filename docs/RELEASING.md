# Releasing

There are no published binary releases yet.

Current install path:

```bash
go install github.com/kierandotai/pp-osrs-ge/cmd/osrs-ge@latest
```

## First Release Checklist

1. Confirm the working tree is clean.
2. Run the full local check:

   ```bash
   make check
   ```

3. Create a version tag:

   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

4. Create a GitHub release from the tag.

## Binary Releases

When binary distribution matters, add GoReleaser rather than hand-building
archives. A minimal release setup should:

- build `./cmd/osrs-ge`
- target macOS, Linux, and Windows
- include `README.md`, `LICENSE`, and docs in source archives
- run only on version tags such as `v0.1.0`
- keep the normal CI workflow as the required pre-merge signal

Do not publish binaries that automate gameplay, place offers, or require account
credentials. This project is market research tooling only.

## Branch Protection

For a solo repo, branch protection is optional. Before adding collaborators,
enable protection on `main` and require the CI workflow to pass before merging.

