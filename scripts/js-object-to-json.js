// JavaScript object literal  ->  JSON
//
// THIS ONE IS YOURS TO WRITE. Fixtures are in js-object-to-json.test.json and
// the rules are in README.md.
//
// This is the easier of the two parsers and a good one to do first: the input
// is already very close to JSON. What it adds is:
//
//     unquoted keys        { a: 1 }
//     single quotes        { a: 'x' }
//     trailing commas      { a: 1, }
//     comments             { /* note */ a: 1 }  and  { a: 1 // note
//
// A tempting shortcut is eval(), or `new Function("return " + input)`. Do not.
// This runs against whatever happens to be on your clipboard, and both of those
// execute it. A parser that reads the text is barely more work and cannot be
// made to run anything.
//
// The structure is the classic recursive descent pair:
//
//     parseValue()   look at the next character, dispatch to the right reader
//     parseObject()  '{' then key : value pairs then '}'
//     parseArray()   '[' then values then ']'
//     parseString()  a quote, then characters until the matching quote
//     parseNumber()  digits, and the things that can appear in a number
//
// Each one consumes from a shared position and returns a value; nesting falls
// out of them calling each other. Keep a single index into the string and a
// skipWhitespace() that also skips comments -- doing that in one place is what
// keeps the rest readable.

var name = "Js object to json";
var group = "Json";
var tags = ["js", "jsobject", "literal", "json5"];

function transform(input) {
    // TODO: replace this with a real parser.
    throw new Error("not implemented yet: see scripts/README.md");

    // return JSON.stringify(parse(input), null, 2);
}
