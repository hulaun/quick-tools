// Java toString() output  ->  JSON
//
// THIS ONE IS YOURS TO WRITE. The scaffolding around it is finished: save this
// file and it reloads instantly, and `quicktools.exe -test-scripts` runs the
// fixtures in java-to-json.test.json. They are currently red.
//
// The grammar, the type rules and the ambiguous cases are written up in
// README.md in this folder. Read that first -- the hard part of this problem is
// deciding what the input *means*, not writing the loop.
//
// csv-line-to-json.js in this folder is a complete worked parser. It solves a
// different problem, but it has the shape you want: scan character by character
// tracking state, build a value, emit JSON.
//
// A sketch of one way through, to ignore freely:
//
//   1. Read the class name and the opening bracket:  Person(  or  Person{
//   2. Loop over "key=value" pairs separated by commas.
//   3. For each value, decide what it is by looking at the first character:
//        (   or  {    a nested object -- recurse
//        [            a list -- recurse for each element
//        '   or  "    a quoted string -- read to the closing quote
//        otherwise    a bare word -- read until the field ends
//   4. Bare words become numbers, booleans or null where they look like it.
//
// Step 3's "read until the field ends" is the interesting part, because a bare
// value may itself contain commas. README.md defines the rule.

var name = "Java tostring to json";
var group = "Json";
var tags = ["j2j", "javajson", "tostring", "lombok"];

function transform(input) {
    // TODO: replace this with a real parser.
    //
    // parse(input) should return an ordinary JavaScript value -- an object, an
    // array, a string, a number, a boolean or null -- and the line below turns
    // it into the JSON the fixtures expect.
    throw new Error("not implemented yet: see scripts/README.md");

    // return JSON.stringify(parse(input), null, 2);
}
