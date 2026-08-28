# Writing transforms

Every `.js` file in this folder becomes an entry in the palette. Save the file
and it reloads within a second — no rebuild, no restart.

## The format

```js
var name  = "Csv line to json";   // shown in the palette. Defaults to the filename.
var group = "Json";               // the prefix, as in "Json: Csv line to json".
var tags  = ["csv", "split"];     // extra words the search should match.

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
  { "name": "simple",       "in": "a,b",  "out": "[\n  \"a\",\n  \"b\"\n]" },
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

---

# The two parsers

`java-to-json.js` and `js-object-to-json.js` are stubs. The fixtures are
written and currently red. `csv-line-to-json.js` is a complete worked parser to
copy the shape from — it solves a different problem but has the same
scan → build → emit structure.

## Shared type rules

A bare (unquoted) value becomes:

| Input | Result |
|---|---|
| `null` | `null` |
| `true` / `false` | boolean |
| something `Number()` parses **entirely** | number |
| anything else | string |

"Entirely" matters: `12abc` is the string `"12abc"`, not `12`. The check in
`csv-line-to-json.js` is `!isNaN(n) && String(n) === s`.

Quoted values are always strings, never coerced.

Output is always `JSON.stringify(value, null, 2)` — two-space indent.

---

## `java-to-json.js`

Two shapes appear in the wild, and both must work:

```
Person(name=John, age=30)         Lombok's @ToString
Person{name='John', age=30}       IDE-generated toString
```

The leading class name is **discarded** — only the fields become JSON.

### Grammar

```
object   := NAME? ( "(" fields? ")" | "{" fields? "}" )
fields   := field ("," field)*
field    := KEY "=" value
value    := object | list | quoted | bare
list     := "[" (value ("," value)*)? "]"
quoted   := "'" ... "'" | '"' ... '"'
bare     := characters up to the end of the field
```

Nesting is arbitrary: objects in lists, lists in objects, objects in objects.

### The hard part

**A bare value can contain commas and equals signs.** Java's `toString` does no
quoting, so this is genuinely ambiguous:

```
P(note=hello, world, age=null)
```

`hello, world` could be one value, or `hello` could be the value and `world` a
malformed field. Nothing in the text settles it.

**The rule this project uses:** a comma ends the current field only when the
text after it looks like `identifier=`. Otherwise the comma is part of the
value. So the above is `note = "hello, world"` and `age = null`, which is what
the fixture expects.

It is a heuristic, and it is wrong for a field whose value ends in something
that looks like an assignment. That is unavoidable — `toString` output is lossy
and the information simply is not there. Pick the rule, apply it consistently,
and let the fixture record the decision.

**Depth tracking.** Inside `P(a=[1, 2], b=3)` the commas within `[...]` are not
field separators. Counting `(){}[]` depth as you scan, and only treating a comma
as a separator at depth zero, is the way through. A regex cannot do this.

**`{` is overloaded.** It opens an IDE-style object *and* a Java map:

```
P(m={a=1, b=2})    ->   { "m": { "a": 1, "b": 2 } }
```

Both parse as key/value pairs, so this needs no special case — but it is worth
knowing that is why it works.

---

## `js-object-to-json.js`

Easier: the input is nearly JSON already. What it adds:

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
