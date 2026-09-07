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
| `env.json` | the environments requests take their `{{vars}}` from |

Everything hot-reloads within a second, so these can be edited here or in the
palette and neither fights the other. `scripts/` is deliberately *not* in here:
it is half checked-in — the stubs, their fixtures and its README — which makes
it source rather than data.

One folder means one thing to back up, sync or put in a private repo.
