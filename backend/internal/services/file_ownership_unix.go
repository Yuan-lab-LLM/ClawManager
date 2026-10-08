//go:build !windows

package services

import "os"

var setFileOwnership = os.Chown
