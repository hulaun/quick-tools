// JSON  ->  JavaScript object literal
//
// YOURS TO WRITE.  Difficulty: 7 of 7.
//
//     { "name": "jo", "my-key": 1 }
//
//     ->  {
//           name: 'jo',
//           'my-key': 1
//         }
//
// The reverse of js-object-to-json.js. Do that one first -- this reuses its
// reader almost unchanged, and the two together are one round trip you can
// check by hand.
//
// It looks like the easiest script in the folder and it is the hardest, for one
// reason:
//
//     { "id": 9007199254740993 }
//
// JSON.parse turns that into a float, and 9007199254740992 is what comes back
// out. Every Long id in a Java response is in the range where this happens. So
// the number cannot go through a JavaScript number at all -- it has to be
// carried from the input to the output as the text it was written as, which
// means reading the JSON yourself rather than asking JSON.parse for it. A
// fixture pins this; passing it is the whole exercise.
//
// The rest is a recursive writer, and two small decisions:
//
//   - a key is left unquoted when it is a valid identifier, quoted otherwise
//   - a string is single-quoted, so an apostrophe inside it needs escaping
//
// README.md has the exact rules and the layout the fixtures expect.

var name = "Json to js object";
var group = "Json";
var tags = ["js", "jsobject", "literal", "unquote"];

function transform(input) {
    // TODO: your reader and writer here.
    throw new Error("not implemented yet: see scripts/README.md");
}
