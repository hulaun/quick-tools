// JSON  ->  a single-row INSERT statement
//
//     { "sysMenu": { "menuId": 1, "menuName": "Home" } }
//
//     ->  INSERT INTO sys_menu (menu_id, menu_name) VALUES (1, 'Home');
//
// ONE ROW. An array is an error on purpose -- see rowOf below.
//
// The assembly is three lines; everything interesting is in the three helpers
// it shares with json-to-update-query.js. Values are never glued in as raw
// text: every one of them goes through sqlValue, which is the only place that
// knows how a literal is written, so there is one place to look when a quote
// comes out wrong.

var name = "Json to insert query";
var group = "Sql";
var tags = ["sql", "insert", "row", "db"];

function transform(input) {
    var row = rowOf(readJson(input));

    var columns = [];
    var values = [];
    for (var i = 0; i < row.keys.length; i++) {
        columns.push(snake(row.keys[i]));
        values.push(sqlValue(row.vals[i]));
    }

    return "INSERT INTO " + row.table +
        " (" + columns.join(", ") + ")" +
        " VALUES (" + values.join(", ") + ");";
}

// ---------------------------------------------------------------------------
// The table name, the column names and the value writer, shared verbatim with
// the other SQL transform. The two of them wanted to be one file with two entry
// points; a .js here is one palette entry and cannot import another, so the
// duplication is the price of having both show up.

// rowOf decides what is the table and what is the row.
//
// An object with exactly one key whose value is an object is the shape a Spring
// response or a MyBatis log line already has, so that key is the table and the
// inner object is the row. Anything else is a row with a placeholder table to
// be typed over -- including {"a": 1}, which is one key but a scalar one, and
// so is a row with one column rather than a table with none.
function rowOf(root) {
    if (root.k === "arr") {
        // Deliberate. Multi-row syntax is VALUES (..),(..) in MySQL and
        // Postgres, INSERT ALL in Oracle and something else again in T-SQL, so
        // a batch would be right in one place and wrong everywhere else. One
        // row is correct everywhere; paste it as many times as you need rows.
        throw new Error("an array is a batch, and this writes one row -- pass a single object");
    }
    if (root.k !== "obj") {
        throw new Error("expected a JSON object");
    }
    if (!root.keys.length) {
        throw new Error("the object is empty -- there are no columns");
    }

    if (root.keys.length === 1 && root.vals[0].k === "obj") {
        var inner = root.vals[0];
        if (!inner.keys.length) {
            throw new Error("the object is empty -- there are no columns");
        }
        return { table: snake(root.keys[0]), keys: inner.keys, vals: inner.vals };
    }
    return { table: "table_name", keys: root.keys, vals: root.vals };
}

// snake turns a camelCase key into a snake_case column. Java DTOs are camelCase
// and the columns behind them are almost always snake_case, so this is the
// conversion that saves the typing; when it guesses wrong it is one
// search-and-replace away.
//
// Two rules, and the second is the one that is easy to miss:
//
//   menuId      ->  menu_id         a capital after a lowercase or a digit
//   line1Text   ->  line1_text      a digit counts, and does not split
//   userID      ->  user_id         a run of capitals stays one word
//   HTTPStatus  ->  http_status     ... until a lowercase follows, and then the
//                                   LAST capital of the run starts the word
//   menu_id     ->  menu_id         already snake_case, left alone
function snake(key) {
    return key
        .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
        .replace(/([A-Z]+)([A-Z][a-z])/g, "$1_$2")
        .toLowerCase();
}

// sqlValue writes one value as a literal.
function sqlValue(node) {
    if (node.k === "str") return quote(node.text);
    if (node.k === "num") return node.raw; // verbatim, per the shared rule
    if (node.k === "bool") return node.v ? "TRUE" : "FALSE";
    if (node.k === "null") return "NULL";
    // An object or an array has no scalar form, so it goes in as JSON text --
    // which is a JSON column, common enough to be worth handling and the only
    // sensible thing to do with it.
    return quote(compact(node));
}

// quote wraps a string literal, doubling any quote inside it.
//
// Doubling is the standard SQL escape and the only portable one. A backslash is
// NOT an escape character here and must not be treated as one: C:\temp is a
// path, and 'C:\temp' is what it should come out as.
function quote(s) {
    return "'" + s.replace(/'/g, "''") + "'";
}

// compact renders a nested value as JSON with no spaces. JSON.stringify would
// do it, except that the value is not a JavaScript value -- the numbers are
// still text, which is the whole point -- so the walk is written out. Strings
// go back in by their raw source form, which keeps their escapes exactly as
// they arrived.
function compact(node) {
    var parts = [];
    var i;

    if (node.k === "obj") {
        for (i = 0; i < node.keys.length; i++) {
            parts.push(JSON.stringify(node.keys[i]) + ":" + compact(node.vals[i]));
        }
        return "{" + parts.join(",") + "}";
    }
    if (node.k === "arr") {
        for (i = 0; i < node.items.length; i++) {
            parts.push(compact(node.items[i]));
        }
        return "[" + parts.join(",") + "]";
    }
    if (node.k === "str") return '"' + node.raw + '"';
    if (node.k === "num") return node.raw;
    if (node.k === "bool") return node.v ? "true" : "false";
    return "null";
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
