// Java class fields  ->  a JSON template
//
//     public class SysMenu {
//         private static final long serialVersionUID = 1L;
//
//         /** Menu ID */
//         private Long menuId;
//     }
//
//     ->  { "menuId": "" }
//
// Every field name becomes a key and every value is the empty string: this is a
// skeleton to fill in, not a conversion of data, so the Java type is discarded
// along with the class name, the comments and any initialiser.
//
// One pass over the source. The whole problem is deciding what counts as a
// field, and three things make that harder than it looks:
//
//   - A keyword is a token, not a substring. "static" appears inside the
//     perfectly ordinary field name "staticCount", and so do "class" in
//     "className" and "return" in "returnCode". Nothing here matches a keyword
//     against a whole line.
//   - Brace depth is the only thing separating a field from an assignment.
//     "count = count + 1;" inside a method body is a statement ending in a
//     semicolon and looks exactly like a field with an initialiser. Depth 0 is
//     a bare list of fields pasted with no class around them, depth 1 is a
//     class body, and anything deeper is inside a method.
//   - Comments and string literals hide semicolons and braces. "a; b" is one
//     value, not the end of a statement, so both are consumed by the scanner
//     before anything counts a delimiter.
//
// Rejecting bad input needs no rule of its own: text with no field-shaped
// statement in it produces no names, and no names is the error.

var name = "Java class to json";
var group = "Json";
var tags = ["j2j", "javajson", "pojo", "dto", "entity", "template"];

var identifier = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

function transform(input) {
    var names = fields(input);
    if (!names.length) {
        throw new Error("no fields found");
    }
    var out = {};
    for (var i = 0; i < names.length; i++) {
        out[names[i]] = "";
    }
    return JSON.stringify(out, null, 2);
}

// fields scans the source once and returns the field names in declaration
// order. It accumulates the current statement in buf and hands it to collect at
// each semicolon; an opening or closing brace throws the buffer away, because
// what precedes a brace is a class or method header and never a field.
function fields(src) {
    var names = [];
    var buf = "";
    var depth = 0;
    var i = 0;

    while (i < src.length) {
        var c = src.charAt(i);

        if (c === "/" && src.charAt(i + 1) === "/") {
            i = skipLineComment(src, i);
            continue;
        }
        if (c === "/" && src.charAt(i + 1) === "*") {
            i = skipBlockComment(src, i);
            continue;
        }
        if (c === '"' || c === "'") {
            i = skipLiteral(src, i);
            buf += " ";
            continue;
        }
        // An annotation belongs to the declaration but is not part of it, and
        // @TableId(value = "menu_id") would otherwise contribute a parenthesis
        // and read as a method signature.
        if (c === "@") {
            i = skipAnnotation(src, i);
            buf += " ";
            continue;
        }
        // Everything after "=" is thrown away. Doing it here rather than in
        // collect is also what keeps an array initialiser or a lambda body from
        // feeding its braces to the depth counter.
        if (c === "=") {
            i = skipInitialiser(src, i + 1);
            continue;
        }

        i++;
        if (c === "{") {
            depth++;
            buf = "";
        } else if (c === "}") {
            depth--;
            buf = "";
        } else if (c === ";") {
            if (depth <= 1) {
                collect(buf, names);
            }
            buf = "";
        } else {
            buf += c;
        }
    }
    return names;
}

// collect turns one statement into zero or more field names.
function collect(stmt, names) {
    // Generic arguments have to go before the split, because the comma in
    // Map<String, Long> is not the comma that separates two declarators. Array
    // brackets become spaces so that both String[] tags and int counts[] leave
    // the name as the last token.
    stmt = stripAngles(stmt).replace(/[\[\]]/g, " ");

    var parts = stmt.split(",");
    var head = words(parts[0]);
    if (head.length < 2) {
        return; // a declaration is at least a type and a name
    }
    // An interface method -- "String getName();" -- is a statement ending in a
    // semicolon at class-body depth, so the parenthesis is what rules it out.
    if (parts[0].indexOf("(") >= 0) {
        return;
    }
    if (head[0] === "package" || head[0] === "import") {
        return;
    }
    for (var i = 0; i < head.length - 1; i++) {
        if (head[i] === "static") {
            return; // a constant is not data
        }
    }

    push(names, head[head.length - 1]);
    // Any further declarators are bare names: private String first, last;
    for (var j = 1; j < parts.length; j++) {
        push(names, trim(parts[j]));
    }
}

function push(names, word) {
    if (identifier.test(word)) {
        names.push(word);
    }
}

// stripAngles removes balanced <...> groups. It tolerates an unmatched ">"
// rather than failing, since the input is often a fragment pasted mid-file.
function stripAngles(s) {
    var out = "";
    var depth = 0;
    for (var i = 0; i < s.length; i++) {
        var c = s.charAt(i);
        if (c === "<") {
            depth++;
        } else if (c === ">") {
            if (depth > 0) depth--;
        } else if (depth === 0) {
            out += c;
        }
    }
    return out;
}

function skipLineComment(s, i) {
    while (i < s.length && s.charAt(i) !== "\n") i++;
    return i;
}

function skipBlockComment(s, i) {
    i += 2;
    while (i < s.length && !(s.charAt(i) === "*" && s.charAt(i + 1) === "/")) i++;
    return i + 2;
}

// skipLiteral returns the index just past a string or character literal.
// A backslash escape may hide the closing quote.
function skipLiteral(s, i) {
    var quote = s.charAt(i);
    i++;
    while (i < s.length) {
        if (s.charAt(i) === "\\") {
            i += 2;
            continue;
        }
        if (s.charAt(i) === quote) {
            return i + 1;
        }
        i++;
    }
    return i;
}

function skipAnnotation(s, i) {
    i++; // the @
    while (i < s.length && /[A-Za-z0-9_$.]/.test(s.charAt(i))) i++;
    var j = i;
    while (j < s.length && /\s/.test(s.charAt(j))) j++;
    if (s.charAt(j) === "(") {
        return skipBalanced(s, j);
    }
    return i;
}

// skipBalanced returns the index just past a balanced bracket group, ignoring
// brackets that appear inside a literal.
function skipBalanced(s, i) {
    var open = s.charAt(i);
    var close = open === "(" ? ")" : open === "[" ? "]" : "}";
    var depth = 0;
    while (i < s.length) {
        var c = s.charAt(i);
        if (c === '"' || c === "'") {
            i = skipLiteral(s, i);
            continue;
        }
        if (c === open) {
            depth++;
        } else if (c === close) {
            depth--;
            if (depth === 0) return i + 1;
        }
        i++;
    }
    return i;
}

// skipInitialiser runs from just after the "=" to the ";" that ends the
// statement or the "," that starts the next declarator, consuming neither.
// Nesting is tracked so a comma inside {1, 2} or f(a, b) does not look like the
// end. A negative nesting count means the "=" was part of a comparison in a
// method body rather than an assignment, and scanning resumes where it is.
function skipInitialiser(s, i) {
    var nest = 0;
    while (i < s.length) {
        var c = s.charAt(i);
        if (c === '"' || c === "'") {
            i = skipLiteral(s, i);
            continue;
        }
        if (c === "/" && s.charAt(i + 1) === "/") {
            i = skipLineComment(s, i);
            continue;
        }
        if (c === "/" && s.charAt(i + 1) === "*") {
            i = skipBlockComment(s, i);
            continue;
        }
        if (c === "(" || c === "[" || c === "{") {
            nest++;
        } else if (c === ")" || c === "]" || c === "}") {
            nest--;
            if (nest < 0) return i;
        } else if (nest === 0 && (c === ";" || c === ",")) {
            return i;
        }
        i++;
    }
    return i;
}

function trim(s) {
    return s.replace(/^\s+|\s+$/g, "");
}

function words(s) {
    s = trim(s);
    return s === "" ? [] : s.split(/\s+/);
}
