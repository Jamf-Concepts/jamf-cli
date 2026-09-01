---
title: "Two specs tagging a resource the same name merged into one command, and the generator exited 0"
date: 2026-09-01
category: logic-errors
module: generator/parser
problem_type: logic-error
severity: high
applies_when:
  - "Ingesting a new platform spec into a gateway namespace that already has one"
  - "A generated command grew operations that belong to a different service"
  - "Deciding how fine-grained a code-generation override key needs to be"
tags:
  - platform-gateway
  - code-generation
  - naming-collision
  - silent-failure
---

## What happened

SDK build v1993 published `securitycloud_enrollment_api.json` for the first time:
six operations over Security Cloud enrollment activation profiles, tagged
`activation-profiles`.

`securitycloud_uem_connect_api.json` already tags a resource `activation-profiles`
— one operation, `deploy-to-uem`, which deploys a profile code to a UEM. That tag
is a bare noun, so it carries an entry in `platformResourceNameOverrides`:

```go
"securitycloud/activation-profiles": "uem-activation-profiles",
```

The key is `{service}/{name}`, and the service is read off `servers[0].url`. Both
specs declare `https://{region}.api.jamfcloud.com/securitycloud`, so **both tags
resolved to the same key** and both resources were renamed
`uem-activation-profiles`. `generator/platform`'s `mergeInto` then folded two
same-named resources into one, exactly as designed — it is what puts a device's
groups on `platform-devices` from a second spec.

The result: `security uem-activation-profiles` grew to seven subcommands, six of
them the enrollment API's (`list`, `create`, `get`, `pause`, `resume`,
`delete-multiple`), filed under "UEM Connect:" in `security --help`, named after a
service that does not own them, and reached under a resource whose only real
operation is a deploy. Nothing in the enrollment API was missing; it was
mislabelled.

`make generate` exited 0. `make test` passed everything except two count-based
fixtures, which reported a *shortfall* — the honest reading of which is "a tag is
unwired", not "two tags merged".

## Why it was invisible

1. **The merge is a designed behaviour, not an error path.** Two specs
   contributing one resource is how `platform-devices` gets its device-groups
   operation. There is no signal to raise, because the same code path is correct
   most of the time.
2. **The one guard that could have caught it doesn't apply.**
   `checkOperationNameCollisions` rejects a resource carrying two operations of the
   same *name*, and its doc comment names this exact scenario as the thing it
   protects against. But `deploy-to-uem` collides with none of the six enrollment
   operation names, so the merged resource is perfectly well-formed Go and
   perfectly valid cobra. The guard catches the compile-breaking shape of the
   problem and is blind to the mislabelling shape.
3. **A service is not fine-grained enough to name a resource within it, and
   nothing said so.** The override table's key was documented as
   `{service}/{name}` with the service being "the gateway namespace from the
   spec's `servers[0].url`" — which reads as specific, and is, until a second spec
   shares the namespace. Jamf publishes Security Cloud as one namespace with
   several sub-namespaces beneath it (`securitycloud/uem-connect`,
   `securitycloud/…`), so the collision was always available.

## The fix

Add a more specific key level rather than renaming around the collision. Keys are
now tried most-specific first:

1. `{namespace}/{name}` — everything before the version segment of the resource's
   own (already normalised) paths: `securitycloud/uem-connect` versus
   `securitycloud`.
2. `{service}/{name}` — the namespace from `servers[0].url`, as before.
3. `{name}` — matches any service.

The five UEM Connect entries moved to the namespace form. DNS, ZTNA and content
categories keep the service form, because their paths put the version ahead of the
sub-namespace (`/securitycloud/v1/dns/zones`) and their namespace *is*
`securitycloud`. Enrollment takes the vacated service key and becomes
`enrollment-activation-profiles`.

`platformNamespace` (`generator/parser/platform.go`) is the parser-side twin of
the emitter's `namespaceFromPath`, which already keys `platformTableColumns` and
`platformNameLookupFields` the same way. It is duplicated rather than shared
because `generator/platform` imports the parser, not the reverse.

## The test

`TestTwoSpecsSharingATagGetDistinctResourceNames` asserts the resulting **names
and paths** — that `uem-activation-profiles` carries only
`/securitycloud/uem-connect/v1/…` paths and `enrollment-activation-profiles` only
`/securitycloud/v1/…` ones. Not the absence of an error: absence of an error was
the symptom, and a test that only checks `ParsePlatformSpec` returns nil would
have passed throughout.

It also fails when an override *value* stops naming any shipped resource, which is
the other way an entry goes stale: a key pinned to a namespace renames nothing at
all once that namespace moves, and reports nothing when it stops matching.

## The general lesson

When a generator merges by a derived key, the key's granularity is a correctness
property, not a naming convenience. Ask what happens when two inputs derive the
same key — and if the answer is "they merge, and the merge is legal", the guard
you have is checking the wrong failure. Assert the identity of what came out, not
that nothing complained.
