//go:build !darwin && !windows

// Stub for platforms without process connection sampling (e.g. Linux for now).
// 尚无进程连接采样的平台桩实现（例如当前 Linux）。
package proc

// unsupportedSampler always returns ErrNotSupported.
// unsupportedSampler 始终返回 ErrNotSupported。
type unsupportedSampler struct{}

// NewSampler returns a no-op sampler on unsupported platforms.
// NewSampler 在不支持的平台返回空操作采样器。
func NewSampler() ConnectionSampler {
	return unsupportedSampler{}
}

// Sample fails with ErrNotSupported so the daemon can disable only this feature.
// Sample 以 ErrNotSupported 失败，使 daemon 仅禁用该功能。
func (unsupportedSampler) Sample() ([]Connection, error) {
	return nil, ErrNotSupported
}
