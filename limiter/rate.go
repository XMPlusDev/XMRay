package limiter

import (
	"context"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

func waitN(ctx context.Context, lim *rate.Limiter, n int) error {
	for n > 0 {
		burst := lim.Burst()
		if burst <= 0 {
			return nil
		}
		chunk := n
		if chunk > burst {
			chunk = burst
		}
		if err := lim.WaitN(ctx, chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

func (l *Limiter) RateWriter(ctx context.Context, writer buf.Writer, limiter *rate.Limiter) buf.Writer {
	ctx, cancel := context.WithCancel(ctx)
	return &Writer{Writer: writer, Limiter: limiter, ctx: ctx, cancel: cancel}
}

func (l *Limiter) RateReader(ctx context.Context, reader buf.Reader, limiter *rate.Limiter) buf.Reader {
	return &Reader{Reader: reader, Limiter: limiter, ctx: ctx}
}

func (l *Limiter) RateTimeoutReader(ctx context.Context, reader buf.TimeoutReader, limiter *rate.Limiter) buf.TimeoutReader {
	return &TimeoutReader{Reader: reader, Limiter: limiter, ctx: ctx}
}

// ---- Writer ----

type Writer struct {
	Writer  buf.Writer
	Limiter *rate.Limiter
	ctx     context.Context
	cancel  context.CancelFunc
}

func (w *Writer) Close() error {
	w.cancel() 
	return common.Close(w.Writer)
}

func (w *Writer) WriteMultiBuffer(mb buf.MultiBuffer) error {
	if n := int(mb.Len()); n > 0 {
		if err := waitN(w.ctx, w.Limiter, n); err != nil {
			buf.ReleaseMulti(mb)
			return err
		}
	}
	return w.Writer.WriteMultiBuffer(mb)
}

// ---- Reader (no timeout support) ----

type Reader struct {
	Reader  buf.Reader
	Limiter *rate.Limiter
	ctx     context.Context
}

func (r *Reader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if err != nil {
		return mb, err
	}
	if n := int(mb.Len()); n > 0 {
		if err := waitN(r.ctx, r.Limiter, n); err != nil {
			buf.ReleaseMulti(mb)
			return nil, err
		}
	}
	return mb, nil
}

func (r *Reader) Interrupt() { common.Interrupt(r.Reader) }

// ---- TimeoutReader ----

type TimeoutReader struct {
	Reader  buf.TimeoutReader
	Limiter *rate.Limiter
	ctx     context.Context
}

func (r *TimeoutReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if err != nil {
		return mb, err
	}
	if n := int(mb.Len()); n > 0 {
		if err := waitN(r.ctx, r.Limiter, n); err != nil {
			buf.ReleaseMulti(mb)
			return nil, err
		}
	}
	return mb, nil
}

func (r *TimeoutReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBufferTimeout(timeout)
	if err != nil {
		return mb, err
	}
	if n := int(mb.Len()); n > 0 {
		if err := waitN(r.ctx, r.Limiter, n); err != nil {
			buf.ReleaseMulti(mb)
			return nil, err
		}
	}
	return mb, nil
}

func (r *TimeoutReader) Interrupt() { common.Interrupt(r.Reader) }