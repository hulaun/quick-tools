// A worked example, provided as a model to copy the shape of.
//
// It turns one line of CSV into a JSON array:
//
//     alpha,"beta, with comma",42     ->     ["alpha", "beta, with comma", 42]
//
// The problem is small, but it has the same three-part shape as any parser:
//
//     scan   walk the input one character at a time, tracking state
//     build  turn the pieces into a value
//     emit   render that value as JSON
//
// The part worth studying is the scanner. A regex or a plain split(",") breaks
// the moment a delimiter appears inside quotes, and no amount of cleverness
// with regexes fixes that properly. Walking character by character with a
// little state does.

var name = "Csv line to json";
var group = "Json";
var tags = ["csv", "tsv", "split"];

// ---------------------------------------------------------------------------
// scan: split on commas that are not inside quotes.
// ---------------------------------------------------------------------------
function splitFields(line) {
    var fields = [];
    var current = "";
    var inQuotes = false;
    var quoteChar = "";

    for (var i = 0; i < line.length; i++) {
        var c = line[i];

        if (inQuotes) {
            // A doubled quote inside a quoted field is a literal quote: "" -> "
            if (c === quoteChar && line[i + 1] === quoteChar) {
                current += quoteChar;
                i++;
                continue;
            }
            if (c === quoteChar) {
                inQuotes = false;
                continue;
            }
            current += c;
            continue;
        }

        if (c === '"' || c === "'") {
            inQuotes = true;
            quoteChar = c;
            continue;
        }
        if (c === ",") {
            fields.push(current);
            current = "";
            continue;
        }
        current += c;
    }

    // Whatever is left after the last comma is the final field. Forgetting this
    // is the classic off-by-one in a scanner like this.
    fields.push(current);
    return fields;
}

// ---------------------------------------------------------------------------
// build: give each field a sensible type.
// ---------------------------------------------------------------------------
function coerce(raw) {
    var s = raw.trim();
    if (s === "") return "";
    if (s === "null") return null;
    if (s === "true") return true;
    if (s === "false") return false;

    // Only treat it as a number if the whole field is one. Number(" 12 ") is 12,
    // but Number("12abc") is NaN, and Number("") is 0 -- which is why the empty
    // case is handled above.
    var n = Number(s);
    if (!isNaN(n) && String(n) === s) return n;

    return s;
}

// ---------------------------------------------------------------------------
// emit
// ---------------------------------------------------------------------------
function transform(input) {
    var line = input.replace(/\r/g, "").split("\n")[0];
    if (line.trim() === "") {
        throw new Error("no input line to parse");
    }

    var values = splitFields(line).map(coerce);
    return JSON.stringify(values, null, 2);
}
