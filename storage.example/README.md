# storage.example

Everything the app accumulates lives in `storage/`, which is gitignored: notes,
saved requests, named places and the environments they resolve against all hold
credentials, internal hostnames and bearer tokens.

This folder is the checked-in template. To start from it:

```bash
cp -r storage.example storage
```

Then edit the files in `storage/`. What each is:

| | |
|---|---|
| `snippets/` | the Notes tab — any text files, in any folders you like |
| `requests/` | the API tab — one `.http` request per file |
| `places.json` | the Places tab — named paths |
| `macros.json` | the Macros tab — recorded keystroke sequences |
| `env.json` | the base layer of environments requests take their `{{vars}}` from |
| `requests/<project>/env.json` | that project's own values for the same stages |

### Environments

The *stage* is global and the *values* are per project. `Ctrl+E` cycles
`local -> sit -> uat -> prod`, which means "put the app on UAT" rather than
"switch this one project", and each project's `env.json` says what its own
`{{base}}` is on that stage. So the cycle stays four entries long however many
projects you have, instead of being the product of the two.

```
env.json                     shared across every project, per stage
requests/auth/env.json       the auth project's hosts, per stage
requests/auth/login.http     resolves against auth's file, then the base
requests/health.http         at the top of the tree, so the base alone
```

Resolution walks from the request's own folder up to here, first value found
winning, so a project overrides only the keys it needs to. `Alt+E` in the API
tab opens whichever file applies to the highlighted request -- and offers to
create one for a project that has none yet. An `env.json` is never listed as a
request.

Everything hot-reloads within a second, so these can be edited here or in the
palette and neither fights the other. `scripts/` is deliberately *not* in here:
it is half checked-in — the stubs, their fixtures and its README — which makes
it source rather than data.

One folder means one thing to back up, sync or put in a private repo.
