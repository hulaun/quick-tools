// JSON  ->  URL query string
//
// YOURS TO WRITE.  Difficulty: 3 of 7.
//
//     { "page": 2, "tag": ["a", "b"], "u": { "name": "jo" } }
//
//     ->  page=2&tag=a&tag=b&u[name]=jo
//
// The reverse of query-string-to-json.js. Write that one first if you have not
// -- reading a format teaches you where its edges are, and then emitting it is
// mostly a matter of not walking off them.
//
// What this one teaches: flattening a tree into a flat list of paths. A nested
// object has no direct representation in a query string, so the nesting has to
// be encoded into the key. That is a recursion carrying an accumulated prefix
// down with it, which is the same shape as walking a directory tree.
//
// A sketch of one way through, to ignore freely:
//
//   1. Parse the input. Reject anything that is not an object at the top.
//   2. Recurse over it with (prefix, value), collecting "key=value" strings.
//        object   ->  recurse per entry, prefix becomes  prefix[key]
//        array    ->  README.md decides; it depends what is inside
//        scalar   ->  emit  encode(prefix) = encode(value)
//   3. Join with "&".
//
// encodeURIComponent() does the escaping. Note it also escapes "[" and "]",
// which is not what we want -- README.md says why and what to do about it.

var name = "Json to query string";
var group = "Json";
var tags = ["url", "querystring", "params", "encode"];

function transform(input) {
    // TODO: your writer here.
    throw new Error("not implemented yet: see scripts/README.md");
}
