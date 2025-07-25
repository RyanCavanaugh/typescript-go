//// [tests/cases/conformance/jsdoc/declarations/jsDeclarationsTypeReferences2.ts] ////

//// [something.ts]
export const o = {
    a: 1,
    m: 1
}

//// [index.js]
const{ a, m } = require("./something").o;

const thing = a + m

module.exports = {
    thing
};


//// [something.js]
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.o = void 0;
exports.o = {
    a: 1,
    m: 1
};
//// [index.js]
const { a, m } = require("./something").o;
const thing = a + m;
module.exports = {
    thing
};


//// [something.d.ts]
export declare const o: {
    a: number;
    m: number;
};
//// [index.d.ts]
declare const _default: {
    thing: number;
};
export = _default;
