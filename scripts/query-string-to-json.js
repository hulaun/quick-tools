// URL query string  ->  JSON
//
//     https://api.example.com/users?page=2&tag=a&tag=b
//
//     ->  { "page": "2", "tag": ["a", "b"] }
//
// Two things make this more than a split on "&".
//
//   - The same key can appear twice. The first one is a value; the second has
//     to turn what is already there into an array without losing it. So the
//     assignment is a function, not an "=".
//   - A key can carry structure of its own. "u[name]" is a path, "a[]" is an
//     append, and both are decided by the key text rather than by the value.
//
// Values stay STRINGS. A "1" in a URL is nearly always an id, and coercing
// "007" to 7 loses a digit that cannot be recovered -- README.md makes this a
// deliberate exception to the shared type rules.
//
// Brackets are split off the RAW key, before decoding, so that a percent-encoded
// "%5B" stays a literal "[" in a key name instead of becoming structure.

var name = "Query string to json";
var group = "Json";
var tags = ["url", "querystring", "params"];

function transform(input) {
    var query = isolate(input);
    var pairs = query.split("&");
    var out = {};
    var found = 0;

    for (var i = 0; i < pairs.length; i++) {
        var pair = pairs[i];
        if (pair === "") {
            continue; // "a=1&&b=2" and a trailing "&"
        }
        // Only the FIRST "=" separates; a later one belongs to the value.
        // "x=a=b" is the key x with the value "a=b".
        var eq = pair.indexOf("=");
        var rawKey = eq < 0 ? pair : pair.slice(0, eq);
        var rawValue = eq < 0 ? "" : pair.slice(eq + 1); // a bare key has the value ""

        assign(out, keyPath(rawKey), decode(rawValue));
        found++;
    }

    if (!found) {
        throw new Error("no key=value pairs found");
    }
    return JSON.stringify(out, null, 2);
}

// isolate reduces a whole URL to its query string. Everything up to and
// including the first "?" goes, and so does any "#fragment" -- which is cut
// first, because it sits after the query and would otherwise end up inside the
// last value.
function isolate(input) {
    var s = input.replace(/^\s+|\s+$/g, "");
    if (s === "") {
        throw new Error("nothing to convert");
    }
    var hash = s.indexOf("#");
    if (hash >= 0) {
        s = s.slice(0, hash);
    }
    var q = s.indexOf("?");
    if (q >= 0) {
        s = s.slice(q + 1);
    }
    return s;
}

// keyPath splits a raw key into the path it addresses.
//
//     "page"      ->  ["page"]
//     "u[name]"   ->  ["u", "name"]
//     "a[]"       ->  ["a", ""]          an empty segment means "append"
//
// Anything that is not a clean run of [..] groups to the end is taken as one
// literal key. A key like "a[b" is far more likely to be a key containing a
// bracket than an attempt at nesting, and guessing wrong on it would silently
// reshape the output.
function keyPath(raw) {
    var open = raw.indexOf("[");
    if (open < 0 || raw.charAt(raw.length - 1) !== "]") {
        return [decode(raw)];
    }

    var segs = [decode(raw.slice(0, open))];
    var rest = raw.slice(open);
    var i = 0;
    while (i < rest.length) {
        if (rest.charAt(i) !== "[") {
            return [decode(raw)]; // "a[b]c[d]" -- not a path
        }
        var close = rest.indexOf("]", i);
        if (close < 0) {
            return [decode(raw)];
        }
        segs.push(decode(rest.slice(i + 1, close)));
        i = close + 1;
    }
    return segs;
}

// assign places value at segs within container, creating what it needs on the
// way down. The segment AFTER the one being created is what decides whether the
// child is an array or an object: "a[]" wants an array, "a[b]" wants an object.
function assign(container, segs, value) {
    var key = segs[0];

    if (segs.length === 1) {
        if (key === "") {
            container.push(value); // a[]=1 -- container is the array
        } else {
            put(container, key, value);
        }
        return;
    }

    var child = container[key];
    if (child === null || typeof child !== "object") {
        child = segs[1] === "" ? [] : {};
        container[key] = child;
    }
    assign(child, segs.slice(1), value);
}

// put sets container[key], promoting to an array on a repeat.
function put(container, key, value) {
    if (!Object.prototype.hasOwnProperty.call(container, key)) {
        container[key] = value;
    } else if (isArray(container[key])) {
        container[key].push(value);
    } else {
        container[key] = [container[key], value];
    }
}

// decode percent-decodes one key or value. "+" means space in a query string
// and decodeURIComponent does not know that, so it is replaced first -- after
// decoding would be wrong, since a real "+" arrives as "%2B".
//
// A malformed escape falls back to the literal text rather than throwing. This
// runs against whatever is on the clipboard, and a stray "%" in one value is no
// reason to refuse the other twenty.
function decode(s) {
    var plussed = s.replace(/\+/g, " ");
    try {
        return decodeURIComponent(plussed);
    } catch (e) {
        return plussed;
    }
}

function isArray(v) {
    return Object.prototype.toString.call(v) === "[object Array]";
}
