// JSON  ->  a single-row INSERT statement
//
// YOURS TO WRITE.  Difficulty: 4 of 7.
//
//     { "sysMenu": { "menuId": 1, "menuName": "Home" } }
//
//     ->  INSERT INTO sys_menu (menu_id, menu_name) VALUES (1, 'Home');
//
// ONE ROW, not a batch. Multi-row insert syntax differs between databases and
// picking one would make the transform wrong everywhere else; an array as input
// is an error, deliberately.
//
// What this one teaches: turning a value into text for a target that has its
// own quoting rules, and doing it without ever gluing a raw string into the
// output. Every branch of the value writer is a small decision -- README.md
// lists them and the fixtures pin every one.
//
// It shares almost everything with json-to-update-query.js: the table name
// rule, the column name rule, the value writer. Write one, then the other; if
// you find yourself copying the third function across, that is the sign this
// pair wanted to be one file with two entry points.
//
// A sketch of one way through, to ignore freely:
//
//   1. Parse. Reject an array, a scalar, and an empty object.
//   2. Work out the table name, then the row -- README.md has the rule.
//   3. Column names: the JSON key, camelCase turned into snake_case.
//   4. Values: one function, dispatching on type. Strings are the only case
//      that needs care, and only because of the quote.
//   5. Assemble.

var name = "Json to insert query";
var group = "Sql";
var tags = ["sql", "insert", "row", "db"];

function transform(input) {
    // TODO: your writer here.
    throw new Error("not implemented yet: see scripts/README.md");
}
