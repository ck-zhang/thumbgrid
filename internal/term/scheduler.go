package term

import "sync/atomic"

type DrawReq struct {
	Path       string
	X, Y, W, H int
	done       chan struct{}
	gen        uint64
}

type Scheduler struct {
	r     Renderer
	queue chan DrawReq
	quit  chan struct{}
	gen   atomic.Uint64
}

func NewScheduler(r Renderer, buf int) *Scheduler {
	if buf <= 0 {
		buf = 64
	}
	s := &Scheduler{
		r:     r,
		queue: make(chan DrawReq, buf),
		quit:  make(chan struct{}),
	}
	s.gen.Store(1)
	go s.loop()
	return s
}

func (s *Scheduler) loop() {
	for {
		select {
		case req := <-s.queue:
			if req.done != nil {
				close(req.done)
				continue
			}

			if req.gen != s.gen.Load() {
				continue
			}
			if s.r != nil {
				_ = s.r.Draw(req.Path, req.X, req.Y, req.W, req.H)
			}
		case <-s.quit:
			return
		}
	}
}

func (s *Scheduler) Enqueue(path string, x, y, w, h int) {
	g := s.gen.Load()
	select {
	case s.queue <- DrawReq{Path: path, X: x, Y: y, W: w, H: h, gen: g}:
	default:
	}
}

func (s *Scheduler) Drain() {
	done := make(chan struct{})
	s.queue <- DrawReq{done: done, gen: s.gen.Load()}
	<-done
}

func (s *Scheduler) NextFrame() {
	s.gen.Add(1)
}

func (s *Scheduler) Close() {
	close(s.quit)
	if s.r != nil {
		_ = s.r.Close()
	}
}
