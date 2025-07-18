//// [tests/cases/conformance/es7/exponentiationOperator/exponentiationOperatorInTemplateStringWithSyntaxError2.ts] ////

//// [exponentiationOperatorInTemplateStringWithSyntaxError2.ts]
var t1 = 10;
var t2 = 10;
var s;

// Error: early syntax error using ES7 SimpleUnaryExpression on left-hand side without ()
// With templateHead
`hello ${-t1 ** t2 - t1}`;
`hello ${-++t1 ** t2 - t1}`;
`hello ${-t1++ ** t2 - t1}`;
`hello ${!t1 ** t2 ** --t1 }`;
`hello ${typeof t1 ** t2 ** t1}`;
`hello ${1 + typeof t1 ** t2 ** t1}`;

`hello ${-t1 ** t2 - t1}${-t1 ** t2 - t1}`;
`hello ${-++t1 ** t2 - t1}${-++t1 ** t2 - t1}`;
`hello ${-t1++ ** t2 - t1}${-t1++ ** t2 - t1}`;
`hello ${!t1 ** t2 ** --t1 }${!t1 ** t2 ** --t1 }`;
`hello ${typeof t1 ** t2 ** t1}${typeof t1 ** t2 ** t1}`;
`hello ${1 + typeof t1 ** t2 ** t1}${1 + typeof t1 ** t2 ** t1}`;

`hello ${-t1 ** t2 - t1} hello world ${-t1 ** t2 - t1}`;
`hello ${-++t1 ** t2 - t1} hello world ${-++t1 ** t2 - t1}`;
`hello ${-t1++ ** t2 - t1} hello world ${-t1++ ** t2 - t1}`;
`hello ${!t1 ** t2 ** --t1 } hello world ${!t1 ** t2 ** --t1 }`;
`hello ${typeof t1 ** t2 ** t1} hello world ${typeof t1 ** t2 ** t1}`;
`hello ${1 + typeof t1 ** t2 ** t1} hello world ${1 + typeof t1 ** t2 ** t1}`;


//// [exponentiationOperatorInTemplateStringWithSyntaxError2.js]
var t1 = 10;
var t2 = 10;
var s;
// Error: early syntax error using ES7 SimpleUnaryExpression on left-hand side without ()
// With templateHead
`hello ${Math.pow(-t1, t2) - t1}`;
`hello ${Math.pow(-++t1, t2) - t1}`;
`hello ${Math.pow(-t1++, t2) - t1}`;
`hello ${Math.pow(!t1, Math.pow(t2, --t1))}`;
`hello ${Math.pow(typeof t1, Math.pow(t2, t1))}`;
`hello ${1 + Math.pow(typeof t1, Math.pow(t2, t1))}`;
`hello ${Math.pow(-t1, t2) - t1}${Math.pow(-t1, t2) - t1}`;
`hello ${Math.pow(-++t1, t2) - t1}${Math.pow(-++t1, t2) - t1}`;
`hello ${Math.pow(-t1++, t2) - t1}${Math.pow(-t1++, t2) - t1}`;
`hello ${Math.pow(!t1, Math.pow(t2, --t1))}${Math.pow(!t1, Math.pow(t2, --t1))}`;
`hello ${Math.pow(typeof t1, Math.pow(t2, t1))}${Math.pow(typeof t1, Math.pow(t2, t1))}`;
`hello ${1 + Math.pow(typeof t1, Math.pow(t2, t1))}${1 + Math.pow(typeof t1, Math.pow(t2, t1))}`;
`hello ${Math.pow(-t1, t2) - t1} hello world ${Math.pow(-t1, t2) - t1}`;
`hello ${Math.pow(-++t1, t2) - t1} hello world ${Math.pow(-++t1, t2) - t1}`;
`hello ${Math.pow(-t1++, t2) - t1} hello world ${Math.pow(-t1++, t2) - t1}`;
`hello ${Math.pow(!t1, Math.pow(t2, --t1))} hello world ${Math.pow(!t1, Math.pow(t2, --t1))}`;
`hello ${Math.pow(typeof t1, Math.pow(t2, t1))} hello world ${Math.pow(typeof t1, Math.pow(t2, t1))}`;
`hello ${1 + Math.pow(typeof t1, Math.pow(t2, t1))} hello world ${1 + Math.pow(typeof t1, Math.pow(t2, t1))}`;
