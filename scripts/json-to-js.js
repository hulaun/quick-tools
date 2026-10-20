// JSON  ->  JavaScript object literal
//
//     { "name": "jo", "my-key": 1, "id": 9007199254740993 }
//
//     ->  {
//           name: 'jo',
//           'my-key': 1,
//           id: 9007199254740993
//         }
//
// The reverse of js-object-to-json.js, and the harder of the pair for one
// reason that has nothing to do with the writing:
//
//     JSON.parse('{"id":9007199254740993}').id   // 9007199254740992
//
// A JavaScript number is a float64 and cannot hold that value. Every Long id
// coming out of a Java service is in the range where this happens -- silently,
// with no error -- and copying an id from one place to another is this
// transform's entire job. Losing the last digit while looking like it worked is
// the worst failure this folder can have.
//
// So the number never becomes a number. It is carried from the input to the
// output as the text it was written as, which means reading the JSON here
// instead of asking JSON.parse for it. "1.50" stays "1.50" and "1e3" stays
// "1e3" for the same reason: the source text is the only form that is not
// lossy. Strings are carried the same way, by their raw source, so an escape
// arrives in the output as the two characters it was written as.
//
// The input must be valid JSON and not already a JS literal. { a: 1 } is an
// error here, and so is a trailing comma -- those belong to the other
// direction, and accepting them would make the pair meaningless as a round
// trip.

var name = "Json to js object";
var group = "Json";
var tags = ["js", "jsobject", "literal", "unquote"];

function transform(input) {
    return write(readJson(input), "");
}

// write renders one node. The layout matches JSON.stringify(.., null, 2): two
// spaces per level, one entry per line, and an empty object or array on a
// single line.
function write(node, indent) {
    var inner = indent + "  ";
    var parts = [];
    var i;

    if (node.k === "obj") {
        if (!node.keys.length) {
            return "{}";
        }
        for (i = 0; i < node.keys.length; i++) {
            parts.push(inner + keyText(node.keys[i]) + ": " + write(node.vals[i], inner));
        }
        return "{\n" + parts.join(",\n") + "\n" + indent + "}";
    }

    if (node.k === "arr") {
        if (!node.items.length) {
            return "[]";
        }
        for (i = 0; i < node.items.length; i++) {
            parts.push(inner + write(node.items[i], inner));
        }
        return "[\n" + parts.join(",\n") + "\n" + indent + "]";
    }

    if (node.k === "str") return "'" + jsString(node.raw) + "'";
    if (node.k === "num") return node.raw; // the source text, never a number
    if (node.k === "bool") return node.v ? "true" : "false";
    return "null";
}

// keyText drops the quotes from a key that can go without them.
//
// A reserved word needs no special case: { if: 1 } is legal in ES5 and later,
// and quoting it would only add noise.
function keyText(key) {
    if (/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(key)) {
        return key;
    }
    return "'" + key.replace(/\\/g, "\\\\").replace(/'/g, "\\'") + "'";
}

// jsString rewrites a JSON string's raw source as the body of a single-quoted
// JavaScript string. Working from the raw source rather than the decoded value
// is what keeps every escape as it was written -- a \n in the input is a \n in
// the output, not a real newline.
//
// Two characters change and nothing else does. An apostrophe has to be escaped,
// because it is now the delimiter; a \" does not need to be, because it no
// longer is.
function jsString(raw) {
    var out = "";
    for (var i = 0; i < raw.length; i++) {
        var c = raw.charAt(i);
        if (c === "\\") {
            var e = raw.charAt(i + 1);
            out += e === '"' ? '"' : "\\" + e;
            i++;
            continue;
        }
        out += c === "'" ? "\\'" : c;
    }
    return out;
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
