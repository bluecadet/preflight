package runner

import (
	"context"
	"fmt"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/secrets"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/internal/template"
)

type BoundTask struct {
	Name   string
	When   string
	Params map[string]any
	Become map[string]any
}

func evaluateTaskWhen(task *PlanTask, execCtx *executionContext) (bool, error) {
	if task.When == "" {
		return true, nil
	}
	return taskEngine(task, execCtx).RenderBool(task.When)
}

func bindTask(task *PlanTask, execCtx *executionContext, preserveUnknown bool) (*BoundTask, error) {
	eng := taskEngine(task, execCtx)
	if preserveUnknown {
		eng = template.New(task.TemplateVars).WithTarget(execCtx.target).WithPreserveUnknown()
	}
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

func (b *BoundTask) resolveSecrets(ctx context.Context, resolver *secrets.Resolver) error {
	if resolver == nil || !resolver.HasProviders() {
		return nil
	}
	var err error
	b.Params, err = resolver.ResolveMap(ctx, b.Params)
	if err != nil {
		return fmt.Errorf("resolve params: %w", err)
	}
	b.Become, _, err = resolveExecutionOptions(ctx, resolver, b.Become)
	if err != nil {
		return fmt.Errorf("resolve become: %w", err)
	}
	return nil
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
