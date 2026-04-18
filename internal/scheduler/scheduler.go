package scheduler

import (
	"context"
	"sync"
	"time"

	"hijarr/internal/logger"
)

var schedLog = logger.For("scheduler")

// Job 是所有定时任务需要实现的接口。
type Job interface {
	Name() string
	Run(ctx context.Context)
}

// Triggerable 是可手动触发的 Job 扩展接口。
// 触发后 Scheduler 会立即执行一次并重置 ticker，避免短时间内重复扫描。
type Triggerable interface {
	TriggerChan() <-chan struct{}
}

type entry struct {
	job      Job
	interval time.Duration
}

// Scheduler 管理一组定时任务，每个任务在独立 goroutine 中运行。
type Scheduler struct {
	mu          sync.Mutex
	jobs        []entry
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	pauseCheck  func() string // returns "" to run, or a status message to pause and log
}

// PauseWhen sets a predicate evaluated before each ticker-driven job run.
// Return "" to allow the job to run; return a non-empty status string to skip it.
// The status string is logged on the first tick of a new pause and on recovery.
// Must be called before Start. Manual triggers (Triggerable) are never skipped.
func (s *Scheduler) PauseWhen(fn func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pauseCheck = fn
}

// New 返回一个空的 Scheduler。
func New() *Scheduler {
	return &Scheduler{}
}

// Register 注册一个 Job，interval 必须 > 0。
func (s *Scheduler) Register(job Job, interval time.Duration) {
	if interval <= 0 {
		panic("scheduler: interval must be > 0")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, entry{job: job, interval: interval})
}

// Start 启动所有已注册 Job 的 ticker goroutine。
// 每个 Job 立即执行一次，然后按 interval 定时执行。
// 传入的 ctx 取消时所有 goroutine 干净退出。
func (s *Scheduler) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	jobs := make([]entry, len(s.jobs))
	copy(jobs, s.jobs)
	s.cancel = cancel
	pauseCheck := s.pauseCheck // captured once; the fn itself reads live state on each call
	s.mu.Unlock()

	if len(jobs) == 0 {
		schedLog.Info("ℹ️  [调度器] 无已注册任务，调度器空转\n")
		<-runCtx.Done()
		return
	}

	for _, e := range jobs {
		e := e
		s.wg.Add(1)
		schedLog.Info("🕐 [调度器] 启动任务 %s（间隔 %v）\n", e.job.Name(), e.interval)
		go func() {
			defer s.wg.Done()

			// pauseStatus returns "" when the job should run, or a status string when paused.
			pauseStatus := func() string {
				if pauseCheck == nil {
					return ""
				}
				return pauseCheck()
			}

			// Initial run respects the pause predicate too.
			pauseMsg := pauseStatus()
			if pauseMsg != "" {
				schedLog.Warn("⏸️  [调度器] 跳过启动运行 [%s]: %s\n", e.job.Name(), pauseMsg)
			} else {
				schedLog.Debug("▶️  [调度器] 执行任务: %s\n", e.job.Name())
				e.job.Run(runCtx)
			}
			paused := pauseMsg != ""

			ticker := time.NewTicker(e.interval)
			defer ticker.Stop()

			var triggerCh <-chan struct{}
			if t, ok := e.job.(Triggerable); ok {
				triggerCh = t.TriggerChan()
			}

			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					msg := pauseStatus()
					if msg != "" {
						if !paused {
							// Transition: unblocked → blocked. Log once with status.
							schedLog.Warn("⏸️  [调度器] 暂停任务 [%s]: %s\n", e.job.Name(), msg)
							paused = true
						}
						continue
					}
					if paused {
						// Transition: blocked → unblocked. Log once.
						schedLog.Info("✅ [调度器] SRN 队列已恢复，恢复任务: %s\n", e.job.Name())
						paused = false
					}
					schedLog.Debug("▶️  [调度器] 执行任务: %s\n", e.job.Name())
					e.job.Run(runCtx)
				case <-triggerCh:
					// Manual triggers bypass the pause predicate.
					schedLog.Info("⚡ [调度器] 手动触发任务: %s，重置间隔\n", e.job.Name())
					e.job.Run(runCtx)
					ticker.Reset(e.interval) // 从现在起重新计时，避免立刻又触发定时扫描
				}
			}
		}()
	}
}

// Stop 取消所有 goroutine 并等待它们退出。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}
