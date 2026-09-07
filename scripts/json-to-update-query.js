// JSON  ->  a single-row UPDATE statement
//
// YOURS TO WRITE.  Difficulty: 5 of 7.
//
//     { "sysMenu": { "menuId": 1, "menuName": "Home", "path": "/home" } }
//
//     ->  UPDATE sys_menu SET menu_name = 'Home', path = '/home'
//         WHERE menu_id = 1;                              (all on one line)
//
// ONE ROW, same reason as the insert: batch update syntax is different in every
// database. An array is an error.
//
// The table name, the column names and the value writer are exactly the insert
// transform's, so do that one first and this is mostly assembly. What is new is
// the one decision the insert never had to make:
//
//   WHICH KEY IS THE WHERE CLAUSE?
//
// Nothing in the JSON says. Every key looks the same as every other. There has
// to be a rule, it has to be applied consistently, and it will occasionally be
// wrong -- README.md states the one this project uses and the fixtures pin each
// branch of it. Whatever key it picks is the WHERE and is NOT in the SET; an
// UPDATE that also sets the column it matches on is a bug that hides for a long
// time, because it works fine until the value is wrong.
//
// A sketch of one way through, to ignore freely:
//
//   1. Parse. Reject an array, a scalar, an empty object, and a one-key object
//      -- with one column there is nothing left to SET.
//   2. Table name and row, as in the insert.
//   3. Choose the key column by the README rule.
//   4. SET is every other key, in input order.

var name = "Json to update query";
var group = "Sql";
var tags = ["sql", "update", "row", "db"];

function transform(input) {
    // TODO: your writer here.
    throw new Error("not implemented yet: see scripts/README.md");
}
