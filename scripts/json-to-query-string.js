// JSON  ->  URL query string
//
//     { "page": 2, "tag": ["a", "b"], "u": { "name": "jo" } }
//
//     ->  page=2&tag=a&tag=b&u[name]=jo
//
// The reverse of query-string-to-json.js. A query string is flat and JSON is a
// tree, so the nesting has to be encoded into the key: one recursion carrying
// an accumulated prefix down with it, emitting a pair at every leaf.
//
// Arrays get two different treatments, which is the only interesting decision
// here. An array of scalars repeats the key -- tag=a&tag=b -- because that is
// the form every server already understands. But the repeated-key form says
// "two values" and nothing more, so as soon as an element has parts of its own
// it needs an address, and an index is the only one available.
//
// Numbers are written from the source text, per the shared rule in README.md,
// which is why this reads the JSON itself instead of calling JSON.parse.

var name = "Json to query string";
var group = "Json";
var tags = ["url", "querystring", "params", "encode"];

function transform(input) {
    var root = readJson(input);
    if (root.k !== "obj") {
        throw new Error("a query string needs a JSON object at the top level");
    }
    if (!root.keys.length) {
        throw new Error("the object is empty -- there is nothing to encode");
    }

    var parts = [];
    emit("", root, parts);
    if (!parts.length) {
        // {"a": [], "o": {}} -- every key held an empty container, so every key
        // dropped out.
        throw new Error("every value was empty -- there is nothing to encode");
    }
    return parts.join("&");
}

// emit walks the tree, appending one "key=value" per leaf. prefix is the key
// built so far, in its readable bracket form; "" means the top level, where a
// key stands on its own rather than inside brackets.
//
// An empty object and an empty array both fall out of this for free: the loop
// runs no times, so the key never appears.
function emit(prefix, node, parts) {
    var i;

    if (node.k === "obj") {
        for (i = 0; i < node.keys.length; i++) {
            emit(extend(prefix, node.keys[i]), node.vals[i], parts);
        }
        return;
    }

    if (node.k === "arr") {
        var addressed = false;
        for (i = 0; i < node.items.length; i++) {
            var k = node.items[i].k;
            if (k === "obj" || k === "arr") {
                addressed = true;
            }
        }
        for (i = 0; i < node.items.length; i++) {
            emit(addressed ? extend(prefix, String(i)) : prefix, node.items[i], parts);
        }
        return;
    }

    parts.push(encodeKey(prefix) + "=" + encodeURIComponent(scalar(node)));
}

function extend(prefix, key) {
    return prefix === "" ? key : prefix + "[" + key + "]";
}

// scalar is the text a leaf contributes. null and "" are both an empty value --
// a query string has no way to tell them apart, and inventing one ("a=null")
// would be a value, not an absence.
function scalar(node) {
    if (node.k === "str") return node.text;
    if (node.k === "num") return node.raw; // verbatim, per the shared rule
    if (node.k === "bool") return node.v ? "true" : "false";
    return ""; // null
}

// encodeKey escapes a key but puts the structural brackets back.
// encodeURIComponent turns them into %5B and %5D, which would defeat the whole
// point -- u[name]=jo is the readable form and it is what
// query-string-to-json.js reads back.
//
// A literal bracket inside a key name is unescaped by this too. That is the
// cost of the readable form, and it is the right trade: nested keys are common
// and a "[" in a key name is not.
function encodeKey(prefix) {
    return encodeURIComponent(prefix).replace(/%5B/g, "[").replace(/%5D/g, "]");
}

// ---------------------------------------------------------------------------
// A JSON reader that does not lose numbers.
//
// JSON.parse would be shorter and is not usable here: it turns every number
// into a float64, and 9007199254740993 comes back out as ...992. Every Java
// Long id lives in that range, so the number is kept as the TEXT it was written
// as and never becomes a JavaScript number at all. "1.50" stays "1.50" for the
// same reason -- the source text is the only lossless form.
//
// It is strict JSON: quoted keys, no trailing commas, no comments, nothing
// after the top-level value. Each node carries its kind in .k --
//
//     {k:"obj",  keys:[], vals:[]}   insertion order preserved
//     {k:"arr",  items:[]}
//     {k:"str",  text:"decoded", raw:"source between the quotes"}
//     {k:"num",  raw:"the source text"}
//     {k:"bool", v:true}
//     {k:"null"}
//
// This block is duplicated in the other transforms that read JSON. Scripts are
// compiled one file at a time with no way to share code, and every .js here is
// a palette entry, so a shared library file would show up as one.
function readJson(src) {
    var pos = 0;

    function fail(msg) {
        throw new Error(msg + " at character " + (pos + 1));
    }

    function ws() {
        while (pos < src.length && " \t\n\r".indexOf(src.charAt(pos)) >= 0) {
            pos++;
        }
    }

    function digit(c) {
        return c >= "0" && c <= "9";
    }

    function value() {
        ws();
        if (pos >= src.length) {
            fail("unexpected end of input");
        }
        var c = src.charAt(pos);
        if (c === "{") return object();
        if (c === "[") return array();
        if (c === '"') return string();
        if (c === "-" || digit(c)) return number();
        if (src.substr(pos, 4) === "true") {
            pos += 4;
            return { k: "bool", v: true };
        }
        if (src.substr(pos, 5) === "false") {
            pos += 5;
            return { k: "bool", v: false };
        }
        if (src.substr(pos, 4) === "null") {
            pos += 4;
            return { k: "null" };
        }
        fail("unexpected " + JSON.stringify(c));
    }

    function object() {
        pos++; // {
        var node = { k: "obj", keys: [], vals: [] };
        ws();
        if (src.charAt(pos) === "}") {
            pos++;
            return node;
        }
        for (;;) {
            ws();
            if (src.charAt(pos) !== '"') {
                // Also where a trailing comma lands: {"a": 1,} looks for a key
                // and finds the brace.
                fail("expected a quoted key");
            }
            node.keys.push(string().text);
            ws();
            if (src.charAt(pos) !== ":") {
                fail('expected ":"');
            }
            pos++;
            node.vals.push(value());
            ws();
            var c = src.charAt(pos);
            if (c === ",") {
                pos++;
                continue;
            }
            if (c === "}") {
                pos++;
                return node;
            }
            fail('expected "," or "}"');
        }
    }

    function array() {
        pos++; // [
        var node = { k: "arr", items: [] };
        ws();
        if (src.charAt(pos) === "]") {
            pos++;
            return node;
        }
        for (;;) {
            node.items.push(value());
            ws();
            var c = src.charAt(pos);
            if (c === ",") {
                pos++;
                continue;
            }
            if (c === "]") {
                pos++;
                return node;
            }
            fail('expected "," or "]"');
        }
    }

    // string returns both forms. text is what a consumer that needs the value
    // uses; raw is the untouched source, which is what a writer emitting
    // another escaped format wants so that \n survives as the two characters it
    // was written as rather than as a real newline.
    function string() {
        pos++; // the opening quote
        var start = pos;
        var text = "";
        for (;;) {
            if (pos >= src.length) {
                fail("unterminated string");
            }
            var c = src.charAt(pos);
            if (c === '"') {
                break;
            }
            if (c === "\\") {
                var e = src.charAt(pos + 1);
                pos += 2;
                if (e === '"') text += '"';
                else if (e === "\\") text += "\\";
                else if (e === "/") text += "/";
                else if (e === "b") text += "\b";
                else if (e === "f") text += "\f";
                else if (e === "n") text += "\n";
                else if (e === "r") text += "\r";
                else if (e === "t") text += "\t";
                else if (e === "u") {
                    var hex = src.substr(pos, 4);
                    if (!/^[0-9a-fA-F]{4}$/.test(hex)) {
                        fail("expected four hex digits after \\u");
                    }
                    text += String.fromCharCode(parseInt(hex, 16));
                    pos += 4;
                } else {
                    pos -= 2;
                    fail("unknown escape \\" + e);
                }
                continue;
            }
            if (c < " ") {
                fail("a control character has to be escaped");
            }
            text += c;
            pos++;
        }
        var raw = src.slice(start, pos);
        pos++; // the closing quote
        return { k: "str", text: text, raw: raw };
    }

    // number validates against the JSON grammar and keeps the text. Validating
    // matters even though the value is never computed: it is what makes "01"
    // and "1." errors here rather than surprises in whatever reads the output.
    function number() {
        var start = pos;
        if (src.charAt(pos) === "-") {
            pos++;
        }
        if (src.charAt(pos) === "0") {
            pos++;
        } else {
            if (!digit(src.charAt(pos))) {
                fail("expected a digit");
            }
            while (digit(src.charAt(pos))) pos++;
        }
        if (src.charAt(pos) === ".") {
            pos++;
            if (!digit(src.charAt(pos))) {
                fail("expected a digit after the decimal point");
            }
            while (digit(src.charAt(pos))) pos++;
        }
        var c = src.charAt(pos);
        if (c === "e" || c === "E") {
            pos++;
            c = src.charAt(pos);
            if (c === "+" || c === "-") {
                pos++;
            }
            if (!digit(src.charAt(pos))) {
                fail("expected a digit in the exponent");
            }
            while (digit(src.charAt(pos))) pos++;
        }
        return { k: "num", raw: src.slice(start, pos) };
    }

    if (!/\S/.test(src)) {
        throw new Error("nothing to convert");
    }
    var root = value();
    ws();
    if (pos < src.length) {
        fail("unexpected text after the value");
    }
    return root;
}
