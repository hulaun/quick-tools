// Java stack trace  ->  JSON
//
// YOURS TO WRITE.  Difficulty: 4 of 4 -- the capstone.
//
//     java.lang.IllegalStateException: boom
//         at com.example.Service.doWork(Service.java:42)
//     Caused by: java.lang.NullPointerException: inner
//         at com.example.Repo.find(Repo.java:88)
//         ... 3 more
//
//     ->  { "type": ..., "message": ..., "frames": [...], "causedBy": { ... } }
//
// What this one teaches: recursion over a structure that is not bracketed. The
// "Caused by:" chain nests arbitrarily deep, but nothing marks where one
// exception ends and the next begins except the next "Caused by:" line. You
// have to decide the boundaries yourself while scanning forwards.
//
// This is the closest of the four to the Java toString parser, and a good one
// to do last -- if that one is written, this will feel familiar.
//
// The frame line has a shape worth pulling apart carefully:
//
//     at com.example.Service.doWork(Service.java:42)
//        \_____________________/ \____/ \_________/ \/
//         class (dots!)          method   file      line
//
// The class and method are separated by the LAST dot before the "(", because
// the package itself contains dots.

var name = "Stacktrace to json";
var group = "Json";
var tags = ["java", "stacktrace", "exception", "trace"];

function transform(input) {
    // TODO: your parser here.
    //
    // Sketch:
    //   1. Split into lines and trim each.
    //   2. The first line is "type: message" -- split on the FIRST ": ".
    //      A trace with no message has no colon at all.
    //   3. Collect "at ..." lines into frames until you hit "Caused by:"
    //      or run out. Ignore "... N more".
    //   4. On "Caused by:", recurse on the rest and attach it as causedBy.
    //   5. Use null for causedBy when there is none.
    throw new Error("not implemented yet: see scripts/README.md");
}
