package fourslash_test

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/fourslash"
	"github.com/microsoft/typescript-go/internal/testutil"
)

func TestMoveToFileBasic(t *testing.T) {
	t.Parallel()
	defer testutil.RecoverAndFail(t, "Panic on fourslash test")

	const content = `
// @Filename: /a.ts
export const x = 1;
[|export const y = x + 1;|]
y;

// @Filename: /tsconfig.json
{ "compilerOptions": { "module": "es2015" } }`

	f, done := fourslash.NewFourslash(t, nil /*capabilities*/, content)
	defer done()

	ranges := f.Ranges()
	f.GoToSelectRange(t, ranges[0])

	f.VerifyMoveToFile(t, &fourslash.MoveToFileOptions{
		TargetFile: "/b.ts",
		NewFileContents: map[string]string{
			"/a.ts": `import { y } from "./b";

export const x = 1;
y;
`,
			"/b.ts": `import { x } from "./a";

export const y = x + 1;
`,
		},
	})
}
