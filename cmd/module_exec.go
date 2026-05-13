package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bluecadet/preflight/internal/module"
	"github.com/bluecadet/preflight/internal/target"
	"github.com/bluecadet/preflight/pkg/plugin/sdk"
)

var moduleExecCmd = &cobra.Command{
	Use:    "__module-exec <module-name>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runModuleExec,
}

func runModuleExec(_ *cobra.Command, args []string) error {
	name := args[0]
	reg := module.Registry()
	mod, ok := reg[name]
	if !ok {
		resp := fmt.Sprintf(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"module-exec: unknown module %s"}}`, name)
		fmt.Fprintln(os.Stdout, resp)
		os.Exit(1)
	}
	adapter := target.NewSDKModuleAdapter(name, mod)
	sdk.Serve(adapter)
	return nil
}
