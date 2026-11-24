//go:build windows

package envelope

import "os"

// Windows supports local functional fixtures only. Linux capture Jobs require
// directory fsync before returning a sealed staging receipt.
func syncDirectory(*os.Root) error { return nil }
