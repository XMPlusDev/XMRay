package counter

import (
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/features/stats"
)

type StatWriter struct {
	Writer  buf.Writer
	Counter stats.Counter
}

func (w *StatWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	n := int64(mb.Len())
	err := w.Writer.WriteMultiBuffer(mb)
	if n > 0 && w.Counter != nil {
		w.Counter.Add(n)
	}
	return err
}

func (w *StatWriter) Close() error { return common.Close(w.Writer) }
func (w *StatWriter) Interrupt()   { common.Interrupt(w.Writer) }

type StatReader struct {
	Reader  buf.TimeoutReader
	Counter stats.Counter
}

func (r *StatReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if err != nil {
		return nil, err
	}
	if mb.Len() > 0 && r.Counter != nil {
		r.Counter.Add(int64(mb.Len()))
	}
	return mb, nil
}

func (r *StatReader) ReadMultiBufferTimeout(d interface{ Nanoseconds() int64 }) (buf.MultiBuffer, error) {
	return r.ReadMultiBuffer()
}

func (r *StatReader) Interrupt() { common.Interrupt(r.Reader) }
