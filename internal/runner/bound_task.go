package runner

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/template"
)

// BoundTask holds the template-rendered task fields before secret resolution.
type BoundTask struct {
	Name   string
	When   string
	Params map[string]any
}

// BoundTaskResult is the output of the consolidated bind pipeline: a fully
// resolved task ready to execute, plus a pre-resolution copy of its params for
// state hashing.
type BoundTaskResult struct {
	Name         string
	Params       map[string]any
	SourceParams map[string]any // pre-secret-resolution copy for state
}

// evaluateTaskWhen renders the task's when condition against the RuntimeContext.
func evaluateTaskWhen(task *PlanTask, rt *template.RuntimeContext) (bool, error) {
	if task.When == "" {
		return true, nil
	}
	eng := task.Scope.Engine(rt, template.Bind)
	return eng.RenderBool(task.When)
}

// preservesSecretRefs reports whether the named module requires template
// rendering to preserve secret refs rather than resolving them eagerly.
// The file module defers secret resolution because content_template is
// bound as a late step after main param rendering.
func preservesSecretRefs(name string) bool {
	return name == "file"
}

// bindTask renders template expressions in the task against the RuntimeContext.
func bindTask(task *PlanTask, rt *template.RuntimeContext, bindMode template.BindMode) (*BoundTask, error) {
	eng := task.Scope.Engine(rt, bindMode)
	if preservesSecretRefs(task.Module) {
		eng = eng.WithPreserveSecretRefs()
	}

	params, err := eng.RenderMap(task.Params)
	if err != nil {
		return nil, err
	}
	name := task.Name
	if task.Name != "" {
		name, err = eng.Render(task.Name)
		if err != nil {
			return nil, err
		}
	}
	when := task.When
	if task.When != "" {
		when, err = eng.Render(task.When)
		if err != nil {
			return nil, err
		}
	}
	return &BoundTask{Name: name, When: when, Params: params}, nil
}

// bindAndResolveTask consolidates the bind-time pipeline: template rendering,
// secret resolution, and file-content_template rendering in one call.
func bindAndResolveTask(ctx context.Context, task *PlanTask, rt *template.RuntimeContext, resolver *secrets.Resolver) (*BoundTaskResult, error) {
	bound, err := bindTask(task, rt, template.Bind)
	if err != nil {
		return nil, err
	}

	sourceParams := cloneMap(bound.Params)

	if err := bound.resolveSecrets(ctx, resolver); err != nil {
		return nil, err
	}

	return &BoundTaskResult{
		Name:         bound.Name,
		Params:       bound.Params,
		SourceParams: sourceParams,
	}, nil
}

func (b *BoundTask) resolveSecrets(ctx context.Context, resolver *secrets.Resolver) error {
	if resolver != nil && resolver.HasProviders() {
		var err error
		b.Params, err = resolver.ResolveMap(ctx, b.Params)
		if err != nil {
			return fmt.Errorf("resolve params: %w", err)
		}
	}
	// content_template is a late-bind param rendered after main param resolution.
	// No-op when the param is absent, so safe to call for every task.
	return b.renderFileContentTemplate(ctx, resolver)
}

func (b *BoundTask) renderFileContentTemplate(ctx context.Context, resolver *secrets.Resolver) error {
	if b.Params == nil {
		return nil
	}
	value, ok := b.Params["content_template"]
	if !ok {
		return nil
	}
	if _, hasSrc := b.Params["src"]; hasSrc {
		return fmt.Errorf("file: src and content_template are mutually exclusive")
	}
	if _, hasContent := b.Params["content"]; hasContent {
		return fmt.Errorf("file: content and content_template are mutually exclusive")
	}
	source, ok := value.(string)
	if !ok {
		return fmt.Errorf("file: content_template must be a string, got %T", value)
	}
	eng := template.New(nil).WithSecretLookup(func(name string) (string, error) {
		if resolver == nil || !resolver.HasProviders() {
			return "", fmt.Errorf("secret provider %q is not configured", secrets.DefaultProviderName)
		}
		return resolver.ResolveRef(ctx, secrets.DefaultProviderName+":"+name)
	})
	rendered, err := eng.Render(source)
	if err != nil {
		return fmt.Errorf("render file content_template: %w", err)
	}
	delete(b.Params, "content_template")
	b.Params["content"] = rendered
	return nil
}
