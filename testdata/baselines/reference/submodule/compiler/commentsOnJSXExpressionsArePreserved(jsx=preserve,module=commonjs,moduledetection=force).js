//// [tests/cases/compiler/commentsOnJSXExpressionsArePreserved.tsx] ////

//// [commentsOnJSXExpressionsArePreserved.tsx]
// file is intentionally not a module - this tests for a crash in the module/system transforms alongside the `react-jsx` and `react-jsxdev` outputs
namespace JSX {}
class Component {
    render() {
        return <div>
            {/* missing */}
            {null/* preserved */}
            {
                // ??? 1
            }
            { // ??? 2
            }
            {// ??? 3
            }
            {
                // ??? 4
            /* ??? 5 */}
        </div>;
    }
}

//// [commentsOnJSXExpressionsArePreserved.jsx]
"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
class Component {
    render() {
        return <div>
            
            {null /* preserved */}
            
            
            
            
        </div>;
    }
}
