//go:build !linux || !cgo

package linuxgpu

import "fmt"

func loadDynamicNVML() (nvmlClient, error) {
	return nil, fmt.Errorf("NVML runtime loading requires Linux with cgo enabled")
}
