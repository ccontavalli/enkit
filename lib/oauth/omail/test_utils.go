package omail

import "bytes"

type bodyWriter struct {
	bytes.Buffer
}

func (bw *bodyWriter) Close() error {
	return nil
}
