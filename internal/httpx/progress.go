package httpx

import "io"

// ProgressWriter passes writes on to w and tells report, after each one, how
// many bytes it has written of total, which is -1 while unknown. Without a
// report it is w itself.
func ProgressWriter(w io.Writer, total int64, report func(done, total int64)) io.Writer {
	if report == nil {
		return w
	}
	return &progressWriter{w: w, total: total, report: report}
}

type progressWriter struct {
	w      io.Writer
	done   int64
	total  int64
	report func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	p.report(p.done, p.total)
	return n, err
}
