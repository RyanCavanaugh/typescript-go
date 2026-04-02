package ls

import (
	"context"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/lsp/lsproto"
)

// RefactorProvider represents a provider for a specific type of refactoring.
type RefactorProvider struct {
	// Name is the programmatic name of the refactoring.
	Name string
	// Description is a human-readable description of the refactoring.
	Description string
	// Kinds lists the code action kinds this refactoring provides.
	Kinds []lsproto.CodeActionKind
	// GetAvailableActions returns the available refactoring actions for the given context.
	GetAvailableActions func(ctx context.Context, refactorContext *RefactorContext) []*RefactorActionInfo
	// GetEditsForAction returns the edits for a specific refactoring action.
	GetEditsForAction func(ctx context.Context, refactorContext *RefactorContext, actionName string, args *lsproto.InteractiveRefactorArguments) *RefactorEditInfo
}

// RefactorContext contains the context needed to generate refactorings.
type RefactorContext struct {
	SourceFile    *ast.SourceFile
	StartPosition int
	EndPosition   int
	Program       *compiler.Program
	LS            *LanguageService
	TriggerKind   lsproto.CodeActionTriggerKind
}

// RefactorActionInfo describes a single available refactoring action.
type RefactorActionInfo struct {
	Name                string
	Description         string
	Kind                lsproto.CodeActionKind
	NotApplicableReason string
	IsInteractive       bool
	Range               *lsproto.Range
}

// RefactorEditInfo contains the edits produced by a refactoring.
type RefactorEditInfo struct {
	Edits               map[string][]*lsproto.TextEdit
	NewFiles            []NewFileEdit
	NotApplicableReason string
}

// NewFileEdit represents content to be written to a new file.
type NewFileEdit struct {
	FileName string
	Content  string
}

// refactorProviders is the list of all registered refactoring providers.
var refactorProviders = []*RefactorProvider{
	moveToFileRefactorProvider,
}

// refactorActionWithProvider pairs an action with its provider name.
type refactorActionWithProvider struct {
	Action       *RefactorActionInfo
	ProviderName string
}

// getApplicableRefactorsWithProviders returns all applicable refactoring actions paired with their provider names.
func getApplicableRefactorsWithProviders(ctx context.Context, refactorContext *RefactorContext, requestedKinds []lsproto.CodeActionKind) []refactorActionWithProvider {
	var result []refactorActionWithProvider
	for _, provider := range refactorProviders {
		if len(requestedKinds) > 0 && !providerMatchesKinds(provider, requestedKinds) {
			continue
		}
		actions := provider.GetAvailableActions(ctx, refactorContext)
		for _, action := range actions {
			result = append(result, refactorActionWithProvider{Action: action, ProviderName: provider.Name})
		}
	}
	return result
}

// providerMatchesKinds checks if a provider handles any of the requested kinds.
func providerMatchesKinds(provider *RefactorProvider, requestedKinds []lsproto.CodeActionKind) bool {
	for _, requestedKind := range requestedKinds {
		for _, providerKind := range provider.Kinds {
			if strings.HasPrefix(string(providerKind), string(requestedKind)) ||
				strings.HasPrefix(string(requestedKind), string(providerKind)) {
				return true
			}
		}
	}
	return false
}
