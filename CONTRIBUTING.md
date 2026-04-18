# Contributing to Hijarr

## Contributor Agreement

By submitting a pull request, you irrevocably assign all copyright and related rights in your contribution to the project maintainer, and waive any moral rights you may hold in such contribution to the fullest extent permitted by applicable law. You represent that you are legally entitled to make this assignment and that your contribution is your original work.

If you are contributing on behalf of your employer, you confirm that your employer has authorized you to make this assignment.

---

## Scope

Contributions are welcome for the open-source modules:

- **hijarr-proxy**: DNS hijacking, subtitle aggregation, Sonarr sync
- **hijarr-uploader**: SRN upload SDK/CLI

The scraping and cleaning core (`hijarr-scraper`) is not open-source and does not accept external contributions.

---

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat:  new feature
fix:   bug fix
chore: CI, deps, config
docs:  documentation only
test:  tests only
```

## Submitting a PR

1. Branch off `main`
2. `CGO_ENABLED=0 go build ./...` — confirm clean build before and after
3. `CGO_ENABLED=0 go test ./...` — all tests must pass
4. Keep PRs focused — one concern per PR
