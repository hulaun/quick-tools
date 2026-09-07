# Writing transforms

Every `.js` file in this folder becomes an entry in the palette. Save the file
and it reloads within a second — no rebuild, no restart.

## The format

```js
var name  = "Json to js object";  // shown in the palette. Defaults to the filename.
var group = "Json";               // the prefix, as in "Json: Json to js object".
var tags  = ["js", "literal"];    // extra words the search should match.

function transform(input) {
    return input.toUpperCase();   // input is the clipboard; return the result
}
```

This is **not** ES module syntax. The engine is
[goja](https://github.com/dop251/goja), which implements ECMAScript 5.1 plus
parts of later editions but does not parse `export` in an ordinary script.
Plain globals and a `transform` function are what work.

Scripts get the JavaScript standard library — `JSON`, `String`, `RegExp`,
`Math`, `Array` — and nothing else. No filesystem, no network. A transform that
needs those belongs in Go.

Each run is cut off after **2 seconds**. An accidental `while (true)` while
you are mid-edit shows a timeout error instead of freezing the palette.

## Testing

Put a `<name>.test.json` beside the script:

```json
[
  { "name": "simple",       "in": "a=1",  "out": "{\n  \"a\": \"1\"\n}" },
  { "name": "rejects junk", "in": "",     "error": true }
]
```

`out` is compared exactly. `"error": true` means the case is expected to throw —
a parser has to *reject* some inputs, and that deserves pinning down as much as
a success does.

```
quicktools.exe -test-scripts
```

Prints a pass/fail line per case and a diff for each failure, with whitespace
made visible. Exits non-zero if anything fails, so it drops into a watch loop.

### Running one thing

`-only <text>` narrows it to the scripts and cases whose name contains that
text, matched without regard to case:

```
quicktools.exe -test-scripts -only java          # one script, all its cases
quicktools.exe -test-scripts -only "static"      # one case, wherever it lives
quicktools.exe -test-scripts -only "run of capitals"
```

A match on the **filename** keeps every case in that file; otherwise **case
names** are matched one at a time, and a script with nothing matching is not
printed at all. `-only` with nothing matching exits non-zero and says so, rather
than reporting a green run of zero cases.

`-only` also turns off the 200-character limit on the values it prints. The
unfiltered run keeps lines short so one long failure does not push the other
scripts off the screen; once you have narrowed to a case, reading the whole
value is the entire point.

To see what a case is asking for without running anything, the fixture file is
the spec — `"in"` and `"out"` are the exact strings, with `\n` for the newlines:

```bash
grep -A3 '"the class from the readme"' scripts/java-to-json.test.json
```

---

# The stubs

Six scripts are yours to write. All have fixtures and all are currently red.

They ladder in difficulty — doing them roughly in this order means each one
uses a skill the previous one built:

| # | Script | Teaches |
|---|---|---|
| 1 | `query-string-to-json.js` | normalising messy input; repeated keys becoming arrays |
| 2 | `json-to-query-string.js` | the reverse — flattening a tree into one key per leaf |
| 3 | `json-to-insert-query.js` | writing for a target with its own quoting rules |
| 4 | `json-to-update-query.js` | the same, plus one genuinely ambiguous choice |
| 5 | `js-object-to-json.js` | recursive descent over a bracketed grammar |
| 6 | `json-to-js.js` | the reverse, and why `JSON.parse` cannot be used |

**`java-to-json.js` is written**, and is the worked example to copy the shape
from. It is the same scan-and-decide structure the rest want: consume the things
that can hide a delimiter (comments, string literals, annotations) inside the
scanner so nothing downstream has to think about them, keep one counter for
depth, and build the result as a value that `JSON.stringify` renders rather than
as text you concatenate.

None of these duplicate a built-in, so nothing stops working while they are
unwritten — each is a new capability the tool does not have yet.

Three of them are pairs pointing in opposite directions: query string, SQL
values, and JS literal. Write a pair together. Reading a format teaches you
where its edges are, and the fixtures of one direction are largely the outputs
of the other, so the second of a pair is always faster than the first.

## Shared rules

**Output that is JSON** is always `JSON.stringify(value, null, 2)` — two-space
indent.

**Numbers pass through as written.** `1.50` comes out `1.50`, not `1.5`, and
`9007199254740993` comes out unchanged rather than rounded to
`...992`. Every Java `Long` id lives in the range where a JavaScript number
silently loses the last digit, and there is no way to get it back once lost. So
in the four transforms that read JSON and write something else, a number is
carried from input to output as the text it was written as. Fixtures pin this in
each of them; it is the one rule that shapes how you read the input.

**Empty input is an error** everywhere. A transform previews against whatever
happens to be on the clipboard, and "" should say so rather than produce `{}`.

---

## 1. `java-to-json.js`

A Java class in, a JSON skeleton out. Every field becomes a key; every value is
`""`.

```java
public class SysMenu {
    private static final long serialVersionUID = 1L;

    /** Menu ID */
    private Long menuId;

    /** Menu name */
    private String menuName;
}
```

```json
{
  "menuId": "",
  "menuName": ""
}
```

The field's Java type is discarded along with the class name and the comments.
This produces a body to fill in by hand, not a conversion of data — mapping
`Long` to `0` and `String` to `""` would only mean deleting two kinds of
placeholder instead of one.

Keys come out in **declaration order**.

### What counts as a field

A Java file is full of lines ending in a semicolon and only some of them are
fields. What the fixtures pin:

| Input | Why |
|---|---|
| `private static final String KIND = "x";` | **skipped** — a constant is not data |
| `public String getName() { return name; }` | **skipped** — a `(` before the `;` |
| `this.name = name;` inside a setter | **skipped** — brace depth 2, not 1 |
| `@TableId(value = "menu_id")` | **skipped**, and its `(` is not a method |
| `private Map<String, Long> attrs;` | one field, `attrs` — the comma is in the type |
| `private String first, last;` | two fields |
| `private String[] tags;` / `private int counts[];` | one field each |
| `private String sep = "a; b";` | one field — the `;` is inside a string |
| `String bare;` | a field; the access modifier is optional |

Two of those are the reason this is not a regex. `this.name = name;` inside a
method body looks exactly like a field with an initialiser, and only the brace
depth tells them apart. `"a; b"` puts a statement terminator inside a string
literal, and `// private String x;` puts a whole fake field inside a comment.

**Strip comments and string literals first**, before counting anything. Then
walk the text tracking brace depth, and only look at statements at depth 1 —
the class body. Doing it in that order is what makes the rest short.

A class with no fields is an error, and so is text that is not a class.

---

## 2. `query-string-to-json.js`

Accepts either a bare query string or a whole URL. If there is a `?`, take what
follows it; drop any `#fragment`.

```
a=1&b=two          simple pairs
q=hello+world      "+" means space -- decodeURIComponent does NOT do this
x=%3D              percent-decode both keys and values
a=1&a=2            a repeated key becomes ["1", "2"]
a&b=1              a bare key has the value ""
u[name]=jo         bracket keys nest:  { "u": { "name": "jo" } }
a[]=1&a[]=2        empty brackets force an array
```

**Values stay strings.** A `1` in a URL is nearly always an id, and coercing
`007` to `7` loses information you cannot get back. Nothing here is turned into
a number or a boolean.

---

## 3. `json-to-query-string.js`

The other direction.

```json
{ "page": 2, "tag": ["a", "b"], "u": { "name": "jo" } }
```

```
page=2&tag=a&tag=b&u[name]=jo
```

No leading `?`. Key order is the order in the input.

### Flattening

A query string is flat and JSON is a tree, so the nesting has to go into the
key. Recurse carrying a prefix:

| Value | Becomes |
|---|---|
| scalar | `prefix=value` |
| object | recurse per entry, prefix becomes `prefix[key]` |
| array of scalars | the key repeated — `tag=a&tag=b` |
| array containing an object or array | indices — `a[0][x]=1&a[1][x]=2` |
| `[]` or `{}` | nothing at all; the key does not appear |

The array rule has two branches because the repeated-key form has no way to
express a nested structure — `a=1&a=2` says two values, and nothing more. Once
an element has parts of its own it needs an address, and the index is the only
one available.

### Encoding

`encodeURIComponent` on keys and values. Two adjustments:

- It escapes `[` and `]`, which defeats the whole point of the bracket keys —
  put them back. `u[name]=jo` is the readable form and it is what
  `query-string-to-json.js` reads.
- It leaves a space as `%20`, which is correct. Do not emit `+`.

`null` and `""` both become an empty value: `a=`. Booleans spell themselves
out. Numbers go through verbatim, per the shared rule.

A top-level array, a top-level scalar and an empty object are all errors.

---

## 4. `json-to-insert-query.js`

One row.

```json
{ "sysMenu": { "menuId": 1, "menuName": "Home" } }
```

```sql
INSERT INTO sys_menu (menu_id, menu_name) VALUES (1, 'Home');
```

**An array is an error.** Multi-row insert syntax differs between databases —
`VALUES (..),(..)` in MySQL and Postgres, `INSERT ALL` in Oracle, a whole other
shape in T-SQL — so a batch would be right in one place and wrong in the rest.
One row is correct everywhere. Paste it as many times as you need rows.

### The table name

- An object with **exactly one key whose value is an object**: that key is the
  table, and the inner object is the row. This is the shape a Spring response
  or a MyBatis log line already has.
- Anything else: the row is the whole object and the table is the literal
  `table_name`, to be typed over.

`{"a": 1}` is one key, but its value is a scalar, so it is a row with one
column — not a table with no columns. The fixture says so.

### Column names

The JSON key, camelCase turned to snake_case:

- insert `_` before an uppercase letter that follows a lowercase letter or a
  digit — `menuId` → `menu_id`, `line1Text` → `line1_text`
- insert `_` before the last uppercase letter of a run when a lowercase letter
  follows it — `HTTPStatus` → `HTTP_Status`
- lowercase the result

So a run of capitals stays one word: `userID` → `user_id`. A key that is already
snake_case comes out unchanged. Java DTOs are camelCase and the columns behind
them are almost always snake_case, so this is the conversion that saves the
typing; when it is wrong, it is one search-and-replace away.

### Values

| JSON | SQL |
|---|---|
| string | `'...'`, with `'` doubled to `''` |
| number | the source text, verbatim |
| `true` / `false` | `TRUE` / `FALSE` |
| `null` | `NULL` |
| object or array | compact `JSON.stringify`, then quoted as a string |

Doubling the quote is the standard SQL escape and the only one that is portable;
a backslash is **not** an escape character here and must not be treated as one.
The nested case covers a JSON column, which is common enough to be worth
handling and is the only sensible thing to do with a value that has no scalar
form.

An empty object, a scalar and empty input are errors.

---

## 5. `json-to-update-query.js`

```json
{ "sysMenu": { "menuId": 1, "menuName": "Home", "path": "/home" } }
```

```sql
UPDATE sys_menu SET menu_name = 'Home', path = '/home' WHERE menu_id = 1;
```

Table name, column names and value formatting are all exactly the insert's. An
array is an error for the same reason.

### The hard part

**Which key is the `WHERE`?** Nothing in the JSON says. Every key looks the
same as every other, and the input is one flat object with no metadata.

**The rule this project uses**, in order:

1. a column named exactly `id`, wherever it appears
2. otherwise the **first** column whose name ends in `_id`
3. otherwise the **first** column, whatever it is

The chosen key goes in the `WHERE` and is **removed from the `SET`**. An
`UPDATE` that also sets the column it matches on is a bug with a long fuse: it
works perfectly until the day the value is wrong, and then it rewrites the row's
identity instead of failing.

It is a heuristic and it will sometimes pick wrong — a table keyed on `code`
with a `user_id` column in it gets the wrong one. That is unavoidable; the
information is not in the input. Pick the rule, apply it consistently, and read
the `WHERE` before running the statement. You were going to anyway.

An object with **one key** is an error: with the key column removed there is
nothing left to set.

---

## 6. `js-object-to-json.js`

The input is nearly JSON already. What it adds:

```js
{ a: 1 }            unquoted keys
{ a: 'x' }          single quotes
{ a: 1, }           trailing commas
{ /* n */ a: 1 }    block comments
{ a: 1 // n
}                   line comments
```

### Do not use `eval`

Nor `new Function("return " + input)`. This runs against whatever happens to be
on your clipboard. Both of those **execute** it, so copying the wrong snippet
would run it with your privileges. One fixture exists specifically to catch
this:

```js
{a: (function(){ return 1 })()}
```

A parser rejects it because `(` is not a valid start to a value. `eval` returns
`{a: 1}` and passes the other tests — so if that case passes while looking like
success, check how you got there.

### Structure

The classic recursive-descent set, sharing one position index:

```
parseValue()    dispatch on the next character
parseObject()   '{' then key ':' value pairs then '}'
parseArray()    '[' then values then ']'
parseString()   a quote, then chars to the matching quote, handling \ escapes
parseNumber()   digits, sign, decimal point, exponent
skipWhitespace()  also skips // and /* */ comments
```

Putting comment-skipping inside `skipWhitespace` and calling it between every
token is what keeps the rest readable — the alternative is checking for comments
in a dozen places.

Recursion gives you nesting for free: `parseArray` calls `parseValue`, which may
call `parseObject`, which calls `parseValue` again.

---

## 7. `json-to-js.js`

The other direction, and the last one for a reason.

```json
{ "name": "jo", "my-key": 1, "id": 9007199254740993 }
```

```js
{
  name: 'jo',
  'my-key': 1,
  id: 9007199254740993
}
```

### Why `JSON.parse` cannot be used

```js
JSON.parse('{"id":9007199254740993}').id   // 9007199254740992
```

A JavaScript number is a float64 and cannot hold that value. Every `Long` id
coming out of a Java service is in the range where this happens, silently, with
no error and no warning — and the transform's whole job is copying an id from
one place to another. Losing the last digit while looking like it worked is the
worst failure this folder can have.

So the number never becomes a JavaScript number. It is copied from the input to
the output as the **text it was written as**, which means reading the JSON
yourself instead of asking `JSON.parse` for it. `1.50` stays `1.50` and `1e3`
stays `1e3` for the same reason: the source text is the only thing that is not
lossy.

`js-object-to-json.js` is the reader you need, minus the comments and the loose
quoting. Do that one first and this is a writer over the same walk.

### Rules

- Layout matches `JSON.stringify(..., null, 2)`: two spaces per level, one
  entry per line, and `{}` and `[]` on a single line when empty.
- A key is unquoted when it matches `/^[A-Za-z_$][A-Za-z0-9_$]*$/`. Otherwise
  it is single-quoted. Reserved words need no special case — `{ if: 1 }` is
  legal in ES5 and later, and quoting them would only add noise.
- Strings are single-quoted. `'` inside becomes `\'`; `"` is left alone; every
  other escape sequence in the JSON survives as it was written.
- The input must be valid **JSON**, not a JS literal. `{a: 1}` is an error, and
  so is a trailing comma or anything after the closing brace — those belong to
  the other direction, and accepting them here would make the pair meaningless
  as a round trip.
