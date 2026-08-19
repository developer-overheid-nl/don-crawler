package cmd

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecuteReturnsCommandErrorsToTheCaller(t *testing.T) {
	rootCmd.SetArgs([]string{"does-not-exist"})
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	err := Execute()

	require.ErrorContains(t, err, "unknown command")
}
