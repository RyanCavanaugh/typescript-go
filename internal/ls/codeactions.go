package ls

import (
	"context"
	"slices"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/ls/lsconv"
	"github.com/microsoft/typescript-go/internal/lsp/lsproto"
)

// CodeFixProvider represents a provider for a specific type of code fix
type CodeFixProvider struct {
	ErrorCodes        []int32
	GetCodeActions    func(ctx context.Context, fixContext *CodeFixContext) ([]CodeAction, error)
	FixIds            []string
	GetAllCodeActions func(ctx context.Context, fixContext *CodeFixContext) (*CombinedCodeActions, error)
}

// CodeFixContext contains the context needed to generate code fixes
type CodeFixContext struct {
	SourceFile *ast.SourceFile
	Span       core.TextRange
	ErrorCode  int32
	Program    *compiler.Program
	LS         *LanguageService
	Diagnostic *lsproto.Diagnostic
	Params     *lsproto.CodeActionParams
}

// CodeAction represents a single code action fix
type CodeAction struct {
	Description string
	Changes     []*lsproto.TextEdit
}

// CombinedCodeActions represents combined code actions for fix-all scenarios
type CombinedCodeActions struct {
	Description string
	Changes     []*lsproto.TextEdit
}

// codeFixProviders is the list of all registered code fix providers
var codeFixProviders = []*CodeFixProvider{
	ImportFixProvider,
	// Add more code fix providers here as they are implemented
}

// ProvideCodeActions returns code actions for the given range and context
func (l *LanguageService) ProvideCodeActions(ctx context.Context, params *lsproto.CodeActionParams) (lsproto.CodeActionResponse, error) {
	program, file := l.getProgramAndFile(params.TextDocument.Uri)

	var actions []lsproto.CommandOrCodeAction

	// Collect requested kinds for filtering
	var requestedKinds []lsproto.CodeActionKind
	if params.Context != nil && params.Context.Only != nil {
		requestedKinds = *params.Context.Only
	}

	// Handle source actions (like organize imports)
	if params.Context != nil && params.Context.Only != nil {
		for _, kind := range *params.Context.Only {
			// Get all matching organize imports actions for the requested kind
			matchingKinds := getOrganizeImportsActionsForKind(kind)
			for _, matchingKind := range matchingKinds {
				organizeAction := l.createOrganizeImportsAction(ctx, program, file, matchingKind)
				actions = append(actions, *organizeAction)
			}
		}
	}

	// Process diagnostics in the context to generate quick fixes
	if params.Context != nil && params.Context.Diagnostics != nil {
		for _, diag := range params.Context.Diagnostics {
			if diag.Code == nil || diag.Code.Integer == nil {
				continue
			}

			errorCode := *diag.Code.Integer

			// Check all code fix providers
			for _, provider := range codeFixProviders {
				if !containsErrorCode(provider.ErrorCodes, errorCode) {
					continue
				}

				// Create context for the provider
				position := l.converters.LineAndCharacterToPosition(file, diag.Range.Start)
				endPosition := l.converters.LineAndCharacterToPosition(file, diag.Range.End)
				fixContext := &CodeFixContext{
					SourceFile: file,
					Span:       core.NewTextRange(int(position), int(endPosition)),
					ErrorCode:  errorCode,
					Program:    program,
					LS:         l,
					Diagnostic: diag,
					Params:     params,
				}

				// Get code actions from the provider
				providerActions, err := provider.GetCodeActions(ctx, fixContext)
				if err != nil {
					return lsproto.CodeActionResponse{}, err
				}
				for _, action := range providerActions {
					actions = append(actions, convertToLSPCodeAction(&action, diag, params.TextDocument.Uri))
				}
			}
		}
	}

	// Handle refactoring providers
	triggerKind := lsproto.CodeActionTriggerKindInvoked
	if params.Context != nil && params.Context.TriggerKind != nil {
		triggerKind = *params.Context.TriggerKind
	}

	startPosition := l.converters.LineAndCharacterToPosition(file, params.Range.Start)
	endPosition := l.converters.LineAndCharacterToPosition(file, params.Range.End)
	refactorContext := &RefactorContext{
		SourceFile:    file,
		StartPosition: int(startPosition),
		EndPosition:   int(endPosition),
		Program:       program,
		LS:            l,
		TriggerKind:   triggerKind,
	}

	refactorActions := getApplicableRefactorsWithProviders(ctx, refactorContext, requestedKinds)
	for _, ra := range refactorActions {
		lspAction := convertRefactorToLSPCodeAction(ra.Action, ra.ProviderName, params)
		if lspAction != nil {
			actions = append(actions, *lspAction)
		}
	}

	return lsproto.CommandOrCodeActionArrayOrNull{CommandOrCodeActionArray: &actions}, nil
}

// GetEditsForRefactor returns the edits for a specific refactoring action.
func (l *LanguageService) GetEditsForRefactor(ctx context.Context, fileName string, startPosition int, endPosition int, refactorName string, actionName string, args *lsproto.InteractiveRefactorArguments) *RefactorEditInfo {
	program, file := l.tryGetProgramAndFile(fileName)
	if file == nil {
		return nil
	}

	refactorContext := &RefactorContext{
		SourceFile:    file,
		StartPosition: startPosition,
		EndPosition:   endPosition,
		Program:       program,
		LS:            l,
		TriggerKind:   lsproto.CodeActionTriggerKindInvoked,
	}

	for _, provider := range refactorProviders {
		if provider.Name == refactorName {
			return provider.GetEditsForAction(ctx, refactorContext, actionName, args)
		}
	}
	return nil
}

// ResolveCodeAction resolves a code action by computing its edits.
func (l *LanguageService) ResolveCodeAction(ctx context.Context, codeAction *lsproto.CodeAction) *lsproto.CodeAction {
	if codeAction.Data == nil || codeAction.Data.Type != "refactor" {
		return codeAction
	}

	fileName := codeAction.Data.Uri.FileName()
	_, file := l.tryGetProgramAndFile(fileName)
	if file == nil {
		return codeAction
	}

	r := codeAction.Data.Range
	if r == nil {
		return codeAction
	}
	startPos := int(l.converters.LineAndCharacterToPosition(file, r.Start))
	endPos := int(l.converters.LineAndCharacterToPosition(file, r.End))

	editInfo := l.GetEditsForRefactor(ctx, fileName, startPos, endPos, codeAction.Data.RefactorName, codeAction.Data.ActionName, codeAction.Data.InteractiveRefactorArguments)
	if editInfo == nil {
		return codeAction
	}

	workspaceEdit := refactorEditInfoToWorkspaceEdit(editInfo)
	codeAction.Edit = workspaceEdit
	return codeAction
}

// refactorEditInfoToWorkspaceEdit converts a RefactorEditInfo to an LSP WorkspaceEdit.
func refactorEditInfoToWorkspaceEdit(editInfo *RefactorEditInfo) *lsproto.WorkspaceEdit {
	changes := make(map[lsproto.DocumentUri][]*lsproto.TextEdit)
	for fileName, edits := range editInfo.Edits {
		uri := lsconv.FileNameToDocumentURI(fileName)
		changes[uri] = edits
	}

	var documentChanges []lsproto.TextDocumentEditOrCreateFileOrRenameFileOrDeleteFile
	// Handle new files
	for _, newFile := range editInfo.NewFiles {
		uri := lsconv.FileNameToDocumentURI(newFile.FileName)
		createOp := &lsproto.CreateFile{
			Kind: lsproto.StringLiteralCreate{},
			Uri:  uri,
		}
		documentChanges = append(documentChanges, lsproto.TextDocumentEditOrCreateFileOrRenameFileOrDeleteFile{
			CreateFile: createOp,
		})
		// Also add the content as a text edit on the new file
		edit := &lsproto.TextDocumentEdit{
			TextDocument: lsproto.OptionalVersionedTextDocumentIdentifier{
				Uri: uri,
			},
			Edits: []lsproto.TextEditOrAnnotatedTextEditOrSnippetTextEdit{
				{
					TextEdit: &lsproto.TextEdit{
						Range:   lsproto.Range{},
						NewText: newFile.Content,
					},
				},
			},
		}
		documentChanges = append(documentChanges, lsproto.TextDocumentEditOrCreateFileOrRenameFileOrDeleteFile{
			TextDocumentEdit: edit,
		})
	}

	result := &lsproto.WorkspaceEdit{}
	if len(changes) > 0 {
		result.Changes = &changes
	}
	if len(documentChanges) > 0 {
		result.DocumentChanges = &documentChanges
	}
	return result
}

// getOrganizeImportsActionTitle returns the appropriate title for the given organize imports kind
func getOrganizeImportsActionTitle(kind lsproto.CodeActionKind) string {
	switch kind {
	case lsproto.CodeActionKindSourceRemoveUnusedImports:
		return "Remove Unused Imports"
	case lsproto.CodeActionKindSourceSortImports:
		return "Sort Imports"
	default:
		return "Organize Imports"
	}
}

// getOrganizeImportsActionsForKind returns the organize imports code action kinds that should be
// returned for the given requested kind.
func getOrganizeImportsActionsForKind(requestedKind lsproto.CodeActionKind) []lsproto.CodeActionKind {
	organizeImportsKinds := []lsproto.CodeActionKind{
		lsproto.CodeActionKindSourceOrganizeImports,
		lsproto.CodeActionKindSourceRemoveUnusedImports,
		lsproto.CodeActionKindSourceSortImports,
	}

	var result []lsproto.CodeActionKind
	for _, organizeKind := range organizeImportsKinds {
		if strings.HasPrefix(string(organizeKind), string(requestedKind)) {
			result = append(result, organizeKind)
		}
	}

	if slices.Contains(result, requestedKind) {
		return []lsproto.CodeActionKind{requestedKind}
	}

	return result
}

// createOrganizeImportsAction creates the organize imports code action
func (l *LanguageService) createOrganizeImportsAction(
	ctx context.Context,
	program *compiler.Program,
	file *ast.SourceFile,
	kind lsproto.CodeActionKind,
) *lsproto.CommandOrCodeAction {
	title := getOrganizeImportsActionTitle(kind)
	changes := l.OrganizeImports(
		ctx,
		file,
		program,
		kind,
	)
	if len(changes) == 0 {
		return &lsproto.CommandOrCodeAction{
			CodeAction: &lsproto.CodeAction{
				Title: title,
				Kind:  &kind,
				Edit:  &lsproto.WorkspaceEdit{Changes: &map[lsproto.DocumentUri][]*lsproto.TextEdit{}},
			},
		}
	}

	lspChanges := make(map[lsproto.DocumentUri][]*lsproto.TextEdit)
	for fileName, edits := range changes {
		fileURI := lsconv.FileNameToDocumentURI(fileName)
		lspChanges[fileURI] = edits
	}

	return &lsproto.CommandOrCodeAction{
		CodeAction: &lsproto.CodeAction{
			Title: title,
			Kind:  &kind,
			Edit:  &lsproto.WorkspaceEdit{Changes: &lspChanges},
		},
	}
}

// containsErrorCode checks if the error code is in the list
func containsErrorCode(codes []int32, code int32) bool {
	return slices.Contains(codes, code)
}

// convertToLSPCodeAction converts an internal CodeAction to an LSP CodeAction
func convertToLSPCodeAction(action *CodeAction, diag *lsproto.Diagnostic, uri lsproto.DocumentUri) lsproto.CommandOrCodeAction {
	kind := lsproto.CodeActionKindQuickFix
	changes := map[lsproto.DocumentUri][]*lsproto.TextEdit{
		uri: action.Changes,
	}
	diagnostics := []*lsproto.Diagnostic{diag}

	return lsproto.CommandOrCodeAction{
		CodeAction: &lsproto.CodeAction{
			Title:       action.Description,
			Kind:        &kind,
			Edit:        &lsproto.WorkspaceEdit{Changes: &changes},
			Diagnostics: &diagnostics,
		},
	}
}

// convertRefactorToLSPCodeAction converts a refactoring action info to an LSP CodeAction.
func convertRefactorToLSPCodeAction(action *RefactorActionInfo, providerName string, params *lsproto.CodeActionParams) *lsproto.CommandOrCodeAction {
	data := &lsproto.CodeActionData{
		Type:         "refactor",
		RefactorName: providerName,
		ActionName:   action.Name,
		Uri:          params.TextDocument.Uri,
		Range:        &params.Range,
	}

	if action.NotApplicableReason != "" {
		disabled := &lsproto.CodeActionDisabled{Reason: action.NotApplicableReason}
		return &lsproto.CommandOrCodeAction{
			CodeAction: &lsproto.CodeAction{
				Title:    action.Description,
				Kind:     &action.Kind,
				Disabled: disabled,
				Data:     data,
			},
		}
	}

	return &lsproto.CommandOrCodeAction{
		CodeAction: &lsproto.CodeAction{
			Title: action.Description,
			Kind:  &action.Kind,
			Data:  data,
		},
	}
}
