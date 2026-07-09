//go:build !darwin

// Non-macOS stub: process connection sampling is not implemented yet.
// 非 macOS 桩实现：进程连接采样尚未实现。
package proc

// unsupportedSampler always returns ErrNotSupported.
// unsupportedSampler 始终返回 ErrNotSupported。
type unsupportedSampler struct{}

// NewSampler returns a no-op sampler on Linux/Windows/etc.
// NewSampler 在 Linux/Windows 等平台返回空操作采样器。
func NewSampler() ConnectionSampler {
	return unsupportedSampler{}
}

// Sample fails with ErrNotSupported so the daemon can disable only this feature.
// Sample 以 ErrNotSupported 失败，使 daemon 仅禁用该功能。
func (unsupportedSampler) Sample() ([]Connection, error) {
	return nil, ErrNotSupported
}
