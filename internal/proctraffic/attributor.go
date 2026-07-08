package proctraffic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"time"
)

var ErrNotSupported = errors.New("process traffic attribution is not supported on this platform")

type Attributor interface {
	Run(ctx context.Context, onSample func([]Sample)) error
}

type unsupportedAttributor struct{}

func (unsupportedAttributor) Run(context.Context, func([]Sample)) error {
	return ErrNotSupported
}

type readerAttributor struct {
	reader io.Reader
}

func NewReaderAttributor(reader io.Reader) Attributor {
	return readerAttributor{reader: reader}
}

func (a readerAttributor) Run(ctx context.Context, onSample func([]Sample)) error {
	return scanNettopCSV(ctx, a.reader, onSample)
}

func scanNettopCSV(ctx context.Context, reader io.Reader, onSample func([]Sample)) error {
	scanner := bufio.NewScanner(reader)
	var header string
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		if header == "" {
			header = line
			continue
		}
		block := header + "\n" + line + "\n"
		samples, err := ParseNettopCSV(bytes.NewBufferString(block), time.Now())
		if err != nil || len(samples) == 0 {
			continue
		}
		onSample(samples)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
