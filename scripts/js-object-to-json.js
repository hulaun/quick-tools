// JavaScript object literal  ->  JSON
//
//     { a: 1, b: 'x', /* note */ c: [1, 2,], }
//
//     ->  { "a": 1, "b": "x", "c": [1, 2] }
//
// The input is nearly JSON already. What it adds is unquoted keys, single
// quotes, trailing commas and comments -- so this is the classic
// recursive-descent set over a grammar that is JSON's with four relaxations.
//
// NOT eval, and not new Function("return " + input). This runs against whatever
// happens to be on the clipboard, and both of those EXECUTE it, so copying the
// wrong snippet would run it with your privileges. A parser that only reads the
// text cannot be made to run anything, and it is barely more work. One fixture
// exists to catch the shortcut:
//
//     {a: (function(){ return 1 })()}
//
// A parser rejects that, because "(" is not a valid start to a value, while
// eval returns {a: 1} and passes every other test.
//
// Comment-skipping lives inside skipWhitespace, which is called between every
// token. That is what keeps the rest of the functions short -- the alternative
// is remembering to check for a comment in a dozen separate places.

var name = "Js object to json";
var group = "Json";
var tags = ["js", "jsobject", "literal", "json5"];

function transform(input) {
    if (!/\S/.test(input)) {
        throw new Error("nothing to convert");
    }
    return JSON.stringify(parse(input), null, 2);
}

function parse(src) {
    var pos = 0;

    function fail(msg) {
        throw new Error(msg + " at character " + (pos + 1));
    }

    function peek() {
        return pos < src.length ? src.charAt(pos) : "";
    }

    // skipWhitespace also eats comments, so everything downstream can treat a
    // comment as if it were a space.
    function skipWhitespace() {
        for (;;) {
            while (pos < src.length && /\s/.test(src.charAt(pos))) {
                pos++;
            }
            if (src.charAt(pos) === "/" && src.charAt(pos + 1) === "/") {
                while (pos < src.length && src.charAt(pos) !== "\n") {
                    pos++;
                }
                continue;
            }
            if (src.charAt(pos) === "/" && src.charAt(pos + 1) === "*") {
                var end = src.indexOf("*/", pos + 2);
                if (end < 0) {
                    fail("unterminated block comment");
                }
                pos = end + 2;
                continue;
            }
            return;
        }
    }

    // value dispatches on the next character. Nesting falls out of these
    // functions calling each other: array calls value, which may call object,
    // which calls value again.
    function value() {
        skipWhitespace();
        var c = peek();
        if (c === "") fail("unexpected end of input");
        if (c === "{") return object();
        if (c === "[") return array();
        if (c === '"' || c === "'") return string();
        if (c === "-" || c === "+" || c === "." || (c >= "0" && c <= "9")) return number();
        if (/[A-Za-z_$]/.test(c)) {
            var word = identifier();
            if (word === "true") return true;
            if (word === "false") return false;
            if (word === "null") return null;
            // undefined, NaN, a variable name, a function call -- all things a
            // JavaScript engine could evaluate and a converter cannot.
            fail("cannot convert " + JSON.stringify(word) + " to JSON");
        }
        fail("unexpected " + JSON.stringify(c));
    }

    function object() {
        pos++; // {
        var out = {};
        skipWhitespace();
        if (peek() === "}") {
            pos++;
            return out;
        }
        for (;;) {
            skipWhitespace();
            var key;
            var c = peek();
            if (c === '"' || c === "'") {
                key = string();
            } else if (/[A-Za-z_$]/.test(c)) {
                key = identifier();
            } else if (c >= "0" && c <= "9") {
                key = String(number()); // { 1: "x" } is legal
            } else {
                fail("expected a key");
            }

            skipWhitespace();
            if (peek() !== ":") {
                fail('expected ":" after the key');
            }
            pos++;
            out[key] = value();

            skipWhitespace();
            c = peek();
            if (c === ",") {
                pos++;
                skipWhitespace();
                if (peek() === "}") {
                    pos++; // a trailing comma
                    return out;
                }
                continue;
            }
            if (c === "}") {
                pos++;
                return out;
            }
            fail('expected "," or "}"');
        }
    }

    function array() {
        pos++; // [
        var out = [];
        skipWhitespace();
        if (peek() === "]") {
            pos++;
            return out;
        }
        for (;;) {
            out.push(value());
            skipWhitespace();
            var c = peek();
            if (c === ",") {
                pos++;
                skipWhitespace();
                if (peek() === "]") {
                    pos++; // a trailing comma
                    return out;
                }
                continue;
            }
            if (c === "]") {
                pos++;
                return out;
            }
            fail('expected "," or "]"');
        }
    }

    // string reads either quoting style.
    //
    // The awkward part is an unescaped quote inside the string: 'it's' is not
    // valid JavaScript, but it is what a careless copy produces and throwing it
    // away would be unhelpful. So a quote only ends the string when what
    // follows it could follow a value; otherwise it is content. That turns
    // 'it's' into "it's" rather than an error, and costs nothing in the normal
    // case, where the next thing really is a comma or a brace.
    function string() {
        var quote = src.charAt(pos);
        pos++;
        var out = "";
        for (;;) {
            if (pos >= src.length) {
                fail("unterminated string");
            }
            var c = src.charAt(pos);
            if (c === "\\") {
                out += escaped();
                continue;
            }
            if (c === quote) {
                if (terminates(pos + 1)) {
                    pos++;
                    return out;
                }
                out += c;
                pos++;
                continue;
            }
            out += c;
            pos++;
        }
    }

    // terminates reports whether the position after a closing quote is a place
    // a value can end. It looks ahead without consuming, so the caller is free
    // to carry on reading the string.
    function terminates(at) {
        var save = pos;
        pos = at;
        skipWhitespace();
        var c = peek();
        pos = save;
        return c === "" || c === "," || c === "}" || c === "]" || c === ":";
    }

    function escaped() {
        var e = src.charAt(pos + 1);
        pos += 2;
        if (e === "n") return "\n";
        if (e === "t") return "\t";
        if (e === "r") return "\r";
        if (e === "b") return "\b";
        if (e === "f") return "\f";
        if (e === "v") return "\v";
        if (e === "0") return "\0";
        if (e === "\n") return ""; // a line continuation
        if (e === "u" || e === "x") {
            var width = e === "u" ? 4 : 2;
            var hex = src.substr(pos, width);
            if (hex.length === width && /^[0-9a-fA-F]+$/.test(hex)) {
                pos += width;
                return String.fromCharCode(parseInt(hex, 16));
            }
            return e; // not an escape after all
        }
        return e; // the quote escapes, the backslash, the slash, anything else
    }

    function number() {
        var m = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?/.exec(src.slice(pos));
        if (!m) {
            fail("expected a number");
        }
        pos += m[0].length;
        return Number(m[0]);
    }

    function identifier() {
        var m = /^[A-Za-z_$][A-Za-z0-9_$]*/.exec(src.slice(pos));
        if (!m) {
            fail("expected a name");
        }
        pos += m[0].length;
        return m[0];
    }

    var root = value();
    skipWhitespace();
    if (pos < src.length) {
        fail("unexpected text after the value");
    }
    return root;
}
