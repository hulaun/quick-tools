// Java .properties / .env  ->  JSON
//
// YOURS TO WRITE.  Difficulty: 1 of 4 -- start here.
//
//     # database
//     db.host=localhost
//     db.port=5432
//     name = John Doe
//
//     ->  { "db": { "host": "localhost", "port": 5432 }, "name": "John Doe" }
//
// What this one teaches: line-oriented parsing, and building nested objects
// from flat dotted keys. No character-level scanner needed -- work line by
// line. The dotted-key nesting is the interesting part: "db.host" has to
// create the "db" object if it is not already there, then set "host" on it.
//
// The rules are in README.md under "properties-to-json".

var name = "Properties to json";
var group = "Json";
var tags = ["properties", "env", "config", "ini"];

function transform(input) {
    // TODO: your parser here.
    //
    // Sketch:
    //   1. Split the input into lines.
    //   2. Skip blanks and comments.
    //   3. Split each line on the FIRST separator only.
    //   4. Trim, strip surrounding quotes, coerce the type.
    //   5. Assign into the result, creating objects for dotted keys.
    throw new Error("not implemented yet: see scripts/README.md");

    // return JSON.stringify(result, null, 2);
}
