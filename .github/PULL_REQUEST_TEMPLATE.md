## What

<!-- What does this PR do? Keep it brief. -->

## Why

<!-- Why is this change needed? Link related issues with `Fixes #123`. -->

## Testing

<!-- How did you test this? For Windows module changes, note whether you tested on real hardware. -->

## Checklist

- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] New modules implement both `Check()` and `Apply()`
- [ ] No `localhost` assumptions in runner code (always uses `Target` interface)
- [ ] Schema-breaking changes are documented
