# Rule ID namespace policy for plugin authors

## Required formats

Rule IDs must be lowercase and use hyphen-delimited segments matching `[a-z0-9][a-z0-9-]*`.

Use one of these formats:

- Built-in rules: `core/<id>`
- External/plugin rules: `custom/<provider>/<id>`

Examples:

- `core/no-duplicate-node-ids`
- `core/max-fanout`
- `custom/acme/max-depth-guard`
- `custom/contoso/forbid-cross-team-links`

## Reserved prefixes and validation

- `core/` is reserved for built-ins and will be rejected for non-built-in rule IDs.
- `custom/` requires exactly two segments after the prefix: provider and rule id.
- Any other namespace prefix (for example `vendor/`) is rejected.

## Legacy migration compatibility

To avoid breaking existing integrations immediately, unnamespaced custom IDs are accepted during a transition window.

- Legacy accepted custom ID example: `max-depth-guard`
- Warning emitted: migrate to `custom/<provider>/<id>`
- Transition window removal target: `v1.4.0`

During the transition window, legacy custom IDs are canonicalized as `custom/legacy/<id>` for collision detection.
This prevents duplicate registrations across old/new forms.

## Collision behavior

Registration fails when two rules resolve to the same canonical ID.

Examples:

- `max-fanout` and `core/max-fanout` collide.
- `legacy-check` and `custom/legacy/legacy-check` collide.
- `custom/acme/check` and `custom/acme/check` collide.
