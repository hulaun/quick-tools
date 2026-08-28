// A cURL command  ->  JSON describing the request
//
// YOURS TO WRITE.  Difficulty: 3 of 4.
//
// Paste a "Copy as cURL" from browser devtools and get back a readable
// description of the request:
//
//     curl 'https://api.example.com/users?page=2' -X POST
//          -H 'Content-Type: application/json'
//          --data-raw '{"a":1}'
//
//     (devtools puts a backslash at the end of each line to continue it; your
//     tokeniser has to swallow those)
//
//     ->  { "method": "POST", "url": "...", "headers": {...}, "body": "..." }
//
// What this one teaches: writing a real tokeniser. The input is a shell command
// line, so you cannot split on spaces -- a quoted argument may contain spaces,
// colons and its own punctuation. This is the same character-by-character
// scanning technique as csv-line-to-json.js, applied to a messier grammar, and
// it is the single most reusable skill in this folder.
//
// Once tokenised, the rest is a loop over the arguments deciding what each flag
// means. Do that as a separate pass -- mixing tokenising and interpreting is
// what makes this kind of parser unreadable.

var name = "Curl to json";
var group = "Json";
var tags = ["curl", "http", "request", "devtools"];

function transform(input) {
    // TODO: your parser here.
    //
    // Sketch:
    //   1. Tokenise: walk the string, tracking whether you are inside a single
    //      or double quote. A backslash followed by a newline is a line
    //      continuation and should vanish. Push a token on unquoted whitespace.
    //   2. Walk the tokens:
    //        -X / --request      the next token is the method
    //        -H / --header       the next token is "Name: value"
    //        -d / --data / --data-raw / --data-binary   the next token is the body
    //        anything not a flag and not consumed      the URL
    //   3. Default the method to GET, or POST if there is a body and no -X.
    throw new Error("not implemented yet: see scripts/README.md");
}
