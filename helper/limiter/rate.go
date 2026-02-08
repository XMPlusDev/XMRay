package limiter

import (
	"context"
	"io"
	"time"
	
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

// Writer wraps a buf.Writer with rate limiting
type Writer struct {
	Writer  buf.Writer    // Exported (capitalized) so dispatcher can access
	Limiter *rate.Limiter // Exported
	w       io.Writer
}

func (l *Limiter) RateWriter(writer buf.Writer, limiter *rate.Limiter) buf.Writer {
	return &Writer{
		Writer:  writer,
		Limiter: limiter,
	}
}

func (w *Writer) Close() error {
	return common.Close(w.Writer)
}

func (w *Writer) WriteMultiBuffer(mb buf.MultiBuffer) error {
	ctx := context.Background()
	w.Limiter.WaitN(ctx, int(mb.Len()))
	return w.Writer.WriteMultiBuffer(mb)
}

// Reader wraps a buf.Reader with rate limiting
type Reader struct {
	Reader  buf.Reader    // Exported
	Limiter *rate.Limiter // Exported
}

func (l *Limiter) RateReader(reader buf.Reader, limiter *rate.Limiter) buf.Reader {
	return &Reader{
		Reader:  reader,
		Limiter: limiter,
	}
}

func (r *Reader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if err != nil {
		return nil, err
	}

	totalBytes := mb.Len()
	if totalBytes > 0 {
		ctx := context.Background()
		if err := r.Limiter.WaitN(ctx, int(totalBytes)); err != nil {
			buf.ReleaseMulti(mb)
			return nil, err
		}
	}

	return mb, nil
}

// TimeoutReader wraps a buf.TimeoutReader with rate limiting
type TimeoutReader struct {
	Reader  buf.TimeoutReader // Exported (CHANGED from lowercase to uppercase)
	Limiter *rate.Limiter     // Exported
}

func (l *Limiter) RateTimeoutReader(reader buf.TimeoutReader, limiter *rate.Limiter) buf.TimeoutReader {
	return &TimeoutReader{
		Reader:  reader,
		Limiter: limiter,
	}
}

func (r *TimeoutReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if err != nil {
		return nil, err
	}

	totalBytes := mb.Len()
	if totalBytes > 0 {
		ctx := context.Background()
		if err := r.Limiter.WaitN(ctx, int(totalBytes)); err != nil {
			buf.ReleaseMulti(mb)
			return nil, err
		}
	}

	return mb, nil
}

func (r *TimeoutReader) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBufferTimeout(timeout)
	if err != nil {
		return nil, err
	}

	totalBytes := mb.Len()
	if totalBytes > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := r.Limiter.WaitN(ctx, int(totalBytes)); err != nil {
			buf.ReleaseMulti(mb)
			return nil, err
		}
	}

	return mb, nil
}

func (r *TimeoutReader) Interrupt() {
	common.Interrupt(r.Reader)
}