currentDirectory::/home/src/workspaces/project
useCaseSensitiveFileNames::true
Input::
//// [/home/src/workspaces/project/index.ts] *new* 
const a: number = 1;
const b: string = 2;
const c: number = 3;
const d: number = 4;
const e: number = 5;
//// [/home/src/workspaces/project/tsconfig.json] *new* 
{
    "compilerOptions": {
        "expandedErrorContext": 2
    }
}

tsgo 
ExitStatus:: DiagnosticsPresent_OutputsGenerated
Output::
[96mindex.ts[0m:[93m2[0m:[93m7[0m - [91merror[0m[90m TS2322: [0mType 'number' is not assignable to type 'string'.

[7m2[0m const b: string = 2;
[7m [0m [91m      ~[0m


Found 1 error in index.ts[90m:2[0m

//// [/home/src/tslibs/TS/Lib/lib.es2025.full.d.ts] *Lib*
/// <reference no-default-lib="true"/>
interface Boolean {}
interface Function {}
interface CallableFunction {}
interface NewableFunction {}
interface IArguments {}
interface Number { toExponential: any; }
interface Object {}
interface RegExp {}
interface String { charAt: any; }
interface Array<T> { length: number; [n: number]: T; }
interface ReadonlyArray<T> {}
interface SymbolConstructor {
    (desc?: string | number): symbol;
    for(name: string): symbol;
    readonly toStringTag: symbol;
}
declare var Symbol: SymbolConstructor;
interface Symbol {
    readonly [Symbol.toStringTag]: string;
}
declare const console: { log(msg: any): void; };
//// [/home/src/workspaces/project/index.js] *new* 
"use strict";
const a = 1;
const b = 2;
const c = 3;
const d = 4;
const e = 5;


