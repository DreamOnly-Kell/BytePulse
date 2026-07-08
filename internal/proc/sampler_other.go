//go:build !darwin

package proc

type unsupportedSampler struct{}

func NewSampler() ConnectionSampler {
	return unsupportedSampler{}
}

func (unsupportedSampler) Sample() ([]Connection, error) {
	return nil, ErrNotSupported
}
