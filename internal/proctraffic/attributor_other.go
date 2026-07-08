//go:build !darwin

package proctraffic

func NewNettopAttributor() Attributor {
	return unsupportedAttributor{}
}

func nettopArgs() []string {
	return nil
}
