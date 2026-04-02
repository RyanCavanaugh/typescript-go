package ls

import (
	"context"
	"slices"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/astnav"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/ls/change"
	"github.com/microsoft/typescript-go/internal/ls/lsutil"
	"github.com/microsoft/typescript-go/internal/lsp/lsproto"
	"github.com/microsoft/typescript-go/internal/modulespecifiers"
	"github.com/microsoft/typescript-go/internal/printer"
	"github.com/microsoft/typescript-go/internal/tspath"
)

const (
	refactorNameMoveToFile = "Move to file"
	moveToFileActionName   = "Move to file"
)

var moveToFileActionKind = lsproto.CodeActionKind("refactor.move.file")

var moveToFileRefactorProvider = &RefactorProvider{
	Name:        refactorNameMoveToFile,
	Description: diagnostics.Move_to_file.String(),
	Kinds:       []lsproto.CodeActionKind{moveToFileActionKind},
	GetAvailableActions: func(ctx context.Context, refactorContext *RefactorContext) []*RefactorActionInfo {
		return getRefactorActionsToMoveToFile(ctx, refactorContext)
	},
	GetEditsForAction: func(ctx context.Context, refactorContext *RefactorContext, actionName string, args *lsproto.InteractiveRefactorArguments) *RefactorEditInfo {
		return getRefactorEditsToMoveToFile(ctx, refactorContext, actionName, args)
	},
}

// --- Available Actions ---

func getRefactorActionsToMoveToFile(_ context.Context, refactorContext *RefactorContext) []*RefactorActionInfo {
	file := refactorContext.SourceFile
	statements := getStatementsToMove(refactorContext)

	// If trigger was automatic/implicit and selection is inside a block, don't show
	if refactorContext.TriggerKind == lsproto.CodeActionTriggerKindAutomatic && refactorContext.EndPosition > 0 {
		startToken := astnav.GetTokenAtPosition(file, refactorContext.StartPosition)
		endToken := astnav.GetTokenAtPosition(file, refactorContext.EndPosition)
		startAncestor := findBlockLikeAncestor(startToken)
		endAncestor := findBlockLikeAncestor(endToken)
		if startAncestor != nil && !ast.IsSourceFile(startAncestor) &&
			endAncestor != nil && !ast.IsSourceFile(endAncestor) {
			return nil
		}
	}

	if refactorContext.LS.UserPreferences().AllowTextChangesInNewFiles && statements != nil {
		converters := refactorContext.LS.converters
		firstStmt := statements.all[0]
		lastStmt := statements.all[len(statements.all)-1]
		startPos := converters.PositionToLineAndCharacter(file, core.TextPos(astnav.GetStartOfNode(firstStmt, file, false)))
		endPos := converters.PositionToLineAndCharacter(file, core.TextPos(lastStmt.End()))
		rng := lsproto.Range{Start: startPos, End: endPos}
		return []*RefactorActionInfo{
			{
				Name:          moveToFileActionName,
				Description:   diagnostics.Move_to_file.String(),
				Kind:          moveToFileActionKind,
				IsInteractive: true,
				Range:         &rng,
			},
		}
	}

	if refactorContext.LS.UserPreferences().ProvideRefactorNotApplicableReason {
		return []*RefactorActionInfo{
			{
				Name:                moveToFileActionName,
				Description:         diagnostics.Move_to_file.String(),
				Kind:                moveToFileActionKind,
				IsInteractive:       true,
				NotApplicableReason: diagnostics.Selection_is_not_a_valid_statement_or_statements.String(),
			},
		}
	}

	return nil
}

// --- Edits for Action ---

func getRefactorEditsToMoveToFile(_ context.Context, refactorContext *RefactorContext, actionName string, args *lsproto.InteractiveRefactorArguments) *RefactorEditInfo {
	if actionName != moveToFileActionName {
		return nil
	}
	if args == nil {
		return &RefactorEditInfo{
			NotApplicableReason: diagnostics.Cannot_move_to_file_selected_file_is_invalid.String(),
		}
	}

	statements := getStatementsToMove(refactorContext)
	if statements == nil {
		return &RefactorEditInfo{
			NotApplicableReason: diagnostics.Selection_is_not_a_valid_statement_or_statements.String(),
		}
	}

	targetFile := args.TargetFile
	if !tspath.HasJSFileExtension(targetFile) && !tspath.HasTSFileExtension(targetFile) {
		return &RefactorEditInfo{
			NotApplicableReason: diagnostics.Cannot_move_to_file_selected_file_is_invalid.String(),
		}
	}

	program := refactorContext.Program
	oldFile := refactorContext.SourceFile
	ls := refactorContext.LS

	// Check if target file is in the program (if it exists on disk)
	targetSourceFile := program.GetSourceFile(targetFile)
	isForNewFile := targetSourceFile == nil

	if !isForNewFile && targetSourceFile == nil {
		return &RefactorEditInfo{
			NotApplicableReason: diagnostics.Cannot_move_statements_to_the_selected_file.String(),
		}
	}

	tracker := change.NewTracker(context.Background(), program.Options(), ls.FormatOptions(), ls.converters)
	edits := doMoveToFileChange(tracker, oldFile, targetFile, targetSourceFile, isForNewFile, program, statements, ls)
	return edits
}

// --- Core Change Logic ---

func doMoveToFileChange(
	tracker *change.Tracker,
	oldFile *ast.SourceFile,
	targetFileName string,
	targetSourceFile *ast.SourceFile,
	isForNewFile bool,
	program *compiler.Program,
	toMove *toMove,
	ls *LanguageService,
) *RefactorEditInfo {
	ctx := context.Background()
	typeChecker, done := program.GetTypeChecker(ctx)
	defer done()

	useEsModuleSyntax := oldFile.ExternalModuleIndicator != nil
	quotePreference := lsutil.GetQuotePreference(oldFile, ls.UserPreferences())

	var existingTargetLocals map[*ast.Symbol]bool
	if !isForNewFile {
		existingTargetLocals = getExistingLocals(targetSourceFile, toMove.all, typeChecker)
	}

	usage := getUsageInfo(oldFile, toMove.all, typeChecker, existingTargetLocals)

	// Add imports in old file for moved symbols still referenced there
	addImportsForMovedSymbolsToOldFile(tracker, oldFile, targetFileName, usage.oldFileImportsFromTargetFile, program, quotePreference, useEsModuleSyntax)

	// Delete moved statements from old file
	deleteMovedStatements(oldFile, toMove.ranges, tracker)

	// Add exports in old file for symbols still referenced by other code
	addExportsInOldFile(oldFile, usage.targetFileImportsFromOldFile, tracker, useEsModuleSyntax)

	// Prepare the statements to add to target file (with export modifiers)
	body := addExportModifiers(oldFile, toMove.all, usage.oldFileImportsFromTargetFile, useEsModuleSyntax, tracker)

	// Update imports in other files that import from old file
	updateImportsInOtherFiles(tracker, program, oldFile, usage.movedSymbols, targetFileName, quotePreference, ls)

	// Build the result
	changes := tracker.GetChanges()

	result := &RefactorEditInfo{
		Edits: changes,
	}

	if isForNewFile {
		// Generate the new file content
		newFileContent := generateNewFileContent(
			oldFile, targetFileName, body, usage, program, ls,
			quotePreference, useEsModuleSyntax, typeChecker,
		)
		result.NewFiles = append(result.NewFiles, NewFileEdit{
			FileName: targetFileName,
			Content:  newFileContent,
		})
	} else {
		// Insert into existing target file
		insertIntoExistingFile(tracker, targetSourceFile, body, usage, oldFile, program, ls,
			quotePreference, useEsModuleSyntax, typeChecker)
		// Re-get changes now that target file edits are added
		result.Edits = tracker.GetChanges()
	}

	return result
}

// --- Statement Selection ---

// toMove tracks the statements selected for moving.
type toMove struct {
	all    []*ast.Node // All statements to move
	ranges []statementRange
}

// statementRange represents a contiguous range of statements.
type statementRange struct {
	first     *ast.Node
	afterLast *ast.Node // may be nil if range extends to end of file
}

func getStatementsToMove(refactorContext *RefactorContext) *toMove {
	rangeToMove := getRangeToMove(refactorContext)
	if rangeToMove == nil {
		return nil
	}

	var all []*ast.Node
	var ranges []statementRange
	stmts := rangeToMove.toMove
	afterLast := rangeToMove.afterLast

	// Group contiguous allowed statements
	inRange := false
	rangeStart := -1
	for i, stmt := range stmts {
		if isAllowedStatementToMove(stmt) {
			if !inRange {
				inRange = true
				rangeStart = i
			}
			all = append(all, stmt)
		} else {
			if inRange {
				ranges = append(ranges, statementRange{
					first:     stmts[rangeStart],
					afterLast: afterLast,
				})
				inRange = false
			}
		}
	}
	if inRange {
		ranges = append(ranges, statementRange{
			first:     stmts[rangeStart],
			afterLast: afterLast,
		})
	}

	if len(all) == 0 {
		return nil
	}
	return &toMove{all: all, ranges: ranges}
}

type rangeToMoveResult struct {
	toMove    []*ast.Node
	afterLast *ast.Node
}

func getRangeToMove(refactorContext *RefactorContext) *rangeToMoveResult {
	file := refactorContext.SourceFile
	startPos := refactorContext.StartPosition
	endPos := refactorContext.EndPosition
	if endPos == 0 {
		endPos = startPos
	}

	statements := file.Statements.Nodes

	// Find start statement index
	startNodeIndex := -1
	for i, s := range statements {
		if s.End() > startPos {
			startNodeIndex = i
			break
		}
	}
	if startNodeIndex == -1 {
		return nil
	}

	// Handle overloads at start
	overloadRange := getOverloadRangeToMove(file, statements[startNodeIndex])
	if overloadRange != nil {
		startNodeIndex = overloadRange.start
	}

	// Find end statement index
	endNodeIndex := -1
	for i := startNodeIndex; i < len(statements); i++ {
		if statements[i].End() >= endPos {
			endNodeIndex = i
			break
		}
	}

	// If selection ends before the start of the end statement, go back one
	if endNodeIndex != -1 && endPos <= astnav.GetStartOfNode(statements[endNodeIndex], file, false) {
		endNodeIndex--
	}

	// Handle overloads at end
	if endNodeIndex >= 0 && endNodeIndex < len(statements) {
		endOverloadRange := getOverloadRangeToMove(file, statements[endNodeIndex])
		if endOverloadRange != nil {
			endNodeIndex = endOverloadRange.end
		}
	}

	var toMoveStmts []*ast.Node
	var afterLast *ast.Node
	if endNodeIndex == -1 {
		toMoveStmts = statements[startNodeIndex:]
	} else {
		toMoveStmts = statements[startNodeIndex : endNodeIndex+1]
		if endNodeIndex+1 < len(statements) {
			afterLast = statements[endNodeIndex+1]
		}
	}

	return &rangeToMoveResult{
		toMove:    toMoveStmts,
		afterLast: afterLast,
	}
}

type overloadRange struct {
	start int
	end   int
}

func getOverloadRangeToMove(sourceFile *ast.SourceFile, statement *ast.Node) *overloadRange {
	if !ast.IsFunctionLikeDeclaration(statement) {
		return nil
	}
	sym := statement.Symbol()
	if sym == nil {
		return nil
	}
	declarations := sym.Declarations
	if len(declarations) <= 1 {
		return nil
	}

	if !slices.Contains(declarations, statement) {
		return nil
	}

	firstDecl := declarations[0]
	lastDecl := declarations[len(declarations)-1]
	stmts := sourceFile.Statements.Nodes

	startIdx := -1
	endIdx := -1
	for i, s := range stmts {
		if s.End() >= firstDecl.End() && startIdx == -1 {
			startIdx = i
		}
		if s.End() >= lastDecl.End() && endIdx == -1 {
			endIdx = i
		}
	}
	if startIdx == -1 || endIdx == -1 {
		return nil
	}

	return &overloadRange{start: startIdx, end: endIdx}
}

func isAllowedStatementToMove(statement *ast.Node) bool {
	return !isPureImport(statement) && !ast.IsPrologueDirective(statement)
}

func isPureImport(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindImportDeclaration:
		return true
	case ast.KindImportEqualsDeclaration:
		return !ast.HasSyntacticModifier(node, ast.ModifierFlagsExport)
	case ast.KindVariableStatement:
		decls := node.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes
		for _, d := range decls {
			if d.Initializer() == nil || !ast.IsRequireCall(d.Initializer(), true) {
				return false
			}
		}
		return len(decls) > 0
	default:
		return false
	}
}

// --- Usage Analysis ---

type usageInfo struct {
	movedSymbols                 map[*ast.Symbol]bool
	targetFileImportsFromOldFile map[*ast.Symbol]bool // symbol -> isValidTypeOnlyUseSite
	oldFileImportsFromTargetFile map[*ast.Symbol]bool // symbol -> isValidTypeOnlyUseSite
	oldImportsNeededByTargetFile map[*ast.Symbol]importInfo
	unusedImportsFromOldFile     map[*ast.Symbol]bool
}

type importInfo struct {
	isValidTypeOnlyUseSite bool
	declaration            *ast.Node
}

func getUsageInfo(oldFile *ast.SourceFile, stmtsToMove []*ast.Node, typeChecker *checker.Checker, existingTargetLocals map[*ast.Symbol]bool) *usageInfo {
	movedSymbols := make(map[*ast.Symbol]bool)
	oldImportsNeededByTargetFile := make(map[*ast.Symbol]importInfo)
	targetFileImportsFromOldFile := make(map[*ast.Symbol]bool)
	unusedImportsFromOldFile := make(map[*ast.Symbol]bool)

	// Collect symbols declared in moved statements
	for _, statement := range stmtsToMove {
		forEachTopLevelDeclaration(statement, func(decl *ast.Node) {
			sym := getDeclaredSymbol(decl, typeChecker)
			if sym != nil {
				movedSymbols[sym] = true
			}
		})
	}

	// Analyze references in moved statements
	for _, statement := range stmtsToMove {
		forEachReference(statement, typeChecker, func(sym *ast.Symbol, isValidTypeOnlyUseSite bool) {
			if len(sym.Declarations) == 0 {
				return
			}

			if existingTargetLocals != nil {
				skipped := checker.SkipAlias(sym, typeChecker)
				if existingTargetLocals[skipped] {
					unusedImportsFromOldFile[sym] = true
					return
				}
			}

			importedDecl := findImportDeclaration(sym)
			if importedDecl != nil {
				prev, hasPrev := oldImportsNeededByTargetFile[sym]
				isTypeOnly := isValidTypeOnlyUseSite
				if hasPrev {
					isTypeOnly = prev.isValidTypeOnlyUseSite && isValidTypeOnlyUseSite
				}
				oldImportsNeededByTargetFile[sym] = importInfo{
					isValidTypeOnlyUseSite: isTypeOnly,
					declaration:            importedDecl,
				}
			} else if !movedSymbols[sym] {
				if isTopLevelDeclarationInFile(sym, oldFile) {
					targetFileImportsFromOldFile[sym] = isValidTypeOnlyUseSite
				}
			}
		})
	}

	// Mark imports needed by target as unused in old file
	for sym := range oldImportsNeededByTargetFile {
		unusedImportsFromOldFile[sym] = true
	}

	// Check what remaining statements in old file reference
	oldFileImportsFromTargetFile := make(map[*ast.Symbol]bool)
	for _, statement := range oldFile.Statements.Nodes {
		if containsNode(stmtsToMove, statement) {
			continue
		}

		forEachReference(statement, typeChecker, func(sym *ast.Symbol, isValidTypeOnlyUseSite bool) {
			if movedSymbols[sym] {
				oldFileImportsFromTargetFile[sym] = isValidTypeOnlyUseSite
			}
			delete(unusedImportsFromOldFile, sym)
		})
	}

	return &usageInfo{
		movedSymbols:                 movedSymbols,
		targetFileImportsFromOldFile: targetFileImportsFromOldFile,
		oldFileImportsFromTargetFile: oldFileImportsFromTargetFile,
		oldImportsNeededByTargetFile: oldImportsNeededByTargetFile,
		unusedImportsFromOldFile:     unusedImportsFromOldFile,
	}
}

func getExistingLocals(sourceFile *ast.SourceFile, stmtsToMove []*ast.Node, typeChecker *checker.Checker) map[*ast.Symbol]bool {
	existingLocals := make(map[*ast.Symbol]bool)
	for _, moduleSpecifier := range sourceFile.Imports() {
		declaration := ast.ImportFromModuleSpecifier(moduleSpecifier)
		if ast.IsImportDeclaration(declaration) {
			importClause := declaration.AsImportDeclaration().ImportClause
			if importClause != nil && importClause.AsImportClause().NamedBindings != nil {
				nb := importClause.AsImportClause().NamedBindings
				if ast.IsNamedImports(nb) {
					for _, e := range nb.AsNamedImports().Elements.Nodes {
						propName := e.AsImportSpecifier().PropertyName
						name := e.AsImportSpecifier().Name()
						lookupNode := name
						if propName != nil {
							lookupNode = propName
						}
						sym := typeChecker.GetSymbolAtLocation(lookupNode)
						if sym != nil {
							existingLocals[checker.SkipAlias(sym, typeChecker)] = true
						}
					}
				}
			}
		}
	}
	return existingLocals
}

// --- Edit Generation ---

func deleteMovedStatements(sourceFile *ast.SourceFile, ranges []statementRange, tracker *change.Tracker) {
	for _, r := range ranges {
		if r.afterLast != nil {
			// Delete from first up to (but not including) afterLast
			// DeleteNodeRange is inclusive of endNode, so we need to compute the correct range.
			// Use ReplaceRangeWithText to delete from the start of r.first to the start of r.afterLast.
			tracker.DeleteNodeRangeExcludingEnd(sourceFile, r.first, r.afterLast, change.LeadingTriviaOptionIncludeAll)
		} else {
			// Delete from first to end of file
			last := r.first
			stmts := sourceFile.Statements.Nodes
			for _, s := range stmts {
				if s.Pos() >= r.first.Pos() {
					last = s
				}
			}
			tracker.DeleteNodeRange(sourceFile, r.first, last, change.LeadingTriviaOptionIncludeAll, change.TrailingTriviaOptionInclude)
		}
	}
}

// addImportsForMovedSymbolsToOldFile adds import statements to the old file for symbols
// that moved to the target file but are still referenced in the old file.
func addImportsForMovedSymbolsToOldFile(
	tracker *change.Tracker,
	oldFile *ast.SourceFile,
	targetFileName string,
	oldFileImportsFromTargetFile map[*ast.Symbol]bool,
	program *compiler.Program,
	quotePreference lsutil.QuotePreference,
	useEsModuleSyntax bool,
) {
	if len(oldFileImportsFromTargetFile) == 0 || !useEsModuleSyntax {
		return
	}

	moduleSpecifier := modulespecifiers.GetModuleSpecifier(
		program.Options(),
		program,
		oldFile,
		oldFile.FileName(),
		"",
		targetFileName,
		modulespecifiers.ModuleSpecifierOptions{},
	)
	if moduleSpecifier == "" {
		return
	}

	quote := "\""
	if quotePreference == lsutil.QuotePreferenceSingle {
		quote = "'"
	}

	// Build the import statement text
	var sb strings.Builder
	sb.WriteString("import { ")
	first := true
	for sym := range oldFileImportsFromTargetFile {
		if !first {
			sb.WriteString(", ")
		}
		sb.WriteString(sym.Name)
		first = false
	}
	sb.WriteString(" } from ")
	sb.WriteString(quote)
	sb.WriteString(moduleSpecifier)
	sb.WriteString(quote)
	sb.WriteString(";\n\n")

	tracker.InsertText(oldFile, lsproto.Position{Line: 0, Character: 0}, sb.String())
}

func addExportsInOldFile(
	oldFile *ast.SourceFile,
	targetFileImportsFromOldFile map[*ast.Symbol]bool,
	tracker *change.Tracker,
	useEsModuleSyntax bool,
) {
	seen := make(map[*ast.Node]bool)
	for sym := range targetFileImportsFromOldFile {
		if len(sym.Declarations) == 0 {
			continue
		}
		for _, decl := range sym.Declarations {
			if !isTopLevelDeclaration(decl) {
				continue
			}
			name := nameOfTopLevelDeclaration(decl)
			if name == nil {
				continue
			}
			top := getTopLevelDeclarationStatement(decl)
			if top != nil && !seen[top] {
				seen[top] = true
				addExportToChanges(oldFile, top, tracker, useEsModuleSyntax)
			}
		}
	}
}

func addExportToChanges(
	sourceFile *ast.SourceFile,
	decl *ast.Node,
	tracker *change.Tracker,
	useEsModuleSyntax bool,
) {
	if isExported(sourceFile, decl, useEsModuleSyntax) {
		return
	}
	if useEsModuleSyntax && !ast.IsExpressionStatement(decl) {
		tracker.InsertModifierBefore(sourceFile, ast.KindExportKeyword, decl)
	}
}

func isExported(sourceFile *ast.SourceFile, decl *ast.Node, useEsModuleSyntax bool) bool {
	if useEsModuleSyntax {
		return !ast.IsExpressionStatement(decl) && ast.HasSyntacticModifier(decl, ast.ModifierFlagsExport)
	}
	return false
}

func addExportModifiers(
	oldFile *ast.SourceFile,
	stmtsToMove []*ast.Node,
	oldFileImportsFromTargetFile map[*ast.Symbol]bool,
	useEsModuleSyntax bool,
	tracker *change.Tracker,
) []*ast.Node {
	if !useEsModuleSyntax {
		return stmtsToMove
	}

	// Determine which moved symbols need to be exported
	needExportSymbols := make(map[*ast.Symbol]bool)
	for sym := range oldFileImportsFromTargetFile {
		needExportSymbols[sym] = true
	}

	emitContext := printer.NewEmitContext()
	factory := &emitContext.Factory.NodeFactory

	var result []*ast.Node
	for _, stmt := range stmtsToMove {
		needsExport := false
		if isTopLevelDeclarationStatement(stmt) && !isExported(oldFile, stmt, useEsModuleSyntax) {
			forEachTopLevelDeclaration(stmt, func(decl *ast.Node) {
				sym := decl.Symbol()
				if sym != nil && needExportSymbols[sym] {
					needsExport = true
				}
			})
		}

		if needsExport {
			exported := addEs6Export(stmt, factory, emitContext)
			if exported != nil {
				result = append(result, exported)
				continue
			}
		}
		result = append(result, stmt)
	}
	return result
}

func addEs6Export(d *ast.Node, factory *ast.NodeFactory, emitContext *printer.EmitContext) *ast.Node {
	exportKeyword := factory.NewToken(ast.KindExportKeyword)
	var existingModifiers []*ast.Node
	if d.Modifiers() != nil {
		existingModifiers = d.Modifiers().Nodes
	}
	modifiers := append([]*ast.Node{exportKeyword}, existingModifiers...)
	modifierList := factory.NewModifierList(modifiers)

	switch d.Kind {
	case ast.KindFunctionDeclaration:
		fn := d.AsFunctionDeclaration()
		return emitContext.Factory.UpdateFunctionDeclaration(fn,
			modifierList,
			fn.AsteriskToken,
			fn.Name(),
			fn.TypeParameters,
			fn.Parameters,
			fn.Type,
			fn.FullSignature,
			fn.Body,
		)
	case ast.KindClassDeclaration:
		cls := d.AsClassDeclaration()
		return emitContext.Factory.UpdateClassDeclaration(cls,
			modifierList,
			cls.Name(),
			cls.TypeParameters,
			cls.HeritageClauses,
			cls.Members,
		)
	case ast.KindVariableStatement:
		vs := d.AsVariableStatement()
		return emitContext.Factory.UpdateVariableStatement(vs,
			modifierList,
			vs.DeclarationList,
		)
	case ast.KindEnumDeclaration:
		ed := d.AsEnumDeclaration()
		return emitContext.Factory.UpdateEnumDeclaration(ed,
			modifierList,
			ed.Name(),
			ed.Members,
		)
	case ast.KindTypeAliasDeclaration:
		ta := d.AsTypeAliasDeclaration()
		return emitContext.Factory.UpdateTypeAliasDeclaration(ta,
			modifierList,
			ta.Name(),
			ta.TypeParameters,
			ta.Type,
		)
	case ast.KindInterfaceDeclaration:
		id := d.AsInterfaceDeclaration()
		return emitContext.Factory.UpdateInterfaceDeclaration(id,
			modifierList,
			id.Name(),
			id.TypeParameters,
			id.HeritageClauses,
			id.Members,
		)
	case ast.KindModuleDeclaration:
		md := d.AsModuleDeclaration()
		return emitContext.Factory.UpdateModuleDeclaration(md,
			modifierList,
			md.Keyword,
			md.Name(),
			md.Body,
		)
	}
	return nil
}

// --- Import Management ---

func updateImportsInOtherFiles(
	tracker *change.Tracker,
	program *compiler.Program,
	oldFile *ast.SourceFile,
	movedSymbols map[*ast.Symbol]bool,
	targetFileName string,
	quotePreference lsutil.QuotePreference,
	ls *LanguageService,
) {
	ctx := context.Background()
	typeChecker, done := program.GetTypeChecker(ctx)
	defer done()
	emitContext := printer.NewEmitContext()
	factory := &emitContext.Factory.NodeFactory

	for _, sourceFile := range program.SourceFiles() {
		if sourceFile == oldFile {
			continue
		}

		for _, statement := range sourceFile.Statements.Nodes {
			forEachImportInStatement(statement, func(importNode *ast.Node) {
				// Check if this import is from the old file
				moduleSpecifier := moduleSpecifierFromImport(importNode)
				if moduleSpecifier == nil {
					return
				}
				sym := typeChecker.GetSymbolAtLocation(moduleSpecifier)
				if sym != oldFile.Symbol {
					return
				}

				shouldMove := func(name *ast.Node) bool {
					if name == nil {
						return false
					}
					nameSym := typeChecker.GetSymbolAtLocation(name)
					if nameSym != nil {
						skipped := checker.SkipAlias(nameSym, typeChecker)
						return movedSymbols[skipped]
					}
					return false
				}

				// Delete imports for moved symbols
				deleteUnusedImports(sourceFile, importNode, tracker, shouldMove)

				// Compute the path to the target file
				pathToTarget := tspath.ResolvePath(
					tspath.GetDirectoryPath(tspath.GetNormalizedAbsolutePath(oldFile.FileName(), program.GetCurrentDirectory())),
					targetFileName,
				)

				// Skip self-imports
				if tspath.ComparePaths(pathToTarget, sourceFile.FileName(), tspath.ComparePathsOptions{
					UseCaseSensitiveFileNames: ls.UseCaseSensitiveFileNames(),
				}) == 0 {
					return
				}

				// Generate new module specifier for the target file
				newModuleSpecifier := modulespecifiers.GetModuleSpecifier(
					program.Options(),
					program,
					sourceFile,
					sourceFile.FileName(),
					"", // no old import specifier
					pathToTarget,
					modulespecifiers.ModuleSpecifierOptions{},
				)
				if newModuleSpecifier == "" {
					return
				}

				// Create a new import for the moved symbols
				newImport := filterImport(importNode, newModuleSpecifier, quotePreference, shouldMove, factory, emitContext)
				if newImport != nil {
					tracker.InsertNodeAfter(sourceFile, statement, newImport)
				}
			})
		}
	}
}

func deleteUnusedImports(sourceFile *ast.SourceFile, importDecl *ast.Node, tracker *change.Tracker, isUnused func(*ast.Node) bool) {
	if ast.IsImportDeclaration(importDecl) {
		clause := importDecl.AsImportDeclaration().ImportClause
		if clause != nil {
			name := clause.AsImportClause().Name()
			namedBindings := clause.AsImportClause().NamedBindings

			nameUnused := name == nil || isUnused(name)
			bindingsUnused := true
			if namedBindings != nil && ast.IsNamedImports(namedBindings) {
				elements := namedBindings.AsNamedImports().Elements.Nodes
				if len(elements) > 0 {
					for _, e := range elements {
						if !isUnused(e.AsImportSpecifier().Name()) {
							bindingsUnused = false
							break
						}
					}
				} else {
					bindingsUnused = false
				}
			} else if namedBindings != nil {
				bindingsUnused = false
			}

			if nameUnused && bindingsUnused {
				tracker.Delete(sourceFile, importDecl)
				return
			}
		}
	}

	// Delete individual unused import specifiers
	forEachAliasDeclarationInImportOrRequire(importDecl, func(decl *ast.Node) {
		name := decl.Name()
		if name != nil && ast.IsIdentifier(name) && isUnused(name) {
			tracker.Delete(sourceFile, decl)
		}
	})
}

func filterImport(
	importNode *ast.Node,
	newModuleSpecifier string,
	quotePreference lsutil.QuotePreference,
	keep func(*ast.Node) bool,
	factory *ast.NodeFactory,
	emitContext *printer.EmitContext,
) *ast.Node {
	moduleSpecLit := factory.NewStringLiteral(newModuleSpecifier, 0)

	switch importNode.Kind {
	case ast.KindImportDeclaration:
		clause := importNode.AsImportDeclaration().ImportClause
		if clause == nil {
			return nil
		}
		defaultImport := clause.AsImportClause().Name()
		if defaultImport != nil && !keep(defaultImport) {
			defaultImport = nil
		}
		namedBindings := filterNamedBindings(clause.AsImportClause().NamedBindings, keep, factory)
		if defaultImport != nil || namedBindings != nil {
			newClause := factory.NewImportClause(0, defaultImport, namedBindings)
			return factory.NewImportDeclaration(nil, newClause, moduleSpecLit, nil)
		}
		return nil
	case ast.KindImportEqualsDeclaration:
		if keep(importNode.AsImportEqualsDeclaration().Name()) {
			return importNode
		}
		return nil
	}
	return nil
}

func filterNamedBindings(namedBindings *ast.Node, keep func(*ast.Node) bool, factory *ast.NodeFactory) *ast.Node {
	if namedBindings == nil {
		return nil
	}
	if ast.IsNamespaceImport(namedBindings) {
		if keep(namedBindings.AsNamespaceImport().Name()) {
			return namedBindings
		}
		return nil
	}
	if ast.IsNamedImports(namedBindings) {
		var newElements []*ast.Node
		for _, e := range namedBindings.AsNamedImports().Elements.Nodes {
			if keep(e.AsImportSpecifier().Name()) {
				newElements = append(newElements, e)
			}
		}
		if len(newElements) > 0 {
			return factory.NewNamedImports(factory.NewNodeList(newElements))
		}
	}
	return nil
}

// --- New File Content Generation ---

func generateNewFileContent(
	oldFile *ast.SourceFile,
	targetFileName string,
	body []*ast.Node,
	usage *usageInfo,
	program *compiler.Program,
	ls *LanguageService,
	quotePreference lsutil.QuotePreference,
	useEsModuleSyntax bool,
	typeChecker *checker.Checker,
) string {
	emitContext := printer.NewEmitContext()
	factory := &emitContext.Factory.NodeFactory
	var sb strings.Builder

	// Generate imports for dependencies needed by the moved code
	imports := generateImportsForNewFile(oldFile, targetFileName, usage, program, ls,
		quotePreference, useEsModuleSyntax, typeChecker, factory, emitContext)

	newLine := program.Options().NewLine.GetNewLineCharacter()

	// Print imports
	for _, imp := range imports {
		text := printNode(imp, oldFile, emitContext)
		sb.WriteString(text)
		sb.WriteString(newLine)
	}

	if len(imports) > 0 {
		sb.WriteString(newLine)
	}

	// Print body statements
	for _, stmt := range body {
		text := printNode(stmt, oldFile, emitContext)
		sb.WriteString(text)
		sb.WriteString(newLine)
	}

	return sb.String()
}

func generateImportsForNewFile(
	oldFile *ast.SourceFile,
	targetFileName string,
	usage *usageInfo,
	program *compiler.Program,
	ls *LanguageService,
	quotePreference lsutil.QuotePreference,
	useEsModuleSyntax bool,
	typeChecker *checker.Checker,
	factory *ast.NodeFactory,
	emitContext *printer.EmitContext,
) []*ast.Node {
	var imports []*ast.Node

	// Generate imports for symbols that stay in the old file but are referenced by moved code
	if len(usage.targetFileImportsFromOldFile) > 0 && useEsModuleSyntax {
		// Compute module specifier from target -> old file
		pathToOldFile := tspath.ResolvePath(
			tspath.GetDirectoryPath(tspath.GetNormalizedAbsolutePath(oldFile.FileName(), program.GetCurrentDirectory())),
			targetFileName,
		)
		_ = pathToOldFile
		moduleSpec := modulespecifiers.GetModuleSpecifier(
			program.Options(),
			program,
			oldFile,
			targetFileName,
			"",
			oldFile.FileName(),
			modulespecifiers.ModuleSpecifierOptions{},
		)
		if moduleSpec != "" {
			var specifiers []*ast.Node
			for sym := range usage.targetFileImportsFromOldFile {
				name := sym.Name
				if name != "" {
					specifier := factory.NewImportSpecifier(false, nil, factory.NewIdentifier(name))
					specifiers = append(specifiers, specifier)
				}
			}
			if len(specifiers) > 0 {
				namedImports := factory.NewNamedImports(factory.NewNodeList(specifiers))
				importClause := factory.NewImportClause(0, nil, namedImports)
				moduleSpecLit := factory.NewStringLiteral(moduleSpec, 0)
				importDecl := factory.NewImportDeclaration(nil, importClause, moduleSpecLit, nil)
				imports = append(imports, importDecl)
			}
		}
	}

	// Generate imports for external dependencies (imports that were in old file and are needed by moved code)
	// Group by module specifier
	importsByModule := make(map[string][]*ast.Symbol)
	for sym, info := range usage.oldImportsNeededByTargetFile {
		_ = info
		if info.declaration != nil {
			moduleSpec := getModuleSpecifierFromImportDeclaration(info.declaration)
			if moduleSpec != "" {
				importsByModule[moduleSpec] = append(importsByModule[moduleSpec], sym)
			}
		}
	}

	for moduleSpec, syms := range importsByModule {
		var specifiers []*ast.Node
		for _, sym := range syms {
			name := sym.Name
			if name != "" {
				specifier := factory.NewImportSpecifier(false, nil, factory.NewIdentifier(name))
				specifiers = append(specifiers, specifier)
			}
		}
		if len(specifiers) > 0 {
			namedImports := factory.NewNamedImports(factory.NewNodeList(specifiers))
			importClause := factory.NewImportClause(0, nil, namedImports)
			moduleSpecLit := factory.NewStringLiteral(moduleSpec, 0)
			importDecl := factory.NewImportDeclaration(nil, importClause, moduleSpecLit, nil)
			imports = append(imports, importDecl)
		}
	}

	return imports
}

func insertIntoExistingFile(
	tracker *change.Tracker,
	targetFile *ast.SourceFile,
	body []*ast.Node,
	usage *usageInfo,
	oldFile *ast.SourceFile,
	program *compiler.Program,
	ls *LanguageService,
	quotePreference lsutil.QuotePreference,
	useEsModuleSyntax bool,
	typeChecker *checker.Checker,
) {
	// Generate imports for the target file and insert them
	emitContext := printer.NewEmitContext()
	factory := &emitContext.Factory.NodeFactory
	imports := generateImportsForNewFile(oldFile, targetFile.FileName(), usage, program, ls,
		quotePreference, useEsModuleSyntax, typeChecker, factory, emitContext)

	if len(imports) > 0 {
		stmtImports := make([]*ast.Statement, len(imports))
		for i, imp := range imports {
			stmtImports[i] = imp
		}
		tracker.InsertAtTopOfFile(targetFile, stmtImports, true)
	}

	// Insert moved statements
	if len(body) > 0 {
		stmts := targetFile.Statements.Nodes
		if len(stmts) > 0 {
			lastStmt := stmts[len(stmts)-1]
			tracker.InsertNodesAfter(targetFile, lastStmt, body)
		} else {
			stmtBody := make([]*ast.Statement, len(body))
			for i, b := range body {
				stmtBody[i] = b
			}
			tracker.InsertAtTopOfFile(targetFile, stmtBody, false)
		}
	}
}

// --- Helper Functions ---

func findBlockLikeAncestor(node *ast.Node) *ast.Node {
	for current := node; current != nil; current = current.Parent {
		if isBlockLike(current) {
			return current
		}
	}
	return nil
}

func isBlockLike(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindBlock, ast.KindModuleBlock, ast.KindCaseClause, ast.KindDefaultClause, ast.KindSourceFile:
		return true
	}
	return false
}

func forEachTopLevelDeclaration(statement *ast.Node, cb func(decl *ast.Node)) {
	switch statement.Kind {
	case ast.KindFunctionDeclaration,
		ast.KindClassDeclaration,
		ast.KindModuleDeclaration,
		ast.KindEnumDeclaration,
		ast.KindTypeAliasDeclaration,
		ast.KindInterfaceDeclaration,
		ast.KindImportEqualsDeclaration:
		cb(statement)
	case ast.KindVariableStatement:
		for _, decl := range statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
			cb(decl)
		}
	case ast.KindExpressionStatement:
		expr := statement.AsExpressionStatement().Expression
		if ast.IsBinaryExpression(expr) && ast.GetAssignmentDeclarationKind(expr) == ast.JSDeclarationKindExportsProperty {
			cb(statement)
		}
	}
}

func isTopLevelDeclarationStatement(node *ast.Node) bool {
	if node.Parent == nil || !ast.IsSourceFile(node.Parent) {
		return false
	}
	return isNonVariableTopLevelDeclaration(node) || ast.IsVariableStatement(node)
}

func isNonVariableTopLevelDeclaration(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration,
		ast.KindClassDeclaration,
		ast.KindModuleDeclaration,
		ast.KindEnumDeclaration,
		ast.KindTypeAliasDeclaration,
		ast.KindInterfaceDeclaration,
		ast.KindImportEqualsDeclaration:
		return true
	}
	return false
}

func isTopLevelDeclaration(node *ast.Node) bool {
	if isNonVariableTopLevelDeclaration(node) && node.Parent != nil && ast.IsSourceFile(node.Parent) {
		return true
	}
	if ast.IsVariableDeclaration(node) {
		parent := node.Parent // VariableDeclarationList
		if parent != nil {
			parent = parent.Parent // VariableStatement
			if parent != nil {
				parent = parent.Parent // SourceFile
				if parent != nil && ast.IsSourceFile(parent) {
					return true
				}
			}
		}
	}
	return false
}

func isTopLevelDeclarationInFile(sym *ast.Symbol, file *ast.SourceFile) bool {
	for _, decl := range sym.Declarations {
		if !isTopLevelDeclaration(decl) {
			continue
		}
		declFile := ast.GetSourceFileOfNode(decl)
		if declFile == file {
			return true
		}
	}
	return false
}

func nameOfTopLevelDeclaration(d *ast.Node) *ast.Node {
	if ast.IsExpressionStatement(d) {
		expr := d.AsExpressionStatement().Expression
		if ast.IsBinaryExpression(expr) {
			left := expr.AsBinaryExpression().Left
			if ast.IsPropertyAccessExpression(left) {
				name := left.AsPropertyAccessExpression().Name()
				if ast.IsIdentifier(name) {
					return name
				}
			}
		}
		return nil
	}
	name := d.Name()
	if name != nil && ast.IsIdentifier(name) {
		return name
	}
	return nil
}

func getTopLevelDeclarationStatement(d *ast.Node) *ast.Node {
	if ast.IsVariableDeclaration(d) {
		return d.Parent.Parent // VariableDeclarationList -> VariableStatement
	}
	if ast.IsBindingElement(d) {
		parent := d.Parent.Parent
		if ast.IsVariableDeclaration(parent) {
			return getTopLevelDeclarationStatement(parent)
		}
		if ast.IsBindingElement(parent) {
			return getTopLevelDeclarationStatement(parent)
		}
	}
	return d
}

func getDeclaredSymbol(decl *ast.Node, typeChecker *checker.Checker) *ast.Symbol {
	if ast.IsExpressionStatement(decl) {
		expr := decl.AsExpressionStatement().Expression
		if ast.IsBinaryExpression(expr) {
			return typeChecker.GetSymbolAtLocation(expr.AsBinaryExpression().Left)
		}
		return nil
	}
	return decl.Symbol()
}

func forEachReference(node *ast.Node, typeChecker *checker.Checker, onReference func(sym *ast.Symbol, isValidTypeOnlyUseSite bool)) {
	var visit func(child *ast.Node) bool
	visit = func(child *ast.Node) bool {
		if ast.IsIdentifier(child) && !ast.IsDeclarationName(child) {
			sym := typeChecker.GetSymbolAtLocation(child)
			if sym != nil {
				onReference(sym, ast.IsValidTypeOnlyAliasUseSite(child))
			}
		} else {
			child.ForEachChild(visit)
		}
		return false
	}
	node.ForEachChild(visit)
}

func findImportDeclaration(sym *ast.Symbol) *ast.Node {
	for _, decl := range sym.Declarations {
		if isInImport(decl) {
			return decl
		}
	}
	return nil
}

func isInImport(decl *ast.Node) bool {
	switch decl.Kind {
	case ast.KindImportEqualsDeclaration,
		ast.KindImportSpecifier,
		ast.KindImportClause,
		ast.KindNamespaceImport:
		return true
	case ast.KindVariableDeclaration:
		return isVariableDeclarationInImport(decl)
	case ast.KindBindingElement:
		parent := decl.Parent.Parent
		if ast.IsVariableDeclaration(parent) {
			return isVariableDeclarationInImport(parent)
		}
	}
	return false
}

func isVariableDeclarationInImport(decl *ast.Node) bool {
	parent := decl.Parent // VariableDeclarationList
	if parent == nil {
		return false
	}
	parent = parent.Parent // VariableStatement
	if parent == nil {
		return false
	}
	parent = parent.Parent // SourceFile
	if parent == nil || !ast.IsSourceFile(parent) {
		return false
	}
	init := decl.AsVariableDeclaration().Initializer
	return init != nil && ast.IsRequireCall(init, true)
}

func forEachImportInStatement(statement *ast.Node, cb func(importNode *ast.Node)) {
	if ast.IsImportDeclaration(statement) {
		moduleSpec := statement.AsImportDeclaration().ModuleSpecifier
		if ast.IsStringLiteral(moduleSpec) {
			cb(statement)
		}
	} else if ast.IsImportEqualsDeclaration(statement) {
		modRef := statement.AsImportEqualsDeclaration().ModuleReference
		if ast.IsExternalModuleReference(modRef) {
			expr := modRef.AsExternalModuleReference().Expression
			if ast.IsStringLiteralLike(expr) {
				cb(statement)
			}
		}
	} else if ast.IsVariableStatement(statement) {
		for _, decl := range statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
			init := decl.AsVariableDeclaration().Initializer
			if init != nil && ast.IsRequireCall(init, true) {
				cb(decl)
			}
		}
	}
}

func forEachAliasDeclarationInImportOrRequire(importOrRequire *ast.Node, cb func(decl *ast.Node)) {
	if ast.IsImportDeclaration(importOrRequire) {
		clause := importOrRequire.AsImportDeclaration().ImportClause
		if clause == nil {
			return
		}
		ic := clause.AsImportClause()
		if ic.Name() != nil {
			cb(clause)
		}
		nb := ic.NamedBindings
		if nb != nil {
			if ast.IsNamespaceImport(nb) {
				cb(nb)
			} else if ast.IsNamedImports(nb) {
				for _, element := range nb.AsNamedImports().Elements.Nodes {
					cb(element)
				}
			}
		}
	} else if ast.IsImportEqualsDeclaration(importOrRequire) {
		cb(importOrRequire)
	} else if ast.IsVariableDeclaration(importOrRequire) {
		nameNode := importOrRequire.AsVariableDeclaration().Name()
		if ast.IsIdentifier(nameNode) {
			cb(importOrRequire)
		} else if nameNode != nil && ast.IsObjectBindingPattern(nameNode) {
			for _, element := range nameNode.AsBindingPattern().Elements.Nodes {
				if ast.IsIdentifier(element.AsBindingElement().Name()) {
					cb(element)
				}
			}
		}
	}
}

func moduleSpecifierFromImport(i *ast.Node) *ast.Node {
	switch i.Kind {
	case ast.KindImportDeclaration:
		return i.AsImportDeclaration().ModuleSpecifier
	case ast.KindImportEqualsDeclaration:
		modRef := i.AsImportEqualsDeclaration().ModuleReference
		if ast.IsExternalModuleReference(modRef) {
			return modRef.AsExternalModuleReference().Expression
		}
	case ast.KindVariableDeclaration:
		init := i.AsVariableDeclaration().Initializer
		if init != nil && ast.IsRequireCall(init, true) {
			args := init.AsCallExpression().Arguments.Nodes
			if len(args) > 0 {
				return args[0]
			}
		}
	}
	return nil
}

func getModuleSpecifierFromImportDeclaration(node *ast.Node) string {
	// Walk up to the import statement
	current := node
	for current != nil {
		switch current.Kind {
		case ast.KindImportDeclaration:
			spec := current.AsImportDeclaration().ModuleSpecifier
			if ast.IsStringLiteralLike(spec) {
				return spec.Text()
			}
			return ""
		case ast.KindImportEqualsDeclaration:
			modRef := current.AsImportEqualsDeclaration().ModuleReference
			if ast.IsExternalModuleReference(modRef) {
				expr := modRef.AsExternalModuleReference().Expression
				if ast.IsStringLiteralLike(expr) {
					return expr.Text()
				}
			}
			return ""
		case ast.KindVariableDeclaration:
			init := current.AsVariableDeclaration().Initializer
			if init != nil && ast.IsRequireCall(init, true) {
				args := init.AsCallExpression().Arguments.Nodes
				if len(args) > 0 && ast.IsStringLiteralLike(args[0]) {
					return args[0].Text()
				}
			}
			return ""
		}
		current = current.Parent
	}
	return ""
}

func containsNode(nodes []*ast.Node, node *ast.Node) bool {
	return slices.Contains(nodes, node)
}

func printNode(node *ast.Node, sourceFile *ast.SourceFile, emitContext *printer.EmitContext) string {
	// Use the source file text for existing nodes
	if node.Pos() >= 0 && node.End() <= len(sourceFile.Text()) && node.Flags&ast.NodeFlagsSynthesized == 0 {
		text := sourceFile.Text()[node.Pos():node.End()]
		text = trimLeadingWhitespace(text)
		return text
	}
	// For synthesized nodes, print them
	p := printer.NewPrinter(printer.PrinterOptions{NewLine: core.NewLineKindLF}, printer.PrintHandlers{}, emitContext)
	return p.Emit(node, sourceFile)
}

func trimLeadingWhitespace(s string) string {
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == ' ' || ch == '\t' {
			i++
		} else if ch == '\r' || ch == '\n' {
			i++
		} else {
			break
		}
	}
	return s[i:]
}
