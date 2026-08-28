// URL query string  ->  JSON
//
// YOURS TO WRITE.  Difficulty: 2 of 4.
//
//     https://api.example.com/users?page=2&tag=a&tag=b
//
//     ->  { "page": "2", "tag": ["a", "b"] }
//
// What this one teaches: normalising messy real-world input before parsing it,
// and the "same key twice" problem. A key that appears once is a value; the
// same key appearing again has to turn what is already there into an array
// without losing the first value.
//
// Note values stay STRINGS here -- no type coercion. A "1" in a URL is usually
// an id, and turning it into a number is how you lose a leading zero. This is a
// deliberate exception to the shared type rules; README.md says so.
//
// decodeURIComponent() does the percent-decoding for you. "+" means space and
// it does NOT handle that -- you do.

var name = "Query string to json";
var group = "Json";
var tags = ["url", "querystring", "params"];

function transform(input) {
    // TODO: your parser here.
    //
    // Sketch:
    //   1. If there is a "?", keep what follows it. Drop any "#fragment".
    //   2. Split on "&", ignoring empty pieces.
    //   3. Split each piece on the first "=" only.
    //   4. Replace "+" with " ", then decodeURIComponent both halves.
    //   5. Assign, promoting to an array on a repeated key.
    //   6. Handle bracket keys: u[name]=jo  and  a[]=1
    throw new Error("not implemented yet: see scripts/README.md");
}
