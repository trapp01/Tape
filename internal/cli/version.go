package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tape version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, style := bareStyler(cmd)
			style.banner(out, bannerMode(), "version")
			fmt.Fprintf(out, "\ntape %s  %s/%s  %s\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
			return nil
		},
	}
}
