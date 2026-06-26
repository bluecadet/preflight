package runner

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/template"
)

// BoundTask holds the template-rendered task fields before secret resolution.
type BoundTask struct {
	Name   string
	When   string
	Params map[string]any
	Become map[string]any
}

// BoundTaskResult is the output of the consolidated bind pipeline: a fully
// resolved task ready to execute, plus pre-resolution copies for state hashing.
type BoundTaskResult struct {
	Name         string
	Params       map[string]any
	Become       map[string]any
	SourceParams map[string]any // pre-secret-resolution copy for state
	SourceBecome map[string]any // pre-secret-resolution copy for state
	ExecOpts     target.ExecutionOptions
}

// evaluateTaskWhen renders the task's when condition against the RuntimeContext.
func evaluateTaskWhen(task *PlanTask, rt *template.RuntimeContext) (bool, error) {
	if task.When == "" {
		return true, nil
	}
	eng := task.Scope.Engine(rt, template.Bind)
	return eng.RenderBool(task.When)
}

// bindTask renders template expressions in the task against the RuntimeContext.
func bindTask(task *PlanTask, rt *template.RuntimeContext, bindMode template.BindMode) (*BoundTask, error) {
	eng := task.Scope.Engine(rt, bindMode)
	if module.PreservesSecretRefs(task.Module) {
		eng = eng.WithPreserveSecretRefs()
	}

	params, err := eng.RenderMap(task.Params)
	if err != nil {
		return nil, err
	}
	var become map[string]any
	if len(task.Become) > 0 {
		become, err = eng.RenderMap(task.Become)
		if err != nil {
			return nil, err
		}
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
	return &BoundTask{Name: name, When: when, Params: params, Become: become}, nil
}

// bindAndResolveTask consolidates the bind-time pipeline: template rendering,
// secret resolution, file-content_template rendering, and execution-option
// normalization — all in one call.
func bindAndResolveTask(ctx context.Context, task *PlanTask, rt *template.RuntimeContext, resolver *secrets.Resolver) (*BoundTaskResult, error) {
	bound, err := bindTask(task, rt, template.Bind)
	if err != nil {
		return nil, err
	}

	sourceParams := cloneMap(bound.Params)
	sourceBecome := cloneMap(bound.Become)

	if err := bound.resolveSecrets(ctx, resolver); err != nil {
		return nil, err
	}
	resolvedBecome := cloneMap(bound.Become)
	_, execOpts, err := bound.executionOptions()
	if err != nil {
		return nil, err
	}

	return &BoundTaskResult{
		Name:         bound.Name,
		Params:       bound.Params,
		Become:       resolvedBecome,
		SourceParams: sourceParams,
		SourceBecome: sourceBecome,
		ExecOpts:     execOpts,
	}, nil
}

func (b *BoundTask) resolveSecrets(ctx context.Context, resolver *secrets.Resolver) error {
	if resolver != nil && resolver.HasProviders() {
		var err error
		b.Params, err = resolver.ResolveMap(ctx, b.Params)
		if err != nil {
			return fmt.Errorf("resolve params: %w", err)
		}
		b.Become, _, err = resolveExecutionOptions(ctx, resolver, b.Become)
		if err != nil {
			return fmt.Errorf("resolve become: %w", err)
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

func (b *BoundTask) executionOptions() (map[string]any, target.ExecutionOptions, error) {
	if len(b.Become) == 0 {
		return nil, target.ExecutionOptions{}, nil
	}
	opts, err := target.NormalizeExecutionOptions(map[string]any{"become": b.Become})
	if err != nil {
		return nil, target.ExecutionOptions{}, err
	}
	return b.Become, opts, nil
}
